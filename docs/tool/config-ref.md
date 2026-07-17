# 配置参考

> 文档路径: docs/tool/config-ref.md
> 上级: README.md 11

---

## 11. 配置参考

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
        working_dir: "/workspace"
        max_output_bytes: 65536
    http:
      enabled: true
      timeout: 30s
      options:
        max_redirects: 5
        allowed_domains: []
        blocked_domains: []
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
      options:
        redact_patterns: ["api_key", "password", "secret", "token"]
    config_set:
      enabled: true
      options:
        allowed_paths: []                          # 空=全部允许
        blocked_paths: ["runtime.security", "tools.builtin.config"]
        allow_persist: true
    config_reload:
      enabled: true
    config_scheme:
      enabled: true
    config_save:
      enabled: true
      options:
        allowed_paths: ["./config.yaml"]
        backup: true
    config_diff:
      enabled: true

    # 内视类工具（只读，默认启用）
    runtime_status:
      enabled: true
      options:
        full_detail_requires_admin: true           # detail=full 需管理权限
    agent_list:
      enabled: true
    agent_inspect:
      enabled: true
      options:
        context_requires_admin: true               # include_context 需管理权限
    session_list:
      enabled: true
    session_inspect:
      enabled: true
      options:
        tool_results_requires_admin: true           # include_tool_results 需管理权限
    tool_list:
      enabled: true
    skill_list:
      enabled: true
    provider_list:
      enabled: true
    mcp_list:
      enabled: true
    log_query:
      enabled: true
      options:
        redact_patterns: ["api_key", "password", "secret", "token"]
        max_results: 500
    metric_query:
      enabled: true

    # 管理类工具（默认禁用，需显式启用）
    skill_install:
      enabled: false
    skill_uninstall:
      enabled: false
    skill_enable:
      enabled: false
    skill_disable:
      enabled: false
    provider_health:
      enabled: true

  custom:
    - name: "my_tool"
      type: "plugin"
      plugin: "my_plugin.so"
      enabled: true
      timeout: 60s
      options:
        api_key: "${MY_API_KEY}"
```

### 11.2 Agent 级别覆盖

```yaml
agents:
  - id: "dev-agent"
    tools: ["shell", "http", "file_read", "file_write", "file_list"]
    tool_choice: "auto"
    tools_config:
      shell:
        timeout: 120s          # 覆盖全局 shell timeout
        options:
          working_dir: "/workspace/project"
```

---

