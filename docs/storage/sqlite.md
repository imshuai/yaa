# SQLite Storage 实现

> 上级: [Storage 系统设计](README.md)
> 驱动: `modernc.org/sqlite`（纯 Go）

---

## 1. 表结构

```sql
CREATE TABLE IF NOT EXISTS root_kv (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER
);

CREATE INDEX IF NOT EXISTS root_kv_expiry
    ON root_kv (expires_at)
    WHERE expires_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS root_storage_schema_version (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
```

时间使用 Unix nanoseconds；`expires_at=NULL` 表示永不过期。表名和 schema version namespace 与 Memory ContentStore 分离，因此两个模块显式使用同一 SQLite 文件时不会冲突。

## 2. 初始化

```go
func NewSQLite(cfg StorageConfig) (*SQLiteStorage, error) {
    if cfg.Path == "" {
        return nil, ErrInvalidPath
    }
    db, err := sql.Open("sqlite", cfg.Path)
    if err != nil {
        return nil, fmt.Errorf("open root storage: %w", err)
    }
    db.SetMaxOpenConns(1)
    if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
        db.Close()
        return nil, fmt.Errorf("configure root storage: %w", err)
    }
    s := &SQLiteStorage{
        db:   db,
        stop: make(chan struct{}),
        done: make(chan struct{}),
    }
    if err = s.migrate(); err != nil {
        db.Close()
        return nil, err
    }
    go s.cleanupLoop()
    return s, nil
}
```

目录创建、权限检查和 migration 在 Runtime Ready 前完成。只接受当前或可向前迁移的 schema version；未知更高版本使启动失败。

## 3. 方法到 SQL 的映射

| 方法 | 行为 |
|------|------|
| Get | 查询 key 且 `expires_at IS NULL OR expires_at > now`；无 row 返回 ErrNotFound |
| Set | `INSERT ... ON CONFLICT DO UPDATE`，完整替换 value/expiry，保留 created_at |
| Delete | `DELETE WHERE key=?`，0 row 也成功 |
| Has | 与 Get 相同过滤，只查询存在性 |
| Keys | 按前缀和 expiry 过滤，`ORDER BY key ASC` |

Set 的核心 SQL：

```sql
INSERT INTO root_kv (key, value, created_at, expires_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    value = excluded.value,
    expires_at = excluded.expires_at;
```

前缀查询不用拼接未转义的 `LIKE`：

```sql
SELECT key
FROM root_kv
WHERE substr(key, 1, length(?)) = ?
  AND (expires_at IS NULL OR expires_at > ?)
ORDER BY key ASC;
```

## 4. TTL

共同参数校验先将可选 TTL 转成绝对 Unix nanoseconds：

```go
func expiresAt(now time.Time, ttl []time.Duration) (*int64, error) {
    if len(ttl) > 1 || (len(ttl) == 1 && ttl[0] < 0) {
        return nil, ErrInvalidTTL
    }
    if len(ttl) == 0 || ttl[0] == 0 {
        return nil, nil
    }
    v := now.Add(ttl[0]).UTC().UnixNano()
    return &v, nil
}
```

后台 worker 每 60 秒执行有限 batch 删除；若一批满额则立即继续，期间检查 stop channel：

```sql
DELETE FROM root_kv
WHERE key IN (
    SELECT key FROM root_kv
    WHERE expires_at IS NOT NULL AND expires_at <= ?
    ORDER BY expires_at, key
    LIMIT 1000
);
```

cleanup 失败记录稳定错误并等下一 tick，不关闭 Storage。Get/Has/Keys 的 expiry filter 保证失败期间也不会暴露过期值。

## 5. 拷贝、并发和关闭

- Set 在调用 SQL 前复制 value；Get 在 Scan 后返回新的 byte slice。
- `database/sql` 与单连接负责序列化；v1 不增加应用层事务接口。
- cleanup goroutine 退出前关闭 `done`；Close 使用 `sync.Once` 标记 closed、关闭 `stop`、等待 `<-done`，再关闭 DB。重复 Close 返回首次 close error。
- Close 开始后所有公开方法返回 ErrClosed。
- key/value 上限在共同 wrapper 中校验，SQLite 和 memory 后端一致。

## 6. Key 约定

v1 只有：

```text
session:<session-id> -> 完整 Session snapshot
```

Session message 不拆 key，Memory item 不写入 `root_kv`。完整所有权见 [集成](integration.md)。

## 7. 备份与恢复

运行中备份使用 SQLite online backup API，或先停止写入、checkpoint WAL 后复制。恢复文件必须先通过 `PRAGMA integrity_check` 和 schema version 校验，再允许 Session Restore；失败时保留原文件，不自动丢弃 rows。

## 8. 测试

1. 两次 Set 保留 created_at、替换 value/TTL。
2. Get/Has/Keys 同时隐藏刚过期但尚未物理删除的 key。
3. 前缀含 `%`、`_`、Unicode 时仍按字面匹配并稳定排序。
4. Delete 缺失 key 幂等；Close 两次幂等；Close 后方法返回 ErrClosed。
5. migration、数据库忙、损坏 row 和 cleanup 失败均保留明确错误链。

---

*最后更新: 2026-07-22*
