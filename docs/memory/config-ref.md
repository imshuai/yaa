# Memory 配置参考

> 上级: [Memory 系统设计](README.md)
> 根配置: [完整配置参考](../config/reference.md)

---

## 1. 合并层级

只有根 `memory` 和 Agent `memory` 两层。合并顺序为根配置 → `agents[].memory` pointer override；没有 Session 级 Memory 配置。Storage 和 embedding 连接是 Runtime 共享基础设施，Agent 只能覆盖策略字段。

## 2. 根类型与默认值

```go
type MemoryConfig struct {
    Enabled        bool                  `yaml:"enabled" json:"enabled"`
    MaxItems       int                   `yaml:"max_items" json:"max_items"`
    DefaultTTL     time.Duration         `yaml:"default_ttl" json:"default_ttl"`
    ExpireInterval time.Duration         `yaml:"expire_interval" json:"expire_interval"`
    ExpireBatchSize int                  `yaml:"expire_batch_size" json:"expire_batch_size"`
    EvictionPolicy string                `yaml:"eviction_policy" json:"eviction_policy"` // fifo | ttl
    Storage        MemoryStorageConfig   `yaml:"storage" json:"storage"`
    Vector         MemoryVectorConfig    `yaml:"vector" json:"vector"`
    Embedding      MemoryEmbeddingConfig `yaml:"embedding" json:"embedding"`
}

type MemoryStorageConfig struct {
    Type string `yaml:"type" json:"type"` // sqlite | memory
    Path string `yaml:"path" json:"path"`
}

type MemoryVectorConfig struct {
    Enabled             bool    `yaml:"enabled" json:"enabled"`
    SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
    TopK                int     `yaml:"top_k" json:"top_k"`
    FallbackToKeyword   bool    `yaml:"fallback_to_keyword" json:"fallback_to_keyword"`
}

type MemoryEmbeddingConfig struct {
    Provider  string        `yaml:"provider" json:"provider"`
    Model     string        `yaml:"model" json:"model"`
    APIKey    string        `yaml:"api_key" json:"api_key"`
    BaseURL   string        `yaml:"base_url" json:"base_url"`
    Dimension int           `yaml:"dimension" json:"dimension"`
    Timeout   time.Duration `yaml:"timeout" json:"timeout"`
}
```

| 字段 | 默认值 | 规则 |
|------|--------|------|
| `enabled` | `true` | false 时该 Runtime 不创建 Memory 操作；Agent 不能在根 false 上重新启用 |
| `max_items` | `10000` | 每 Agent 未过期 item 上限，`>0` |
| `default_ttl` | `0` | 0 表示永不过期，否则 `>=1m` |
| `expire_interval` | `5m` | 全局 cleanup worker 周期，`>=1s` |
| `expire_batch_size` | `500` | 每批 1..10000 |
| `eviction_policy` | `fifo` | 只能 `fifo` 或 `ttl` |
| `storage.type` | `sqlite` | 只能 `sqlite` 或 `memory` |
| `storage.path` | `./data/yaa-memory.db` | sqlite 必填；memory 忽略 |
| `vector.enabled` | `false` | 启用纯 Go exact cosine index |
| `vector.similarity_threshold` | `0.7` | `(0,1]`，只用于向量结果 |
| `vector.top_k` | `10` | `1..100`；Search Limit=0 使用此值 |
| `vector.fallback_to_keyword` | `true` | 向量失败时是否走关键词路径 |
| `embedding.provider` | `openai-compatible` | vector 启用时必填 |
| `embedding.model` | — | vector 启用时必填 |
| `embedding.api_key` | — | 按 provider 要求，可使用环境变量引用 |
| `embedding.base_url` | — | vector 启用时必填，例如 `https://api.openai.com/v1` |
| `embedding.dimension` | — | vector 启用时正数，且响应长度必须相同 |
| `embedding.timeout` | `30s` | `>0` |

未知字段必须硬失败。v1 不接受另一种 Memory 内容后端、全文索引扩展或外部向量服务配置；未来新增能力必须增加版本化字段和对应接口。

## 3. YAML 示例

```yaml
memory:
  enabled: true
  max_items: 10000
  default_ttl: 0
  expire_interval: 5m
  expire_batch_size: 500
  eviction_policy: fifo
  storage:
    type: sqlite
    path: ./data/yaa-memory.db
  vector:
    enabled: false
    similarity_threshold: 0.7
    top_k: 10
    fallback_to_keyword: true
  embedding:
    provider: openai-compatible
    model: text-embedding-3-small
    api_key: ${OPENAI_API_KEY}
    base_url: https://api.openai.com/v1
    dimension: 1536
    timeout: 30s
```

## 4. Agent `MemoryOverride`

