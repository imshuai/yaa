# Memory 实现检查清单

> 文档路径: `docs/memory/checklist.md`
> 上级: `docs/memory/README.md` §9

---

## Memory 接口

- [ ] `Memory` interface 定义（Add, Get, Search, Delete, Clear）
- [ ] `MemoryItem` 结构体定义（Key, Content, Metadata, Layer, Score, CreatedAt, UpdatedAt, ExpiresAt）
- [ ] `MemoryLayer` 枚举定义（LayerShortTerm, LayerLongTerm, LayerSummary）
- [ ] `MemoryExtended` 扩展接口定义（AddBatch, SearchWithFilter, Update, ListByLayer, Count, Promote, Expire）
- [ ] `MemoryFilter` 结构体定义（Layer, Metadata, After, Before, Tags）
- [ ] 能力检测模式实现（类型断言 `mem.(MemoryExtended)`）
- [ ] Key 命名规范校验（语义化路径，如 `user_preference:theme`）
- [ ] Score 字段仅在 Search 返回时有意义，Add/Get 时为 0

## 三层架构

- [ ] Short-term Memory — 当前 Session 消息历史，内存或 Storage 存储
- [ ] Long-term Memory — 跨 Session 持久化，SQLite 或向量数据库
- [ ] Summary Memory — Session 压缩摘要，由 Context Manager 生成
- [ ] 三层记忆相互独立，各自有独立的存储和检索策略
- [ ] Short-term → Long-term 自动晋升机制（Progressive Persistence）
- [ ] Summary 生成触发条件（Token 阈值 / Session 关闭 / 手动触发）
- [ ] 各层记忆的 Agent 隔离（Agent Scoped）

## 存储后端

- [ ] `MemoryStore` 接口定义（屏蔽存储差异）
- [ ] SQLite 存储后端实现（默认）
- [ ] 向量数据库存储后端实现（可选）
- [ ] 外部服务存储后端实现（可选）
- [ ] 存储后端通过配置切换，无需修改代码（Configurable Backend）
- [ ] SQLite 表结构设计（memory_items 表，含 layer / metadata / timestamps）
- [ ] 存储 TTL 与过期清理支持
- [ ] 批量写入支持（AddBatch）

## 向量搜索

- [ ] `Embedder` 接口定义（文本 → 向量）
- [ ] 向量嵌入器实现（支持可配置的 Embedding Provider）
- [ ] 向量索引存储（SQLite 向量扩展或外部向量库）
- [ ] 语义相似度检索（cosine similarity）
- [ ] Search 方法支持向量语义搜索
- [ ] 向量搜索不可用时回退到关键词搜索（Graceful Degradation）
- [ ] SearchWithFilter 支持元数据 + 向量联合检索
- [ ] Embedding 缓存（避免重复计算）
- [ ] 向量维度配置化

## 生命周期管理

- [ ] 记忆创建（Add 写入，记录 CreatedAt）
- [ ] 记忆更新（Update 修改 Content / Metadata，更新 UpdatedAt）
- [ ] 记忆晋升（Promote: Short-term → Long-term）
- [ ] 记忆过期（ExpiresAt 零值表示永不过期）
- [ ] 过期清理任务（Expire 方法，批量清理已过期记忆）
- [ ] 记忆淘汰策略（LRU / 容量上限 / 手动 Delete）
- [ ] 记忆清除（Clear 清除当前作用域所有记忆）
- [ ] Session 关闭时的记忆处理（晋升或清除）

## 集成

- [ ] `MemoryManager` 结构体定义（instances map, store, embedder, config, logger, mu）
- [ ] `GetMemory(agentID)` — 获取 Agent 的 Memory 实例（懒加载）
- [ ] `CloseAll()` — 关闭所有 Memory 实例，释放资源
- [ ] 与 Session 集成 — Session 生命周期内记忆的读写
- [ ] 与 Context Manager 集成 — 检索记忆注入 Context
- [ ] 与 Agent 集成 — 每个 Agent 独立 Memory 空间
- [ ] Summary 记忆由 Context Manager 压缩时生成并写入 Memory
- [ ] 记忆注入 Context 的策略（检索后按需注入，非全量加载）

## 配置

- [ ] `MemoryConfig` 结构体定义（全局配置）
- [ ] 存储后端配置（backend: sqlite / vector / external）
- [ ] 向量搜索配置（embedder provider, 维度, 相似度阈值）
- [ ] 过期清理配置（interval, batch_size）
- [ ] Agent 级别 Memory 配置覆盖
- [ ] 三级配置合并逻辑（全局 → Agent 级别 → Session 级别）
- [ ] 淘汰策略配置（max_items, ttl, eviction_policy）
- [ ] Embedding Provider 配置（model, api_key, endpoint）

## 错误处理

- [ ] `ErrMemoryNotFound` — 记忆不存在
- [ ] `ErrMemoryAlreadyExists` — Key 已存在（当策略为报错时）
- [ ] `ErrMemoryStoreUnavailable` — 存储后端不可用
- [ ] `ErrMemoryEmbedFailed` — 向量嵌入失败
- [ ] `ErrMemorySearchFailed` — 检索失败
- [ ] 向量搜索失败回退关键词搜索（降级策略）
- [ ] 存储后端不可用时的降级处理（内存临时存储）
- [ ] 批量操作部分失败的错误聚合

## 可观测性

- [ ] 记忆操作日志（Add / Get / Search / Delete / Clear）
- [ ] 记忆晋升日志（Promote）
- [ ] 过期清理日志（Expire）
- [ ] 指标: `memory_total` (Gauge, 按 Layer 分维度)
- [ ] 指标: `memory_add_total` (Counter)
- [ ] 指标: `memory_search_total` (Counter)
- [ ] 指标: `memory_search_duration` (Histogram)
- [ ] 指标: `memory_expire_total` (Counter)
- [ ] 指标: `memory_promote_total` (Counter)
- [ ] 指标: `memory_embed_duration` (Histogram)
- [ ] `HealthCheck()` 方法 — Memory 系统健康检查
- [ ] `MemoryHealthReport` 结构体（store 状态, embedder 状态, 记忆数量）
- [ ] SSE 事件: `memory.added`
- [ ] SSE 事件: `memory.searched`
- [ ] SSE 事件: `memory.deleted`
- [ ] SSE 事件: `memory.promoted`
- [ ] SSE 事件: `memory.expired`
- [ ] SSE 事件: `memory.error`
- [ ] 调用链追踪（span: memory.add, memory.search, memory.promote）

---

*最后更新: 2025-07-16*
