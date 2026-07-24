# 实施进度

## 当前阶段

Phase 1：核心骨架。

## 已完成

- Phase 0 文档基线已完成。
- 配置迁移检查清单已与权威迁移契约对齐。
- Go 1.20 模块、最小入口、Makefile 和 CI 已建立。
- 配置版本解析已实现并覆盖严格格式、比较和错误边界。
- 显式配置迁移图已实现，拒绝降级、重复起点和缺失路径。
- 配置环境变量已支持单次展开、默认值、递归 Map/Slice 和路径错误。
- 配置格式检测与 YAML/JSON/TOML raw Map 解析已实现。
- 完整配置 DTO 与序列化标签已按各模块权威文档实现。
- 已明确 `tools.builtin` 的 14 个 v1 配置键及 `file` 共享配置组语义。
- 已补充 File Tool `timeout: 0` 继承全局超时的 canonical 说明。
- 根配置及各子系统 canonical 默认值已实现，所有容器按调用独立初始化。
- 配置文件中的数组和动态 Map 元素默认值已实现，并统一规范化 TOML 的对象数组。
- Presence-aware typed decode 已实现：保留缺失字段、整体替换切片、按 key 合并 Map，并严格处理 null、duration、标量转换、未知字段和完整错误路径。
- Session、Memory、Context 与 Planner 的 presence-aware policy resolver 已实现，显式 `false`/`0` 可覆盖上层值。
- Validator 契约已补齐所有静态 helper、稳定错误、NaN、inactive descriptor、Skill option 编码及静态/binding 两阶段边界。
- 基础配置 Validator 已实现：聚合结构化错误、校验根与 Agent effective policy，并保持配置只读。

- 配置文件路径发现已实现：按 `--config` → `YAA_CONFIG_PATH` → 当前目录/`~/.yaa`/`/etc/yaa` 顺序、目录内 `.yaml→.yml→.toml→.json`，只接受普通文件、返回词法绝对路径，权限/I/O 错误保留 cause，全部未命中返回空串；显式或环境路径无效返回 `ErrConfigFileNotFound`。
- 统一配置加载器 `config.Load` 已实现：`Default()` → 解析 → `migrateRaw`（版本检测+迁移）→ 环境变量展开 → `ApplyElementDefaults` → `DecodeInto` → `applyFlags` → `Validate`，默认路径全未命中时用纯默认值启动并输出 warning。
- 命令行参数覆盖 `applyFlags`/`setByPath` 已实现：按点路径反射设标量叶字段，支持 string/bool/int/uint/float/duration，拒绝数组/动态 Map/非标量/未注册路径。
- `golang.org/x/exp/slog` 已按架构契约引入为固定版本（兼容 Go 1.20）。
- `loader_test.go` 覆盖路径发现优先级、显式路径不存在、目录非普通文件、默认配置启动、完整管线与环境变量展开、flag 覆盖与非标量/未知字段拒绝。

- 统一 REST `Envelope`、错误 writer（`writeError`，HTTP 状态+两位子码）、成功 writer、request-id middleware 与生成器已实现：复用入站 `X-Request-ID` 否则生成 `req_` 前缀有序 ID，响应回写 `X-Request-ID` 与 envelope `request_id`。
- Remote API HTTP Server 骨架已实现：`NewServer`、`Start`/`Shutdown`、`recoverMiddleware`、方法校验与未匹配路由 40401。
- `/api/v1/health` 与 `/api/v1/version` 端点已实现：health 按 `HealthProvider` 返回 200/code0/data（ready=true，degraded 也 200）或 503/code50301/data=null；version 返回 ldflags 可注入的 version/git_commit/build_time/go_version。
- 已确认 `golang.org/x/exp/slog` 的 `Logger.Error(msg, err, args...)` 与标准 `log/slog` 签名不同，后续迁移需注意。
- `api_test.go` 覆盖 envelope、X-Request-ID 复用/生成、health ready/degraded/unready、version 四字段、405/404、未启动 Shutdown。