```go
type MemoryOverride struct {
    Enabled        *bool                     `yaml:"enabled" json:"enabled"`
    MaxItems       *int                      `yaml:"max_items" json:"max_items"`
    DefaultTTL     *time.Duration            `yaml:"default_ttl" json:"default_ttl"`
    EvictionPolicy *string                   `yaml:"eviction_policy" json:"eviction_policy"`
    Vector         *MemoryVectorOverride     `yaml:"vector" json:"vector"`
}

type MemoryVectorOverride struct {
    Enabled             *bool    `yaml:"enabled" json:"enabled"`
    SimilarityThreshold *float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
    TopK                *int     `yaml:"top_k" json:"top_k"`
    FallbackToKeyword   *bool    `yaml:"fallback_to_keyword" json:"fallback_to_keyword"`
}
```

`ResolveMemoryPolicy` 从调用方捕获的 Config snapshot 解析 Agent policy；Memory Manager 不保存另一份可热更新 map。cleanup、storage 和 embedding 不进入该结构：

```go
type MemoryPolicy struct {
    Enabled        bool
    MaxItems       int
    DefaultTTL     time.Duration
    EvictionPolicy string
    Vector         MemoryVectorConfig
}

func ResolveMemoryPolicy(root MemoryConfig, override *MemoryOverride) MemoryPolicy {
    out := MemoryPolicy{
        Enabled:        root.Enabled,
        MaxItems:       root.MaxItems,
        DefaultTTL:     root.DefaultTTL,
        EvictionPolicy: root.EvictionPolicy,
        Vector:         root.Vector,
    }
    if override == nil {
        return out
    }
    if override.Enabled != nil { out.Enabled = *override.Enabled }
    if override.MaxItems != nil { out.MaxItems = *override.MaxItems }
    if override.DefaultTTL != nil { out.DefaultTTL = *override.DefaultTTL }
    if override.EvictionPolicy != nil { out.EvictionPolicy = *override.EvictionPolicy }
    if v := override.Vector; v != nil {
        if v.Enabled != nil { out.Vector.Enabled = *v.Enabled }
        if v.SimilarityThreshold != nil { out.Vector.SimilarityThreshold = *v.SimilarityThreshold }
        if v.TopK != nil { out.Vector.TopK = *v.TopK }
        if v.FallbackToKeyword != nil { out.Vector.FallbackToKeyword = *v.FallbackToKeyword }
    }
    return out
}
```

所有 override scalar 都是 pointer；显式 `false`、`0` 能覆盖根值。Agent 不得覆盖 `storage` 或 `embedding`，也不能覆盖 `expire_interval`/`expire_batch_size`。`vector.*` 允许声明但改变向量基础设施，需重启；其余 policy 字段在下一次 Agent turn 或 Remote request 捕获 snapshot 时生效。

```yaml
agents:
  - id: researcher
    name: Researcher
    provider: openai
    model: gpt-4o
    memory:
      max_items: 20000
      default_ttl: 168h
      eviction_policy: ttl
  - id: ephemeral
    name: Ephemeral
    provider: ollama
    model: llama3
    memory:
      enabled: false
```

`ResolveMemoryPolicy(root, override)` 只复制非 nil 字段并返回 Effective Policy；如果 root `enabled=false` 而 override `enabled=true`，校验失败。

## 5. 校验

加载和 reload 时按以下顺序校验：

1. 先注入默认值，再校验所有根字段；未知字段在 strict decoder 阶段拒绝。
2. `max_items`、batch、interval、TTL 和 eviction enum 满足上表范围。
3. storage type 为 `sqlite|memory`；sqlite path 非空。
4. threshold/top_k 始终满足范围；至少一个根或 Agent effective policy 同时启用 Memory 和 vector 时，embedding provider/model/base_url/dimension/timeout 必须完整且 dimension>0。
5. 对每个 Agent 合并 override 后重新校验 policy；Agent 不能越过 root disabled 或修改共享连接。

`enabled=false` 仍校验 storage、cleanup、policy 与 vector 的结构、枚举和范围；只跳过未启用能力的 embedding 完整性依赖。配置 reload 不重新解释已有 item 的绝对 ExpiresAt。

## 6. 热更新

| 路径 | 生效时机 |
|------|----------|
| `memory.max_items`, `default_ttl`, `eviction_policy` | 下一次 Agent turn/Remote request；已有 ExpiresAt 不变 |
| `memory.expire_interval`, `expire_batch_size` | 下一次 cleanup worker tick |
| `agents[].memory.max_items`, `default_ttl`, `eviction_policy` | 下一次该 Agent turn/Remote request |
| `enabled`, `storage.*`, `vector.*`, `embedding.*`（根或 Agent） | 需要重启 |

热字段与重启字段混在同一 reload 批次时整批拒绝。Agent turn 内所有 Memory 调用显式传同一个旧 policy；Remote 每请求、cleanup 每 tick各自捕获一次，不能在操作中途切换。

---

*最后更新: 2026-07-22*
