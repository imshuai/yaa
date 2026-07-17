# Memory 配置参考

> 文档路径: `docs/memory/config-ref.md`
> 上级: `docs/memory/README.md` §9
> 依赖: `docs/architecture.md` §3.12 (Config), `docs/memory/storage.md`

---

## 1. 配置层级概览

Yaa! Memory 系统采用**三级配置合并**机制：

| 层级 | 作用域 | 覆盖关系 |
|------|--------|----------|
| **全局配置** | 所有 Agent 共享 | 基线默认值 |
| **Agent 级别** | 单个 Agent | 覆盖全局配置 |
| **Session 级别** | 单次会话 | 覆盖 Agent 级别（运行时注入，不持久化） |

合并优先级：全局 → Agent → Session（后者覆盖前者同名键）。

---

## 2. 全局 Memory 配置

全局配置定义在 `yaa.yaml` 的顶层 `memory` 节点下。

### 2.1 配置项一览

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `true` | 是否启用 Memory 系统 |
| `backend` | string | `"sqlite"` | 存储后端类型：`sqlite` / `vector` / `external` |
| `eviction_policy` | string | `"lru"` | 淘汰策略：`lru` / `fifo` / `ttl` |
| `max_items` | int | `10000` | 每个 Agent 最大记忆条数 |
| `default_ttl` | duration | `0`（永不过期） | 默认记忆过期时间 |
| `expire_interval` | duration | `5m` | 过期清理任务执行间隔 |
| `expire_batch_size` | int | `500` | 每次过期清理批量删除上限 |

### 2.2 YAML 示例

```yaml
# yaa.yaml — 全局 Memory 配置
memory:
  enabled: true
  backend: sqlite
  eviction_policy: lru
  max_items: 10000
  default_ttl: 0
  expire_interval: 5m
  expire_batch_size: 500

  # 存储后端配置（见 §4）
  storage:
    type: sqlite
    path: ./data/yaa_memory.db

  # 向量搜索配置（见 §5）
  vector:
    enabled: false

  # Embedding Provider 配置
  embedding:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    dimension: 1536
    cache_enabled: true
```

### 2.3 Go 结构体定义

```go
// MemoryConfig 是 Memory 系统的全局配置。
type MemoryConfig struct {
    Enabled         bool            `yaml:"enabled"`
    Backend         string          `yaml:"backend"`
    EvictionPolicy  string          `yaml:"eviction_policy"`
    MaxItems        int             `yaml:"max_items"`
    DefaultTTL      time.Duration   `yaml:"default_ttl"`
    ExpireInterval  time.Duration   `yaml:"expire_interval"`
    ExpireBatchSize int             `yaml:"expire_batch_size"`
    Storage         StorageConfig   `yaml:"storage"`
    Vector          VectorConfig    `yaml:"vector"`
    Embedding       EmbeddingConfig `yaml:"embedding"`
}
```

---

## 3. Agent 级别覆盖

每个 Agent 可在其配置中定义 `memory` 节点，覆盖全局配置。

### 3.1 可覆盖字段

| 字段 | 说明 | 覆盖行为 |
|------|------|----------|
| `enabled` | 是否为该 Agent 启用 Memory | 全局 `true` → Agent 可设 `false` 禁用 |
| `max_items` | 该 Agent 的最大记忆条数 | 覆盖全局值 |
| `default_ttl` | 该 Agent 的默认过期时间 | 覆盖全局值 |
| `eviction_policy` | 该 Agent 的淘汰策略 | 覆盖全局值 |
| `vector.enabled` | 是否为该 Agent 启用向量搜索 | 覆盖全局值 |
| `embedding` | 该 Agent 专属 Embedding Provider | 覆盖全局值 |

> **注意**：`backend` 和 `storage` 字段仅在全局级别配置，Agent 级别不支持覆盖。所有 Agent 共享同一存储后端实例，通过 `agent_id` 字段隔离数据。

### 3.2 YAML 示例