- 最小 Runtime 生命周期已实现：`runtime.New`（拒绝 nil config）→ `Start`（启动 API、标记 Ready、记录 components.api）→ `Ready`/`Health`/`UptimeSeconds` → `Shutdown`（先原子 Not Ready 再按逆序关 API，错误用 `errors.Join`）；启动失败按逆序 `rollback`。
- Runtime 实现 `api.HealthProvider`，health 接入 Ready 状态；`APIAddr()` 暴露实际监听地址。
- `cmd/yaa` 入口已接入：`--config` flag → `config.Load` → `runtime.New` → `signal.NotifyContext(Interrupt, SIGTERM)` 等 ctx → 按 `http.write_timeout` 限期 `Shutdown`。
- Makefile `build` 已通过 ldflags 注入 `api.Version/GitCommit/BuildTime`。
- Runtime 真实二进制冒烟通过：`/api/v1/health` 200/code0/data、`/api/v1/version` 含 ldflags 值、未匹配路由 404、kill 后正常退出。
- `runtime_test.go` 覆盖 nil config、Start/Ready/Health/components、Shutdown 清除 Ready、启动前 not ready、端到端真实 HTTP /health。

- 根 Storage 抽象已实现：`storage.Storage` 接口（Get/Set/Delete/Has/Keys/Close）、错误集（NotFound/Closed/InvalidKey/InvalidTTL/InvalidPath/ValueTooLarge）、MaxValueBytes 上限、共享 `validateKey`/`validateValue`/`expiresAt` 校验。
- Memory 后端已实现：注入 Clock、60s batch 1000 清理 worker、惰性隐藏过期值、Set/Get 拷贝 bytes、Close sync.Once 幂等并等 worker、Close 后返回 ErrClosed；`memory_test.go` 13 用例覆盖拷贝/Keys 排序/TTL 隐藏与负值/多参数拒绝/超大 value/幂等 Close/cleanup。
- SQLite 后端已实现（`modernc.org/sqlite` v1.28.0，纯 Go 无 CGO）：`root_kv`+`root_storage_schema_version` 表、WAL+busy_timeout、单连接、atomic upsert 保留 created_at、Get/Has/Keys 按 expiry 过滤、前缀 `substr` 字面匹配（`%`/`_`/Unicode 不被当通配）、cleanup batch 1000、migration 与目录创建失败阻止 Ready、Close 幂等；`sqlite_test.go` 覆盖 upsert/特殊字符前缀/TTL 过滤/cleanup/超大/空路径/迁移建表/工厂。
- `storage.New` 工厂按 `type` 分发 sqlite/memory，未知类型拒绝。
- Runtime 已接入 Storage：Start 顺序 Storage→API（失败逆序 rollback），Health `components.storage` 反映后端（SQLite=ready、memory=degraded 且 status=degraded、ready=true），Shutdown 先 Not Ready 再 API 再 Storage。
- 发现文档缺口并修复：`docs/storage/alternatives.md` §4 与 `checklist.md` 要求 health 报告 `durable=false`，但 `remote-api/system.md` 的 HealthData schema 无该字段；统一为通过 `components.storage` 状态表达，不增 schema 顶层字段，避免契约漂移。
- 真实二进制冒烟：SQLite 后端 health healthy+storage ready 且 `yaa.db`+WAL 落地；memory 后端 health degraded+durable=false 日志；两者优雅退出。

- 日志系统已实现 `internal/logging`：按 `config.LogConfig`（level/format/output）创建 slog.Logger，level 映射 debug/info/warn/error、format text/json、output stderr/stdout/文件路径（append），返回 closer 用于文件句柄关闭；`SetDefault` 同时设 `slog.Default`。
- `cmd/yaa` 已接入日志：`logging.SetDefault(cfg.Log)` 替换裸 `slog.Default`，进程退出前关闭日志句柄。
- `logging_test.go` 覆盖级别过滤（error 级抑制 info）、JSON 格式可解析、text 格式 attr、文件输出与 closer、非法 level 拒绝、debug 级打印、SetDefault；7 用例全过。
- 真实二进制冒烟：json+warn+文件输出 → 日志为可解析 JSON 且 INFO 行被级别过滤、stdout 为空（日志入文件）。
- 再次确认 `golang.org/x/exp/slog` 的 `Logger.Error(msg, err, args...)` 与标准库签名不同，所有 Error 调用已按 `Error(msg, err, args...)` 处理并留记。

