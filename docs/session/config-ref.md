# Session 配置参考

> 文档路径: `docs/session/config-ref.md`
> 上级: [README.md](README.md)

---

## 1. 权威类型

配置 DTO 位于 `internal/config`。根配置包含新 Session 的默认 policy 和 Manager 控制项；Agent/Create 只能覆盖 policy 字段。

```go
type SessionConfig struct {
    MaxMessages          int           `yaml:"max_messages" json:"max_messages"`
    MaxMessageBytes      int           `yaml:"max_message_bytes" json:"max_message_bytes"`
    TTL                  time.Duration `yaml:"ttl" json:"ttl"`
    MaxLifetime          time.Duration `yaml:"max_lifetime" json:"max_lifetime"`
    Persist              bool          `yaml:"persist" json:"persist"`
    MaxSessionsPerAgent  int           `yaml:"max_sessions_per_agent" json:"max_sessions_per_agent"`
    CleanupInterval      time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
}

type SessionOverride struct {
    MaxMessages     *int           `yaml:"max_messages" json:"max_messages"`
    MaxMessageBytes *int           `yaml:"max_message_bytes" json:"max_message_bytes"`
    TTL             *time.Duration `yaml:"ttl" json:"ttl"`
    MaxLifetime     *time.Duration `yaml:"max_lifetime" json:"max_lifetime"`
    Persist         *bool          `yaml:"persist" json:"persist"`
}
```

运行时实体持久化解析后的 policy：

```go
type SessionPolicy struct {
    MaxMessages     int
    MaxMessageBytes int
    TTL             time.Duration
    MaxLifetime     time.Duration
    Persist         bool
}
```

`Config.Session` 是 `SessionConfig`；`AgentConfig.Session` 是 `*SessionOverride`。`max_sessions_per_agent` 和 `cleanup_interval` 是 Manager 控制项，不能在 Agent 或单个 Session 覆盖。

## 2. 默认值与规则

| 字段 | 默认值 | 规则 | 生效范围 |
|------|--------|------|----------|
| `max_messages` | `1000` | `> 0`；达到上限拒绝追加，不删除历史 | 新 Session policy |
| `max_message_bytes` | `10485760` | `> 0`；按序列化后的 Provider message 字节数检查 | 新 Session policy |
| `ttl` | `24h` | 0 禁用；否则 `>= 1m`；空闲到期转 `Paused` | 新 Session policy |
| `max_lifetime` | `720h` | 0 禁用；否则 `>= 1m`，且启用 TTL 时必须 `>= ttl` | 新 Session policy |
| `persist` | `true` | false 时不读写 Storage，重启后丢失 | 新 Session policy |
| `max_sessions_per_agent` | `100` | `> 0`；只统计非 Closed Session | Manager |
| `cleanup_interval` | `1m` | `>= 1s`；只控制检查频率 | Manager |

```yaml
session:
  max_messages: 1000
  max_message_bytes: 10485760
  ttl: 24h
  max_lifetime: 720h
  persist: true
  max_sessions_per_agent: 100
  cleanup_interval: 1m
```

Session 不配置历史摘要或自动裁剪。完整历史由 Session 保存；发送给模型的窗口由 Context Manager 管理。

## 3. Presence-aware 合并

优先级固定为根配置、Agent override、Create override。Create 时生成不可变 `SessionPolicy`，连同 schema version 一起持久化；之后热更新只影响新建 Session。

```go
func ResolveSessionPolicy(
    root SessionConfig,
    agent *SessionOverride,
    create *SessionOverride,
) SessionPolicy {
    out := SessionPolicy{
        MaxMessages:     root.MaxMessages,
        MaxMessageBytes: root.MaxMessageBytes,
        TTL:             root.TTL,
        MaxLifetime:     root.MaxLifetime,
        Persist:         root.Persist,
    }
    apply := func(p *SessionOverride) {
        if p == nil { return }
        if p.MaxMessages != nil { out.MaxMessages = *p.MaxMessages }
        if p.MaxMessageBytes != nil { out.MaxMessageBytes = *p.MaxMessageBytes }
        if p.TTL != nil { out.TTL = *p.TTL }
        if p.MaxLifetime != nil { out.MaxLifetime = *p.MaxLifetime }
        if p.Persist != nil { out.Persist = *p.Persist }
    }
    apply(agent)
    apply(create)
    return out
}
```

显式 `persist: false` 和 `ttl: 0` 必须覆盖上层值，禁止按“非零字段”合并。

## 4. Agent 和 Create 示例

```yaml
agents:
  - id: ephemeral-agent
    name: Ephemeral Agent
    provider: ollama
    model: llama3
    session:
      max_messages: 200
      ttl: 2h
      max_lifetime: 168h
      persist: false
```

```go
sess, err := manager.Create(ctx, session.CreateRequest{
    AgentID: "ephemeral-agent",
    Policy: &config.SessionOverride{
        TTL:     ptr(4 * time.Hour),
        Persist: ptr(false),
    },
    Metadata: map[string]any{"title": "temporary analysis"},
})
```

Create override 只通过结构化 `CreateRequest` 传递，不再定义一组可重复、顺序敏感的函数式 Option。

## 5. 校验阶段

配置加载时校验根 `SessionConfig` 和每个 Agent 合并后的 policy。Create 时合并请求 override 后再次校验。错误使用带完整路径的 `config.ValidationError`，例如 `agents[2].session.ttl`。

热更新规则：

- `session.max_sessions_per_agent` 和 `session.cleanup_interval` 在下一次创建/检查生效。
- 根 policy 和 `agents[].session.*` 只影响热更新后新建的 Session。
- 已创建 Session 始终使用自身持久化的 resolved policy，避免重启或 reload 改变生命周期。

---

*最后更新: 2026-07-22*
