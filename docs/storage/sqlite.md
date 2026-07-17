# SQLite 存储实现

> Yaa! 默认存储后端，基于 `modernc.org/sqlite`（纯 Go，零 CGO）。
> 实现 `storage.Storage` 接口，提供 Key-Value 语义与 TTL 支持。

---

## 1. 设计目标

| 目标 | 说明 |
|------|------|
| 零依赖 | 纯 Go SQLite，无需 CGO，Windows 7 兼容 |
| 单文件 | 整个数据库存储在一个 `.db` 文件中，便于备份与迁移 |
| Key-Value 语义 | 对 `Storage` 接口的简单映射，上层无需关心 SQL |
| TTL 支持 | 可选过期时间，后台惰性清理 |
| 前缀查询 | 支持 `Keys(prefix)` 按前缀枚举键 |
| 并发安全 | 依赖 SQLite WAL 模式 + `sync.Mutex` 保护写操作 |

---

## 2. 表结构

### 2.1 主表 `kv_store`

```sql
CREATE TABLE IF NOT EXISTS kv_store (
    key       TEXT    PRIMARY KEY,
    value     BLOB    NOT NULL,
    created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
    expires_at  INTEGER          -- Unix 时间戳，NULL 表示永不过期
);
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `key` | TEXT PK | 键名，主键索引 |
| `value` | BLOB | 值，以字节流存储 |
| `created_at` | INTEGER | 创建时间（Unix 秒） |
| `expires_at` | INTEGER | 过期时间（Unix 秒），NULL = 永不过期 |

### 2.2 辅助索引

```sql
-- 加速过期清理查询
CREATE INDEX IF NOT EXISTS idx_kv_expires ON kv_store (expires_at)
    WHERE expires_at IS NOT NULL;
```

### 2.3 Schema 版本管理

```sql
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
```

启动时检查 `schema_version`，按需执行迁移脚本。

---

## 3. Key-Value 映射

`Storage` 接口到 SQL 操作的映射关系：

| Storage 方法 | SQL 操作 | 说明 |
|---|---|---|
| `Get(key)` | `SELECT value FROM kv_store WHERE key=? AND (expires_at IS NULL OR expires_at > ?)` | 读取时惰性检查过期 |
| `Set(key, val, ttl?)` | `INSERT OR REPLACE INTO kv_store (key, value, expires_at) VALUES (?, ?, ?)` | UPSERT 语义 |
| `Delete(key)` | `DELETE FROM kv_store WHERE key=?` | 直接删除 |
| `Has(key)` | `SELECT 1 FROM kv_store WHERE key=? AND (expires_at IS NULL OR expires_at > ?)` | 存在性检查 |
| `Keys(prefix)` | `SELECT key FROM kv_store WHERE key LIKE ? AND (expires_at IS NULL OR expires_at > ?)` | 前缀匹配 |

**前缀查询说明：** `prefix` 会被转义后拼接为 `prefix%`，使用 `LIKE` 匹配。

---

## 4. TTL 支持

### 4.1 写入时设置 TTL

```go
// 永不过期
storage.Set("agent:1:config", data)

// 10 分钟后过期
storage.Set("session:abc:cache", data, 10*time.Minute)
```

### 4.2 惰性过期

`Get` / `Has` / `Keys` 在读取时检查 `expires_at`：
- 若已过期，`Get` 返回 `ErrNotFound`，`Has` 返回 `false`
- 过期行不被立即删除，由后台清理协程回收

### 4.3 后台清理

```text
启动时创建 goroutine，每 60 秒执行：
    DELETE FROM kv_store WHERE expires_at IS NOT NULL AND expires_at <= strftime('%s','now')
```

---

## 5. Go 代码示例

### 5.1 初始化

```go
package storage

import (
    "database/sql"
    "fmt"
    "sync"
    "time"

    _ "modernc.org/sqlite"
)

type SQLiteStorage struct {
    db   *sql.DB
    mu   sync.Mutex
    stop chan struct{}
}