- Provider 层已实现 `internal/provider`：`Provider` 接口与全部 DTO（ChatRequest/ChatResponse/ChatChunk/Delta/Message/ToolDef/ToolCall/ToolChoice/Usage/ModelInfo/ProviderInfo/ThinkingConfig/ResponseFormat），JSON tags 与 provider.md 一致。
- `ProviderError` 稳定分类与 `errors.Is`、HTTP 状态码→Code/Retryable 映射（401/403/429/5xx/400 子类 model_not_found|context_length|invalid_request/404）、Retry-After 头秒数解析、连接/context 错误分类为 connection|timeout。
- OpenAI adapter（纯 Go `net/http`）：Chat（POST /chat/completions 非流式）、StreamChat（SSE `data:` 行 + `[DONE]`，按 index 维护稳定非空 Tool call ID）、Models、EstimateInputTokens（char/4 估算）、Close、extra merge 到请求体顶层并剥离 `thinking`、Bearer 鉴权。
- `retryingProvider`：一次逻辑调用总 deadline（`ProviderConfig.timeout`）覆盖首次+全部重试；Chat 在完整响应前按 `min(retryInterval*2^n,30s)` 与 `RetryAfter` 较大值退避重试；StreamChat 缓冲到首可见 chunk 前才重试，首个可见 chunk 后不重试/不重放并转发后续（含 error chunk）；`ctx.Done()` 不重试。
- `Manager`：`NewManager` 用 factory registry（默认注册 openai）为每条配置构造 adapter 并 retrying 包装，重复 ID/未知类型拒绝，`Get`/`List`(按 ID 排序)/`Close`(`errors.Join`)，无运行时 Register/Unregister。
- Runtime 已接入 Provider：Start 顺序 Storage→Provider→API（失败逆序 rollback），Health `components.provider=ready`，Shutdown 逆序 API→Provider→Storage。
- provider 测试 19 用例绿：openai Chat 成功/Tool calls/错误分类/Retry-After/SSE 文本+finish/Tool call 稳定 ID/Models/估算/nil 请求/extra+thinking；manager+retry Chat 重试成功/不可重试/耗尽/排序 Close/重复 ID/未知类型/Stream 首可见前重试/首可见后不重试。
- 真实二进制冒烟：health 含 `components.api/provider/storage`，provider=ready。

## 下一步

- 接日志系统（slog handler/级别控制按 config.log）并接入 Runtime；随后开始 Phase2 Provider 层（OpenAI Chat/Stream/Models）。

每个可独立验收的功能完成后单独提交并推送到 `gitea/main`。

## Phase 2：Session 管理器（已完成本地代码）

