# Session 配置参考

> Yaa! Yet Another Agent Runtime
> 文档路径: `docs/session/config-ref.md`
> 依赖: `docs/session/README.md` §2 (核心接口), `docs/architecture.md` §3.3 (Session)

---

## 1. 配置层级

Yaa! 的 Session 配置采用**三层覆盖**策略：

| 层级 | 作用域 | 优先级 | 说明 |
|------|--------|--------|------|
| **全局默认** (`config.yaml`) | 所有 Agent | 低 | Runtime 启动时加载，作为所有 Session 的基线配置 |
| **Agent 级别覆盖** (`agents[].session`) | 单个 Agent | 中 | 在 Agent 定义中覆盖全局默认，仅影响该 Agent 的 Session |
| **Session 级别参数** (`Create()`) | 单个 Session | 高 | 创建 Session 时通过 `SessionOption` 传入，仅影响该 Session |

> **规则**: 高优先级配置覆盖低优先级配置的同名字段，未覆盖的字段继承上层。

```text
全局默认 (config.yaml)
    │
    ├─ Agent A: 覆盖 maxMessages=100
    │     │
    │     └─ Session A1: 覆盖 ttl=2h
    │
    └─ Agent B: 无覆盖 (继承全局)
          │
          └─ Session B1: 继承 Agent B = 继承全局
```

---

## 2. 全局 Session 配置

### 2.1 配置项一览

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `session.maxMessages` | `int` | `1000` | 单个 Session 最大消息数，超出触发截断 |
| `session.maxMessageBytes` | `int` | `10485760` | 单条消息最大字节数 (默认 10MB) |
| `session.ttl` | `duration` | `24h` | Session 空闲超时时间，超时自动 Paused |
| `session.maxLifetime` | `duration` | `720h` | Session 最大存活时间，超时自动 Closed (默认 30 天) |
| `session.persist` | `bool` | `true` | 是否启用 Session 持久化 |
| `session.persistInterval` | `duration` | `5s` | 持久化写入间隔（批量刷盘周期） |
| `session.maxSessionsPerAgent` | `int` | `100` | 单个 Agent 最大并发 Session 数 |
| `session.cleanupInterval` | `duration` | `1m` | 过期 Session 清理检查间隔 |
| `session.historyTrimPolicy` | `string` | `"oldest"` | 消息截断策略: `oldest`(删除最早) / `summarize`(摘要压缩) |

### 2.2 YAML 示例

```yaml
# config.yaml — 全局 Session 配置
session:
  # 消息历史限制
  maxMessages: 1000
  maxMessageBytes: 10485760  # 10MB

  # 超时与生命周期
  ttl: 24h                   # 空闲 24h 后自动暂停
  maxLifetime: 720h          # 30 天后自动关闭

  # 持久化
  persist: true
  persistInterval: 5s        # 每 5s 批量刷盘

  # 并发限制
  maxSessionsPerAgent: 100

  # 清理与截断
  cleanupInterval: 1m
  historyTrimPolicy: oldest  # oldest | summarize
```

---

## 3. Agent 级别覆盖

在 Agent 定义中通过 `session` 字段覆盖全局配置。仅需写明需要覆盖的字段，其余继承全局默认。

### 3.1 YAML 示例

```yaml
# config.yaml — Agent 级别覆盖
agents:
  - id: agent-support
    model: glm-4-flash
    session:
      maxMessages: 200          # 覆盖全局 1000
      ttl: 2h                   # 覆盖全局 24h，客服场景更短
      historyTrimPolicy: summarize

  - id: agent-coder
    model: deepseek-coder-v2
    session:
      maxMessages: 5000         # 代码助手需要更长上下文
      maxLifetime: 2160h         # 90 天
      maxSessionsPerAgent: 20   # 限制并发数
```

### 3.2 覆盖规则表

| 字段 | 全局默认 | agent-support | agent-coder |
|------|----------|---------------|------------|
| `maxMessages` | 1000 | **200** | **5000** |
| `ttl` | 24h | **2h** | 24h (继承) |
| `maxLifetime` | 720h | 720h (继承) | **2160h** |
| `persist` | true | true (继承) | true (继承) |
| `historyTrimPolicy` | oldest | **summarize** | oldest (继承) |
| `maxSessionsPerAgent` | 100 | 100 (继承) | **20** |

---

## 4. Session 级别参数

通过 `SessionOption` 在创建时传入，优先级最高。

### 4.1 Go 代码示例

```go
package main

import (
    "time"
    "github.com/imshuai/yaa/pkg/session"
)

func ExampleCreate() {
    mgr := session.NewManager(store, logger, globalCfg)

    // 方式一: 使用默认配置（继承全局 + Agent 级别覆盖）
    s1, err := mgr.Create("agent-coder")

    // 方式二: 通过 Option 覆盖单个 Session 参数
    s2, err := mgr.Create("agent-coder",
        session.WithTTL(4*time.Hour),           // 覆盖 ttl
        session.WithMaxMessages(8000),         // 覆盖 maxMessages
        session.WithMetadata(map[string]any{
            "title":  "Refactor auth module",
            "source": "vscode",
            "tags":   []string{"refactor", "auth"},
        }),
    )
    _ = s1
    _ = s2
}
```

### 4.2 SessionOption 列表