```yaml
agents:
  - id: "researcher"
    name: "Research Agent"
    provider: "openai"
    model: "gpt-4o"
    memory:
      enabled: true
      max_items: 50000          # 覆盖全局 10000
      default_ttl: 72h          # 覆盖全局 0（永不过期）
      eviction_policy: ttl      # 覆盖全局 lru
      vector:
        enabled: true           # 覆盖全局 false
        similarity_threshold: 0.75

  - id: "casual"
    name: "Casual Chat Agent"
    provider: "ollama"
    model: "llama3"
    memory:
      enabled: false             # 该 Agent 不需要记忆
```

### 3.3 Go 结构体定义

```go
// AgentMemoryConfig 是 Agent 级别的 Memory 配置覆盖。
// 所有字段为指针类型，nil 表示不覆盖（继承全局值）。
type AgentMemoryConfig struct {
    Enabled        *bool          `yaml:"enabled,omitempty"`
    MaxItems       *int           `yaml:"max_items,omitempty"`
    DefaultTTL     *time.Duration `yaml:"default_ttl,omitempty"`
    EvictionPolicy *string        `yaml:"eviction_policy,omitempty"`
    Vector         *VectorConfig  `yaml:"vector,omitempty"`
    Embedding      *EmbeddingConfig `yaml:"embedding,omitempty"`
}

// MergeWithGlobal 将 Agent 级别配置与全局配置合并。
func (a *AgentMemoryConfig) MergeWithGlobal(global MemoryConfig) MemoryConfig {
    merged := global
    if a.Enabled != nil {
        merged.Enabled = *a.Enabled
    }
    if a.MaxItems != nil {
        merged.MaxItems = *a.MaxItems
    }
    if a.DefaultTTL != nil {
        merged.DefaultTTL = *a.DefaultTTL
    }
    if a.EvictionPolicy != nil {
        merged.EvictionPolicy = *a.EvictionPolicy
    }
    if a.Vector != nil {
        merged.Vector = mergeVectorConfig(global.Vector, *a.Vector)
    }
    if a.Embedding != nil {
        merged.Embedding = *a.Embedding
    }
    return merged
}
```

---

## 4. 存储后端配置

### 4.1 SQLite 后端（默认）

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `"sqlite"` | 固定值 |
| `path` | string | `"./data/yaa_memory.db"` | 数据库文件路径 |
| `journal_mode` | string | `"WAL"` | SQLite journal 模式 |
| `busy_timeout` | duration | `5s` | SQLite busy 超时 |
| `migrate` | bool | `true` | 启动时自动执行 schema 迁移 |

```yaml
memory:
  storage:
    type: sqlite
    path: ./data/yaa_memory.db
    journal_mode: WAL
    busy_timeout: 5s
    migrate: true
```

### 4.2 向量数据库后端

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | — | `"vector"` |
| `engine` | string | `"sqlite-vec"` | 向量引擎：`sqlite-vec` / `chroma` / `qdrant` |
| `path` | string | `"./data/yaa_vectors.db"` | 本地引擎路径（sqlite-vec） |
| `endpoint` | string | — | 远程引擎地址（chroma / qdrant） |
| `api_key` | string | — | 远程引擎认证密钥 |
| `collection` | string | `"yaa_memories"` | 向量集合名称 |

```yaml
memory:
  backend: vector
  storage:
    type: vector
    engine: qdrant
    endpoint: http://localhost:6333
    api_key: ${QDRANT_API_KEY}
    collection: yaa_memories
```

### 4.3 外部服务后端

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | — | `"external"` |
| `endpoint` | string | — | 外部 Memory 服务 URL |
| `api_key` | string | — | 认证密钥 |
| `timeout` | duration | `10s` | 请求超时 |

```yaml
memory:
  backend: external
  storage:
    type: external
    endpoint: https://memory.example.com/api/v1
    api_key: ${MEMORY_SERVICE_KEY}
    timeout: 10s
```

---

## 5. 向量搜索配置

### 5.1 配置项

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用向量语义搜索 |
| `similarity_threshold` | float64 | `0.7` | 相似度阈值，低于此值的结果被过滤 |
| `top_k` | int | `10` | 向量检索默认返回数量 |
| `fallback_to_keyword` | bool | `true` | 向量搜索失败时回退到关键词搜索 |
| `reindex_on_update` | bool | `false` | 记忆更新时自动重新计算向量 |

### 5.2 YAML 示例

