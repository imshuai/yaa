# 实现检查清单

> 文档路径: docs/tool/checklist.md
> 上级: README.md 14

---

## 14. 实现检查清单

### 14.1 Tool Manager

- [ ] Tool 注册 / 注销 / 查找
- [ ] 权限检查（白名单 / 黑名单）
- [ ] 参数 JSON Schema 校验
- [ ] 超时控制（多层覆盖）
- [ ] 并发执行 + 并发上限
- [ ] 结果截断
- [ ] 重试逻辑（可重试错误判定 + 指数退避）
- [ ] 结构化日志
- [ ] ToToolDefs 转换
- [ ] ExecuteBatch 并发执行

### 14.2 内置 Tool

- [ ] Shell Tool（命令白/黑名单、超时、输出截断）
- [ ] HTTP Tool（域名白/黑名单、重定向、响应截断）
- [ ] File Read Tool（路径校验、大小限制）
- [ ] File Write Tool（路径校验、创建目录）
- [ ] File List Tool（路径校验、递归选项）
- [ ] File Delete Tool（路径校验、安全确认）
- [ ] Config Query Tool（路径查询、敏感字段脱敏）
- [ ] Config Set Tool（路径白/黑名单、类型校验、persist 开关、OnChange 回调）
- [ ] Config Reload Tool（merge/overwrite 模式、变更摘要）
- [ ] Config Scheme Tool（全量/路径查询、类型/默认值/取值范围）
- [ ] Config Save Tool（脱敏写入、自动备份、include_defaults 开关）
- [ ] Config Diff Tool（运行时 vs 磁盘对比、变更/新增/删除分类）
- [ ] Runtime Status Tool（版本/uptime/内存/goroutine/统计）
- [ ] Agent List Tool（状态过滤、摘要信息）
- [ ] Agent Inspect Tool（详细信息、Session/Context/Tool/Skill 绑定）
- [ ] Session List Tool（Agent 过滤、状态过滤、Token 统计）
- [ ] Session Inspect Tool（消息历史、上下文统计、Tool 结果可选）
- [ ] Tool List Tool（source 过滤、enabled 过滤）
- [ ] Skill List Tool（状态过滤、Tool/Agent 绑定信息）
- [ ] Provider List Tool（模型列表、健康状态、速率限制）
- [ ] MCP List Tool（连接状态、Tool 列表、ping 时间）
- [ ] Log Query Tool（级别/时间/关键词/组件过滤、脱敏）
- [ ] Metric Query Tool（指标名/时间范围/步长/标签过滤）
- [ ] Skill Install Tool（来源解析、下载安装、自动绑定）
- [ ] Skill Uninstall Tool（解绑注销、force/keep_files 选项）
- [ ] Skill Enable Tool（注册 Tool、绑定 Agent）
- [ ] Skill Disable Tool（解绑注销、保留文件）
- [ ] Provider Health Tool（探测请求、延迟记录、状态更新）

### 14.3 自定义 Tool

- [ ] 插件接口定义（`ToolPlugin`）
- [ ] .so 插件加载
- [ ] 配置文件声明注册
- [ ] 编程注册支持

### 14.4 Context 集成

- [ ] Tool 结果 → `role="tool"` Message 转换
- [ ] 原子单元（assistant+tool）截断保护
- [ ] 深度思考模式下 reasoning_content 保留

### 14.5 可观测性

- [ ] 执行日志（tool/agent/session/duration/result_tokens）
- [ ] Prometheus 指标
- [ ] Remote API 事件推送

---

*最后更新: 2025-07-17*