- 依赖 `github.com/oklog/ulid/v2 v2.1.2`（兼容 go1.15）生成 `ses_<ULID>` / `msg_<ULID>`；Turn ID 由调用方传入。
- `internal/session` 包结构：
  - `errors.go`：稳定错误集（22 个 `ErrXxx`）+ `OpError{Op,SessionID,Err}` 带上下文包装。
  - `state.go`：`State` 四态 `created|active|paused|closed`、`transitions` 表、`canTransition`、`desiredState`（max_lifetime 优先于 TTL）、`stateAllowed`（生命矩阵）。
  - `session.go`：`Session`、`SessionMessage`、`CreateRequest`、`AppendInput`、`ListQuery`、`Clock`、`snapshotV1/Policy/Message` DTO、深拷贝。
  - `id.go`：`ulidGen` 用 `crypto/rand` + `MonotonicEntropy` 并发安全生成 ULID。
  - `validate.go`：单条 role 校验（user/assistant final/assistant tool call/tool/system）+ 批次序列校验（首条 user、无悬空 tool、Tool unit 完整、canonical name 格式、JSON arguments、ToolCall ID 唯一）+ messageBytesLimit。
  - `snapshot.go`：`encodeSnapshot`（used_turn_ids 排序、RFC3339Nano UTC、16MiB 硬校验）+ `decodeSnapshot`（DisallowUnknownFields、schema=1、ID/State/时间/Policy/Message/used_turn_ids 全校验）+ `validateResolvedPolicy`（max_messages/message_bytes>0、ttl/max_lifetime 0 或 >=1m、max_lifetime>=ttl）。
  - `manager.go`：`Manager` 含 `sessions/agentIdx/runners/activeTurns` map + `closing/closed` + `agentExists/agentOverride` 注入；`NewManager(opts)`、`Restore(ctx,now)`（Keys 排序、逐 key 解码、desiredState 覆写、原子发布索引）、`Start`（cleanup goroutine）、`Shutdown`（cancel turn + close stop + 等 runner 退出）、`cleanupLoop/cleanupOnce`、`runInSession`（锁内检查→解锁后 select send/stop/closing）、`commit`（persist 时 Set + Lock 替换 sessions[id]）。
  - `lifecycle.go`：`Create`（Lock 内容量检查+ID 预留+unlock 后 Storage.Set，失败 rollbackCreate）、`Get`、`List`（降序 created_at/ID、分页）、`Pause`/`Resume`（max_lifetime 检查→ErrSessionExpired）、`Close`（幂等）、`Delete`（runner task 内 Storage.Delete+索引摘除，task 外 stopRunner）。
  - `turn.go`：`Turn{manager,sessionID,turnID,closed,mu}`、`Snapshot`、`AppendUser`（首条 user、TurnID 写 used_turn_ids、created→active）、`Append`（batch 校验：单 final assistant 或 assistant+tool unit、一一对应、字节限制、max_messages）、`commitCandidate`（乐观深拷贝→校验→commit）。
  - `runturn.go`：`RunTurn`（activeTurns 预留 turnID→conflict 拒绝→onQueued(position)→runner task 内执行 callback；唯一 defer 清理 activeTurns+close(done)；queued cancel 跳过 callback；session Deleted 返回 NotFound；Return context.Cause(turnCtx)）、`CancelTurn`、`CancelAgentTurns`（收集+cancel+wait done）。
  - `messages.go`：`ListMessages`（role 过滤、after 增量、page/page_size，深拷贝）、`DeleteMessage`（Tool unit 原子删除、候选序列重校验）、`ClearMessages`（no-op 空历史）。
  - `sort.go`：sortMessagesByTimeID、deepCopyMessages、containsString。
- Runner 并发安全：用 `stop chan` 而非 `close(tasks)` 终止，`close(r.stop)` 在 `m.mu.Lock` 内执行；`runInSession` 和 `RunTurn` send 都 `select { r.tasks<-t | <-m.closing | <-r.stop }` 防 send-on-closed panic。
- Runtime 接入：Start 顺序 Storage→Provider→Session(Restore+Start)→API；Shutdown 逆序 API→Session→Provider→Storage；`components.session_restore=ready`。
- session 测试 10 用例全绿：validate/states + Create/Get/List/Capacity/Close/Delete + AppendUser+Append + RunTurnConflict + DeleteMessage + ClearMessages + Restore(跨实例) + 100 并发 RunTurn。

## 下一步

- Agent 生命周期与基本对话流程（非流式 POST /messages）。
- Remote API session 端点集成。
- 流式 SSE/WS。

## Remote API Session 端点（已完成）

- `internal/api/session_provider.go`：定义 `SessionProvider` 接口（注入 Session Manager）、`AgentExistsProvider`（注入 Agent 存在性 + override），以及各 REST DTO（`sessionDTO`/`sessionShortDTO`/`clearMessagesDTO`/`deleteSessionDTO`/`messageDTO`/`createSessionRequest`）。
- `internal/api/session_handler.go`：`registerSessionRoutes` 注册 `/api/v1/agents/:id/sessions` 和 `/api/v1/sessions/:id[/子资源]`；Go 1.20 ServeMux 手动路径解析。Handler 全覆盖：Create(201)、List(分页+state 过滤)、Get、Pause、Resume、Close、Delete、ClearMessages、ListMessages(role+after+分页)、DeleteMessage(Tool unit 原子)。`writeSessionError` 按 docs/session/errors.md §4 错误映射（SnapshotTooLarge→422 优先于 PersistenceFailed→503、capacity→429、state→409、not found→404、invalid→400）。`decodeBody` 禁止未知字段。
- `internal/api/server.go`：Server 加 `sessions`/`agentExists` 字段 + `SetSessionProvider` 注入方法；`register` 调 `registerSessionRoutes`。
- `internal/runtime/runtime.go`：Runtime 加 `agentExists`/`agentSessionOverride`（查 cfg.Agents）+ `agentAPIShim` 实现 `AgentExistsProvider`；Session Manager 创建时注入 Agent 信息；API Server 在 Start 后 `SetSessionProvider`。
- api 测试 6 用例绿：Create(201+DTO/agent_id/state/id前缀)、Create-AgentNotFound(404 40401)、List(分页)、Get+Pause+Resume+Close+Delete+AfterDelete404、ListMessages(2条顺序)、ClearMessages.
- 全项目 `go test ./...` + `go vet ./...` + `go build ./...` 全绿。
- Gitea push 仍 hang（服务端 receive-pack POST 无 ACK，连续 5+ 轮验证；已多方案尝试无果），本地 7 提交待推。