```yaml
memory:
  vector:
    enabled: true
    similarity_threshold: 0.75
    top_k: 10
    fallback_to_keyword: true
    reindex_on_update: false
```

### 5.3 Embedding Provider 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `provider` | string | `"openai"` | Embedding 提供者：`openai` / `ollama` / `custom` |
| `model` | string | `"text-embedding-3-small"` | 模型名称 |
| `api_key` | string | — | API 密钥（支持 `${VAR}` 环境变量引用） |
| `base_url` | string | — | API 基础 URL |
| `dimension` | int | `1536` | 向量维度 |
| `cache_enabled` | bool | `true` | 是否启用 Embedding 缓存 |
| `batch_size` | int | `100` | 批量嵌入请求大小 |

```yaml
memory:
  embedding:
    provider: ollama
    model: nomic-embed-text
    base_url: http://localhost:11434
    dimension: 768
    cache_enabled: true
    batch_size: 50
```

### 5.4 Go 结构体定义

```go
// VectorConfig 是向量搜索配置。
type VectorConfig struct {
    Enabled             bool    `yaml:"enabled"`
    SimilarityThreshold float64 `yaml:"similarity_threshold"`
    TopK                int     `yaml:"top_k"`
    FallbackToKeyword   bool    `yaml:"fallback_to_keyword"`
    ReindexOnUpdate     bool    `yaml:"reindex_on_update"`
}

// EmbeddingConfig 是 Embedding Provider 配置。
type EmbeddingConfig struct {
    Provider     string `yaml:"provider"`
    Model        string `yaml:"model"`
    APIKey       string `yaml:"api_key"`
    BaseURL      string `yaml:"base_url"`
    Dimension    int    `yaml:"dimension"`
    CacheEnabled bool   `yaml:"cache_enabled"`
    BatchSize    int    `yaml:"batch_size"`
}

// StorageConfig 是存储后端配置。
type StorageConfig struct {
    Type        string        `yaml:"type"`
    Path        string        `yaml:"path,omitempty"`
    JournalMode string        `yaml:"journal_mode,omitempty"`
    BusyTimeout time.Duration `yaml:"busy_timeout,omitempty"`
    Migrate     bool          `yaml:"migrate,omitempty"`
    Engine      string        `yaml:"engine,omitempty"`
    Endpoint    string        `yaml:"endpoint,omitempty"`
    APIKey      string        `yaml:"api_key,omitempty"`
    Collection  string        `yaml:"collection,omitempty"`
    Timeout     time.Duration `yaml:"timeout,omitempty"`
}
```

---

## 6. 配置校验规则

| 规则 | 说明 |
|------|------|
| `backend` 必须是 `sqlite` / `vector` / `external` 之一 | 否则启动报错 |
| `vector.enabled = true` 时 `embedding.provider` 必须配置 | 否则启动报错 |
| `max_items` 必须 > 0 | 否则使用默认值 `10000` |
| `similarity_threshold` 范围 `[0, 1]` | 超出范围使用默认值 `0.7` |
| `dimension` 必须与 Embedding 模型匹配 | 运行时检测不匹配则报错 |
| `default_ttl = 0` 表示永不过期 | 非零值必须 > 0 |
| Agent 级别 `backend` / `storage` 不可覆盖 | 静默忽略，日志 warn |

---

## 7. 完整配置示例

```yaml
# yaa.yaml — Memory 系统完整配置示例
memory:
  enabled: true
  backend: vector
  eviction_policy: lru
  max_items: 20000
  default_ttl: 168h          # 7 天
  expire_interval: 10m
  expire_batch_size: 1000

  storage:
    type: vector
    engine: sqlite-vec
    path: ./data/yaa_vectors.db
    collection: yaa_memories

  vector:
    enabled: true
    similarity_threshold: 0.78
    top_k: 15
    fallback_to_keyword: true
    reindex_on_update: true

  embedding:
    provider: openai
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    dimension: 1536
    cache_enabled: true
    batch_size: 100

agents:
  - id: "default"
    name: "Default Agent"
    provider: "openai"
    model: "gpt-4o"
    memory:
      enabled: true
      max_items: 50000
      vector:
        enabled: true
        similarity_threshold: 0.80
```

---

*最后更新: 2025-07-17*
