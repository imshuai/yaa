# Session 并发模型

> 文档路径: `docs/session/concurrency.md`
> 上级: `docs/session/README.md` §2
> 参考: `docs/architecture.md` §5 并发模型

---

## 1. 概述

Yaa! 的并发模型遵循「每 Session 串行处理，多 Session 并行执行」的原则。这确保了单个 Session 内消息顺序一致性，同时允许多个 Session 并发处理。

---

## 2. 设计原则

| 原则 | 说明 |
|------|------|
| **每 Session 串行** | 同一 Session 内，消息按到达顺序依次处理 |
| **多 Session 并行** | 不同 Session 完全独立，可并行处理 |
| **无共享状态** | Session 之间不共享可变状态，仅通过 Manager 读取索引 |
| **锁最小化** | Manager 使用 RWMutex 保护索引，Session 内部使用独立锁 |

---

## 3. 消息队列机制

每个 Active Session 拥有一个独立的处理队列，消息按 FIFO 顺序串行处理：

```go
type sessionRunner struct {
    sessionID string
    queue     chan messageTask
    done      chan struct{}
}

type messageTask struct {
    msg      Message
    result   chan error
}

func (m *Manager) startRunner(sessionID string) *sessionRunner {
    r := &sessionRunner{
        sessionID: sessionID,
        queue:     make(chan messageTask, 256),
        done:      make(chan struct{}),
    }
    go r.run(m)
    return r
}

func (r *sessionRunner) run(m *Manager) {
    defer close(r.done)

    for task := range r.queue {
        // 串行处理每条消息
        err := m.processMessage(r.sessionID, task.msg)
        task.result <- err
    }
}
```

```text
                  Manager
                    │
    ┌───────────────┼───────────────┐
    │               │               │
    ▼               ▼               ▼
 Session A       Session B       Session C
 ┌────────┐     ┌────────┐     ┌────────┐
 │ Queue  │     │ Queue  │     │ Queue  │
 │ [msg1] │     │ [msg3] │     │ [msg5] │
 │ [msg2] │     │ [msg4] │     │        │
 └───┬────┘     └───┬────┘     └───┬────┘
     │              │              │
   串行            串行           串行
   goroutine     goroutine      goroutine
     │              │              │
     └──────────────┴──────────────┘
                    │
                 并行执行
```

---

## 4. 锁策略

### 4.1 锁层级

```text
Manager.mu (RWMutex)         ← 保护 sessions map 和 agentIdx map
  └─ Session 串行队列         ← 每个 Session 独立 goroutine，无需锁
      └─ subscribers.mu (RWMutex)  ← 保护订阅者列表
```

### 4.2 Manager 锁

```go
type Manager struct {
    mu         sync.RWMutex   // 保护索引
    sessions   map[string]*Session
    agentIdx   map[string][]string
    runners    map[string]*sessionRunner
    subMu      sync.RWMutex   // 保护订阅者
    subscribers map[string][]Subscriber
}
```

| 操作 | 锁类型 | 持锁范围 |
|------|--------|---------|
| `Create` | Write Lock | 插入索引 |
| `Get` | Read Lock | 查找 Session |
| `List` | Read Lock | 遍历索引 |
| `Delete` | Write Lock | 删除索引 |
| `AppendMessage` | Read Lock → 释放 | 获取 Session 引用后释放，由 Runner 串行处理 |

### 4.3 Session 内部无锁

```go
// AppendMessage 通过 Runner 串行处理，无需 Session 级锁
func (m *Manager) AppendMessage(sessionID string, msg Message) error {
    m.mu.RLock()
    runner, ok := m.runners[sessionID]
    m.mu.RUnlock()

    if !ok {
        return ErrSessionNotFound
    }

    task := messageTask{
        msg:    msg,
        result: make(chan error, 1),
    }
    runner.queue <- task

    return <-task.result
}
```

---

## 5. 多 Session 并行

```go
// processMessage 处理单条消息（在 Session 的专属 goroutine 中执行）。
func (m *Manager) processMessage(sessionID string, msg Message) error {
    sess, err := m.Get(sessionID)
    if err != nil {
        return err
    }

    // 1. 追加用户消息
    sess.Messages = append(sess.Messages, msg)

    // 2. 构建 Context（可能涉及 Memory、Skill 等）
    ctx, err := m.contextMgr.Build(sess)
    if err != nil {
        return err
    }

    // 3. 调用 Agent → Provider（流式返回）
    // 此步骤可能耗时较长，但不阻塞其他 Session
    resp, err := m.agentMgr.Chat(sess.AgentID, ctx)
    if err != nil {
        return err
    }

    // 4. 追加助手消息
    sess.Messages = append(sess.Messages, resp)

    // 5. 持久化
    return m.persist(sess)
}
```

**关键点：**
- `processMessage` 在 Session 专属 goroutine 中执行
- 不同 Session 的 goroutine 互不阻塞
- Provider 调用是 I/O 密集型，Go 协程天然并发

---

## 6. 并发场景分析

### 6.1 同一 Session 多消息快速到达

```text
Client 发送 msg1 → queue: [msg1]
Client 发送 msg2 → queue: [msg1, msg2]
                       │
                  串行处理 msg1 → 完成后处理 msg2
```

- msg2 等待 msg1 处理完成
- 队列容量 256，超出则阻塞调用方

### 6.2 多 Session 同时活跃

```text
Session A: 处理 msg1 (Provider 调用中, ~2s)
Session B: 处理 msg3 (Provider 调用中, ~3s)  ← 并行
Session C: 处理 msg5 (Provider 调用中, ~1s)  ← 并行
```

- 三个 Session 独立 goroutine，完全并行
- Provider 层负责连接池和限流

### 6.3 Runtime 关闭

```go
func (m *Manager) Shutdown(ctx context.Context) error {
    // 1. 停止接收新消息
    m.mu.Lock()
    for _, runner := range m.runners {
        close(runner.queue)
    }
    m.mu.Unlock()

    // 2. 等待所有 Runner 完成
    for _, runner := range m.runners {
        select {
        case <-runner.done:
        case <-ctx.Done():
            return ctx.Err()
        }
    }

    // 3. 持久化所有 Session
    return m.persistAll()
}
```

---

## 7. 性能考量

| 指标 | 目标 | 说明 |
|------|------|------|
| 单 Session 消息吞吐 | 10 msg/s | 受 Provider 延迟限制 |
| 并发 Session 数 | 1000+ | 受内存和文件描述符限制 |
| 队列等待时间 | <100ms | 正常负载下 |
| 锁竞争 | 极低 | RWMutex 读多写少 |

---

*最后更新: 2025-07-16*
