# Storage 模块集成

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/storage/integration.md`
> 依赖: `docs/architecture.md` §3.13, `docs/storage/README.md`

---

## 1. 概述

Storage 是 Yaa! Runtime 的底层持久化基础设施，所有需要持久化或缓存数据的上层模块都通过统一的 `Storage` 接口访问存储后端，而非直接操作文件或数据库。

本文档描述 Storage 与各核心模块的集成方式、Key 命名规范以及典型代码示例。

---

## 2. 模块集成总览

| 模块 | 是否使用 Storage | 用途 | Key 前缀 | 典型 TTL |
|------|:-:|------|----------|----------|
| **Session** | ✅ | 持久化会话状态、消息历史、元数据 | `session:` | 无（持久） |
| **Memory** | ✅ | Summary 层记忆存储、Short-term 缓存 | `memory:` | 可选 |
| **Config** | ✅ | 配置缓存、热更新校验快照 | `config:` | 30min |
| **Auth** | ✅ | Token 缓存、会话令牌 | `auth:` | 15min |
| **Agent** | ✅ | Agent 状态持久化（Paused / Running） | `agent:` | 无 |
| **MCP** | ✅ | MCP Server 连接状态缓存 | `mcp:` | 10min |
| **Provider** | ❌ | 直接调用外部 API，不涉及本地存储 | — | — |
| **Tool** | ❌ | 无状态执行，不持久化 | — | — |
| **Skill** | ❌ | 从文件系统加载，不经过 Storage | — | — |

---

## 3. Key 命名规范

所有 Key 采用冒号分隔的层级命名，保证 `Keys(prefix)` 前缀扫描可按模块隔离数据：

```text
session:{sessionID}                    → Session 序列化数据
session:{sessionID}:messages           → 消息历史（可拆分存储）
memory:{agentID}:{key}                 → Agent 记忆条目
memory:{agentID}:summary:{sessionID}  → Session 摘要
config:runtime:hash                    → 配置文件哈希（热更新检测）
config:cache:{section}                 → 解析后的配置缓存
auth:token:{tokenHash}                 → Token 验证缓存
agent:{agentID}:state                  → Agent 运行状态
mcp:{serverName}:status                → MCP Server 连接状态
```

---

## 4. Session 持久化

Session Manager 在每次消息轮次结束后将 Session 序列化为 JSON 写入 Storage，重启时通过前缀扫描恢复所有活跃 Session。

```go
// session/persist.go

package session

import (
    "encoding/json"
    "fmt"
    "time"
    "yaa/storage"
)

type Persistor struct {
    store storage.Storage
}

func NewPersistor(store storage.Storage) *Persistor {
    return &Persistor{store: store}
}

// Save 将 Session 序列化并持久化到 Storage
func (p *Persistor) Save(s *Session) error {
    data, err := json.Marshal(s)
    if err != nil {
        return fmt.Errorf("session marshal: %w", err)
    }
    key := fmt.Sprintf("session:%s", s.ID)
    return p.store.Set(key, data)
}

// Load 从 Storage 恢复单个 Session
func (p *Persistor) Load(sessionID string) (*Session, error) {
    data, err := p.store.Get(fmt.Sprintf("session:%s", sessionID))
    if err != nil {
        return nil, err
    }
    var s Session
    if err := json.Unmarshal(data, &s); err != nil {
        return nil, fmt.Errorf("session unmarshal: %w", err)
    }
    return &s, nil
}

// LoadAll 扫描所有持久化的 Session（重启恢复用）
func (p *Persistor) LoadAll() ([]*Session, error) {
    keys, err := p.store.Keys("session:")
    if err != nil {
        return nil, err
    }
    var sessions []*Session
    for _, key := range keys {
        data, err := p.store.Get(key)
        if err != nil {
            continue // 跳过已过期或损坏的记录
        }
        var s Session
        if json.Unmarshal(data, &s) == nil {
            sessions = append(sessions, &s)
        }
    }
    return sessions, nil
}

// Delete 关闭 Session 时清除持久化数据
func (p *Persistor) Delete(sessionID string) error {
    return p.store.Delete(fmt.Sprintf("session:%s", sessionID))
}
```

---

## 5. Memory 存储

Memory 系统的 Summary 层和 Short-term 缓存层使用 Storage 持久化，向量检索部分由独立的向量索引处理。

```go
// memory/store.go

package memory

import (
    "encoding/json"
    "fmt"
    "time"
    "yaa/storage"
)

type Store struct {
    store storage.Storage
}

func NewStore(store storage.Storage) *Store {
    return &Store{store: store}
}

// SaveSummary 持久化 Session 摘要（长期记忆）
func (s *Store) SaveSummary(agentID, sessionID string, summary *Summary) error {
    data, _ := json.Marshal(summary)
    key := fmt.Sprintf("memory:%s:summary:%s", agentID, sessionID)
    return s.store.Set(key, data) // 无 TTL，永久保存
}