## Context 窗口管理器（已完成）

- `internal/context` 包实现 docs/context 契约：
  - `errors.go`：8 个稳定错误 sentinel。
  - `budget.go`：`ResolveContextBudget(cfg, modelWindow, modelMaxOutput, outputTokens)` 计算 EffectiveWindow/ReservedOutput/Input，窗口未知或 reserve/output 关系非法返回对应错误。
  - `manager.go`：`Manager` 无状态，`Build(ctx, BuildInput)` 唯一公开入口。流程：校验前置条件→计算预算→groupUnits 构造并校验消息单元→EstimateInputTokens 估算完整候选→不超限直接返回→超限时估算 protected-only→按策略（reject/truncate/hybrid）处理→最终估算确认不超限→返回 BuildOutput+Metadata。
  - `groupUnits`：开头 system 各自成 protected unit；按 user 分 turn；assistant+tool results 组成不可拆分 unit；CurrentTurnStart 所在 turn 及之后受保护；历史 Tool turn 不可压缩；orphan/重复/缺失 Tool result 返回 ErrInvalidMessageSequence。
  - `truncate`：从最旧可删除 unit 开始删除直到不超限，无可删 unit 返回 ErrContextOverflow。
  - `hybrid`：v1 暂降级为 truncate（摘要需 Provider 摘要调用，后续补全）。
  - `copyRequest`：深拷贝 Messages + ToolCalls。
- 7 个 context 测试全绿：under-budget 直接返回、reject 超限报错、truncate 删旧 unit、groupUnits 分组校验、invalid sequence（orphan/非 user 首条/不完整 Tool chain）、ResolveContextBudget 正常+窗口未知。
- Runtime 接入 `contextM *ctxwindow.Manager`（Phase 2 后续 Agent 调用）。
- 全项目 test/vet/build 全绿。
- 2 个本地 pending 提交（session 2c83ef0 + api 0646a52 + ctx 待提交）。

## Agent Manager（已完成 Phase 2 最小版）

- `internal/agent` 包 实 现 docs/agent.md 最小 直接 路 径（无 Tool/Memory/Skill/Planner）：
  - `types.go`：Status(running/paused/stopped)、7 个 稳定 错误、Info/Detail/TurnRequest/TurnEvent/ToolResultEvent/TurnResult。
  - `manager.go`：NewManager(校验 Config/Sessions/Context/Providers 非 nil + 每个 Agent ID/provider/model/max_tokens 有效)、Get/Inspect/List(按 ID 升序)、Start/Pause/Stop(状态机+幂等)、Quiesce、Shutdown、CancelTurn(委托 Session.CancelTurn)。
  - `handle_turn.go`：HandleTurn 校验入参 + Agent 状态(stopped/paused) → session.RunTurn 内执行 runDirectTurn：AppendUser → Session.Snapshot 组装 canonical ChatRequest(system prompt + 历史) → Context.Build → Provider.Chat → Append final assistant。v1 无 Tool loop/alias 投影（Phase 3 补全）。
- 4 个 agent 测试全绿：Get/List/NotFound、Life cycle Pause/Stop/Start 幂等+状态错误、HandleTurn direct 端到端(fake OpenAI httptest provider, Assert assistant=Mock answer)、HandleTurn invalid 入参与 NotFound。
- 全项目 test/vet/build 全绿。

## 对话 API 端点（已完成）

