# 配置参考

> Yaa! Storage 配置详解
> 依赖: `docs/storage/README.md` §4

---

## 1. 配置位置

Storage 配置位于 `config.yaml` 的 `runtime.storage` 段：

```yaml
runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
```

---

## 2. 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `sqlite` | 存储后端类型：`sqlite` / `boltdb` / `memory` |
| `path` | string | `./data/yaa.db` | 数据文件路径（SQLite / BoltDB 有效，memory 忽略） |
| `ttl_interval` | duration | `60s` | TTL 过期清理轮询间隔 |
| `wal` | bool | `true` | SQLite WAL 模式开关（仅 SQLite） |
| `bucket` | string | `yaa` | BoltDB Bucket 名称（仅 BoltDB） |

---

## 3. 后端类型对比

| 类型 | 持久化 | CGO | 文件后缀 | 适用场景 |
|------|--------|-----|----------|----------|
| `sqlite` | ✅ | ❌ | `.db` | 生产默认，数据可检视 |
| `boltdb` | ✅ | ❌ | `.bolt` | 高写入吞吐 |
| `memory` | ❌ | ❌ | — | 测试 / 临时缓存 |

---

## 4. YAML 示例

### 4.1 SQLite（默认）

```yaml
runtime:
  storage:
    type: sqlite
    path: ./data/yaa.db
    ttl_interval: 60s
    wal: true
```

### 4.2 BoltDB

```yaml
runtime:
  storage:
    type: boltdb
    path: ./data/yaa.bolt
    ttl_interval: 30s
    bucket: yaa
```

### 4.3 Memory（测试）

```yaml
runtime:
  storage:
    type: memory
    ttl_interval: 10s
```

---

## 5. Go 代码示例

### 5.1 从配置初始化

```go
package main

import (
    "log"
    "time"
    "github.com/imshuai/yaa/internal/storage"
)

func main() {
    cfg := storage.Config{
        Type:        "sqlite",
        Path:        "./data/yaa.db",
        TTLInterval: 60 * time.Second,
        WAL:         true,
    }

    store, err := storage.New(cfg)
    if err != nil {
        log.Fatalf("init storage: %v", err)
    }
    defer store.Close()

    // 写入，30 分钟 TTL
    store.Set("session:abc:ctx", []byte("hello"), 30*time.Minute)

    // 读取
    val, err := store.Get("session:abc:ctx")
    if err != nil {
        log.Printf("get: %v", err)
    }
    log.Printf("value: %s", val)
}
```

### 5.2 运行时切换后端

```go
// 测试场景快速切换到内存后端
cfg := storage.Config{Type: "memory"}
store, _ := storage.New(cfg)
defer store.Close()
```

---

## 6. TTL 默认值

| 使用方 | Key 前缀 | 默认 TTL |
|--------|----------|----------|
| Session | `session:` | 24h |
| Config 缓存 | `config:` | 5m |
| 临时状态 | `tmp:` | 1m |
| 永久存储 | `agent:` | 无（不过期） |

TTL 到期后由后台轮询协程自动清理，间隔由 `ttl_interval` 控制。

---

*最后更新: 2025-07-17*
