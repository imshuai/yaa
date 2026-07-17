# 插件可观测性

> 文档路径: `docs/plugin/observability.md`
> 上级: `docs/plugin/README.md`

---

本文档定义插件系统的可观测性方案，包括生命周期日志、健康检查、SSE 事件和运行时指标。基础日志与指标约定见 [architecture/observability.md](../architecture/observability.md)。

---

## 1. 生命周期日志

插件加载和卸载时应输出结构化日志，便于运维排查：

```go
// PluginManager 在加载/卸载时输出结构化日志。
func (m *Manager) Load(name string) error {
    logger.Info("plugin.loading",
        slog.String("plugin", name),
        slog.String("version", manifest.Version),
        slog.String("path", manifest.Path),
    )

    if err := m.loadPlugin(name); err != nil {
        logger.Error("plugin.load_failed",
            slog.String("plugin", name),
            slog.String("error", err.Error()),
        )
        return err
    }

    logger.Info("plugin.loaded",
        slog.String("plugin", name),
        slog.String("version", manifest.Version),
        slog.Duration("load_time", elapsed),
    )
    return nil
}

func (m *Manager) Unload(name string) error {
    logger.Info("plugin.unloading",
        slog.String("plugin", name),
    )

    if err := m.unloadPlugin(name); err != nil {
        logger.Error("plugin.unload_failed",
            slog.String("plugin", name),
            slog.String("error", err.Error()),
        )
        return err
    }

    logger.Info("plugin.unloaded",
        slog.String("plugin", name),
        slog.Duration("unload_time", elapsed),
    )
    return nil
}
```

**日志级别约定：**

| 事件 | 级别 | 说明 |
|------|------|------|
| `plugin.loading` | INFO | 开始加载插件 |
| `plugin.loaded` | INFO | 插件加载成功 |
| `plugin.load_failed` | ERROR | 插件加载失败 |
| `plugin.unloading` | INFO | 开始卸载插件 |
| `plugin.unloaded` | INFO | 插件卸载成功 |
| `plugin.unload_failed` | ERROR | 插件卸载失败 |
| `plugin.health_check` | DEBUG | 定期健康检查结果 |

---

## 2. 健康检查

Plugin Manager 提供健康检查接口，供 Runtime 管理面板或外部探针调用：

```go
// PluginHealth 单个插件的健康状态。
type PluginHealth struct {
    Name        string    // 插件名称
    Version     string    // 版本号
    Status      string    // loaded / unloaded / error
    Uptime      time.Duration // 已加载时长
    LastChecked time.Time // 最后检查时间
    Warnings    []string  // 告警信息
}

// PluginHealthReport 全量插件健康报告。
type PluginHealthReport struct {
    Total    int
    Loaded   int
    Unloaded int
    Error    int
    Details  []PluginHealth
}

// HealthCheck 执行所有插件的健康检查。
func (m *Manager) HealthCheck() PluginHealthReport {
    report := PluginHealthReport{
        Total:   len(m.plugins),
        Details: make([]PluginHealth, 0),
    }

    for name, p := range m.plugins {
        h := PluginHealth{
            Name:        name,
            Version:     p.Manifest.Version,
            Status:      p.Status,
            LastChecked: time.Now(),
        }
        if p.Status == "loaded" {
            h.Uptime = time.Since(p.LoadedAt)
            report.Loaded++
            // 检查插件进程/句柄是否存活
            if !p.IsAlive() {
                h.Status = "error"
                h.Warnings = append(h.Warnings, "plugin process not responding")
                report.Error++
                report.Loaded--
            }
        } else if p.Status == "error" {
            report.Error++
        } else {
            report.Unloaded++
        }
        report.Details = append(report.Details, h)
    }
    return report
}
```

---

## 3. SSE 事件

插件生命周期事件通过 SSE 通道推送给已订阅的客户端：

```go
// PluginEvent 表示一个插件 SSE 事件。
type PluginEvent struct {
    Type      string            `json:"type"`       // 事件类型
    Plugin    string            `json:"plugin"`     // 插件名称
    Version   string            `json:"version"`    // 插件版本
    Timestamp time.Time         `json:"timestamp"`  // 发生时间
    Detail    string            `json:"detail"`     // 附加信息
}
```

**SSE 事件表：**

| 事件类型 | 触发时机 | payload 关键字段 |
|----------|----------|------------------|
| `plugin.loaded` | 插件加载成功 | `plugin`, `version`, `timestamp` |
| `plugin.unloaded` | 插件卸载完成 | `plugin`, `timestamp` |
| `plugin.error` | 插件运行或加载出错 | `plugin`, `detail`(错误信息), `timestamp` |
| `plugin.health_changed` | 健康状态变更 | `plugin`, `detail`(旧→新状态), `timestamp` |

**订阅示例：**

```bash
# 通过 Remote API 订阅插件事件
curl -N http://localhost:8080/api/v1/events/stream?types=plugin.loaded,plugin.error

# 输出示例
data: {"type":"plugin.loaded","plugin":"notifier","version":"1.0.0","timestamp":"2026-07-17T09:30:00Z"}

data: {"type":"plugin.error","plugin":"notifier","version":"1.0.0","detail":"panic recovered","timestamp":"2026-07-17T09:35:12Z"}
```

---

## 4. 插件指标

Plugin Manager 通过内置指标接口暴露 Prometheus 格式指标：

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    pluginLoadTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "yaa_plugin_load_total",
            Help: "Total number of plugin load attempts.",
        },
        []string{"plugin", "result"}, // result: success / failure
    )

    pluginLoadDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "yaa_plugin_load_duration_seconds",
            Help:    "Plugin load duration in seconds.",
            Buckets: prometheus.DefBuckets,
        },
        []string{"plugin"},
    )

    pluginActive = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "yaa_plugin_active",
            Help: "Number of currently loaded plugins.",
        },
        []string{}, // 全局指标，无需 label
    )

    pluginErrors = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "yaa_plugin_errors_total",
            Help: "Total number of plugin runtime errors.",
        },
        []string{"plugin", "kind"}, // kind: panic / timeout / rpc
    )
)
```

**指标汇总：**

| 指标名称 | 类型 | Labels | 说明 |
|----------|------|--------|------|
| `yaa_plugin_load_total` | Counter | `plugin`, `result` | 插件加载尝试次数 |
| `yaa_plugin_load_duration_seconds` | Histogram | `plugin` | 插件加载耗时分布 |
| `yaa_plugin_active` | Gauge | — | 当前已加载插件数 |
| `yaa_plugin_errors_total` | Counter | `plugin`, `kind` | 插件运行时错误次数 |

**查询示例（PromQL）：**

```promql
# 加载失败率
sum(rate(yaa_plugin_load_total{result="failure"}[5m]))
  / sum(rate(yaa_plugin_load_total[5m]))

# 错误最多的插件
topk(5, sum by (plugin) (rate(yaa_plugin_errors_total[10m])))

# 当前活跃插件数
yaa_plugin_active
```