| Option 函数 | 对应字段 | 说明 |
|-------------|----------|------|
| `WithTTL(d time.Duration)` | `ttl` | 设置该 Session 的空闲超时 |
| `WithMaxLifetime(d time.Duration)` | `maxLifetime` | 设置该 Session 的最大存活时间 |
| `WithMaxMessages(n int)` | `maxMessages` | 设置该 Session 的最大消息数 |
| `WithPersist(b bool)` | `persist` | 是否持久化该 Session |
| `WithMetadata(m map[string]any)` | `metadata` | 初始元数据 |

---

## 5. 配置解析与合并

### 5.1 合并优先级

```go
// pkg/session/config.go

// SessionConfig 单个 Session 的生效配置（合并后）。
type SessionConfig struct {
    MaxMessages       int
    MaxMessageBytes   int
    TTL               time.Duration
    MaxLifetime        time.Duration
    Persist           bool
    PersistInterval   time.Duration
    MaxSessionsPerAgent int
    CleanupInterval   time.Duration
    HistoryTrimPolicy string
}

// resolveConfig 按优先级合并配置: 全局 → Agent → Session Option。
func resolveConfig(global, agentOver, sessOpt *SessionConfig) SessionConfig {
    out := *global // 复制全局默认

    // Agent 级别覆盖（仅非零值字段生效）
    if agentOver != nil {
        out.OverrideBy(agentOver)
    }

    // Session Option 覆盖（仅非零值字段生效）
    if sessOpt != nil {
        out.OverrideBy(sessOpt)
    }
    return out
}
```

### 5.2 验证规则

| 规则 | 说明 |
|------|------|
| `maxMessages > 0` | 必须为正整数 |
| `ttl >= 1m` | 最小空闲超时 1 分钟 |
| `maxLifetime >= ttl` | 最大存活时间不能小于空闲超时 |
| `persistInterval >= 1s` | 最小刷盘间隔 1 秒 |
| `maxSessionsPerAgent > 0` | 必须为正整数 |
| `historyTrimPolicy ∈ {oldest, summarize}` | 仅允许这两个值 |

```go
// Validate 校验配置合法性，返回第一个违反规则的字段。
func (c *SessionConfig) Validate() error {
    if c.MaxMessages <= 0 {
        return fmt.Errorf("session.maxMessages must be > 0, got %d", c.MaxMessages)
    }
    if c.TTL < time.Minute {
        return fmt.Errorf("session.ttl must be >= 1m, got %s", c.TTL)
    }
    if c.MaxLifetime < c.TTL {
        return fmt.Errorf("session.maxLifetime (%s) must be >= ttl (%s)",
            c.MaxLifetime, c.TTL)
    }
    if c.HistoryTrimPolicy != "oldest" && c.HistoryTrimPolicy != "summarize" {
        return fmt.Errorf("session.historyTrimPolicy must be 'oldest' or 'summarize', got %q",
            c.HistoryTrimPolicy)
    }
    return nil
}
```

---

## 6. 持久化策略详解

### 6.1 策略选择

| 策略 | `persist` | 适用场景 | 恢复行为 |
|------|-----------|----------|----------|
| **全持久化** | `true` | 生产环境，需要重启恢复 | Runtime 启动时 `RestoreAll()` 加载所有 Session |
| **纯内存** | `false` | 临时对话、测试环境 | Runtime 重启后 Session 丢失 |

### 6.2 持久化触发时机

```go
// 持久化触发条件:
// 1. 定时刷盘: 每 persistInterval (默认 5s) 批量写入 dirty Session
// 2. 状态变更: Pause / Close / Close 时立即写入
// 3. 消息追加: AppendMessage 后标记 dirty, 等待下次刷盘

func (m *Manager) flushLoop() {
    ticker := time.NewTicker(m.cfg.PersistInterval)
    defer ticker.Stop()
    for range ticker.C {
        m.flushDirty() // 批量写入所有 dirty Session
    }
}
```

| 触发方式 | 同步/异步 | 说明 |
|----------|-----------|------|
| 定时刷盘 | 异步 | 每 `persistInterval` 批量写入，减少 I/O 压力 |
| 状态变更 | 同步 | Pause/Close 立即写入，保证状态一致性 |
| 消息追加 | 异步 | 标记 dirty，等待下次刷盘周期 |

---

## 7. 完整配置示例

```yaml
# config.yaml — 生产环境完整配置示例
session:
  maxMessages: 2000
  maxMessageBytes: 10485760
  ttl: 48h
  maxLifetime: 1440h          # 60 天
  persist: true
  persistInterval: 5s
  maxSessionsPerAgent: 50
  cleanupInterval: 2m
  historyTrimPolicy: summarize

agents:
  - id: agent-support
    model: glm-4-flash
    session:
      maxMessages: 200
      ttl: 2h
      historyTrimPolicy: oldest

  - id: agent-coder
    model: deepseek-coder-v2
    session:
      maxMessages: 8000
      maxLifetime: 4320h      # 180 天
      maxSessionsPerAgent: 10
```

```go
// 运行时创建 Session，覆盖 Agent 级别配置
s, err := mgr.Create("agent-coder",
    session.WithTTL(8*time.Hour),
    session.WithMetadata(map[string]any{
        "title":  "Bug #142: race condition in flushLoop",
        "source": "cli",
    }),
)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Session created: %s\n", s.ID)
```