- `POST /api/v1/sessions/:id/messages`：提交 user 消息并等待 Agent turn 完成。
  - 入参 `postMessageRequest{turn_id, content, metadata}`，`turn_id` 校验 1..128 UTF-8 bytes 无控制字符。
  - 取 session.AgentID 后调 `agent.Manager.HandleTurn` → 在 Session FIFO 内 AppendUser → Context.Build → Provider.Chat → Append final assistant。
  - 成功返回 `postMessageResponse{turn_id, message(展开 Payload), usage, tool_call_count}`。
- `writeTurnError` 按 conversation.md cause 优先映射：
  - context.DeadlineExceeded → 504/50401；context.Canceled → 不写 response
  - ErrAgentStopped/Paused/InvalidState → 409/40901
  - ErrAgentManagerClosed → 503/50301
  - ErrAgentNotFound → 404/40401；ErrAgentInvalidRequest → 400/40001
  - ErrAgentToolRoundLimit/ProviderProtocol → 500/50001
  - Session 错误复用 session 错误映射
  - `*provider.ProviderError` (errors.As) → 502/50202
- `internal/api/agent_provider.go`：`AgentProvider` 接口（HandleTurn + Get）。
- `server.go`：Server 加 `agents` 字段 + `SetAgentProvider` 方法。
- `runtime.go`：Start 后 `SetAgentProvider(am)` 注入。
- 2 个 conversation 测试全绿：direct turn 端到端（POST → assistant="Hi there"），invalid turn_id 返回 400/40001。
- 全项目 test/vet/build 全绿。

## Phase 2：SSE 流式对话（已完成本地代码）

- `internal/session/hub.go`（新增）：Session 级事件总线 Hub。
  - `Subscribe()` 返回 `*Subscriber`（容量 256 的 buffered `chan any` + Done 信号）。
  - `Publish(ev)` 非阻塞 enqueue；队列满则原子注销该订阅者并关闭其通道（不丢帧、不阻塞发布者）。
  - `Close(reason)` 向所有订阅者投递终态事件后关闭通道，幂等；`Unsubscribe(s)` 单点注销。
  - Hub 存 `any` 事件以解耦 agent 与 session 互 import 循环。
- `internal/session/manager.go`：Manager 持有 `hubs map[string]*Hub`；`Hub(sessionID)` 返回/懒创建对应 Hub（不存在返回 `ErrSessionNotFound`，已关闭返回 `ErrManagerClosed`）；`Shutdown` 关闭所有 Hub（投递 nil reason），`Delete` 关闭对应 Hub。
- `internal/agent/types.go`：`TurnEvent` 扩容 `Assistant *session.SessionMessage`、`Usage *provider.Usage`、`ToolCallCount *int`、`Code/Message/Reason`，支持 `assistant_done / error / session_end` 终态字段。
- `internal/agent/handle_turn.go`：新增 `callProvider`/`callChat`/`callStream`：
  - `req.Stream==true` 时走 `Provider.StreamChat`，累积 delta；首 chunk 带 role 或首字节发 `assistant_start`，每个 reasoning/content chunk 发对应 `reasoning_delta`/`assistant_delta`，累积完整 assistant message 后返回给 Append。
  - `assistant_done` Emit 现在携带已持久化的 assistant SessionMessage 指针 + Usage 指针 + ToolCallCount 指针。
- `internal/api/conversation_frame.go`（新增）：`ConversationFrame` wire DTO（与 conversation.md 一致）+ `ToolResultView` + `SessionMessageView` + `SessionEventView`，`turnEventToFrame` 把 `agent.TurnEvent` 映射成 wire 帧；`sessionMessageView` 把 `*session.SessionMessage` 转 wire 视图。
- `internal/api/sse_handler.go`（新增）：`GET /api/v1/sessions/:id/events` SSE 订阅端点：
  - Header `text/event-stream` + `Cache-Control: no-store` + `X-Accel-Buffering: no`，`WriteHeader(200)` 后立即 `flusher.Flush()` 让 client 拿到响应头。
  - 每帧 `event: conversation\ndata: <json>\n\n`；每 15s `: heartbeat`。
  - Hub 关闭或 `r.Context().Done()` 退出；`defer hub.Unsubscribe(sub)`。