// SaveShortTerm 缓存短期记忆，30 分钟后自动过期
func (s *Store) SaveShortTerm(agentID, key string, item *MemoryItem) error {
    data, _ := json.Marshal(item)
    k := fmt.Sprintf("memory:%s:%s", agentID, key)
    return s.store.Set(k, data, 30*time.Minute)
}

// SearchByAgent 列出某个 Agent 的所有记忆 Key
func (s *Store) SearchByAgent(agentID string) ([]string, error) {
    prefix := fmt.Sprintf("memory:%s:", agentID)
    return s.store.Keys(prefix)
}
```

---

## 6. Config 缓存

Config 模块在加载配置后将解析结果缓存到 Storage，同时定期写入配置文件哈希用于热更新检测。

```go
// config/cache.go

package config

import (
    "crypto/sha256"
    "encoding/hex"
    "time"
    "yaa/storage"
)

type Cache struct {
    store storage.Storage
}

func NewCache(store storage.Storage) *Cache {
    return &Cache{store: store}
}

// SaveHash 保存当前配置文件的哈希，用于热更新比较
func (c *Cache) SaveHash(rawConfig []byte) error {
    hash := sha256.Sum256(rawConfig)
    return c.store.Set("config:runtime:hash", []byte(hex.EncodeToString(hash[:])))
}

// HasChanged 比较当前配置哈希与缓存值
func (c *Cache) HasChanged(rawConfig []byte) (bool, error) {
    hash := sha256.Sum256(rawConfig)
    expected := hex.EncodeToString(hash[:])
    cached, err := c.store.Get("config:runtime:hash")
    if err == storage.ErrNotFound {
        return true, nil // 首次加载
    }
    if err != nil {
        return false, err
    }
    return string(cached) != expected, nil
}

// CacheSection 缓存解析后的配置段，30 分钟 TTL
func (c *Cache) CacheSection(section string, data []byte) error {
    key := "config:cache:" + section
    return c.store.Set(key, data, 30*time.Minute)
}
```

---

## 7. Auth Token 缓存

Auth 模块将已验证的 Token 缓存到 Storage，避免每次请求都执行完整验证流程。

```go
// auth/cache.go

package auth

import (
    "encoding/json"
    "fmt"
    "time"
    "yaa/storage"
)

type TokenCache struct {
    store storage.Storage
}

func NewTokenCache(store storage.Storage) *TokenCache {
    return &TokenCache{store: store}
}

// Set 缓存 Token 验证结果，15 分钟过期
func (tc *TokenCache) Set(tokenHash string, identity *Identity) error {
    data, _ := json.Marshal(identity)
    key := fmt.Sprintf("auth:token:%s", tokenHash)
    return tc.store.Set(key, data, 15*time.Minute)
}

// Get 读取缓存的 Token 身份信息
func (tc *TokenCache) Get(tokenHash string) (*Identity, error) {
    data, err := tc.store.Get(fmt.Sprintf("auth:token:%s", tokenHash))
    if err != nil {
        return nil, err
    }
    var id Identity
    if err := json.Unmarshal(data, &id); err != nil {
        return nil, err
    }
    return &id, nil
}
```

---

## 8. 初始化顺序

Runtime 启动时，Storage 在 Config 之后、其他模块之前初始化，确保所有依赖 Storage 的模块能获取到实例：

```text
Config → Storage → Provider → Memory → Tool → Skill → MCP →
Session → Context → Planner → Agent → Auth → API → Ready
```

```go
// runtime.go（简化）

func New(cfg *config.Config) (*Runtime, error) {
    // 1. 初始化 Storage
    store, err := storage.New(cfg.Runtime.Storage)
    if err != nil {
        return nil, fmt.Errorf("init storage: %w", err)
    }

    // 2. 注入到各模块
    memStore := memory.NewStore(store)
    sessPersistor := session.NewPersistor(store)
    authCache := auth.NewTokenCache(store)
    cfgCache := config.NewCache(store)

    // 3. 重启恢复 — 从 Storage 加载所有活跃 Session
    sessions, err := sessPersistor.LoadAll()
    // ...

    return &Runtime{
        Storage:  store,
        Memory:   memStore,
        Sessions: sessMgr,
        Auth:     authCache,
        Config:   cfgCache,
    }, nil
}
```

---

## 9. 注意事项

| 事项 | 说明 |
|------|------|
| **序列化格式** | 统一使用 JSON，保持可读性与跨平台兼容 |
| **Key 隔离** | 严格遵循命名前缀，避免模块间 Key 冲突 |
| **TTL 策略** | 缓存类数据必须设置 TTL，持久化数据不设 TTL |
| **错误处理** | 上层模块应处理 `ErrNotFound`，区分"无数据"与"存储故障" |
| **并发安全** | Storage 实现自身保证并发安全，上层无需加锁 |
| **优雅关闭** | Runtime 关闭时调用 `store.Close()` 刷写缓冲 |

---

*最后更新: 2025-07-17*
