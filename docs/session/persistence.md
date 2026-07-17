# Session 持久化与恢复

> 文档路径: `docs/session/persistence.md`
> 上级: `docs/session/README.md` §2

---

## 1. 概述

Session 持久化确保 Runtime 重启后能够恢复所有 Session 数据，包括消息历史、状态和元数据。Yaa! 使用 Storage 抽象层，默认实现为 SQLite。

---

## 2. 存储接口

```go
// SessionStore 定义 Session 的持久化接口。
type SessionStore interface {
    Save(sess *Session) error
    Load(sessionID string) (*Session, error)
    LoadByAgent(agentID string) ([]*Session, error)
    LoadAll() ([]*Session, error)
    Delete(sessionID string) error
    Close() error
}
```

---

## 3. 存储格式

### 3.1 SQLite 表结构

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    state       INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    metadata    TEXT,             -- JSON
    messages    TEXT              -- JSON array
);

CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
```

### 3.2 键值存储格式

使用通用 Storage 接口时，Key 格式为：

```
session/{agentID}/{sessionID}     → Session JSON
session/index/{agentID}           → []sessionID JSON
```

---

## 4. 序列化

Session 序列化为 JSON 格式，兼容人类可读和机器解析：

```go
type sessionJSON struct {
    ID        string            `json:"id"`
    AgentID   string            `json:"agent_id"`
    State     int               `json:"state"`
    Messages  []Message         `json:"messages"`
    CreatedAt time.Time         `json:"created_at"`
    UpdatedAt time.Time         `json:"updated_at"`
    Metadata  map[string]any    `json:"metadata,omitempty"`
}

func (s *Session) MarshalJSON() ([]byte, error) {
    return json.Marshal(sessionJSON{
        ID:        s.ID,
        AgentID:   s.AgentID,
        State:     int(s.State),
        Messages:  s.Messages,
        CreatedAt: s.CreatedAt,
        UpdatedAt: s.UpdatedAt,
        Metadata:  s.Metadata,
    })
}

func (s *Session) UnmarshalJSON(data []byte) error {
    var sj sessionJSON
    if err := json.Unmarshal(data, &sj); err != nil {
        return err
    }
    s.ID = sj.ID
    s.AgentID = sj.AgentID
    s.State = SessionState(sj.State)
    s.Messages = sj.Messages
    s.CreatedAt = sj.CreatedAt
    s.UpdatedAt = sj.UpdatedAt
    s.Metadata = sj.Metadata
    return nil
}
```

---

## 5. 持久化策略

| 策略 | 触发时机 | 说明 |
|------|---------|------|
| **同步写入** | `Create()` / `Close()` / `Delete()` | 低频操作，同步确保一致性 |
| **异步写入** | `AppendMessage()` / `UpdateMetadata()` | 高频操作，通过写入队列异步处理 |
| **批量写入** | 定时器触发 | 积攒变更后批量 flush |

```go
type writeQueue struct {
    ch     chan *Session
    store  SessionStore
    logger *slog.Logger
    wg     sync.WaitGroup
}

func (q *writeQueue) run(ctx context.Context) {
    q.wg.Add(1)
    defer q.wg.Done()

    batch := make(map[string]*Session)
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    flush := func() {
        if len(batch) == 0 {
            return
        }
        for _, sess := range batch {
            if err := q.store.Save(sess); err != nil {
                q.logger.Error("persist session failed",
                    "session_id", sess.ID, "err", err)
            }
        }
        batch = make(map[string]*Session)
    }

    for {
        select {
        case <-ctx.Done():
            flush()
            return
        case sess := <-q.ch:
            batch[sess.ID] = sess
        case <-ticker.C:
            flush()
        }
    }
}
```

---

## 6. 启动恢复流程

```text
Runtime 启动
  │
  ├─ 1. 打开 Storage 连接
  │
  ├─ 2. SessionManager.RestoreAll()
  │     │
  │     ├─ store.LoadAll() → []*Session
  │     │
  │     ├─ 遍历每个 Session：
  │     │   ├─ 重建内存索引 (sessions map, agentIdx map)
  │     │   ├─ 状态修正：
  │     │   │   ├─ Created → 保持 Created（无消息的空会话）
  │     │   │   ├─ Active → 保持 Active
  │     │   │   └─ Paused → 保持 Paused
  │     │   └─ Closed → 加载但不注册到活跃索引
  │     │
  │     └─ 记录恢复统计日志
  │
  ├─ 3. 恢复 WebSocket/SSE 连接（客户端重连后重新绑定 Session）
  │
  └─ 4. Runtime Ready
```

```go
func (m *Manager) RestoreAll() error {
    sessions, err := m.store.LoadAll()
    if err != nil {
        return fmt.Errorf("load sessions: %w", err)
    }

    m.mu.Lock()
    defer m.mu.Unlock()

    restored := 0
    for _, sess := range sessions {
        m.sessions[sess.ID] = sess
        m.agentIdx[sess.AgentID] = append(
            m.agentIdx[sess.AgentID], sess.ID)

        if sess.State != SessionStateClosed {
            restored++
        }
    }

    m.logger.Info("sessions restored",
        "total", len(sessions), "active", restored)
    return nil
}
```

---

## 7. 数据一致性

| 场景 | 处理方式 |
|------|---------|
| Runtime 崩溃 | 异步写入可能丢失最近 2s 变更，重启后从 Storage 恢复 |
| 写入失败 | 重试 3 次，仍失败则记录错误日志，内存数据保留 |
| 并发写入 | 同一 Session 串行处理（见 concurrency.md） |
| 存储损坏 | SQLite WAL 模式提供崩溃恢复；定期 VACUUM 优化 |

---

## 8. 存储迁移

```go
// Migrate 检查并执行存储迁移。
func (s *SQLiteStore) Migrate() error {
    // 检查表是否存在，不存在则创建
    // 检查列是否存在，不存在则 ALTER TABLE ADD COLUMN
    // 版本号记录在 schema_version 表中
    return s.db.Ping()
}
```

| 版本 | 变更 |
|------|------|
| v1 | 初始 schema |
| v2 | 新增 `metadata` 列 |
| v3 | 新增 `messages` JSON 列（替代独立 messages 表） |

---

*最后更新: 2025-07-16*
