# 自定义 Tool

> 文档路径: docs/tool/custom.md
> 上级: README.md 7

---

## 7. 自定义 Tool

### 7.1 实现自定义 Tool

```go
// MyTool 是一个自定义 Tool 示例。
type MyTool struct{}

func (t *MyTool) Name() string { return "my_tool" }

func (t *MyTool) Description() string {
    return "Does something useful with the provided input."
}

func (t *MyTool) Parameters() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "input": {
                "type": "string",
                "description": "The input to process"
            }
        },
        "required": ["input"]
    }`)
}

func (t *MyTool) Execute(ctx context.Context, scope ExecutionScope, params map[string]any) (ToolResult, error) {
    input, _ := params["input"].(string)

    // 模拟处理
    result := processInput(input)

    return ToolResult{
        Content: result,
        Meta: map[string]any{
            "input_length": len(input),
        },
    }, nil
}
```

### 7.2 注册方式

#### 方式一：Plugin RPC（第三方二进制能力）

```yaml
plugins:
  entries:
    - id: "my-tool-plugin"
      enabled: true
      config:
        api_key: "${MY_API_KEY}"
```

Plugin Manager 根据 Manifest 的 Tool capability 创建 `Tool` Proxy 并注册到 Tool Manager，Runtime 不加载共享库。

#### 方式二：编程注册（仅内置 Tool）

```go
func registerBuiltins(toolManager *Manager) error {
    return toolManager.Register(&MyTool{}, config.ToolConfig{
        Enabled: true,
        Timeout: 60 * time.Second,
    }, "builtin")
}
```

#### 方式三：MCP 桥接

通过 MCP 协议将外部工具注册为 Yaa! Tool（详见 [MCP 系统设计](../mcp/README.md)）。

### 7.3 Plugin Tool Proxy

`PluginToolProxy` 位于 `internal/plugin` 并实现本包的 `Tool` interface；本包不导入 Plugin 模块，避免循环依赖。唯一结构和 scope/wire 转换见 [Plugin Tool 集成](../plugin/integration.md#2-tool-集成)。Tool Manager 只通过 `Register(tool, cfg, "plugin")` 接收该 Proxy。

---
