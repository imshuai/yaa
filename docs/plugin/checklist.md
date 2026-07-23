# Plugin 实现检查清单

> 文档路径: `docs/plugin/checklist.md`
> 上级: [README.md](README.md)

---

## 协议与 Manifest

- [ ] 文档阶段只以 [interface.md](interface.md) 内嵌 proto 为权威；实现时原样落到 `api/plugin/v1/plugin.proto` 并切换唯一 wire authority
- [ ] 生成 `pkg/pluginrpc/gen`，生成文件不手工修改；CI 校验 proto、生成物和保留镜像一致
- [ ] RPC major v1 只接受 `protocol_version: "1"`
- [ ] Manifest 完整字段和严格未知字段校验
- [ ] `provides[]` 只接受 `tool`，且 name/description/schema 必填
- [ ] Manifest capabilities 与 Ready capabilities 的 type/name/description/schema 集合一致
- [ ] `entries[].config` 在启动进程前通过 `config_schema`
- [ ] `requires_runtime`、dependency version 使用 SemVer parser

## Loader

- [ ] `plugins.paths` 相对主配置目录解析并去重
- [ ] `NewLoader(configDir, paths, logger)` 校验依赖并固定 RPC major；`NewManager` 消费其 typed discovery diagnostics
- [ ] 每个直接子目录只读取一个 `plugin.yaml`
- [ ] entry 规范化后不得逃逸 Manifest 目录，并验证可执行权限
- [ ] Unix Socket / Windows loopback TCP endpoint 与启动 nonce
- [ ] 长期进程使用 `exec.Command`；startup context 取消不会杀死 Ready 进程
- [ ] nonce 仅经 `YAA_PLUGIN_STARTUP_NONCE` 传入并由 HandshakeResponse constant-time 校验
- [ ] Handshake request 使用 `runtime_protocol`/`expected_plugin_id`；response 的 `protocol_version`/`plugin_id`/`plugin_version`/`startup_nonce` 全部与冻结 Descriptor 和启动 nonce 一致
- [ ] Start 依次执行 exec/Dial/Handshake/Init/Ready
- [ ] 每个 `cmd.Start` 成功路径恰有一个 `cmd.Wait` owner；失败统一 Terminate + endpoint cleanup
- [ ] 无法解析 ID 的错误只进 diagnostics；已知 ID 的错误建立 error Entry
- [ ] 重复 ID 全部拒绝，不按路径顺序覆盖
- [ ] `RPCClient` 私有持有 transport/process/endpoint；外部只用 Health/Stop/InvokeTool 转发和幂等生命周期方法

## Manager

- [ ] 状态唯一为 `discovered|starting|ready|error|stopped`
- [ ] 显式 `entries[].enabled` 优先于 Manifest `default_enabled`
- [ ] `auto_start=false` 时只发现/校验，不启动
- [ ] 缺失/循环/版本不匹配依赖在启动进程前检测
- [ ] 非 optional dependency 未 Ready 时下游不启动
- [ ] Proxy 注册事务化，失败回滚；全部成功后才标记 Ready
- [ ] 单 Plugin 失败进入 non-fatal StartupReport
- [ ] 初始启动只尝试一次，`restart.*` 只处理 ready 后 unexpected exit
- [ ] unexpected exit 使 Proxy 返回 unavailable，并有限退避重启
- [ ] 重启成功原子替换 Proxy client；请求不自动 replay
- [ ] Entry 只以冻结的 `Descriptor.Manifest` 为 Manifest 来源，不保存可漂移副本
- [ ] Stop 关闭 lifecycle gate 并取消退避/启动；发布新 client 前在锁内复查 stopping
- [ ] `mu` 覆盖 Entry 的 Client/Handle/ProxyNames/State/Health/StartedAt/LastError；RPC/Wait/退避在锁外
- [ ] Health 使用 `health_timeout`，在 `mu` 下更新 snapshot，失败只标 degraded
- [ ] Runtime Stop 逆序 unavailable/Stop/Wait/Kill+Wait/注销 Proxy/清理 endpoint，继续处理全部 Plugin并聚合错误
- [ ] `StopAll(ctx)` 超时后 teardown 继续；Runtime 在关闭 Tool Manager/退出前等待 `Done()` 并读取 `WaitStopped()`

## 配置与边界

- [ ] `startup_timeout` 覆盖 exec 到 Ready
- [ ] `stop_timeout` 覆盖 Stop 到 Wait
- [ ] `health_interval` / `health_timeout` 生效
- [ ] `restart.enabled/max_attempts/backoff` 只用于运行中 unexpected exit
- [ ] 所有 `plugins.*` 变更返回 restart_required，不热加载
- [ ] v1 不实现远程 endpoint、动态库、下载/安装或签名信任库
- [ ] Plugin 不接收 Runtime 指针、Manager、数据库连接或 internal Go object

## 集成与验证

- [ ] Tool Proxy 保留 AgentID/SessionID scope、request ID、deadline、取消和错误码
- [ ] 错 ID、非法 outcome、`UNSPECIFIED`/未知 enum 原子 invalidate 当前 handle，并由 `RPCClient.Terminate()` 回收
- [ ] Agent/RBAC/配额在跨进程前执行，Plugin 不能绕过
- [ ] Secret 不进入日志、错误、Health、指标或 API
- [ ] 指标名称与 [observability.md](observability.md) 唯一表一致
- [ ] 当前不宣称 Plugin Remote API、SSE endpoint 或未登记 Tool
- [ ] 单元测试覆盖 Manifest/config schema/依赖图/能力冲突
- [ ] 集成测试覆盖启动失败、unexpected exit/restart、Stop timeout 和 Windows loopback

---

*最后更新: 2025-07-17*
