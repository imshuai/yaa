# 配置参考

> 文档路径: docs/tool/config-ref.md
> 上级: README.md 11

---

## 11. 配置参考

字段、类型和默认值由 [Config reference](../config/reference.md#6-tools-节点) 唯一维护；本文件给出组合示例和 Agent override 用法。

### 11.1 完整配置

```yaml
tools:
  default_timeout: 30s
  max_timeout: 300s
  max_concurrent: 5
  max_concurrent_per_session: 3
  default_max_retry: 1
  max_result_tokens: 4000

  builtin:
    shell:
      enabled: true
      timeout: 60s
      options:
        allowed_commands: []
        blocked_commands: ["rm -rf /", "mkfs", "dd if="]
        working_dir: "."
        env: {}
        max_output_bytes: 65536
    http:
      enabled: true
      timeout: 30s
      options:
        max_redirects: 5
        allowed_hosts: []
        blocked_hosts: []
        max_response_bytes: 1048576
    file:
      enabled: true
      timeout: 10s
      options:
        allowed_paths: ["/tmp", "/workspace"]
        blocked_paths: ["/etc", "/root/.ssh", "/proc"]
        max_file_size: "10MB"

    # 配置管理类工具
    config_query:
      enabled: true
      options: {}
    config_reload:
      enabled: true

    # 内视类工具（只读，默认启用）
    runtime_status:
      enabled: true
    agent_list:
      enabled: true
    agent_inspect:
      enabled: true
    session_list:
      enabled: true
    session_inspect:
      enabled: true
    tool_list:
      enabled: true
    skill_list:
      enabled: true
    provider_list:
      enabled: true
    mcp_list:
      enabled: true

  # 第三方二进制 Tool 通过根级 plugins.entries 配置，
  # Plugin Manager 握手后自动注册 Tool Proxy。
```

### 11.2 Agent 级别覆盖

```yaml
agents:
  - id: "dev-agent"
    tools: ["shell", "http", "file_read", "file_write", "file_list"]
    tools_config:
      shell:
        timeout: 120s          # 覆盖全局 shell timeout
        options:
          working_dir: "/workspace/project"
```

`tools_config` 只覆盖该 Agent 的 Tool `timeout` 与 `options`。Manager 将其解码为不含 `Enabled` 的内部 presence-aware override（`Timeout *time.Duration`、nil/非 nil `Options`）；不能复用 root `config.ToolConfig`，也不能让 Agent 重新启用 root disabled Tool。基础 Config Validator 拒绝未知 root builtin key，并检查 root Tool timeout 的 `0..max_timeout` 范围；root 和 Agent options 以及 Agent timeout 在启动 binding 阶段按 [Config reference](../config/reference.md#6-tools-节点) 严格解码，Agent timeout 必须位于 `0..max_timeout`，错误固定为 `agents[i].tools_config.<name>.timeout / range / must be in 0..max_timeout`，未知 key 必须报告完整配置路径。`ToolChoice` 和 `Thinking` 是每次组装 `provider.ChatRequest` 时设置的请求级字段，不是 `AgentConfig` 字段。

---
