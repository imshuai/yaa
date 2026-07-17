# Storage 实现检查清单

> 文档路径: `docs/storage/checklist.md`
> 上级: `docs/storage/README.md` §2-§5

---

## 接口实现

- [ ] `Storage` 接口定义（Get, Set, Delete, Has, Keys）
- [ ] `ErrNotFound` 错误定义
- [ ] `ErrClosed` 错误定义
- [ ] `Closer` 可选接口定义（`Close() error`）
- [ ] `Stats` 可选接口定义（`Stats() StorageStats`）
- [ ] `StorageStats` 结构体定义（KeyCount, TotalBytes, HitCount, MissCount）
- [ ] `New()` 工厂函数（根据 config 返回对应实现）
- [ ] 接口兼容性测试（所有实现通过同一套接口测试）

## SQLite 实现

- [ ] 表结构设计（`kv` 表: key TEXT PRIMARY KEY, value BLOB, expire_at INTEGER）
- [ ] 索引创建（expire_at 列索引，用于 TTL 清理）
- [ ] `Get()` — SQL 查询 + 过期检查
- [ ] `Set()` — UPSERT 语法（INSERT OR REPLACE）
- [ ] `Delete()` — DELETE 语句
- [ ] `Has()` — SELECT 1 + 过期检查
- [ ] `Keys(prefix)` — SQL `LIKE 'prefix%'` 查询
- [ ] WAL 模式启用（`PRAGMA journal_mode=WAL`）
- [ ] 连接池配置（max_open_conns, max_idle_conns）
- [ ] `Close()` — 关闭数据库连接
- [ ] `Stats()` — 统计信息查询
- [ ] modernc.org/sqlite 依赖引入（纯 Go，零 CGO）

## BoltDB 实现

- [ ] Bucket 设计（`kv` bucket 存储数据，`ttl` bucket 存储 expire_at）
- [ ] `Get()` — Bucket 查询 + 过期检查
- [ ] `Set()` — 读写事务写入 kv + ttl
- [ ] `Delete()` — 读写事务删除 kv + ttl
- [ ] `Has()` — Bucket 查询 + 过期检查
- [ ] `Keys(prefix)` — 前缀游标遍历（`bucket.Cursor()` + `Seek(prefix)`）
- [ ] `Close()` — 关闭 BoltDB 文件
- [ ] go.etcd.io/bbolt 依赖引入
- [ ] 文件锁与并发读写处理

## 内存存储实现

- [ ] `map[string]item` 结构体定义（value []byte, expireAt time.Time）
- [ ] `sync.RWMutex` 并发控制
- [ ] `Get()` — 读锁 + 过期检查
- [ ] `Set()` — 写锁 + TTL timer 注册
- [ ] `Delete()` — 写锁删除
- [ ] `Has()` — 读锁 + 过期检查
- [ ] `Keys(prefix)` — 读锁 + 内存遍历过滤
- [ ] `Close()` — 清空 map + 停止所有 timer
- [ ] TTL 过期通过 `time.AfterFunc` 自动删除

## TTL 机制

- [ ] TTL 参数解析（`ttl ...time.Duration` 可变参数）
- [ ] SQLite TTL 清理轮询（后台 goroutine + `DELETE WHERE expire_at < ?`）
- [ ] BoltDB TTL 清理轮询（后台 goroutine + 遍历 ttl bucket）
- [ ] Memory TTL 自动过期（`time.AfterFunc`）
- [ ] TTL 清理间隔可配置（`ttl_interval`，默认 60s）
- [ ] Get/Has 时惰性过期检查（读取时发现过期则删除并返回 ErrNotFound）
- [ ] TTL 为 0 或不传 = 永不过期

## 集成

- [ ] Runtime 初始化时创建 Storage 实例
- [ ] Session 模块通过 Storage 持久化会话数据
- [ ] Memory 模块通过 Storage 缓存配置数据
- [ ] Config 模块通过 Storage 读取/缓存配置
- [ ] Storage 生命周期与 Runtime 绑定（随 Runtime 关闭而 Close）
- [ ] Storage 健康检查集成到 Runtime 健康检查

## 配置

- [ ] `runtime.storage.type` 配置解析（sqlite / boltdb / memory）
- [ ] `runtime.storage.path` 配置解析（文件路径）
- [ ] `runtime.storage.ttl_interval` 配置解析（TTL 清理间隔）
- [ ] `runtime.storage.wal` 配置解析（SQLite WAL 开关，默认 true）
- [ ] 未知 type 时返回明确错误
- [ ] memory 类型忽略 path 配置
- [ ] 默认配置：type=sqlite, path=./data/yaa.db, wal=true

---

*最后更新: 2025-07-17*
