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

func (t *MyTool) Execute(ctx context.Context, params map[string]any) (ToolResult, error) {
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

#### 方式一：配置文件声明（推荐）

```yaml
tools:
  custom:
    - name: "my_tool"
      type: "plugin"
      plugin: "my_plugin.so"    # Go 插件文件
      enabled: true
      timeout: 60s
      options:
        api_key: "${MY_API_KEY}"
```

#### 方式二：编程注册

```go
// 在插件 init() 中注册
func init() {
    toolManager.Register(&MyTool{}, ToolConfig{
        Enabled: true,
        Timeout: 60 * time.Second,
    })
}
```

#### 方式三：MCP 桥接

通过 MCP 协议将外部工具注册为 Yaa! Tool（详见 `mcp.md`）。

### 7.3 插件接口

```go
// ToolPlugin 是 Tool 插件的入口接口。
// 插件 .so 文件必须导出一个实现此接口的变量 `ToolPluginInstance`。
type ToolPlugin interface {
    // Init 初始化插件，读取配置。
    Init(config map[string]any) error

    // Tools 返回插件提供的所有 Tool 实例。
    Tools() []Tool
}

// 导出符号
var ToolPluginInstance ToolPlugin = &MyPlugin{}
```

---