- `internal/api/conversation_handler.go`：`handlePostMessage` 注入 SSE 转发：
  - 当 `sessionMgr != nil` 时取该 Session 的 Hub，设 `turnReq.Stream=true` + `Emit` 把 `TurnEvent` 经 `turnEventToFrame` 转 ConversationFrame 后 `hub.Publish`；REST 同步仍等结果以 JSON 返回。
  - HandleTurn 失败时通过 `errorFrameFromTurnError` 向 Hub 发送一个 `error` ConversationFrame 终态（cause→code 十进制字符串 / cancel→"canceled"），与 REST `writeTurnError` 映射一致。
- `internal/api/server.go`：Server 新增 `sessionMgr *session.Manager` 字段 + `SetSessionManager` setter。
- `internal/runtime/runtime.go`：Start 注入 `rt.api.SetSessionManager(sm)`。
- `internal/api/session_handler.go`：路由分发加 `sub == "events" && Method==GET` → `handleSSEEvents`。
- 测试：
  - `internal/session/hub_test.go` 4 例：publish/subscribe、慢消费者 drop、Close 广播、Close 后 Publish no-op。
  - `internal/agent/manager_test.go` 增 `TestAgentHandleTurnStreamEmit`：SSE mock Provider，校验 emit 序列含 queued/assistant_start/assistant_delta/assistant_done，delta 累积 "Hello"，assistant_done 带 assistant+usage+tool_call_count。
  - `internal/api/conversation_handler_test.go` 增 `TestAPISSEEvents`：建立 SSE 订阅，手工 Publish 五帧，校验 wire `event:`/`data:` 透传 + `assistant_delta` 累积 "Hi there"；并扩展 `conversationTestEnv` mock server 按 `"stream":true` 返回 SSE 流，注入 `SetSessionManager` 让 POST 也走 stream 路径。
- 全项目 build/vet/test 全绿。

## Phase 2：WebSocket /stream 端点（已完成本地代码）

- 依赖 `github.com/gorilla/websocket v1.5.3`（兼容 Go 1.20）。
- `internal/api/ws_handler.go`（新增）：`GET /api/v1/sessions/:id/stream` WebSocket Upgrade。
  - 握手必须 Authorization Header（v1 只校验存在；具体 auth 校验后续 Auth 中间件接入）。
  - 前置校验 agents/sessionMgr 注入、Session 存在；错过则在 Upgrade 前返回 503/404。
  - 三个 goroutine：reader（解析 client 应用 frame）、writer（从 Hub 订阅流 ConversationFrame）、ping loop（ws transport ping，与 Session 无关）。
  - 双向通过共享 Session Hub：POST/WS 都可向 hub Publish，WS 单 writer 串行写 JSON，wmu 互斥。
  - reader：message frame → 校验 turn_id 重复 → 启动 `HandleTurn(Stream=true, Emit=hub.Publish)` 同步执行（hub 帧自动被 writer 推回此连接）；cancel frame → 调用本地 `turnCtx.cancel`。未知 cancel 回 40001/turn not active。
  - 连接断开：reader goroutine 退出 → `cancelAllTurns()` 取消所有本连接发起的非终态 turn，并 close conn、Unsubscribe hub；writer/ping loop 同步退出。
- `internal/session/hub.go`：新增 `SessionEndEvent{Reason string}`，作为 hub Close 时 publish 给订阅者的终态事件。
- `internal/session/lifecycle.go`：`Close` 成功 transition 后 `closeHubLocked(sessionID, &SessionEndEvent{Reason:"closed"})`；`Delete` 改 `&SessionEndEvent{Reason:"deleted"}`。
- `internal/api/conversation_frame.go`：新增 `sessionEndToFrame`，转出 `ConversationFrame{Type:"session_end", Reason}`。
- `internal/api/sse_handler.go` + `ws_handler.go`：事件 type switch 增加 `*session.SessionEndEvent` 分支；写完后 return 结束订阅（session_end 为订阅终止终态）。
- `internal/api/session_handler.go`：路由分发加 `sub == "stream" && Method==GET` → `handleWSStream`。
- 测试：
  - `ws_handler_test.go` 4 例：TurnFlow（SSE mock provider 流式，校验 queued/assistant_start/assistant_delta/assistant_done + delta 累积 "Hi there"）、CancelBeforeStart（缺 Authorization 返回 401）、CancelRunningTurn（cancel 未知 turn_id 返回 40001 "turn not active"）、DisconnectCancelsTurn（断连后复用同 turn_id 重新发起成功）+ SessionEndOnClose（Session Close 后 WS 收到 session_end reason=closed）。
  - `conversation_handler_test.go` 增 `TestAPISSESessionEnd`：SSE 订阅 + sm.Close 后客户端拿到session_end/closed。
  - 修复 SSE handler 因移除 `data:` 前缀造成的 TestAPISSEEvents 回归。
