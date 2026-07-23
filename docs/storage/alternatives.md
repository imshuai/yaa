# Memory Storage 后端

> 上级: [Storage 系统设计](README.md)

---

## 1. 用途

`runtime.storage.type=memory` 实现同一个根 `Storage` 接口，但不持久化。它用于单元测试、临时运行和故障注入；生产需要重启恢复 Session 时使用默认 SQLite。

v1 不提供第三种根 KV 后端。新增后端只有在 SQLite/memory 的实测指标不满足时才进入配置 enum，避免为未使用的依赖维护迁移、TTL 和平台兼容矩阵。

## 2. 数据模型

```go
type memValue struct {
    data      []byte
    expiresAt time.Time // zero 表示永不过期
}

type MemoryStorage struct {
    mu        sync.RWMutex
    values    map[string]memValue
    closed    bool
    clock     Clock
    stop      chan struct{}
    done      chan struct{}
    closeOnce sync.Once
}
```

实现规则：

- Set/Get 均复制 byte slice，不能把 map 内部内存暴露给调用方。
- Get/Has/Keys 在锁内检查 injected clock，隐藏并可顺手删除过期项。
- Keys 使用 `strings.HasPrefix` 后 `sort.Strings`，与 SQLite 的字节升序一致。
- Delete 对缺失 key 成功。
- 构造器启动唯一 cleanup worker，每 60 秒调用 `cleanupExpired(1000)`；一批满额时立即继续，规则与 SQLite 相同。
- Get/Has/Keys 仍惰性隐藏过期项，因此 cleanup 失败或尚未 tick 不会暴露过期值。
- Close 先在写锁内标记 closed，再关闭 `stop`，等待 `done`，最后清空 map；`sync.Once` 保证重复调用成功。
- 不为每个 TTL 创建 timer/goroutine。

## 3. 最小实现

```go
func (s *MemoryStorage) Get(key string) ([]byte, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.closed {
        return nil, ErrClosed
    }
    item, ok := s.values[key]
    if !ok || item.expired(s.clock.Now()) {
        delete(s.values, key)
        return nil, ErrNotFound
    }
    return bytes.Clone(item.data), nil
}

func (s *MemoryStorage) Keys(prefix string) ([]string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.closed {
        return nil, ErrClosed
    }
    now := s.clock.Now()
    keys := make([]string, 0)
    for key, item := range s.values {
        if item.expired(now) {
            delete(s.values, key)
            continue
        }
        if strings.HasPrefix(key, prefix) {
            keys = append(keys, key)
        }
    }
    sort.Strings(keys)
    return keys, nil
}
```

Set/Delete/Has 使用相同的 closed、key/value/TTL 校验。测试 Clock 避免 sleep。

worker 只负责物理清理；其循环固定为：

```go
func (s *MemoryStorage) cleanupLoop() {
    defer close(s.done)
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            for {
                select {
                case <-s.stop:
                    return
                default:
                }
                n := s.cleanupExpired(1000)
                if n < 1000 {
                    break
                }
            }
        case <-s.stop:
            return
        }
    }
}
```

单元测试直接调用 `cleanupExpired`；不等待真实 ticker。Close 不得持有 `mu` 等待 `<-done>`，否则会与正在清理的 worker 死锁。

## 4. 与 persist 的关系

`SessionPolicy.Persist=true` 表示 Session Manager 会调用根 Storage，不保证选中的后端跨进程持久化。若 root type 是 memory，调用在进程内仍成功，但 shutdown 后无法 Restore；启动日志和 `/api/v1/health` 必须明确报告 `durable=false`。

## 5. 后端对比

| 特性 | SQLite | Memory |
|------|--------|--------|
| 跨进程持久化 | 是 | 否 |
| 纯 Go / CGO | 是 / 无 | 是 / 无 |
| TTL | 惰性 + batch | 惰性 + batch |
| Keys 排序 | SQL ORDER BY | sort.Strings |
| 默认用途 | 生产 | 测试/临时 |

---

*最后更新: 2026-07-22*
