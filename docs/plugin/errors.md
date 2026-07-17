# Plugin 错误处理与可观测性

> 文档路径: `docs/plugin/errors.md`
> 上级: `docs/plugin/README.md`

---

## 错误分类

| 错误类型 | 说明 | 处理方式 |
|---------|------|---------|
| `ErrPluginNotFound` | Plugin 不存在 | 返回给调用方，提示检查名称或路径 |
| `ErrPluginAlreadyExists` | 安装时同名 Plugin 已存在 | 返回给调用方，提示 Force 选项 |
| `ErrPluginDisabled` | Plugin 已禁用 | 返回给调用方，跳过加载 |
| `ErrPluginPermissionDenied` | Agent 无权使用该 Plugin | 返回给 LLM |
| `ErrPluginDependencyMissing` | 依赖的 Plugin 或 Tool 不存在 | 标记 Plugin 为 Error 状态 |
| `ErrPluginCircularDependency` | 循环依赖 | 加载时报错并记录依赖链 |
| `ErrPluginLoadFailed` | Plugin 加载/解析失败 | 记录错误日志，跳过该 Plugin |
| `ErrPluginVersionIncompatible` | Plugin 要求的 Runtime 版本不兼容 | 拒绝加载，返回版本约束信息 |
| `ErrPluginExecutionPanic` | Plugin 执行时 panic | 捕获并恢复，记录堆栈，返回错误信息 |
| `ErrPluginExecutionTimeout` | Plugin 执行超时 | 取消执行，返回超时信息 |
| `ErrPluginInstallFailed` | 安装失败（下载/解压/校验） | 回滚，返回错误详情 |
| `ErrPluginHookFailed` | Hook 回调执行失败 | 记录日志，根据策略决定是否中断流程 |

---

## 错误传递

```text
Plugin 错误 → Plugin Manager → Agent / Runtime
                                │
                                ├─ 可恢复错误 → 降级运行或重试
                                └─ 不可恢复错误 → 停止 Plugin，通知调用方
```

**Plugin Manager 层错误处理：**

```go
func (m *Manager) loadPlugin(path string) error {
    // 1. 解析 plugin.yaml
    meta, err := m.parseMetadata(path)
    if err != nil {
        return fmt.Errorf("parse plugin metadata: %w", err)
    }

    // 2. 版本兼容性检查
    if !m.runtimeVersion.Satisfies(meta.RequiresRuntime) {
        return ErrPluginVersionIncompatible
    }

    // 3. 依赖检查
    if err := m.checkDependencies(meta); err != nil {
        return fmt.Errorf("dependency check failed: %w", err)
    }

    // 4. 加载 Plugin 入口
    plugin, err := m.loader.Load(path, meta)
    if err != nil {
        return fmt.Errorf("load plugin '%s': %w", meta.Name, err)
    }

    // 5. 注册 Hooks
    if err := m.registerHooks(meta.Name, plugin); err != nil {
        return fmt.Errorf("register hooks for '%s': %w", meta.Name, err)
    }

    m.logger.Info("plugin loaded",
        "name", meta.Name,
        "version", meta.Version,
        "hooks", meta.Hooks,
    )

    return nil
}
```

---

## Panic 恢复

Plugin 在独立函数中执行，Runtime 通过 `recover()` 捕获 panic，避免单个 Plugin 崩溃影响整体服务。

```go
func (m *Manager) executeHook(name string, hook HookFunc, ctx context.Context) (err error) {
    defer func() {
        if r := recover(); r != nil {
            err = fmt.Errorf("%w: %v", ErrPluginExecutionPanic, r)
            m.logger.Error("plugin hook panic recovered",
                "plugin", name,
                "panic", r,
                "stack", debug.Stack(),
            )
            m.metrics.PanicCounter.WithLabelValues(name).Inc()
        }
    }()

    return hook(ctx)
}
```

**策略：**

| 场景 | 行为 |
|------|------|
| Hook panic | 捕获 → 记录堆栈 → 返回错误 → 根据策略跳过或中止 |
| 后台任务 panic | 捕获 → 记录 → 标记 Plugin 为 Error 状态 |
| 连续 panic ≥ 3 次 | 自动禁用 Plugin，发送告警事件 |

---

## 版本不兼容处理

Plugin 通过 `requires_runtime` 字段声明最低 Runtime 版本：

```yaml
# plugin.yaml
name: my-plugin
version: 1.2.0
requires_runtime: ">=0.3.0"
```

```go
func (m *Manager) checkVersion(meta *Metadata) error {
    if meta.RequiresRuntime == "" {
        return nil // 无约束，允许加载
    }
    constraint, err := semver.NewConstraint(meta.RequiresRuntime)
    if err != nil {
        return fmt.Errorf("invalid version constraint '%s': %w", meta.RequiresRuntime, err)
    }
    if !constraint.Check(m.runtimeVersion) {
        return fmt.Errorf("%w: plugin '%s' requires runtime %s, current is %s",
            ErrPluginVersionIncompatible,
            meta.Name, meta.RequiresRuntime, m.runtimeVersion)
    }
    return nil
}
```

---

## 重试策略

| 操作 | 重试条件 | 重试次数 |
|------|---------|---------|
| 加载 Plugin | 文件解析失败 | 不重试 |
| 安装 Plugin | 下载失败 | 3 次（指数退避） |
| Hook 执行 | panic 恢复后 | 不重试，记录后跳过 |
| 依赖就绪检查 | 依赖未就绪 | 5 次（1s 间隔） |
| 后台任务 | 临时性错误 | 3 次（指数退避） |

---

## 可观测性

### 日志

```go
m.logger.Info("plugin loaded", "name", name, "version", ver, "hooks", hooks)
m.logger.Warn("plugin load failed", "path", path, "error", err)
m.logger.Error("plugin panic recovered", "plugin", name, "stack", stack)
m.logger.Info("plugin disabled", "name", name, "reason", "panic threshold exceeded")
```

### 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `plugin_total` | Gauge | source, status | 已注册 Plugin 总数 |
| `plugin_hook_total` | Counter | plugin, hook | Hook 触发次数 |
| `plugin_hook_failed_total` | Counter | plugin, hook | Hook 失败次数 |
| `plugin_panic_total` | Counter | plugin | Panic 恢复次数 |
| `plugin_load_duration` | Histogram | - | Plugin 加载耗时 |

### Remote API 事件

| 事件 | 触发时机 | Payload |
|------|---------|---------|
| `plugin.loaded` | Plugin 加载完成 | name, version, source |
| `plugin.installed` | Plugin 安装完成 | name, version, source |
| `plugin.uninstalled` | Plugin 卸载完成 | name |
| `plugin.disabled` | Plugin 被禁用 | name, reason |
| `plugin.error` | Plugin 发生错误 | name, error, type |