- 全项目 build/vet/test 全绿。

## Phase 2：Claude adapter（已完成本地代码）

- `internal/provider/claude.go`（新增）：Anthropic Messages API adapter。
  - `Chat`：POST `<base>/v1/messages`，header `x-api-key` + `anthropic-version: 2023-06-01`。
  - 请求转换：
    - `messages` 中 `system` role 提取到 top-level `system`（多条拼接）。
    - `tool` role（即 tool 结果） → `user` role + `tool_result` block（带 `tool_use_id`/`content`）。
    - `assistant` role 带 `tool_calls` 转 content blocks：`thinking`（可选）+ `text` + 每个 `tool_use`（ID/name/input JSON）。
    - `ToolDef` → Anthropic `{name, description, input_schema}`。
    - `ToolChoice` 模式映射：auto/none/required/specific → Anthropic `{type:"...","name":...}`。
    - `ThinkingConfig` → `{type:"enabled", budget_tokens}`。
    - `Extra` 注入顶层。
  - `StreamChat`：解析 Anthropic SSE 事件 `message_start`、`content_block_start/delta/stop`、`message_delta`、`message_stop`，映射到 ChatChunk：
    - `text_delta` → `Delta.Content`；`input_json_delta` → `Delta.ToolCalls[0].Arguments` 增量（带 stable block id）；`thinking_delta` → `Delta.ReasoningContent`。
    - `stop_reason` → `FinishReason`（end_turn/stop_sequence=stop、tool_use=tool_calls、max_tokens=length）。
    - usage 合并 input_tokens+output_tokens。
  - 错误分类：HTTP 401/403/429/5xx → ProviderError Code（401=unauthorized、403=forbidden、429=rate_limit+RetryAfter、5xx=server）；复用 openai 的 `classifyHTTPStatus`。
- manager.go `init()` 注册 factory `"claude"`。
- 测试 `claude_test.go` 6 例：Chat 文本（system/max_tokens/Anthropic-Version 校验）、Chat tool_use（input JSON 转 string args）、错误分类（401/403/429 retryable）、Stream text+tool delta（delta 拼接 + tool_use input 增量 + stop_reason→tool_calls）、Stream thinking（`thinking_delta`/`text_delta`/usage）、Manager factory 注册端到端。
- 全项目 build/vet/test 全绿。

## Phase 2：Gemini adapter（已完成本地代码）

- `internal/provider/gemini.go`（新增）：Google Generative AI REST API adapter。
  - `Chat` POST `<base>/v1beta/models/{model}:generateContent?key=<APIKey>`；`StreamChat` `:streamGenerateContent?alt=sse&key=<APIKey>` SSE。
  - 请求转换：`messages` → `contents[{role:user|model, parts:[{text|functionCall|functionResponse|thought}]}]`；`system` role → top-level `systemInstruction`；tool role → user role `functionResponse`；assistant tool_calls → `functionCall` parts。tools 包 `{functionDeclarations: [...]}`；toolChoice 映射 AUTO/ANY/NONE + `allowedFunctionNames`。ThinkingConfig → `generationConfig.thinkingConfig{includeThoughts, thinkingBudget}`。Extra 注顶层。
  - 响应：candidates[0].content.parts text/thought/functionCall → ChatResponse；finish STOP→stop、MAX_TOKENS→max_tokens 等；usageMetadata → Usage。
  - 流式：解析 SSE `data:` JSON GenerateContentResponse。每个 candidate 的 text/thought/functionCall 累加为 delta chunk；finishReason 单独一个终态 chunk；usageMetadata chunk。
- Factory `"gemini"` 在 init 注册。
- 测试 `gemini_test.go` 6 例：Chat text（URL/key + systemInstruction 校验）、Chat tool_call（args JSON）、Chat 错误 429 rate_limit retryable、Stream text 累积、Stream thinking + tool（thought/text/functionCall 同时帧）、Manager factory。
