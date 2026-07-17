# Storage 系统设计

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/storage/` (原计划单文件 `docs/storage.md`，拆分为多文件)
> 依赖: `docs/architecture.md` §3.13

---

## 1. 概述

### 1.1 什么是 Storage

Storage 是 Yaa! Runtime 的**底层持久化抽象**，为上层模块提供统一的 Key-Value 存储接口。

| 层级 | 抽象 | 用途 |
|------|------|------|
| Memory | 语义记忆 + 向量检索 | Agent 长期记忆 |
| **Storage** | **KV 存储 + TTL** | **Session 持久化、配置缓存、状态存储** |
| 文件系统 | 原始文件 | Skill 资源、日志、配置文件 |

Storage 不直接面向 Agent，而是作为 Runtime 基础设施，被 Session、Memory、Config 等模块依赖。

### 1.2 设计理念

| 特性 | 说明 |
|------|------|
| **零 CGO** | 默认实现使用纯 Go SQLite，保证 Windows 7 兼容 |
| **单文件** | 默认 SQLite 单文件部署，简化运维 |
| **可替换** | 通过接口抽象，支持 BoltDB、内存等多种后端 |
| **TTL 原生** | 接口层面支持过期时间，适配缓存场景 |
| **前缀扫描** | `Keys(prefix)` 支持按命名空间批量查询 |

### 1.3 核心原则

1. **接口优先** — 上层只依赖 `Storage` 接口，不绑定具体实现
2. **零依赖默认** — 默认实现不引入 CGO，纯 Go 可交叉编译
3. **配置驱动** — 通过 `config.yaml` 的 `runtime.storage.type` 切换后端
4. **向后兼容** — 接口新增方法使用默认实现，不破坏已有代码

---

## 2. 核心接口

```go
// Storage 是 Yaa! 的底层 Key-Value 存储抽象。
// 所有上层模块（Session、Memory、Config 等）通过此接口访问持久化数据。
type Storage interface {
    // Get 根据 key 读取数据，key 不存在时返回 ErrNotFound。
    Get(key string) ([]byte, error)

    // Set 写入数据，可选 TTL；TTL 到期后 key 自动删除。
    Set(key string, value []byte, ttl ...time.Duration) error

    // Delete 删除指定 key，key 不存在时不报错。
    Delete(key string) error

    // Has 判断 key 是否存在（且未过期）。
    Has(key string) (bool, error)

    // Keys 返回匹配指定前缀的所有 key，无匹配时返回空切片。
    Keys(prefix string) ([]string, error)
}
```

### 2.1 错误定义

```go
var (
    // ErrNotFound 表示 key 不存在或已过期
    ErrNotFound = errors.New("storage: key not found")
    // ErrClosed 表示 Storage 已关闭
    ErrClosed = errors.New("storage: already closed")
)
```

### 2.2 可选接口

```go
// Closer 提供优雅关闭能力，所有实现应支持。
type Closer interface {
    Close() error
}

// Stats 提供存储统计信息（可选，用于监控）。
type Stats interface {
    Stats() StorageStats
}

type StorageStats struct {
    KeyCount    int64   // 总 key 数量
    TotalBytes  int64   // 总数据大小（估算）
    HitCount    int64   // Get 命中次数
    MissCount   int64   // Get 未命中次数
}
```

---

## 3. 实现对比

| 特性 | SQLite | BoltDB | Memory |
|------|--------|--------|--------|
| **依赖** | modernc.org/sqlite (纯 Go) | go.etcd.io/bbolt | 无 |
| **CGO** | ❌ 零 CGO | ❌ 零 CGO | ❌ 零 CGO |
| **持久化** | ✅ 单文件 `.db` | ✅ 单文件 `.bolt` | ❌ 进程内 |
| **TTL 支持** | ✅ 轮询清理 | ✅ 轮询清理 | ✅ timer |
| **前缀扫描** | ✅ SQL `LIKE` | ✅ 前缀游标 | ✅ 内存过滤 |
| **并发模型** | SQL 事务 | B+Tree 读写锁 | sync.RWMutex |
| **适合场景** | **生产默认** | 高写入吞吐 | 测试 / 临时缓存 |
| **跨平台** | ✅ Windows 7+ | ✅ Windows 7+ | ✅ 全平台 |
| **数据可检视** | ✅ sqlite3 CLI | ❌ 需工具 | ❌ |
| **体积开销** | ~中 | ~小 | ~零 |

### 3.1 选型建议

```text
生产部署 → SQLite（默认，零配置，可检视）
高写入场景 → BoltDB（B+Tree 写入优化）
单元测试 → Memory（快速，无磁盘 IO）
```

---

## 4. 配置参考

```yaml
# yaa.yaml
runtime:
  storage:
    type: sqlite          # sqlite | boltdb | memory
    path: ./data/yaa.db   # SQLite / BoltDB 文件路径
    # ttl_interval: 60s   # TTL 清理轮询间隔（可选）
    # wal: true           # SQLite WAL 模式（可选，默认 true）
```

---

## 5. 使用示例

```go
// 初始化 Storage
store, err := storage.New(cfg.Storage)
if err != nil {
    log.Fatal("failed to init storage: %v", err)
}
defer store.Close()

// 基本读写
if err := store.Set("agent:default:name", []byte("Pico")); err != nil {
    log.Fatal(err)
}
val, err := store.Get("agent:default:name")
// val == []byte("Pico")

// 带 TTL 的写入（5 分钟后自动过期）
store.Set("session:abc:token", []byte("xyz"), 5*time.Minute)

// 前缀扫描 — 列出所有 agent 的 key
keys, _ := store.Keys("agent:")

// 判断存在
exists, _ := store.Has("session:abc:token")

// 删除
store.Delete("session:abc:token")
```

---

## 文档索引

| 文件 | 内容 |
|------|------|
| [sqlite.md](sqlite.md) | SQLite 实现 — 表结构、WAL 模式、TTL 清理、SQL 细节 |
| [alternatives.md](alternatives.md) | BoltDB 与内存存储 — Bucket 设计、前缀游标、TTL 清理、内存实现 |
| [integration.md](integration.md) | 与各模块的集成 — Session / Memory / Config 的存储交互 |
| [config-ref.md](config-ref.md) | 配置参考 — 全局配置、后端切换、路径与参数 |
| [decisions.md](decisions.md) | 设计决策（ST-001 ~ ST-NNN）+ 模块关系 |
| [checklist.md](checklist.md) | 实现检查清单 |

---

*最后更新: 2025-07-17*