func NewSQLiteStorage(path string) (*SQLiteStorage, error) {
    db, err := sql.Open("sqlite", path)
    if err != nil {
        return nil, fmt.Errorf("open sqlite: %w", err)
    }
    // 启用 WAL 模式，提升并发读性能
    if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
        db.Close()
        return nil, err
    }
    s := &SQLiteStorage{db: db, stop: make(chan struct{})}
    if err := s.initSchema(); err != nil {
        db.Close()
        return nil, err
    }
    go s.cleanupLoop()
    return s, nil
}
```

### 5.2 Schema 初始化

```go
func (s *SQLiteStorage) initSchema() error {
    _, err := s.db.Exec(`
        CREATE TABLE IF NOT EXISTS kv_store (
            key         TEXT    PRIMARY KEY,
            value       BLOB    NOT NULL,
            created_at  INTEGER NOT NULL DEFAULT (strftime('%s','now')),
            expires_at  INTEGER
        );
        CREATE INDEX IF NOT EXISTS idx_kv_expires
            ON kv_store (expires_at) WHERE expires_at IS NOT NULL;
        CREATE TABLE IF NOT EXISTS schema_version (
            version     INTEGER PRIMARY KEY,
            applied_at  INTEGER NOT NULL DEFAULT (strftime('%s','now'))
        );
        INSERT OR IGNORE INTO schema_version (version) VALUES (1);
    `)
    return err
}
```

### 5.3 Set / Get

```go
var ErrNotFound = fmt.Errorf("storage: key not found")

func (s *SQLiteStorage) Set(key string, value []byte, ttl ...time.Duration) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    var expiresAt interface{} // nil = 永不过期
    if len(ttl) > 0 && ttl[0] > 0 {
        expiresAt = time.Now().Add(ttl[0]).Unix()
    }
    _, err := s.db.Exec(
        `INSERT OR REPLACE INTO kv_store (key, value, expires_at) VALUES (?, ?, ?)`,
        key, value, expiresAt,
    )
    return err
}

func (s *SQLiteStorage) Get(key string) ([]byte, error) {
    var value []byte
    now := time.Now().Unix()
    err := s.db.QueryRow(
        `SELECT value FROM kv_store WHERE key=? AND (expires_at IS NULL OR expires_at > ?)`,
        key, now,
    ).Scan(&value)
    if err == sql.ErrNoRows {
        return nil, ErrNotFound
    }
    return value, err
}
```

### 5.4 Delete / Has / Keys

```go
func (s *SQLiteStorage) Delete(key string) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    _, err := s.db.Exec(`DELETE FROM kv_store WHERE key=?`, key)
    return err
}

func (s *SQLiteStorage) Has(key string) (bool, error) {
    var one int
    now := time.Now().Unix()
    err := s.db.QueryRow(
        `SELECT 1 FROM kv_store WHERE key=? AND (expires_at IS NULL OR expires_at > ?)`,
        key, now,
    ).Scan(&one)
    if err == sql.ErrNoRows {
        return false, nil
    }
    return err == nil, err
}

func (s *SQLiteStorage) Keys(prefix string) ([]string, error) {
    now := time.Now().Unix()
    pattern := prefix + "%"
    rows, err := s.db.Query(
        `SELECT key FROM kv_store WHERE key LIKE ? AND (expires_at IS NULL OR expires_at > ?)`,
        pattern, now,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var keys []string
    for rows.Next() {
        var k string
        if err := rows.Scan(&k); err != nil {
            return nil, err
        }
        keys = append(keys, k)
    }
    return keys, rows.Err()
}
```

### 5.5 后台清理 & 关闭

```go
func (s *SQLiteStorage) cleanupLoop() {
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-s.stop:
            return
        case <-ticker.C:
            now := time.Now().Unix()
            s.db.Exec(`DELETE FROM kv_store WHERE expires_at IS NOT NULL AND expires_at <= ?`, now)
        }
    }
}

func (s *SQLiteStorage) Close() error {
    close(s.stop)
    return s.db.Close()
}
```

---

## 6. Key 命名约定

建议上层模块使用冒号分层命名，便于 `Keys(prefix)` 查询：

| Key 模式 | 用途 | 示例 |
|---|---|---|
| `agent:{id}:config` | Agent 配置 | `agent:default:config` |
| `session:{id}:msg:{n}` | Session 消息 | `session:abc123:msg:0` |
| `memory:{agentID}:{key}` | 长期记忆 | `memory:default:pref01` |
| `skill:{name}:cache` | Skill 缓存 | `skill:weather:cache` |

---

## 7. 配置

```yaml
runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `sqlite` | 存储类型标识 |
| `path` | string | `./data/yaa.db` | 数据库文件路径 |
