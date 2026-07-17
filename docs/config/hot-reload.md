# 配置热更新

> Yaa! Yet Another Agent Runtime
> 依赖: [README.md](README.md) §1.2, [loading.md](loading.md), [validation.md](validation.md)

---

## 1. 概述

热更新（Hot Reload）允许 Yaa! Runtime 在不重启进程的前提下，感知配置文件变更并应用新配置。这保证了 Agent 会话不中断、连接不断开，是生产环境长稳运行的关键能力。

### 1.1 核心目标

| 目标 | 说明 |
|------|------|
| **零停机** | 配置变更后无需重启 Runtime 进程 |
| **原子替换** | 新配置整体校验通过后才替换旧配置，拒绝半成品状态 |
| **变更传播** | 仅通知受影响的模块，避免全量刷新 |
| **优雅降级** | 校验失败时保留旧配置，记录错误并发出告警 |
| **防抖** | 编辑器保存触发多次事件时只处理一次 |

---

## 2. 文件监听

### 2.1 fsnotify 集成

Yaa! 使用 [fsnotify](https://github.com/fsnotify/fsnotify) 监听配置文件变更：

```go
package config

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Watcher struct {
	fw       *fsnotify.Watcher
	path     string
	onChange func()
	debounce time.Duration
}

func NewWatcher(path string, onChange func()) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// 监听文件所在目录，兼容编辑器原子写入（删除+创建）
	dir := filepath.Dir(path)
	if err := fw.Add(dir); err != nil {
		fw.Close()
		return nil, err
	}

	return &Watcher{
		fw:       fw,
		path:     filepath.Clean(path),
		onChange: onChange,
		debounce: 300 * time.Millisecond,
	}, nil
}

func (w *Watcher) Run(ctx context.Context) {
	var timer *time.Timer

	for {
		select {
		case <-ctx.Done():
			w.fw.Close()
			return

		case event, ok := <-w.fw.Events:
			if !ok {
				return
			}
			// 仅关心目标文件的 Write/Create/Rename
			if filepath.Clean(event.Name) != w.path {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			// 防抖：重置定时器，合并短时间内的多次事件
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, w.onChange)

		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			// TODO: 记录日志，可考虑重连 watcher
			_ = err
		}
	}
}
```

### 2.2 防抖机制

编辑器（如 vim、vscode）保存文件时可能触发多次 Write 事件或触发 Rename + Create 组合。通过 300ms 防抖定时器合并事件，确保只触发一次重载。

---

## 3. 变更传播

### 3.1 订阅模式

模块通过 `Subscribe()` 注册回调，配置变更时仅通知感兴趣的模块：

```go
type ReloadManager struct {
	cfgPath string
	loader  *Loader
	current *Config
	mu      sync.RWMutex
	subs    []Subscriber
}

type Subscriber interface {
	// OnConfigReload 接收新旧配置，返回 error 表示该模块应用失败
	OnConfigReload(oldCfg, newCfg *Config) error
	// Name 返回模块名，用于日志和错误追踪
	Name() string
}

func (m *ReloadManager) Subscribe(s Subscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs = append(m.subs, s)
}

func (m *ReloadManager) reload() error {
	// 1. 加载新配置
	newCfg, err := m.loader.Load(m.cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. 校验新配置
	if err := Validate(newCfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	// 3. 原子替换 + 逐模块通知
	m.mu.Lock()
	oldCfg := m.current
	m.mu.Unlock()

	// 4. 逐个通知订阅者，任一失败则回滚
	applied := make([]Subscriber, 0, len(m.subs))
	for _, s := range m.subs {
		if err := s.OnConfigReload(oldCfg, newCfg); err != nil {
			// 回滚已应用的模块
			for _, done := range applied {
				_ = done.OnConfigReload(newCfg, oldCfg)
			}
			return fmt.Errorf("subscriber %s apply failed: %w", s.Name(), err)
		}
		applied = append(applied, s)
	}

	// 5. 所有模块成功后才替换全局引用
	m.mu.Lock()
	m.current = newCfg
	m.mu.Unlock()
	return nil
}
```

### 3.2 传播流程

```text
文件变更
  │
  ▼
Watcher (防抖 300ms)
  │
  ▼
ReloadManager.reload()
  ├─ 1. Load(path)        → 新 Config
  ├─ 2. Validate(newCfg)  → 校验失败则中止，保留旧配置
  ├─ 3. 逐模块 OnReload() → 任一失败则回滚
  └─ 4. 原子替换 m.current
```

---

## 4. 原子替换

### 4.1 原则

- **整体替换**：新配置作为不可变对象整体替换，不存在部分更新的中间态
- **读多写少**：运行时使用 `sync.RWMutex` 保护，读操作无锁竞争
- **回滚保护**：任一订阅模块应用失败，已应用的模块逐个回滚

### 4.2 配置访问

```go
func (m *ReloadManager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}
```

---

## 5. 热更新支持范围

### 5.1 配置项热更新分类

| 配置分组 | 配置项 | 热更新 | 说明 |
|----------|--------|:------:|------|
| **HTTP Server** | `http.addr` | ❌ 需重启 | 监听地址变更需重新绑定端口 |
| | `http.read_timeout` | ✅ | 下次请求生效 |
| | `http.write_timeout` | ✅ | 下次请求生效 |
| | `http.max_header_bytes` | ✅ | 下次请求生效 |
| **Logging** | `log.level` | ✅ | 立即生效 |
| | `log.format` | ✅ | 立即生效 |
| | `log.output` | ✅ | 下次写入生效 |
| **Provider** | `providers.*.api_key` | ✅ | 下次请求生效 |
| | `providers.*.model` | ✅ | 下次请求生效 |
| | `providers.*.base_url` | ✅ | 下次请求生效 |
| | `providers.*.timeout` | ✅ | 下次请求生效 |
| | `providers` 新增/删除 | ❌ 需重启 | 结构变更需重新初始化 |
| **Agent** | `agents.*.system_prompt` | ✅ | 新会话生效 |
| | `agents.*.max_tokens` | ✅ | 新会话生效 |
| | `agents.*.temperature` | ✅ | 新会话生效 |
| | `agents` 新增/删除 | ❌ 需重启 | 结构变更需重新初始化 |
| **Skill** | `skills.*.enabled` | ✅ | 立即生效 |
| | `skills.*.config` | ✅ | 下次调用生效 |
| **MCP** | `mcp.servers.*.command` | ❌ 需重启 | 子进程需重启 |
| | `mcp.servers.*.args` | ❌ 需重启 | 子进程需重启 |
| | `mcp.servers.*.env` | ❌ 需重启 | 子进程需重启 |
| **Memory** | `memory.backend` | ❌ 需重启 | 后端切换需重新初始化 |
| | `memory.max_size` | ✅ | 下次写入生效 |
| **Session** | `session.max_idle` | ✅ | 立即生效 |
| | `session.max_count` | ✅ | 立即生效 |

### 5.2 判断原则

| 判断维度 | 热更新 ✅ | 需重启 ❌ |
|----------|----------|-----------|
| **资源绑定** | 无外部资源依赖 | 绑定端口、文件句柄、子进程 |
| **结构变更** | 仅修改值 | 新增/删除数组元素或 map 键 |
| **连接状态** | 无状态或短生命周期请求 | 长连接、子进程、持久化后端 |
| **数据一致性** | 新配置从下次操作生效即可 | 需要迁移已有数据 |

---

## 6. 错误处理

| 场景 | 行为 |
|------|------|
| 配置文件语法错误 | 保留旧配置，日志记录解析错误，告警 |
| 校验失败 | 保留旧配置，日志记录校验错误，告警 |
| 模块应用失败 | 回滚已应用模块，保留旧配置，日志记录失败模块 |
| 文件被删除 | 保留旧配置，日志告警，等待文件恢复后自动恢复监听 |
| Watcher 系统错误 | 保留旧配置，日志告警，尝试重建 Watcher |

---

## 7. 最佳实践

1. **编辑器原子写入**：vim 等编辑器使用 "删除+创建" 方式保存，Watcher 应监听目录而非文件本身
2. **防抖时间**：300ms 适合大多数编辑器；SSD 上可降至 100ms
3. **灰度验证**：生产环境可在 `OnConfigReload` 中加入灰度逻辑，先对小比例 Agent 生效
4. **审计日志**：每次热更新记录变更前后的 diff，便于追溯
5. **配置备份**：热更新前自动备份当前配置，支持一键回滚

---

*最后更新: 2025-07-17*
