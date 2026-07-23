# Storage 系统设计

> 根 KV 持久化抽象
> 配置: [Storage 配置](config-ref.md)

---

## 1. 职责

根 Storage 是简单的 Key-Value 存储，v1 只服务 Session snapshot。Memory 需要复合主键、查询和 Version，因此使用自己的 `memory.ContentStore`；不要把两套接口混为一谈。

| 组件 | 抽象 | 默认用途 |
|------|------|----------|
| Root Storage | KV + 可选 TTL | Session snapshot |
| Memory ContentStore | 复合 item + 查询 | Agent long-term Memory |
| 文件系统 | 文件 | Config、Skill 资源、日志 |

根 Storage 后端只有 `sqlite` 和 `memory`。SQLite 使用纯 Go `modernc.org/sqlite`，不需要 CGO；memory 后端用于测试或明确接受进程退出丢失的运行。

## 2. 核心接口

```go
type Storage interface {
    Get(key string) ([]byte, error)
    Set(key string, value []byte, ttl ...time.Duration) error
    Delete(key string) error
    Has(key string) (bool, error)
    Keys(prefix string) ([]string, error)
    Close() error
}

const MaxValueBytes = 16 << 20

var (
    ErrNotFound     = errors.New("storage: key not found")
    ErrClosed       = errors.New("storage: already closed")
    ErrInvalidKey   = errors.New("storage: invalid key")
    ErrInvalidTTL   = errors.New("storage: invalid ttl")
    ErrInvalidPath  = errors.New("storage: invalid path")
    ErrValueTooLarge = errors.New("storage: value too large")
)

type Clock interface { Now() time.Time }
```

契约：

- key 非空、必须是合法 UTF-8、最多 512 bytes；value 最多 `MaxValueBytes`。超限 value 返回 `ErrValueTooLarge`。实现必须拷贝输入和输出 byte slice。
- `Set` 最多接收一个 TTL 参数；未传或值为 0 表示永不过期；负值拒绝。
- `Get`/`Has`/`Keys` 不返回已过期 key；`Get` 对缺失返回 `ErrNotFound`。
- `Delete` 对缺失 key 幂等成功。
- 所有方法在 Close 后返回 `ErrClosed`（Close 本身幂等）。
- `Keys(prefix)` 只返回匹配前缀的 key，并按字节升序排序。
- 接口没有 `context.Context`。一次方法调用开始后不可由调用方取消；上层只能在调用前检查 context，并在方法返回后完成自己的提交。

Stats 是可选观察接口，不能成为业务依赖：

```go
type Stats interface { Stats() StorageStats }
type StorageStats struct { KeyCount, TotalBytes, HitCount, MissCount int64 }
```

## 3. TTL

TTL 由后端统一实现：读取时惰性隐藏，后台 worker 每 60 秒批量删除已过期 rows。清理周期是实现常量，不是 Config 字段；需要不同周期时应先增加版本化配置并同步所有后端。

Session 永远不传 TTL。Session 的 `ttl`/`max_lifetime` 是状态机 policy，不能让 Storage 自动删除 snapshot。

## 4. 初始化与关闭

```go
store, err := storage.New(cfg.Runtime.Storage)
if err != nil {
    return nil, err
}
// Runtime owns the lifecycle.
defer store.Close()
```

`storage.New` 根据 `runtime.storage.type` 创建 SQLite 或 memory 实现；未知类型、SQLite path 为空、schema migration 失败都返回错误。Runtime 只关闭一次根 Storage，Session Manager 不调用 Close。

## 5. 文档索引

| 文件 | 内容 |
|------|------|
| [sqlite.md](sqlite.md) | SQLite 表结构、事务、TTL 与实现要点 |
| [alternatives.md](alternatives.md) | 内存后端与后端选择 |
| [integration.md](integration.md) | Session 与 Memory 的所有权边界 |
| [config-ref.md](config-ref.md) | `runtime.storage` 字段 |
| [decisions.md](decisions.md) | Storage 设计决策 |
| [checklist.md](checklist.md) | 实现和门禁 |

---

*最后更新: 2026-07-22*
