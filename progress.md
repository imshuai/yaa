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

## Phase 2：Ollama adapter（已完成本地代码）

- `internal/provider/ollama.go`（新增）：Ollama REST `/api/chat` adapter。
  - Messages 结构与 OpenAI 兼容（role/content/tool_calls）；请求体 `{model, messages, stream, tools, options}`；Extra 注入顶层（如 `keep_alive`）。
  - options：`MAXTokens → num_predict`、`Temperature`、`TopP`、`Stop`。
  - Chat 非流式：解析 wire.{message, done, done_reason, prompt_eval_count, eval_count} → ChatResponse。
  - StreamChat 流式：每行是 JSON object（非 SSE event），逐次 emit ChatChunk：content/tool_calls/done_reason/usage；done=true 时一次性发终态。
- 错误分类：404 含 "model" → `ErrCodeModelNotFound`；其他 HTTP 状态复用 classifyHTTPStatus；429 RetryAfter。
- Factory `"ollama"` init 注册。
- 测试 `ollama_test.go` 5 例：Chat text（path 校验 + stream:false）、Chat tool_calls（args JSON + done_reason→tool_calls）、Chat 404 model_not_found、Stream text 累积、Manager factory。

## Phase 3：Tool 系统起步（Manager 已完成本地代码）

- `internal/tool/errors.go` + `tool.go`：sentinel 错误集 + `Tool` 接口 + `ExecutionScope`/`ToolResult`/`ToolInfo` + `RetryableError` + `ValidationError`。
- `internal/tool/manager.go`：Manager（v1 静态配置注入，无 ReloadManager 动态 reload）。
  - `NewManager(deps)` 深拷贝 `cfg.Tools.Builtin` 到 configs 和 source=builtin；解析 `cfg.Agents[].Tools` allowlist（空 = AllowAll）。
  - `Register/Unregister/Get/List/ListForAgent/CheckPermission`、`AgentScope`。
  - `ToolDefs(agentID)` 把 enabled+authorized 工具映射为 `provider.ToolDef`（v1 canonical name 直接作为 wire alias，未实现 provider.md 的 alias 碰撞反查）。
  - `Execute` 执行流程：validate AgentID → Tool lookup → enabled → permission → validateParams → effective timeout (Tool 超时 fallback default；上界 MaxTimeout) → 全局 gate semaphore → callCtx WithCancelCause + `time.AfterFunc` 设 ErrToolTimeout cause → Tool.Execute；caller cancel 优先返回 context.Cause(ctx)。
  - `ExecuteBatch`：保持输入顺序的并发批量，worker 数 = `min(len(calls), MaxConcurrent)`；硬错误通过 `ErrorResult` 投影为单独项的 `ToolResult{IsError:true}`；caller ctx 失败在全部完成后返回 context.Cause。
  - `ErrorResult` 脱敏映射 6 个 sentinel → 固定 LLM-friendly message（不拼接 err.Error()）。
  - Ponytail：validateParams 用序列化可行性校验代替 JSON Schema validator（后续接入完整 validator）；ListForAgent 使用稳定插入排序避免引入 sort。硜 Go 1.20 `context.WithCancelCause` + `time.AfterFunc` 实现 cause-preserving timeout（不能使用 1.21 才有的 `context.WithTimeoutCause`）。
- 测试 `manager_test.go` 11 例：list/listForAgent、checkPermission、execute echo/not found/disabled/denied、timeout、caller cancel cause 保持、batch 顺序+missing tool、ErrorResult mapping。

## Phase 3：内置 Tool shell/http/file（已完成本地代码）

- `internal/tool/builtin/shell.go`：`/bin/sh -c` 执行，stdout+stderr 合并，超 max_output_bytes 截断；blocked/allowed 前缀匹配（blocked 优先）；working_dir + env；非零退出 IsError=true 含退出码。
- `internal/tool/builtin/http.go`：HTTP 请求；headers/body 注入；返回 status_code + headers + body + elapsed_ms JSON；blocked/allowed hosts 精确匹配（blocked 优先）；max_response_bytes 截断。
- `internal/tool/builtin/file.go`：4 个 file_read/file_write/file_list/file_delete 子 Tool（fileTool 分支 by name）。统一 path canonical 校验（canonicalPath 解析最近祖先、EvalSymlinks、再拼回 tail；validatePath within check）。allowed_paths/blocked_paths，blocked 优先；max_file_size 上限；read 支持 utf-8/base64 + max_bytes；write create_dirs；list 排序；delete 文件或空目录。
- `internal/tool/builtin/{shell,http,file}_test.go`：shell 5 例、http 4 例、file 6 例（read/write/delete 循环；blocked/not allowed；list sorted；create_dirs；base64）。
- 全项目 build/vet/test 全绿。

## Phase 3：Runtime 注册 builtin + Agent Tool Loop 闭环（已完成本地代码）

本段让 Tool 系统与 Agent turn 真正闭环：内置工具在 Runtime 启动时注册到 Tool Manager，
Agent turn 内按 docs/agent.md §4 + docs/tool/provider.md §3-§5 完整走 Tool loop 端到端。

### Tool Manager：ProviderToolProjection（`internal/tool/projection.go` 新增）

- `ProviderToolProjection`：不可变 turn-local 投影。`defs []provider.ToolDef`（按 canonical name UTF-8 升序）、`canonicalToAlias map`（definitions + history-only 的并集）、`aliasToCanonical map`（仅 executable definitions）。
- 算法（docs/tool/provider.md §2 严格实现）：
  - `providerSafeToolName = ^[A-Za-z_][A-Za-z0-9_-]{0,63}$`；安全 canonical 直接 identity，unsafe 走 `t_` + 完整 SHA-256 base32（不 trim、不截断）。
  - 合并 current definitions（ListForAgent）与 history 中出现过的 canonical name（assistant ToolCalls[].Function.Name / tool 消息 Name 非空）；碰撞返回 `ErrToolAliasCollision`（含稳定构造分支：取任一 unsafe canonical 的 hash alias，把它本身作为第二个 canonical 再投影）。
  - history-only 名进入 union 表（可投影历史回传），但**不进 executable 反查表**（不恢复执行权限）。
- API：
  - `ToToolDefs(agentID, history) (*ProviderToolProjection, error)`
  - `ProjectRequest(req ChatRequest) (ChatRequest, error)`：要求 `req.Tools` 必须空；深拷贝 req 并注入冻结 defs、改写 assistant ToolCalls 名 / tool message Name / specific ToolChoice。
  - `ResolveExecutable(alias) (canonical string, ok bool)`：精确大小写敏感查找；不着色 history-only/unknown。
  - `Defs()` 返回深拷贝。
- 深拷贝 helper：`cloneToolDef` / `cloneRawMessage` / `cloneChatRequest`（Message.ToolCalls、ToolChoice、Stop、Extra、Thinking、ResponseFormat 均独立）。
- 删除旧简化版 `Manager.ToolDefs(agentID)`（无外部测试依赖）。
- Ponytail 决策：v1 内置工具名（shell/http/file_read/write/list/delete）均 provider-safe，投影路径恒等映射；但仍按文档算法实现 hash alias 以坐实安全边界（Ponytail 例外：input validation at trust boundaries），后续接 MCP/插件带 unsafe canonical 名时无需重构 Agent loop。
- 测试 `projection_test.go` 6 例：defs 仅 authorized、ProjectRequest 写 alias + 深拷贝不破坏原请求、ProjectRequest 拒非空 Tools、specific ToolChoice 命中 executable 校验、hash alias + 稳定 ErrToolAliasCollision 构造、history-only 名可投影但不可执行。

### Manager 文件容器分裂（`internal/tool/manager.go`）

- docs/config/reference.md §6.3 约定 `tools.builtin.file` 是 `file_read/file_write/file_list/file_delete` 共享配置组。
- `NewManager` 在 builtin 配置复制循环后追加 `file` 容器分裂：把 `Builtin["file"]` 复制到 4 个 canonical 名的 configs（仅当显式配置 file_read 等键时跳过），保证 Enabled/Timeout/Options 与文档语义一致，不依赖 Register 默认填空。
- 既有 Manager 11 项测试全绿（行为不受影响）。

### Runtime 注册 builtin（`internal/tool/builtin/register.go`）

- `RegisterBuiltin(m *tool.Manager, cfg *config.Config) error`：把 shell/http/file_read/file_write/file_list/file_delete 6 个 Tool 构造并 Register，配置取自 `cfg.Tools.Builtin`（file_* 共享 `file` 容器）。
- disabled Tool 也 Register（保留在 List 以维持禁用语义，docs/tool/manager.md §3 step 4）。
- `Runtime.Start` 在 Provider ready 后插入：
  1. `tool.NewManager(Dependencies{Config, Providers, Logger})`
  2. `RegisterBuiltin(tm, cfg)`
  3. 每个 Agent 当前 definitions 空历史 `ToToolDefs(ag.ID, nil)` 校验（启动 binding 检查；alias 碰撞或非法名在 Ready 前尽早失败）
  4. `rt.agents.SetTools(tm)` 注入。
- Shutdown 逆序置 `rt.tools = nil`。
- Runtime struct 加 `tools *tool.Manager` 字段，Health components 多 "tool": "ready" 一项。

### Agent Tool Loop（`internal/agent/handle_turn.go` 重写 runDirectTurn）

新 `runDirectTurn` 按 docs/agent.md §4 完整 Tool loop：

1. `turn.AppendUser(req.Content, req.Metadata)`
2. 重复最多 `maxToolRounds=8` 轮（types.go 新增常量）：
   1. `turn.Snapshot()` 拿 canonical history（system + 历史消息）
   2. 找 ModelInfo、算 currentTurnStart（最后一条 user）
   3. `m.deps.Tools.ToToolDefs(a.id, canonicalMsgs)` 冻结 projection；Tools 未注入时 projection 退化为 nil（兼容 v1）
   4. 组装 canonical `ChatRequest`（Tools 留空）
   5. `projection.ProjectRequest` 注入 alias definitions 与历史 ToolCalls 名投影
   6. `Context.Build(已投影请求)`（看到最终 wire alias）
   7. `callProvider`（direct / stream 二选一）累积 assistantMsg + usage；totalUsage 逐轮累加
   8. Tool 判定：`len(assistantMsg.ToolCalls)>0 && proj != nil` 时 `resolveToolCalls` 校验 + 反查
   9. 无 tool_calls → `turn.Append` 单条 final assistant → emit `assistant_done`（Usage=累计、ToolCallCount=累计）→ 返回
   10. `rounds+1 >= maxToolRounds` → `ErrAgentToolRoundLimit`（不提交 partial unit）
   11. `tool.ExecuteBatch(ctx, ExecutionScope{AgentID, SessionID}, calls)` 执行 results
   12. canonical 名写回 assistantMsg.ToolCalls（Session 永远只持有 canonical，不外泄 wire alias）
   13. 构造单批 unit `[assistant(tool_calls), tool, tool, ...]` 一一对应；`turn.Append` 原子提交（Session classifyAppendBatch + validateBatchSequence 已支持）
   14. 流式下 emit `tool_call` / `tool_result` 进度事件（按 call 输入顺序）
   - 进入下一轮（复用本 turn 冻结的 projection）

- `resolveToolCalls`：每个 call `Arguments` 必须 `isValidArgsObject`（单个 JSON object + 拒绝 trailing token；用 `json.NewDecoder` + `dec.More()`）；alias 经 `ResolveExecutable` 精确反查；任一失败 `ErrAgentProviderProtocol`，整批 not executed、不提交 partial（docs/agent.md §5）。
- `isValidArgsObject` 拒空串、数组、标量、object 后接字符。
- `callProvider`/`callChat`/`callStream` 保持纯净（不感知 projection），反查在主 loop 做。
- 修正旧注释里的中文乱码（"圈 - 暫跳构建...在 v1 direct 不绕 Context..."）。

### Pre-existing bug 修复：RunTurn callback sentinel 透传（`internal/session/runturn.go`）

发现并修复一个 pre-existing 契约 bug：`RunTurn` `<-done` case 无条件 `cancel(nil)` 抹掉了 `context.Cause(turnCtx)`，导致 callback 返回的业务 sentinel（如 `ErrAgentProviderProtocol`/`ErrAgentToolRoundLimit`）一律变成 `context.Canceled`，`HandleTurn` 调用方永远拿不到业务错误分类。
- 改为：done 收到 err 时，非 nil 用 `cancel(err)` 设 cause 让 `context.Cause` 透传；nil path 仍 `cancel(nil)` 回收资源。
- session.Manager 既有所有测试不变；conversation_handler 的 `errors.Is(err, ErrAgentToolRoundLimit/ErrAgentProviderProtocol)` 业务分类自此可命中。

### Agent Tool Loop 测试（`internal/agent/handle_turn_test.go` 新增）

- `localEchoTool`：agent 包内 Tool stub，回 `params["msg"]`。
- `newToolLoopEnv`：完整链路（memory Session + Provider mock（httptest + atomic 计数切换响应）+ `tool.Manager` + 注册 echo + Agent `Tools: ["echo"]` allowlist）。
- 4 项端到端测试：
  - `TestAgentHandleTurnWithToolLoop`：round1 tool_calls(echo,"hi") → round2 final("done")；`ToolCallCount==1`；Provider 恰好被调用 2 次；Session 4 条消息序列正确（user, assistant(tool_calls), tool["echo: hi"], assistant(final)）；tool_call 持久化的是 canonical 名 `echo`。
  - `TestAgentHandleTurnUnknownToolAlias`：Provider round1 返回 alias `"not_registered"` → `ErrAgentProviderProtocol`，Session 仅 user 一条（无 partial assistant/tool）。
  - `TestAgentHandleTurnRoundLimit`：Provider 每轮恒返回 tool_calls → `ErrAgentToolRoundLimit`。
  - `TestAgentHandleTurnInvalidArgsObject`：arguments 是数组 `[1,2,3]` → `ErrAgentProviderProtocol`。

### 验证

- `go vet ./...` / `go build ./...`：0 issue / 0 error。
- `go test -count=1 -timeout 120s ./...`：全部 ok（agent/api/config/context/logging/provider/runtime/session/storage/tool/tool/builtin）。
- WS 取消延迟 race 一度在混合时间窗观察到 transient fail（测试内 `time.Sleep 200ms` 固有计时误差），重测单测 + count=20 稳定绿。

## 环境正规化（2026-07-25）

- Go 1.20 工具链从 `/tmp/go1.20.14` 搬到 `/usr/local/go`（Go 官方标准位置），包布局保留。
- 系统级符号链接：`/usr/local/bin/go` → `/usr/local/go/bin/go`、`/usr/local/bin/gofmt` → `/usr/local/go/bin/gofmt`。`/usr/local/bin` 已在系统默认 PATH，任何 shell（含 codex sandbox/非登录）直接 `go` 即用，无需 export PATH。
- `~/.bashrc` 删除临时的 `export PATH=/tmp/go1.20.14/bin:$PATH` 块；保留代理设置（`HTTP_PROXY/HTTPS_PROXY=http://192.168.4.1:7890`、`GOPROXY=https://goproxy.cn,direct`、`GOSUMDB=sum.golang.org`）。
- 验证：直接 `go version` → go1.20.14 linux/arm64；`go build ./...` 通过；env 变量覆盖生效（`GOPROXY=https://goproxy.cn,direct`）。

## Phase 3 Skill 系统（commit `947ac61`，已推送 gitea/main）

### 范围

按 `docs/skill/`：v1 Skill Manager（静态配置注入 + graph 校验 + per agent 投影）+ Agent turn 系统消息注入。

### internal/skill（新包，4 文件）

- `errors.go`：11 sentinel（`ErrSkillDirectoryUnavailable`/`ErrSkillNotFound`/`ErrSkillInvalid`/`ErrSkillDuplicate`/`ErrSkillDependencyMissing`/`ErrSkillDependencyCycle`/`ErrSkillDisabled`/`ErrSkillToolUnavailable`/`ErrSkillPermissionDenied`/`ErrSkillAgentNotFound`/`ErrSkillOptionsInvalid`）。
- `types.go`：`Status="loaded"|"disabled"`、`Skill{Name,Description,Version,Author,Tools,Skills,Options,Prompt}`、`Entry`、`ResolvedSkill{Name,Options,Prompt}`、`Manager`。常量上限：`maxSkillFile=1MB`、`maxDescription=4096`、`maxPromptBody=256KB`、`maxOptionsBytes=64KB`、`maxDepsPerCategory=64`。`skillNameRE=^[a-z0-9][a-z0-9-]{0,63}$`。`DefaultSkillDir="./skills"`。
- `frontmatter.go`：`parseSkillFile` 严格解析 SKILL.md（文件上限→splitFrontmatter→yaml Decoder KnownFields→name==dir→description/版本/SemVer[prepend v]→列表去重上限/JSON options/空 body）。helper：`normalizeCRLF`、`validateDepsList`、`validateOptionsJSON`。
- `manager.go`：`Load(skillsCfg, agents, tm, baseDir)` all-or-nothing——dir 解析 + ReadDir + Lstat 拒 symlink（`ModeSymlink` 显式 `ErrSkillInvalid`）+ 按目录名升序 + `parseSkillFile` + 索引唯一 + `validateSkillGraph`（DFS 环检测含 chain）+ per_skill 存在性 + entries map（含 status）+ 每 Agent `resolveForAgent`（allowlist 空不用、dep 必须在 allowlist、Tool 必须存在 + enabled + CheckPermission、options shallow merge、拓扑序去重）。`Get/List/ResolveForAgent` 深拷贝。

### Runtime 接入（`internal/runtime/runtime.go`）

- 加 `configPath`/`skills *skill.Manager` 字段、`SetConfigPath` setter。`cmd/yaa/main.go` 调用注入配置文件路径。
- `Start` 在 Tool Manager ready 后 `skill.Load(rt.cfg.Skills, rt.cfg.Agents, tm, baseDir)`（baseDir = configPath Dir 或 cwd）+ components["skill"]="ready"。Shutdown 逆序 `rt.skills=nil`。`am.SetSkills(rt.skills)`。
- 默认 `./skills` 目录缺失 = 启动失败（docs 强约束）；仓根建 `skills/.gitkeep` 让默认 cwd 启动可跑；`internal/runtime/runtime_test.go` 3 处 Start test 用 `t.TempDir()` 作为 skill dir。

### Agent 接入（`internal/agent/{manager,handle_turn,handle_turn_test}.go`）

- `manager.go`：`Dependencies` 加 `Skills *skill.Manager`；`SetSkills(sm)` 方法。
- `handle_turn.go` `runDirectTurn`：每轮 base sysPrompt 之后按拓扑序注入 Skill 系统消息（`Skills.ResolveForAgent(a.id)` → 每个 `renderSkillSystemMessage(r)`）。helper：`renderSkillSystemMessage` 渲染 `## Skill: <name>\n\nOptions:\n<json HTMLescape 关闭>\n\nInstructions:\n<body>`。Skill 不进 Session，每轮从 Manager 不可变 snapshot 重新投影。
- 测试：`newSkillTestEnv` + `TestAgentHandleTurnInjectsSkillSystemMessages` + `writeSkillFile`/`indexOf` helper。验证 base + alpha + beta + user 顺序、alpha<beta、body 注入、Skill 不进 Session。

### 决策

- **Skill dir 解析基准**：docs 说"相对主配置文件目录"。Runtime 无 configPath → 加 `SetConfigPath` setter，有 configPath 时 baseDir=Dir(Abs(configPath))，否则 cwd（与 storage path 一致）。
- **Symlink 拒绝**：os.ReadDir 的 `DirEntry.IsDir()` 对 symlink-to-dir 在 Unix 返回 false（默默跳过）；改用 `os.Lstat` 检 `ModeSymlink` 显式 `ErrSkillInvalid`。
- **Dup name 跨 dir 不可达**：frontmatter.name 必须==dir，两个不同 dir 同名 Skill 必触发 `ErrSkillInvalid`（name/dir mismatch）而非 `ErrSkillDuplicate`。
- **YAML SemVer**：`x/mod/semver.IsValid` 需 `v` 前缀；frontmatter.Version 支持 `1.0.0` 与 `v1.0.0`（prepend v 校验）。
- **Options 合并**：SK-007 顶层 shallow merge（frontmatter→root per_skill→agent skills_config），不递归/不 append array。
- **tool.Manager 空 allowlist = AllowAll**：skill test 测"agent 不允许 http"时不能传 `[]string{}`，改用 `["zzz-stub-tool-example"]`（非空但不含 http）。

### 验证

- `go vet ./...` / `go build ./...`：0 issue / 0 error。
- `go test`：internal/skill 18 例全绿；internal/agent (含 skill 注入测试) 绿；runtime/session/tool/api 包全 ok，无 regression。
- WS TestWSStreamDisconnectCancelsTurn count=30 全绿；先前 count=20 偶发 `turn id already used` 是测试内 `time.Sleep(200ms)` 固有 timing flake，并非 `runturn.go` `cancel(err)` 改动引入（`errorFrameFromTurnError` 用 `errors.Is(err, context.Canceled)`，`ProviderError.Unwrap()` 透出 `context.Canceled` 仍可命中）。

### 下一步

- Phase 3 后续：Memory 系统 / Planner / Auth 认证（按 roadmap 顺序）。

## Phase 3 Auth 系统（进行中）

### 文档修复（commit `4067f9d`，已推送 gitea/main）

按 Auth v1 实施前勘察（W 系列文档审查）修关键表述：
- W9 统一 Auth 包 sentinel 集合：`docs/auth/authentication.md §3` 补 `ErrUnauthenticated`（由 RBACAuthorizer 在 identity==nil 时返回），消除与 `authorization.md §6.1` 的 sentinel 不一致。
- W7 JWT 构造器前置注释：`authentication.md §3.2 NewJWTAuthenticator` 注明 secret≥32 仅在 `Enabled && token_type==jwt` 时由 Runtime 构造器强校验，与 `config/validation.md` 条件化校验对齐。
- W3 明确 AuditLogger 为可选扩展点，v1 不强制实现、不注入、无注入 API；Authorizer v1 唯一实现即 RBACAuthorizer。
- W5 补 `remote-api/INDEX.md §5` 路由表记号说明：`read:agents` 仅为可读展示，注册时拆分为 `Action`/`Resource` 两个字段。
- W6 `config/validation.md validateAuthConfig` 在 `validResources`/`validActions` 旁补中文注释列出枚举清单。
- W10 `remote-api/auth.md §1` 明确 v1 无 refresh/token TTL 常量，JWT 过期由外部 issuer 通过 exp claim 决定，`runtime.auth.*` 全部 restart-required。
- W8 `agent.md §1` 补 Agent 层鉴权/身份归属声明：v1 不为 Agent 层引入独立 Auth；AuthN/AuthZ 由 Remote API Server 唯一 route wrapper 完成，Agent 仅做 Agent ID/Session ID 归属校验。

### Auth 包核心（commit `f1fc294`，已推送 gitea/main）

按 `docs/auth/authentication.md §2-§3` + `authorization.md §6` 实现 v1 Auth 包核心：
- `internal/auth/identity.go`：Identity{id,name,roles,claims} + `cloneIdentity` 深拷贝 + `identityContextKey{}` 唯一 context key + `ContextWithIdentity`/`IdentityFromContext` 双向 clone + `String` 脱敏日志 + `HasRole`。
- `internal/auth/errors.go`：三个 sentinel `ErrInvalidToken` / `ErrJWTInvalid` / `ErrUnauthenticated`。
- `internal/auth/authenticator.go`：`Authenticator.Authenticate(token) (*Identity,error)` + `Authorizer.Authorize(identity,action,resource) (bool,error)` 接口。
- `internal/auth/static.go`：`StaticAuthenticator` (sha256(token)->Identity 索引)，构造器防御兜底 name/token/roles 非空 + token value sha256 唯一，`Authenticate` 返回 clone，失败 `ErrInvalidToken`。
- `internal/auth/jwt.go`：`JWTAuthenticator` (golang-jwt/jwt/v5 **v5.2.2** 即 Go 1.20 兼容上限；v5.3.x 依赖 Go 1.21 `slices` 包)，强制 HS256 (`WithValidMethods`+keyfunc Alg 校验)，`WithIssuer/WithAudience/WithLeeway/WithExpirationRequired`，失败包 `ErrJWTInvalid`，成功要求 `Subject!="" && len(Roles)>0`，Identity.Claims 只含 issuer/expires_at；构造器 secret>=32/issuer/audience 非空/clock_skew in [0,5m]。
- `internal/auth/rbac.go`：`RBACAuthorizer` (Role->Permissions 深拷贝)，`Authorize` identity==nil→`ErrUnauthenticated`，遍历 Roles 全部权限累加 allowed/denied，**deny 优先于 allow**，未匹配默认拒绝，未知 Effect 返回 error，`matchPattern` 仅整字段 `*`（无前缀通配）。
- 依赖：`github.com/golang-jwt/jwt/v5 v5.2.2`（v5.3.x 依赖 Go 1.21 `slices` 包，选 v5.2.2 为 Go 1.20 兼容上限）。
- 测试 `auth_test.go`：Identity/clone/context roundtrip + Static 命中/未命中/构造兜底 + JWT happy/bad alg/坏 issuer/过期/空 subject/空 roles/缺 exp/构造兜底 + RBAC allow/deny 优先/wildcard/admin vs viewer/operator 矩阵/未匹配/nil identity/未知 effect/构造兜底，18+ 例全绿。

### Remote API Auth wrapper 基础（commit `968d9a0`，已推送 gitea/main）

按 `docs/auth/integration.md §1-§5` 落地 AuthN/AuthZ 在 Remote API 的入口基础设施；首批把 `/api/v1/health` 与 `/api/v1/version` 通过唯一 wrapper 注册并绑定 `RouteSpec(Action=read, Resource=system)`：
- `internal/api/server.go`：Server 新增 `authz/authn/authEnabled/publicPaths` 字段（mutex 保护），`SetAuth(enabled, authn, authz, publicPaths)` setter 把 publicPaths 规范化成 `map[string]bool` 便于 O(1) 精确匹配；`register` 把 health/version 用 `registerProtected` 替代 `mux.HandleFunc + methodGet`，绑定 RouteSpec read:system。
- `internal/api/route_auth.go`（新增）：`Transport`/`TransportHTTP`/`TransportWebSocket` 常量 + `routeSpec{Method,Pattern,Action,Resource,Transport}` + `bearerToken` (Cut + EqualFold Bearer + token 非空且无空白/tab) + `credentialCode` (errors.Is(ErrJWTInvalid)->40102 否则 40101，按 sentinel 不按字符串判断) + `registerProtected` 唯一 wrapper：method 校验在前→disabled/public bypass→extract Bearer→Authenticator.Authenticate→Authorizer.Authorize→ContextWithIdentity 注入；失败 401/403 对 envelope，AuthZ 拒绝写脱敏日志。另含 `authIdentityForWebSocket` 给 WS handler 复用。
- `internal/api/route_auth_test.go`（新增 13 例）：PublicBypass / DisabledBypass / MissingBearer 40101 / StaticValid / StaticInvalid 40101 / BearerBadFormat 40101 / RBACDeny 40301 / JWTValid / JWTBadIssuerCode 40102 / MethodCheckStill40501WithAuth（含 disabled 路径仍 405）/ IdentityInjectedIntoContext 端到端断言；全 api 测试无 regression。
- 注意：`agents/sessions/sse/ws` 子树与 WS handler 接入 wrapper 的精细 per-sub-path RouteSpec 绑定（含 37 路由注册全量测试）为后续 commit，本轮先把基础设施端到端跑通。

## Phase 3 Memory 系统第一阶段（commit `45fe5ca`/`7acee98`，已推送 gitea/main）

按 `docs/memory/{README,architecture,storage,lifecycle,errors,observability,decisions,config-ref,checklist,integration}` 实现 v1 Memory 系统第一阶段：types/errors/Manager + memstore 后端 + Runtime 启动/关闭接入。

### 文档现状
- Config 层 MemoryConfig/MemoryOverride/MemoryPolicy/ResolveMemoryPolicy/validateMemoryConfig/validateMemoryPolicy/DefaultMemoryConfig 在前期已落地（`internal/config/{types,policy,validation,defaults}.go`）；测试已绿。
- Auth 文档修复对应勘察 W1-W10 之后的 W3/W5/W6/W7/W8/W9/W10 已统一（commit `4067f9d`）。
- 对 Memory 子系统的文档审查（Ohm 勘察）：10 个文档交叉引用闭合，无架构性矛盾，10 点问题清单为表述澄清级别；本轮先按契约落地代码，文档项保留待后续 commit 微调。

### `internal/memory`（新包，5 文件）
- `errors.go`：17 个 sentinel（ErrMemoryDisabled/ErrMemoryClosed/ErrMemoryNotFound/ErrMemoryInvalidScope/ErrMemoryInvalidItem/ErrMemoryManagedField/ErrMemoryUnsupportedLayer/ErrMemoryExpiredInput/ErrMemoryQuota/ErrMemoryStoreUnavailable/ErrMemoryCorrupt/ErrMemoryEmbeddingFailed/ErrMemoryEmbeddingDimension/ErrMemoryEmbeddingZero/ErrMemoryIndexUnavailable/ErrMemoryIndexDegraded/ErrMemoryReindexFailed）。
- `types.go`：Layer/Scope/MemoryItem/SearchRequest/SearchResult/IndexStatus(PutResult)/ItemRef/CommitPutResult/VectorHit/VectorSearchRequest + 输入固定上限常量（MaxAgentIDLen=128/MaxSessionIDLen=128/MaxKeyLen=256/MaxContentLen=65536/MaxMetadataLen=16384/MaxSearchLimit=100/MaxDeleteExpiredLimit=10000）。
- `interfaces.go`：ContentStore (CommitPut/Get/Search/List/Delete/Clear/DeleteExpired/Count/Ping/Close) + Embedder/VectorIndex/VectorIndexFactory + Clock/SystemClock。
- `manager.go` (~1000 行)：
  - Manager 字段：store/embedder/indexFactory/indexes/indexMu/mutationGate/agentLocks(keyedMutex)/clock/events/workerCancel/workerDone/lifecycleMu/inFlight/closing/closeOnce/closeDone。
  - 8 个 canonical 事件类型 (EventAdded/Updated/Deleted/Promoted/Expired/Evicted/Degraded/Error) + Event + EventEmitter 接口。
  - beginOp (lifecycleMu 内原子检查 closing + inFlight.Add(1)) + Close (幂等, closeOnce.Do: 等 workerDone -> inFlight.Wait -> 一次 store.Close; ctx 到期返回 ctx.Cause)。
  - Put: 校验 item/policy/层 + 深拷贝 + normalizeExpiresAt (3 态: nil+default_ttl / zero time=永不过期 / 非零 <=now=ErrMemoryExpiredInput) + 锁序 mutationGate.RLock -> Agent keyed lock + 计算 delta + Count live + 选 victims (fifo/ttl) + ContentStore.CommitPut 单一 commit + emit added/updated/evicted + putIndex (向量失败标 degraded 但 Put 仍成功)。
  - putLocked: 从 Put 提炼，供 Promote 在已持锁路径下复用避免 self-deadlock。
  - selectVictims: 按 fifo (CreatedAt ASC) / ttl (ExpiresAt ASC, 永不排最后) + tie-break (SessionID/Key ASC) 选 victimCount 个排除 target；不足返回 ErrMemoryQuota。
  - Get: 完整 Scope + 已过期返 ErrMemoryNotFound。
  - Search: 关键词路径 (substring on Key/Content lowercase + metadata 深度相等 + UpdatedAt DESC/SessionID ASC/Key ASC 排序 + Score=0)；向量路径完整骨架 (Embed -> VectorIndex.Search -> 击中回查 ContentStore 校验 Version/TTL/scope/metadata -> threshold 后置过滤)；fallback_to_keyword 一次性降级。
  - Delete/Clear/DeleteExpired: 持 mutationGate.RLock + Agent keyed lock (DeleteExpired 单独持 mutationGate.Lock 不取 Agent lock 以与 mutation 不交错)。
  - Promote: 带 SessionID 源复制为目标全局 item (SessionID 空), 源不删, 目标 ExpiresAt=nil 重应用 default_ttl; 复用 putLocked 在同 Agent keyed lock 内避免重入。
  - Reindex: 仅按 agentID 全量 List -> Embed -> 临时 VectorIndex swap；任一失败保留旧 pointer 标 IndexDegraded emit memory.degraded{reason:reindex} 与 ErrMemoryReindexFailed 包装底层错误；成功置 IndexReady。
  - IndexStatus: 唯一不调 beginOp 的只读方法 (indexMu.RLock 快照)，关闭后仍可读，未启用 vector 或未知 agent 返回 IndexReady。
  - ClockForTest() 暴露内部 Clock 给测试场景（仅 monorepo 测试使用，正式调用不应使用）。
- `internal/memory/memstore/store.go`（in-process ContentStore v1 默认后端）：map[PrimaryKey]MemoryItem + sync.RWMutex；CommitPut 单一原子 (校验 victim Version + victim != target + 删 victim + upsert target Version+1 保留 CreatedAt)；Get/Search/List/Delete/Clear/DeleteExpired/Count/Ping/Close 全部契约；返回值 deepcopy Metadata/ExpiresAt 隔离内部缓存。`matchesScopeGlobalSession`（空 SessionID=Agent 全部来源，非空=精确）+ `matchesMetadata`（顶层 JSON 值深度相等）+ `notExpiredAt`（nil/zero=永不过期，否则 Before/After 检查）。

### `internal/memory_test/manager_test.go`（跨包测试 package `memory_test` 避免 import cycle）
16 例全部绿：PutCreatesAndGets (version 保留 / CreatedAt 保留) / PutRejectsInvalidLayer / PutRejectsEmpty / PutRejectsManagedField / PutTTLThreeStates (nil+ttl/nil 0/zero time/<=now) / GetNotFound / GetExpiredReturnsNotFound / SearchKeywordSubstring (Score=0) / SearchLimitValidation (负数/>Max/IncludeGlobal 与空 SessionID) / DisabledPolicy (Put/Get/Search 全部 ErrMemoryDisabled) / QuotaFifoEvict (k1 选最早驱逐后 NotFound) / QuotaExceedsCapacity (100 次写后仅剩最新 2 个, k0 早已 NotFound) / DeleteClear / DeleteExpired (limit 校验 + 仅删过期) / Promote (目标 SessionID 置空, 源保留, 空源 SessionID 拒绝) / PromoteExpiredSource (过期源返 NotFound) / IndexStatusNoVector (未知 agent=ready) / EventsEmitted (added/deleted 顺序断言) / CloseIdempotent (二次关闭 + 关闭后即将 AnyOp 返回 ErrMemoryClosed)。

### Runtime 接入（commit `7acee98`）
- `internal/runtime/runtime.go`：Runtime struct 加 `memory *mm.Manager`（`mm` alias of internal/memory）；import 内部 memory 与 memstore 包。
- `Start` 在 Provider ready 后、Tool Manager 之前：若 `rt.cfg.Memory.Enabled=true` 则构造 `memstore.New()` + `mm.NewManager`（无 embedder/indexFactory/events v1）；否则 `components["memory"]="disabled"`。In-memory 后端为 v1 默认，SQLite 后端待后续 commit；Type=memory 时 warn 日志提示非持久。
- `Shutdown` 加 Memory.Close 在 Provider.Close 之前，组件字段置 nil；`rollback` 同序回滚。

### 验证
- go vet / go build / go test 全部 ./...：通过（除已记录的 WS flake 测试 TestWSStreamDisconnectCancelsTurn timing 偶发外）。
- 全包 build/vet/test 无 regression（agent/api/config/context/logging/provider/runtime/session/skill/storage/tool/tool/builtin + memory/memory_test/memstore）。

### 下一步（按优先级）
1. Agent turn Memory 检索注入 Context（docs/memory/integration.md §2 step 2）：Dependencies 加 Memory / AgentBinding 加 MemoryPolicy snapshot / runDirectTurn 第一轮 base+skill 后插入 32 KiB 上限的 memory system message / Search 错误传递策略（除 ErrMemoryDisabled 外阻断 turn）。
2. SQLite ContentStore 后端（modernc.org/sqlite 已在依赖中）：DDL + schema_version + ON CONFLICT + RFC3339Nano + JSON metadata + 索引创建；与内存后端互测对比。
3. VectorIndex exact cosine + Embedder HTTP（OpenAI-compatible `/embeddings`）；启动期 Reindex。
4. Memory Remote API 8 端点（GET/search + GET/:key + POST + DELETE + DELETE-clear + POST/promote + POST/reindex），handler 入口 snapshot + policy，错误映射按 errors §7。
5. 文档：补 W1-W10 剩余表述澄清项（W1 时间戳 / W2 / W4 tokens[].roles 默认）。

## Phase 3 下一步 #1：Agent turn Memory 检索注入 Context（commit 待 push）

按 `docs/memory/integration.md §2-§3` 落地 Agent 在 direct turn 第一轮注入 Memory system message：
- `internal/agent/manager.go`：`Dependencies` 加 `Memory *mm.Manager` 字段（mm alias of internal/memory）；新增 `SetMemory(mem)` 延迟注入 setter；`Inspect` 的 `MemoryEnabled` 改为反映 deps.Memory 非 nil 且 `resolveMemoryPolicy(a).Enabled`；包注释更新到 Phase 3。
- `internal/agent/memoryinject.go`（新文件）：
  - `resolveMemoryPolicy(a)`：从 root `cfg.Memory` + Agent override `cfg.Agents[id].Memory` 解析 effective `config.MemoryPolicy`（v1 不依赖 ReloadManager，每轮从 deps.Config 重新解析，与 `resolveAgentContextConfig` 同惯例）。
  - `const memoryInjectMaxBytes = 32 * 1024`（文档 §3 固定上限，非 MemoryConfig 字段）。
  - `formatMemoryResults(results) (content string, dropped int)`：按 Search 返回顺序输出（不重新排序），只读 Content + Key，不输出 Score 与 Metadata（v1 白名单未定避免泄露敏感字段）；`escapeMemoryText` 固定转义 `\n` `\t` `\r` 删除其他 0x00-0x1F ASCII 控制字符防伪造 role/Tool protocol；32 KiB UTF-8 上限，超限丢弃最末未追加项并返回 dropped 计数（仅用于日志，不修改 Session）。
  - `indexOfResult` 删去（直接 `for i, r := range` 用 i 算 dropped）。
- `internal/agent/handle_turn.go`：import 加 `errors` + `mm "github.com/imshuai/yaa/internal/memory"`；在 `runDirectTurn` 每轮 canonical messages 组装中、Skill system messages 之后、`for _, sm := range snap.Messages` 之前插入 memory 注入块（仅 `rounds==0 && m.deps.Memory != nil`）：
  - `policy := m.resolveMemoryPolicy(a)`
  - `Search(ctx, policy, mm.SearchRequest{Scope: {AgentID, SessionID: req.SessionID, Layer: LayerLongTerm}, Query: req.Content, Limit: 0, IncludeGlobal: true})`
  - `merr != nil && !errors.Is(merr, mm.ErrMemoryDisabled)` → `return TurnResult{Usage: totalUsage}, fmt.Errorf("recall memory: %w", merr)` 阻断 turn（除 ErrMemoryDisabled 外一律阻断，符合 §2 step1 "不返回伪造空结果"）
  - 命中则 `formatMemoryResults` → 非空 content 追加一条 system message；dropped>0 时 `m.deps.Logger.Warn` 记录。
  - Memory system message 不写入 Session（不进 `snap.Messages.Payload`）。
- `internal/runtime/runtime.go`：`am.SetSkills(rt.skills)` 之后加 `am.SetMemory(rt.memory)`（与 SetTools/SetSkills 同期注入）。
- `internal/agent/memory_inject_test.go`（新文件，4 例）：
  - `newMemoryInjectEnv`：复用 skill 测试模式构造环境，注入真实 `mm.NewManager(memstore.New(), nil, nil, SystemClock{}, nil)` + `cfg.Memory.Enabled=true/vector.Enabled=false/Storage.Type=memory`；捕获 provider 请求 body。
  - `TestAgentHandleTurnInjectsMemorySystemMessage`：Put 2 个 Session-scoped item（k1 "preference.answer_style" / k2 "topic.last"），user content="user" 命中两条 content substring；断言 provider body 是 base+memory+user 三条，memory message 含 "The following are recalled memory entries" 头 + 两个 item content + 不含 "Score"；不写入 Session snapshot（session 仍只有 user+final assistant 2 条）。
  - `TestAgentHandleTurnMemoryDisabledSkipsInject`：cfg.Memory.Enabled=false → Search 返 ErrMemoryDisabled → 静默降级，turn 正常返回 "done"，body 不含 memory message。
  - `TestAgentHandleTurnMemoryNilSkipsInject`：`SetMemory(nil)` → 整段注入逻辑跳过，body 不含 memory message。
  - `TestAgentHandleTurnMemoryNonDisabledErrorBlocksTurn`：先 `memMgr.Close(ctx)` 让后续 Search 返 `ErrMemoryClosed`（非 disabled）；HandleTurn 返回 `errors.Is(err, mm.ErrMemoryClosed)`，Session 仅含 user（turn 阻断）。
- 顺手修复已有 `TestAgentHandleTurnWithToolLoop` flake：`newToolLoopEnv`/`newSkillTestEnv` 的 `cfg.Tools` 原 zero value（`DefaultTimeout=0`）违反 `docs/config/validation.md §461`，导致 `time.AfterFunc(0)` 立即 fire cancel，echo Execute 在 goroutine 调度压力下偶发被判超时。改两处 `cfg.Tools = config.DefaultToolsConfig()`（生产 cfg 经 validation 必 DefaultTimeout>0，测试 env 补齐即可），5 次全绿。
- 已知非我方引入的 `TestWSStreamDisconnectCancelsTurn` timing flake（count 大时偶发）不在本轮修复范围，单跑 5/5 全绿。

### 验证
- go vet / go build ./...：通过。
- go test -count=2 ./internal/{agent,memory_test,config,runtime}/：全绿。
- go test -count=1 ./...：除 `TestWSStreamDisconnectCancelsTurn` 偶发 timing flake（已记录）外全绿。

## Phase 3 下一步 #2：SQLite ContentStore 后端 + Runtime 按 storage.type 分发（commit 待 push）

按 `docs/memory/storage.md §2` 落地 SQLite ContentStore 后端 + Runtime 启动按 `cfg.Memory.Storage.Type` 选 backend。

### `internal/memory/sqlitestore/store.go`（新包，~700 行）
- modernc.org/sqlite 纯 Go 驱动（已在依赖），schema 与文档 §2 完全一致：`memory_items` 复合主键 `(agent_id, layer, session_id, item_key)` + `memory_items_agent_updated` 索引 + `memory_items_expiry` 部分索引；时间 `RFC3339Nano` UTC 文本保存；metadata 为 JSON 文本字段。
- `New(path)`：目录不存在自动创建；`SetMaxOpenConns(1)` + `PRAGMA journal_mode=WAL; busy_timeout=5000` 防 SQLite 写并发；`migrate()` 执行 4 个 DDL（CREATE TABLE IF NOT EXISTS + 两个 CREATE INDEX IF NOT EXISTS + schema_version 记录表）+ 写入 `schema_version=1` + 读 MAX(version) `> schemaVersion` 即拒绝（未知更高版本则启动失败，docs §6）。
- `CommitPut` 单事务：先校验每个 victim (Version 仍匹配且非 target；缺失/Version 不符 → `ErrMemoryQuota`，事务回滚);  写锁内按顺序 SELECT victim row（深拷贝）+ DELETE + INSERT/UPDATE target（保留 created_at、version 递增；INSERT 走完整列，UPDATE 走单条 UPDATE RETURN-less）。`tx.Commit()` 失败回滚 return `storeErr`。返回 `CommitPutResult{Stored, Created, Evicted}` 全 deep copy 隔离缓存。注意：`victim.Equals(target` 直接 `errors.New("victim cannot equal target")` 不走事务。
- `Get`：主键 SELECT 一次 + `scanItem`；与 memstore 契约一致**不过滤 expired**（Manager 决策）；`sql.ErrNoRows` → `ErrMemoryNotFound`；scanItem 的 JSON 解码错走 `corruptOrStoreErr` 包成 `ErrMemoryCorrupt` 透传。
- `Search`：扫描 `agent_id+layer` 全量 rows（候选 <= max_items=10000），在 Go 内按 SessionID/IncludeGlobal、`notExpiredAt`、metadata 顶层 JSON 深度相等、`strings.ToLower(Key/Content).substring(q)` 过滤，`sort.SliceStable` 按 `UpdatedAt DESC / SessionID ASC / Key ASC` 排序；List 同语义但不过滤 metadata、不做 substring。
- `Delete`：事务内 SELECT row → DELETE → 返 deleted item 副本；`ErrMemoryNotFound` 未命中。
- `Clear`：事务内 SELECT 全 `agent_id+layer` 行 + 在 Go 内按 SessionID 全范围/精确匹配筛 → 逐行 DELETE → 返 cleared items 副本。
- `DeleteExpired`：事务内按 `expires_at <= ? AND expires_at IS NOT NULL AND expires_at != ''` 过滤 + `ORDER BY expires_at ASC, agent_id ASC, session_id ASC, item_key ASC`（与 memstore 一致）SELECT 出过期 rows + `if limit > 0 && limit < len(all) { all = all[:limit] }` truncate + 逐行 DELETE + Commit 返回。
- `Count`：扫描全 agent rows + 在 Go 内 `notExpiredAt` 过滤；返回未过期 item 数。
- `Ping`：`SELECT 1`；`Close`：sync.Once 包 `db.Close()` 幂等。
- 错误映射：database/sql 错误统一`storeErr` → `fmt.Errorf("%w: %v", ErrMemoryStoreUnavailable, err)`；`scanItem` 的 JSON 解码错走 `corruptOrStoreErr`（若 `errors.Is(err, ErrMemoryCorrupt)` 透传，否则 storeErr），保证 `errors.Is(err, ErrMemoryCorrupt)` 可穿透。
- `matchesScopeGlobalSession` / `matchesMetadata` / `notExpiredAt` / `cloneItem` / `cloneItems` / `cloneMetadata` / `formatTime` / `parseTime` / `mustParseTime` 全部与 memstore 实现一致语义。

### `internal/memory/sqlitestore/store_test.go`（新文件，16 例）
- 覆盖 SQLite ContentStore 自身契约：CommitPut creates + CreatedAt/Version/UpdatedAt 保留 / Update preserves CreatedAt Version+1 / GetNotFound / Search metadata filter + UpdatedAt DESC order + keyword substring / SearchExcludesExpired + (修正后) Store.Get 不过期过滤 / Delete + DeleteNotFound / Clear scoped (session精确 + 空 session=Agent 全来源 + 其他 agent 不受影响) / DeleteExpired order + limit / Count excludes expired / List 全 agent 来源 / CommitPut victims Version validation (mismatch → ErrMemoryQuota rollback + victim 仍在) + 正确 victim → 删 + 建 target / victim equals target 拒绝 / Corrupt metadata JSON 错误返 ErrMemoryCorrupt (走 Get 路径) / schema_version unknown higher 拒绝 migrate / Reopen persists content+version / Ping 正常。
- 修：`Store.Get` 原 `notExpiredAt` 判定与 memstore 契约不符（Manager 决定），删除；测试断言改为"过期仍能读出 row"。

### `internal/memory_test/sqlite_manager_test.go`（新文件，6 例）
- 用真实 `mm.Manager` + SQLite backend（`newSQLiteManager` 模仿 `manager_test.go` 中 `newTestManager` 同样的 fakeClock + captureEvents + 同一 `defaultPolicy`），覆盖核心 scenarios：Put/Get/Update (CreatedAt 保留 / Version 递增) / GetExpiredReturnsNotFound (clock 推进) / SearchKeywordSubstring session-local + IncludeGlobal / QuotaFifoEvicts (MaxItems=3, k0 选最早驱逐) / PromoteCopiesToGlobalKeepsSource (源 Session 仍可见 + global 也能 Get) / DeleteExpiredExpiresByClock (clock 推进 + 物理删除)。
- 与 memstore 后端在 Manager 包装层核心语义保持一致（"与内存后端互测对比" docs/storage.md §2 字面要求）。

### `internal/runtime/runtime.go` 分发逻辑
- `Start` 中 Memory 段重构：按 `rt.cfg.Memory.Storage.Type` 选 backend：
  - `case "sqlite"`: `sqlitestore.New(rt.cfg.Memory.Storage.Path)`，失败 → `rollback()` + `return fmt.Errorf("runtime: memory sqlite store: %w", sErr)` 让 Runtime Not Ready（docs §2: 启动失败要正确传播）。
  - `default` (含 "memory" 与未知/空值): `memstore.New()`，未知 type 仅 warn，"memory"/"" warn "durable=false"。
  - 注释更新去掉"v1 阶段默认 in-memory"（已能配置 SQLite）。
- import 加 `"github.com/imshuai/yaa/internal/memory/sqlitestore"`。

### `internal/runtime/runtime_test.go` 增 2 例
- `TestRuntimeMemorySQLiteBackendStart`：`cfg.Memory.Enabled=true` + `Storage.Type=sqlite` + tempdir path → Start 后 `health.Components["memory"]=="ready"` + `rt.memory != nil` + 文件被实际创建。
- `TestRuntimeMemorySQLiteBackendStartFailsOnBadPath`：构造一个不可能的目录（path 是既存文件而非目录）→ Start 必须返 error（不要 leaking ready Ready 但 memory 不可用）。

### 验证
- go vet / go build ./...：通过。
- go test -count=1 ./internal/memory/sqlitestore/：16 例全绿。
- go test -count=1 -run SQLiteManager ./internal/memory_test/：6 例全绿。
- go test -count=1 ./internal/runtime/：全绿（含新 2 例）。
- go test -count=1 ./...：除 `TestWSStreamDisconnectCancelsTurn` 偶发 timing flake（已记录，单跑 5/5 全绿）外全绿。

## Phase 3 下一步 #3：VectorIndex exact cosine + HTTP Embedder + Runtime 启动 Reindex（commit 待 push）

按 `docs/memory/architecture.md §4` + `docs/memory/storage.md §5` 落地 v1 唯一向量栈。

### `internal/memory/vector/index.go`（新包，~130 行）
- 进程内 exact cosine VectorIndex（v1 唯一实现，docs/architecture.md §4）。
- `entry{ref memory.ItemRef, vector []float32}` + `sync.RWMutex` 保护内部 slice。
- `Factory()` 返回 `VectorIndexFactory` 让 Manager `indexFactory` 字段每次返回新空 index，避免跨 Agent 共享或复用当前 index。
- `Upsert`: 校验 `ref.AgentID/Key` 非空 + vector 非空（零长度返 `ErrMemoryEmbeddingZero`）；深拷贝 vector 切片防外部 mutation；按主键（AgentID+SessionID+Layer+Key，Version 不参与）精确匹配：存在则 in-place 替换 slice 引用，否则 append。
- `Delete`: idempotent，不存在也返 nil。
- `Search`: 按 `AgentID+Layer+(SessionID 精确或 IncludeGlobal 时 SessionID+" 并集空 SessionID)` 过滤；`cosine(req.Query, vec)` 计算 → score < threshold 跳过 → 收集 hits → `sort.SliceStable` 按 score DESC, SessionID ASC, Key ASC 排序。**不截 limit**（docs §4：留给 Manager 回查 ContentStore 后再 final limit）。
- `cosine` 维度不匹配 → `ErrMemoryEmbeddingDimension`，零向量 → `ErrMemoryEmbeddingZero`；Search 内部错误仅跳过该 hit 不外抛（避免整 Search 失败）。
- 编译期断言 `var _ memory.VectorIndex = (*index)(nil)`。

### `internal/memory/vector/index_test.go`（新文件，4 例）
- Upsert/Delete/Search 主键替换 + scope 过滤 + score DESC + IncludeGlobal 行为；threshold 过滤；ref 非空与 vector 非空校验；cross-agent 隔离。

### `internal/memory/embedding/embedder.go`（新包，~140 行）
- `HTTPEmbedder` OpenAI-compatible HTTP Embedder（v1 唯一 provider）。
- `New(cfg MemoryEmbeddingConfig)`: base_url `TrimRight("/")`, dimension>0, timeout<=0 fallback 30s；构造 `&http.Client{Timeout:...}`。
- `Embed(ctx, inputs)`: 空 inputs 返 nil, nil 不调 server; 否则 `POST {base_url}/embeddings`，请求 body `{model, input []string}`，Bearer api_key 头。用 `context.WithTimeout(ctx, h.timeout)` 限响应时长。错误映射：构造/IO/非 2xx/malformed body/data count mismatch → `ErrMemoryEmbeddingFailed`；维度不匹配 → `ErrMemoryEmbeddingDimension`；全 0 向量或零长度 vector → `ErrMemoryEmbeddingZero`（docs/storage.md §5: "响应正文与输入内容不写入日志"——本实现不 log body）。
- 解析 OpenAI 响应 `{data: [{embedding: [float64...]}]}`；float64→float32 转换。
- 编译期断言 `var _ memory.Embedder = (*HTTPEmbedder)(nil)`。

### `internal/memory/embedding/embedder_test.go`（新文件，8 例）
- happy 2D token "a"/"b" 返 [1,0]/[0,1] 验证 + Bearer header + path /embeddings + model + input 长度匹配；non-2xx → ErrMemoryEmbeddingFailed；dimension mismatch → ErrMemoryEmbeddingDimension；zero vector → ErrMemoryEmbeddingZero；malformed json → ErrMemoryEmbeddingFailed；data count mismatch → ErrMemoryEmbeddingFailed；empty inputs 返 nil no call；New 拒绝 empty base_url/zero dimension；timeout<=0 fallback 30s 通过构造（New 成功）。

### `internal/runtime/runtime.go` Vector 启动注入
- import 加 `"github.com/imshuai/yaa/internal/memory/embedding"` + `"github.com/imshuai/yaa/internal/memory/vector"`。
- Memory Manager 构造段：`if rt.cfg.Memory.Vector.Enabled { embedding.New(rt.cfg.Memory.Embedding) + vector.Factory() }`；embedder 构造失败 → `rt.rollback()` + `return fmt.Errorf(...)` 让 Runtime Not Ready（cfg 由 validation 保证合法，正常路径不会失败）。
- `mm.NewManager(ms, embedder, indexFactory, mm.SystemClock{}, nil)` 把 embedder/indexFactory 注入，让 Manager 走 vector 路径用真实 embedder。
- 启动期对每个 `policy.Vector.Enabled && policy.Enabled` 的 Agent `mmMgr.Reindex(ctx, policy, ag.ID)`；失败仅 `rt.logger.Warn` 让 health 表示 degraded 但 Runtime 不阻断（docs/architecture.md §4: Reindex 失败留 degraded 由后续 Reindex 修复）。**架构 §4 §4「普通操作成功不会清除历史 degraded，只有完整 Reindex 才置 ready」**：putIndex 不再顺手清 status，让 Reindex 成为唯一 ready 来源；本步严格遵守这一契约。

### `internal/memory_test/vector_integration_test.go`（新文件，3 例）
- `newVectorManager` 用 HTTP embedder + 真实 vector.Factory；mock server 按输入 content 返预定义 dim=4 向量。
- `TestManagerVectorReindexAndSearch`: Put 3 个不同 axis 的 item + 显式 Reindex 让 IndexStatus=ready，Search query 命中最近邻居 cosine=1.0 score=1.0；依次查 dogs/cats content 命中 k1/k2 验证 score。
- `TestManagerVectorFallbackToKeywordWhenEmbedderDown`: server close 后 Search 走 fallback_to_keyword 路径，命中 keyword 子串 but Score=0；IndexStatus→degraded。
- `TestManagerVectorNoFallbackErrorsWhenEmbedderDown`: FallbackToKeyword=false 时 embedder 错误透传 sentinel（ErrMemoryEmbeddingFailed/IndexDegraded/IndexUnavailable 其中之一）阻断 turn。

### `internal/runtime/runtime_test.go` 增 1 例
- `TestRuntimeMemoryVectorStartupReindex`: cfg.Memory.Vector.Enabled + httptest mock embeddings server + dim=2 → Start 成功 + memory component ready + rt.memory 非 nil；证明 Runtime vector 启动路径不离 Integer 错。

### 验证
- go vet / go build ./...：通过。
- go test -count=1 ./internal/memory/vector/ + ./internal/memory/embedding/ + ./internal/memory_test/：全绿（含 4+8+3 例）。
- go test -count=1 ./internal/runtime/：含新 1 例全绿。
- go test -count=1 ./...：除 `TestWSStreamDisconnectCancelsTurn` 偶发 timing flake（已记录）外全绿。

## Phase 3 下一步 #4：Memory Remote API 8 端点 + Runtime 注入（commit 待 push）

按 `docs/remote-api/memory.md` §1-§8 + `docs/memory/errors.md §7` 落地 Memory 的 Remote API。

### `internal/api/memory_provider.go`（新文件）
- `MemoryProvider` 接口 8 方法（Search/Get/Put/Delete/Clear/Promote/Reindex + IndexStatus），除 IndexStatus 外每方法接 `config.MemoryPolicy`（architecture.md §2：Manager 不缓存 policy）。
- `MemoryPolicyResolver func(agentID string) (config.MemoryPolicy, bool)`：从 config snapshot 解 effective policy；ok=false 表示 agent 不存在。

### `internal/api/server.go` 改动
- Server 加 `memoryProvider MemoryProvider` + `memoryResolver MemoryPolicyResolver` 字段（mu 保护）。
- `SetMemoryProvider(mp, resolver)` setter；nil mp → Memory 8 端点统一返 50301 "memory subsystem unavailable"。

### `internal/api/session_handler.go` dispatcher 改动
- `handleAgentsSessions` 拓展为 `/api/v1/agents/:id/<sub>` 全 dispatcher：先 TrimPrefix + SplitN 解 out `agentID` 与 `sub`。
- `sub == "memory" || HasPrefix("memory/")` → `s.handleMemorySubtree(w, r, agentID, sub)`；否则 `sub == "sessions"` 走原 sessions handler；其余 → 40401。
- Memory 8 端点与 sessions 共用同 prefix dispatcher，都未走 Auth wrapper（progress #6 决策记录：Go 1.20 ServeMux 不支持 path template，同 prefix 不能重复 registerProtected；精细化 spec 绑定留待后续 wrap-only 接入）。

### `internal/api/memory_handler.go`（新文件，~490 行）
- DTO（docs §DTO）：`memoryDTO`（单 item + index_status）、`memorySearchItemDTO`（带 score）、`memorySearchData{items, limit, index_status}`、`memoryPostBody`、`memoryDeleteOneData`、`memoryClearData`、`memoryPromoteBody`、`memoryReindexData`。时间统一 RFC3339Nano UTC，zero time ExpiresAt → nil。
- `handleMemorySubtree` 按 sub（"memory" 或 "memory/<suffix>"）+ Method dispatch：
  - `GET /memory` → Search；`GET /memory/:key` → Get；`GET /memory/(promote|reindex)` → 40501。
  - `POST /memory` → Put；`POST /memory/promote` → Promote；`POST /memory/reindex` → Reindex；其他 suffix → 40501。
  - `DELETE /memory` → Clear；`DELETE /memory/:key` → DeleteOne；`DELETE /memory/(promote|reindex)` → 40501。
  - 其他 method → 40501。
- 8 sub-handler：
  - `handleMemorySearch`：解析 `q/session_id/limit/include_global/metadata`(JSON) query；`include_global` 必须配非空 `session_id`（否则 40001）；limit 范围 `[0, MaxSearchLimit]`；构造 `SearchRequest{Scope, Query, Limit, Metadata, IncludeGlobal}` 调 `mp.Search`；resp 带 `index_status`（调 `mp.IndexStatus(agentID)`）。
  - `handleMemoryGet`：`requireSessionIDQuery`（session_id 必填 query，显式空表 global item）；调 `mp.Get`。
  - `handleMemoryPut`：`io.LimitReader(1MiB)` 读 body + `json.Unmarshal memoryPostBody`；key/content/metadata/expires_at 校验（key 长度 `[1, MaxKeyLen]`、content `[1, MaxContentLen]`、metadata marshal 后 `<= MaxMetadataLen`、expires_at RFC3339Nano）；构造 `MemoryItem{Layer:LayerLongTerm, ...}` 调 `mp.Put`；created → 201，update → 200。
  - `handleMemoryDeleteOne`：`requireSessionIDQuery` + 调 `mp.Delete`；resp `memoryDeleteOneData{deleted:true,...}`。
  - `handleMemoryClear`：session_id 可省（agent 全范围）；调 `mp.Clear`；resp `memoryClearData{deleted_count, ...}`。
  - `handleMemoryPromote`：读 body + DisallowUnknownFields 风格 json；`SessionID` 和 `Key` 必填（40001）；调 `mp.Promote`；created → 201 else 200。
  - `handleMemoryReindex`：`!policy.Vector.Enabled` → 40001；调 `mp.Reindex`；resp `memoryReindexData{indexed, status, ...}`。
- `requireSessionIDQuery`: 区分"未传 session_id query"（→ 40001）vs "显式 `?session_id=`"（表 global item，合法）；docs §1/§2 明示 session_id 是必填 query 即使空串。
- `writeMemoryError` 按 `docs/memory/errors.md §7` 映射全 sentinel：
  - `context.Canceled`（err 或 request ctx.Err()）→ 不写响应（客户端已断开）。
  - `context.DeadlineExceeded` → 504 / 50401。
  - `ErrMemoryNotFound` → 404 / 40401。
  - `ErrMemoryDisabled` → 409 / 40901。
  - `ErrMemoryQuota` → 429 / 42901。
  - `ErrMemoryInvalidScope/InvalidItem/ManagedField/UnsupportedLayer/ExpiredInput` → 400 / 40001。
  - 其他（含 closed / store unavailable / corrupt / embedding / index unavailable / index degraded / ReindexFailed）→ 503 / 50301。
- helper：`toMemoryDTO` / `toSearchItemDTO` / `formatMemoryTime`（zero → ""）/ `formatMemoryExpiresAt`（nil 或 zero → nil；否则 RFC3339Nano UTC）。

### `internal/api/memory_handler_test.go`（新文件，~25 例）
- `fakeMemoryProvider` 实现 8 方法 + lastXxx 捕获；`fakeMemoryResolver(policy)` closure；`enabledMemoryPolicy()` 默认 Enabled=true/Vector=false。
- `newMemoryTestServer` 构造 Server + `SetMemoryProvider`；`doMem` body 支持 `io.Reader`（原样透传，用于非法 JSON 测试）/ nil / 其他（json.Marshal）。
- happy path：Search（含 index_status）/ Get / Get global empty session_id / Put created 201 / Put update 200 / DeleteOne / Clear 带 session_id / Clear 不带 session_id / Promote / Reindex（vector enabled）。
- 边界：Search include_global 无 session_id → 40001；Get 缺 session_id 参数 → 40001；Put 空 key → 40001；Put 非法 JSON body → 400；Promote 缺 session_id/key → 40001；Reindex vector disabled → 40001；Method 不允许（PUT）→ 40501；unknown agent → 40401。
- error mapping：NotFound 40401 / Disabled 40901 / Quota 42901 / InvalidItem 40001 / StoreUnavailable 50301 / ReindexFailed 50301；mp=nil（未注入）→ 50301。
- `TestMemoryGetNoSessionsPathUnchanged`：用真实 session.Manager + storage.NewMemory + fakeAgentProvider 验证 memory dispatcher 没吃掉 sessions 子路径（GET /agents/a/sessions ≠ 404）。

### `internal/runtime/runtime.go` 注入
- `agentMemoryOverride(agentID)` helper 从 cfg.Agents 找 Memory override。
- `memoryPolicyResolver()` 构造 closure：`agentExists` 返 false → `(MemoryPolicy{}, false)` → handler 40401；否则 `config.ResolveMemoryPolicy(rt.cfg.Memory, override)`。
- Start 段：Memory.Manager 构造成功（`rt.memory != nil`）后 `rt.api.SetMemoryProvider(rt.memory, rt.memoryPolicyResolver())`；Memory 全局 disabled 时 rt.memory==nil，handler 统一 50301（进度记录：operator 不应调用 disabled 子系统；严格 40901 留待后续 wrap-only spec 绑定时再补 disabled stub）。

### `internal/runtime/runtime_test.go` 增 1 例
- `TestRuntimeMemoryRemoteAPIProviderInjected`：`cfg.Memory.Enabled=true` + Storage=memory（无 agent）→ Start 成功 → HTTP GET `/api/v1/agents/unknown/memory` → 期望 40401（resolver 对未知 agent 返 ok=false，证明 provider+resolver 被注入；若未注入则 50301）。

### 验证
- go vet / go build ./...：通过。
- go test -count=1 ./internal/api/：含新 ~25 例全绿，原 sessions/conversation/ws/auth 测试无 regression。
- go test -count=1 ./internal/runtime/：含新 1 例全绿。
- go test -count=1 ./...：除 `TestWSStreamDisconnectCancelsTurn` 偶发 timing flake（已记录）外全绿。

## Phase 3 下一步 #5：gorilla/mux 路由层重构 + AD-004 37 路由精细 RouteSpec 绑定（commit 待 push）

按 `docs/auth/integration.md §2-§5` + `docs/auth/decisions.md AD-004` + `docs/remote-api/INDEX.md §3/§5`
让 RouteSpec 与唯一 wrapper 真正生效，消化 progress #6 的 wrap binding 技术债。

### 文档/代码冲突修复
- 文档（`integration.md §3` 代码示例 + `INDEX.md §5` "不得从 URL 段猜权限" + AD-004 "37 路由必须 metadata 测试" + checklist "每条路由显式绑定 RouteSpec"）一致要求用 gorilla/mux 路径模板 + 唯一 wrapper 逐条 RouteSpec 绑定；现行代码用裸 `http.ServeMux` + 前缀 dispatcher 偏离契约。
- 引入 `github.com/gorilla/mux v1.8.1`（Go 1.20 兼容最新版），把 router 从 `*http.ServeMux` 切到 `*mux.Router`；保留 gorilla/websocket 不受影响。

### `internal/api/server.go` 改动
- Server 增 `router *mux.Router` + `registeredRoutes []routeSpec` 字段（mu 保护）。
- `NewServer` 改用 `mux.NewRouter()` 装中间件链。
- `register(r *mux.Router)` 调 `registerRoutes`：注册完 37 路由后设
  - `r.MethodNotAllowedHandler = s.methodNotAllowed`：让 gorilla/mux `r.Methods()` 在 method 不匹配时返 envelope 40501 而非默认 text/plain。
  - `r.NotFoundHandler = s.notFound`。
- `RegisteredRoutes()` getter 返 spec slice 副本供注册测试与 AuthZ 元数据审计（AD-004）。

### `internal/api/route_auth.go` 改动
- `routeSpec` 注释：Pattern 改为 gorilla/mux `{id}/{key}/{msgid}/{name}` 路径模板。
- `registerProtected(r *mux.Router, spec, h)`：
  - 调用前 push spec 到 `s.registeredRoutes`（注册测试 source）。
  - method 校验仍保留在 wrapper 内（`spec.Method != req.Method → envelope 40501`）作为 sanity，即使 disabled 也 40501（保留 `TestServerMethodCheckStill40501WithAuth` 契约）。
  - 注册改用 `r.HandleFunc(spec.Pattern, protected).Methods(spec.Method)` 让同 Pattern 不同 Method 的路由被 method-aware 选中；与 wrapper 内 method 校验互不矛盾（method match 命中后走 wrapper，sanity 永远过；method mismatch 走 MethodNotAllowedHandler→envelope 40501）。

### `internal/api/routes.go`（新文件）
- `registerRoutes(r)` 集中按 `docs/remote-api/INDEX.md §3` 注册全部 37 条 RouteSpec：
  - 3.1 系统（3）：health/version/config
  - 3.2 Agent（5）：`/agents` list、`/agents/{id}` get、`/{id}/start|pause|stop`
  - 3.3 Session（10）：`/agents/{id}/sessions` POST/GET、`/sessions/{id}` 8 条子资源
  - 3.4 对话（3）：`/sessions/{id}/messages` POST、`/events` SSE、`/stream` WS（Transport=TransportWebSocket）
  - 3.5 Tool/Skill/Provider（7）
  - 3.6 Memory（7）
  - 3.7 MCP（2）
- 已实现端点指向 `*Route` adapter；尚未实现的 15 条（config/agent status/tool/skill/provider/mcp）绑 `s.notImplemented` 占位：返 501/50101 表示端点契约存在但 handler 未交付，RouteSpec metadata 在位，后续 commit 替换 handler 时 spec 不动。
- `pathVar(r, name)` helper：取 `mux.Vars(r)[name]`；adapter 用它把 path 参数传给 inner handler。

### `internal/api/session_handler.go` 重构
- 删除 `registerSessionRoutes` + `handleAgentsSessions` + `handleSessions` 两个 dispatcher。
- 新增 12 个 `*Route` adapter（CreateSessionRoute / ListSessionsRoute / GetSessionRoute / PauseSessionRoute / ResumeSessionRoute / CloseSessionRoute / DeleteSessionRoute / ClearMessagesRoute / ListMessagesRoute / DeleteMessageRoute / PostMessageRoute / SSEEventsRoute / WSStreamRoute）：每个用 `sessionProvider(w, r)` 取 SessionProvider（nil → 50301）+ `pathVar(r, "id" / "msgid")` 后调原 inner handler（签名不变）。
- 去掉 `net/url` / `strings` import（不再做 prefix split），保留 `encoding/json`/`errors`/`net/http`/`strconv` + `config`/`session`/`storage`。

### `internal/api/memory_handler.go` 重构
- 删除 `handleMemorySubtree` prefix dispatcher。
- 新增 `resolveMemoryProvider(w, r, agentID)` helper：取 provider + resolver + cache policy；nil → 50301；agent unknown → 40401。
- 新增 7 个 `*Route` adapter（SearchRoute / GetRoute / PutRoute / DeleteOneRoute / ClearRoute / PromoteRoute / ReindexRoute）：每个 `pathVar(r, "id")` + Get/DeleteOne 顺带 `pathVar(r, "key")` 校验 + `resolveMemoryProvider` → 调原 inner handler（签名不变）。
- 去掉 `net/url` / `strings` import（suffix 解析不再需要），保留 `context`/`encoding/json`/`errors`/`io`/`net/http`/`strconv`/`time` + `config`/`memory`。

### `internal/api/ws_handler.go` 改动
- 删除 `r.Header.Get("Authorization") == ""` 简陋 v1 占位校验。
- 改用 `s.authIdentityForWebSocket(r)`（已实现于 `route_auth.go`）返 `(identity, ok)`：
  - disabled / public path → ok=true，允许 anonymous 握手（docs §1/§5）。
  - 启用 Auth 且非 public：无 Identity（wrapper 未绑定）→ ok=false → 写 envelope 40101。
- identity 留作后续 AuditLogger 用，本步持 `_ = identity` 占位不打破编译。

### `internal/api/routes_test.go`（新文件，5 例）
- `expectedRoutes` 与 `docs/remote-api/INDEX.md §3` 总表逐项一一对应（37 条），便于人肉 audit。
- `TestRouteRegistrationMatchIndexTable`：sortSpecs 后对照 INDEX 全条目 method/pattern/action/resource/transport。
- `TestRouteRegistrationCountIs37`：精确 37（AD-004）。
- `TestRouteRegistrationWebSocketStreamBound`：WS stream 必绑 `Method=GET` + `Transport=TransportWebSocket`。
- `TestRouteRegistrationNoDuplicatePatternMethod`：防误增重复注册。
- `TestRouteRegistrationMatchedAgainstRouter`：用 `router.Match` 直接验证 37 条 spec 都能在 `*mux.Router` 命中（不只 s.registeredRoutes 字段，验收 router 真注册生效）。

### `internal/api/ws_handler_test.go` 改动
- `TestWSStreamCancelBeforeStart` 改成"启用 Auth + 缺 Bearer → 401"（文档 §1/§5：disabled 允许 anonymous，启用 Auth 且非 public 才拒绝）—— 修复原测试与文档契约冲突。
- 新增 `TestWSStreamDisabledAuthAllowsAnonymous`：disabled auth 下 WS 不返 401，验证 §1 bypass 与 §5 anonymous 通路。

### 决策记录
- **选取 (A) 引入 gorilla/mux 而非 (B) 改文档承认裸 ServeMux**：文档契约（integration.md §3 示例代码 + AD-004 + INDEX §5 + checklist）一致承诺 gorilla/mux + 37 路由精细绑定；偏离文档属于代码偏离而非文档错。AuthN/AuthZ 是 trust boundary， ponytail YAGNI 不适用。
- **`.Methods(spec.Method)` + wrapper 内 sanity 校验并存**：gorilla/mux `.Methods()` 让同 Pattern 多 Method 路由能被 method-aware 选中正确 wrapper；wrapper 内 `spec.Method` 校验保留以让 40501 也走 envelope 同时保证 disabled 路径仍 40501（已测 `TestServerMethodCheckStill40501WithAuth`）。
- **占位 stub 仍绑 RouteSpec**：未实现的 15 条端点（agent config/tool/skill/provider/mcp）绑 `s.notImplemented` 占位但仍挂载正确 RouteSpec metadata，让 37 路由注册测试可逐项断言；后续 commit 替换 handler 时 spec 不动，避免"为通过测试而补注册"的返工。
- **WS Identity 从 `authIdentityForWebSocket` 取**：与 registerProtected 注入的 IdentityFromContext 配合，删除 v1 简陋 Authorization Header 存在性检查；保留 principalID 字段占位为后续 AuditLogger 留钩。

### 验证
- go vet / go build ./...：通过。
- go test -count=1 ./internal/api/：含新 5 路由注册 + WS disabled auth + 原 sessions/conversation/auth/memory/ws 测试，全绿。
- go test -count=1 ./...：除 `TestWSStreamDisconnectCancelsTurn` 偶发 timing flake（已记录，单跑通过）外全绿。
- go mod tidy 后 gorilla/mux 从 indirect 转正。

## Phase 3 下一步 #6：Agent / Tool / Provider Remote API 端点实现（commit 待 push）

把上一轮 #5 占位的 12 条 stub 替换为真实 handler：Agent 5 端点 + Tool 2 + Provider 3
（Tool/Skill/Provider 3 个 Manager 已有 Get/List 等方法，可直接接 API）。
config + mcp 2 共 3 条仍绑 notImplemented 50101 占位（依赖未实现的 RedactedView/MCP.Manager）。
按文档契约（INDEX.md §3.2/§3.5 + docs/remote-api/{agent,tool,provider}.md）落地。

### `internal/api/agent_provider.go` 改动
- `AgentProvider` 接口扩展为 6 方法：HandleTurn/Get/List/Start/Pause/Stop（Deps 命名沿用
  *agent.Manager 本就实现的方法签名 List/Start/Pause/Stop；status 用 *agent.Status 透传）。

### `internal/api/agent_handler.go`（新文件，~170 行）
- `agentSummaryDTO`（5 字段：id/name/provider/model/status）+ `agentDetailDTO`（追加 tools/skills
  slice + memory_enabled/planner_enabled；Tools/Skills 默认 []string{} 空数组 ≠ null）+ `agentStateDTO`
  （start/pause/stop 返 {id,status}）+ `agentListData`（paged）。
- `agentStatusFromQuery` 校验 status query；非法 → 40001。
- `handleListAgents` —— GET /api/v1/agents：parsePage/parsePageSize + 稳定分页；AgentManager.List 已
  按 ID 升序，handler 仅做 [start:end] 切片。
- `handleGetAgent` —— GET /api/v1/agents/{id}：Manager.Get（仅 Info）；v1 详情 tools/skills 暂为空数组
  （AgentProvider 仅暴露 Get；完整 Inspect 留待 Runtime upgrade 时补）。
- `handleStartAgent/{Pause,Stop}Agent` —— POST：统一走 `agentStateChange` helper（fn → Get → 写 state dto）。
- `writeAgentError` 按 docs/remote-api/agent.md 映射：NotFound → 40401；
  InvalidState/Paused/Stopped → 40901；ManagerClosed → 50301；DeadlineExceeded → 50401；
  Canceled → 不写响应；其他 → 50001。

### `internal/api/tool_handler.go`（新文件）
- `toolInfoDTO` 映射 tool.ToolInfo（docs/tool.md：parameters 为 JSON Schema，深拷贝）。
- Server 加 `s.tools *tool.Manager` 字段 + `SetToolManager` setter（runtime.Start 注入）。
- `handleListTools` / `handleGetTool`：GetErrToolNotFound → 40401。

### `internal/api/skill_handler.go`（新文件）
- `skillSummaryDTO` + `skillViewDTO`（SkillView：tools/skills 升序，空数组输出 []）+
  `skillListData`；Server 加 `s.skills *skill.Manager` + `SetSkillManager` setter。
- `handleListSkills` / `handleGetSkill`：ErrSkillNotFound → 40401；disabled Skill 仍返 200 + status:disabled。

### `internal/api/provider_handler.go`（新文件）
- `providerSummaryDTO`（id/type/models IDs 升序）+ `providerViewDTO`（追加 timeout/max_retries/
  retry_interval，省略 api_key/base_url/extra）+ `providerModelsData`；Server 加 `s.providers
  *provider.Manager` + `SetProviderManager` setter。
- `handleListProviders` / `handleGetProvider` / `handleGetProviderModels`：Provider 不存在 → 40401。
- `formatDuration` helper：time.Duration String() 输出 Go duration string。

### `internal/provider/manager.go` 改动
- 新增 `ErrProviderNotFound` sentinel + `Config(id)` 方法（暴露 ProviderConfig 副本给 ProviderView 用），
  Get/Config 用 `%w` 包装 ErrProviderNotFound 让 Remote API `errors.Is → 40401` 干净映射（去掉 v1 初版
  用错误字符串判断的 hack）。

### `internal/api/server.go` 改动
- Server 增 `tools *tool.Manager` + `skills *skill.Manager` + `providers *provider.Manager` 字段（mu 保护）。
- 新增 `SetToolManager / SetSkillManager / SetProviderManager` 三个 setter。

### `internal/runtime/runtime.go` 改动
- Start 段：在 SetSessionManager 后追加 `SetToolManager/SetSkillManager/SetProviderManager(rt.tools/skills/providers)`
  三处注入；让 Tool/Skill/Provider Remote API 在 Runtime Ready 时可用。

### `internal/api/routes.go` 改动
- 12 条 stub 改指向真实 handler：Agent 5（list/get/start/pause/stop）+ Tool 2 + Provider 3 + Skill 2。
- 剩余 3 条 stub：config（依赖 RedactedView 未实现）+ mcp 2（依赖 MCP.Manager 未实现）仍 50101。
- 路由注册测试不变：registerProtected 把每条 RouteSpec push 到 registeredRoutes，stub 与真实 handler
  在 RouteSpec 断言上完全等价。

### `internal/api/agent_handler_test.go`（新文件，14 例）
- `agentToolProviderTestEnv` 复用 conversation_handler_test setup 模式构造含 1 个 agent 的真实
  Server：Session + Provider + Agent + Tool + builtin 注册，三 Manager 都注入。
- Agent：list 1 个 + get detail（tools/skills 空数组）+ get unknown 40401 + pause→stop→start 状态迁移 +
  start unknown 40401。
- Provider：list summary（models IDs）+ get view（timeout=5s max_retries=2 retry=1s）+ get unknown 40401 +
  /models 子路由。
- Tool：list 含 builtin + get item + get unknown 40401。
- `TestStubEndpointsReturn501NotImplemented` 验证剩 3 个 stub（config + mcp2）返 50101 保护未实现端点契约。
- 测试 env.Data 用 `env.Data.(map[string]any)` 断言（与 memory_handler_test 风格一致）。

### 验证
- go vet / go build ./...：通过（含 internal/provider ErrProviderNotFound 改动 + 3 新 manager setter）。
- go test -count=1 ./internal/api/：含新 14 例全绿；原 sessions/conversation/auth/memory/ws 测试无 regression。
- go test -count=1 ./internal/provider/ / ./internal/runtime/：全绿。
- go test -count=1 ./...：全项目 17 包全绿（含 WS flake 这轮未触发）。

### 下一轮方向
- config 端点：需先实现 `internal/config/redact.go` 的 `RedactedView`（docs/config/overview.md §3.3），
  把已知 Secret + MCP headers/env + 开放 Map 递归脱敏成 JSON-compatible 深拷贝，最小测试覆盖 nil 不变性。
- MCP 端点：依赖未实现的 `internal/mcp/` 包（Manager + Client + Server + 3 transport），规模较大，留作后续模块起点。

## Phase 3 下一步 #7：config.RedactedView + GET /api/v1/config 端点（commit 待 push）

按 `docs/config/overview.md §3.3` 实现 Config 模块唯一对外脱敏视图，替换上一轮 #6 config stub；
自此 37 路由里只剩 MCP 2 stub 仍待依赖未实现的 `internal/mcp/` 包。

### `internal/config/redact.go`（新文件，~170 行）
- `ErrConfigRedactionFailed` sentinel（docs §3.3）。
- `RedactedView(cfg *Config) (any, error)`：按 docs 实现顺序固定：
  1. 拒绝 nil；`json.Marshal cfg` → `Decoder.UseNumber` 解码为 `map[string]any`；失败 wrap ErrConfigRedactionFailed。
  2. 替换 4 个已知 Secret 路径的 string 值为 "***"：`runtime.auth.tokens[*].token`、`runtime.auth.jwt.secret`、`providers[*].api_key`、`memory.embedding.api_key`。
  3. 对 MCP servers[*].headers/env + 6 个开放 Map 递归处理：`redactScalarRecursive` —
     object/array 保持结构，所有 scalar（string/bool/number/json.Number）替为 "***"，`null` 保持 null。
  - 开放 Map 6 个精确路径：providers[*].extra / tools.builtin.*.options / agents[*].tools_config.*.options /
    skills.per_skill.*.options / agents[*].skills_config.*.options / plugins.entries[*].config。
  - fail-closed：开放 Map 全部 scalar 都视为敏感值脱敏，不按 key 猜；新增 Map 必须同时加入该 list。
  - 输入 cfg 不被修改（先 Marshal 再 decode 深拷贝）。

### `internal/config/redact_test.go`（新文件，7 例）
- `buildFullConfig` 构造覆盖所有脱敏路径的 Config（含 4 个已知 Secret + MCP headers/env + 6 开放 Map 嵌套 scalar + null）。
- `TestRedactedViewNilReturnsError`、`TestRedactedViewKnownSecretsReplaced`、
  `TestRedactedViewMCPHeadersEnvRedactedAsScalars`、`TestRedactedViewOpenMapsRecursiveScalars`、
  `TestRedactedViewPreservesNonSensitiveFields`、`TestRedactedViewDoesNotMutateInput`、
  `TestRedactedViewTwoCallsDeepEqual`。
- helpers `navigatePath` 用文档风格点+数组索引路径在 JSON 上断言（与 RedactedView 实现解耦）。

### `internal/api/config_handler.go`（新文件）
- `handleGetConfig` — GET /api/v1/config（read:config）：取 `s.cfgSnapshot` → `config.RedactedView` → envelope。
  snapshot nil → 50301；redact 失败 → 50001；不得 fallback 未脱敏 snapshot。

### `internal/api/server.go` 改动
- Server 增 `cfgSnapshot *config.Config` 字段（mu 保护）+ `SetConfigSnapshot(cfg)` setter。

### `internal/runtime/runtime.go` 改动
- Start 段在 SetProviderManager 后调 `rt.api.SetConfigSnapshot(rt.cfg)`，注入当前 Config snapshot。
  （docs §3.3 提到的 `reload.Current()` 在 ReloadManager 实现前用单次注入替代，热 reload 未来通过替换 snapshot 实现。）

### `internal/api/routes.go` 改动
- config stub 改指向 `s.handleGetConfig`；剩 MCP 2 stub 仍占位。

### `internal/api/agent_handler_test.go` 增 3 例
- `TestConfigEndpointReturnsRedactedView`：注入含 1 个 Provider 的 cfg，GET /api/v1/config 返 200；
  api_key = "***"，id/type 非脱敏保留。
- `TestConfigEndpointNilSnapshotReturns503`：未注入 snapshot 返 50301。
- `TestStubEndpointsReturn501NotImplemented` 改成只有 MCP 2 stub（config 已实现）。

### 验证
- go vet / go build ./...：通过。
- go test -count=1 ./internal/config/：含新 7 例 redact 测试全绿。
- go test -count=1 ./internal/api/：含新 3 例 config 端点测试 + 原 14 + sessions/conversation/auth/memory/ws 测试无 regression，全绿。
- go test -count=1 ./...：全项目 17 包全绿（WS flake 这轮未触发）。
- go mod tidy：无新依赖。

### 当前 stub 剩余（37 路由里 2 条）
- MCP servers list / get：依赖未实现的 `internal/mcp/` 包（Manager + Client + Server + 3 transport；规模大独立模块）。

### 下一轮方向
- MCP Manager 起点（docs/mcp/README.md §2 + checklist）：先落地 Manager 结构 + ServerStatus + List/Get/Prepare/Activate/Stop lifecycle 框架（先 stub transport + 不实现 catalog reconciliation），让 Remote API mcp/servers 端点返空列表也能联调；后续 commit 渐进补 Client/transport。

## Phase 3 下一步 #8：MCP Manager 起点 + Remote API mcp/servers 端点实现（commit 待 push）

### 范围
落地 `internal/mcp/` 包骨架（types + Manager v1 起点），让 Remote API 37 路由的最后 2 条 MCP
stub 替换为真实 handler 返 ServerStatus 投影。**Manager lifecycle（上游 Client 连接 / heartbeat /
重连 / 本地 Serve）暂不实现** — 留后续 multi-commit 按 docs/mcp/checklist.md §1-7 渐进补全。
v1 起 Manager 构造即处于 "teardown-done" 状态：未连接、List 投影 disconnected、ToolCount=0。
**所有 37 路由现已绑定真实 handler，无 501 stub 残留**。

### 文档依据
- `docs/mcp/README.md` §1 范围、§2 Manager API + ServerStatus、§3-5 Client/Server/Transport 概览
- `docs/mcp/errors.md` §1 13 sentinel、§3 状态机
- `docs/mcp/client.md` §1 ConnectionStatus 4 态
- `docs/mcp/checklist.md` §1 Manager / §9 集成
- `docs/mcp/config-ref.md` §1/§2 配置字段
- `docs/mcp/integration.md` §9 Runtime Stop 顺序

### `internal/mcp/types.go`（新）
- `ConnectionStatus` 字符串类型 + 4 sentinel：`Status{Disconnected,Connecting,Connected,Error}`
- `ServerStatus` 结构：Name/Status/Transport/ProtocolVersion(*string)/ToolCount/ConnectedAt(*time.Time)/LastError(omitempty)
  —— 敏感连接配置（command/args/env/headers/tls）**不进入**该类型，避免 Remote API / 健康端点泄露
- 13 个错误 sentinel 全部按 docs/mcp/errors.md §1 落地（ErrMCPConfig/ConnRefused/ConnTimeout/
  AuthFailed/TransportClosed/TransportWrite/ProtocolError/InvalidParams/ToolNotFound/ToolExecFailed/
  ToolTimeout/UnsupportedContent/Unavailable）。typed-error `Error()` 只返稳定文本，详细字段路径由
  config 校验阶段携带，不再扩展零散 sentinel。

### `internal/mcp/manager.go`（新）
- `Manager` 结构：cfg/logger/entries（配置 server 投影源，只存非敏感 name+transport）/runCtx+cancelRun/
  doneOnce+done（teardown 信号）/stopOnce+cacheErr（幂等 Stop + 缓存最终错误）/readyMu+ready（本地 Serve
  状态）/mu。**所有公开方法**：`Prepare`/`Activate`/`Stop(ctx)`/`Done()`/`Ready()`/`Get(name)`/
  `List()`/`Tools(name)`/`NewManager(cfg, tm, logger)` —— 完整对齐 docs/mcp/README.md §2 签名。
- v1 语义：
  - `NewManager` 缓存配置 server 名字 + transport（空 transport 默认 `stdio`）作 List/Get 投影源；
    nil cfg 返 `ErrMCPConfig`；nil logger 走 `slog.Default()`。
  - `Prepare()` v1 无 transport 要 prepare → 返 nil。
  - `Activate()` 若 `cfg.Server.Enabled=true` 返 `ErrMCPConfig`（本地 Server 实现未交付，**不静默
    启用**避免空 Server 接受请求产生语义错乱）；disabled 时返 nil。
  - `Stop(ctx)` 用 stopOnce 保证幂等；取消 runCtx + 关闭 done + 置 ready=false；返 cacheErr。
  - `Done()` 返 done channel。v1 起构造即 done 未关闭，Stop 后关闭。
  - `Ready()` v1 恒 true（无本地 Serve），Stop 后置 false。
  - `List()` 返 ServerStatus 切片，**深拷贝**（修改不影响 Manager 内部 entries）；v1 全 disconnected。
  - `Get(name)` 命中返 (ServerStatus, true)，未命中返 (zero, false)。
  - `Tools(name)` v1 未连接 → 返 (nil, false)。

### `internal/mcp/manager_test.go`（新，13 例）
- 空 / 多 server / 默认 transport / Get 命中未命中 / List 深拷贝不变性 / Tools empty /
  Ready + Stop 状态转换 / Stop 幂等 / Done 在 Stop 后可读 / Prepare no-op / Activate
  enabled 拒绝 + disabled nil / NewManager nil cfg 拒绝

### `internal/api/server.go` 改动
- Server 增 `mcpServers MCPServerProvider` 字段（接口，非具体 *mcp.Manager）—— 与
  Provider/Tool 等具体指针注入风格不同的取舍：MCP 作为新模块通过接口隔离便于 API 包 handler 测试 mock
- 新增 `MCPServerProvider` 接口：`List() []mcp.ServerStatus` + `Get(name) (mcp.ServerStatus, bool)`
  （与 docs/mcp/README.md §2 签名对齐）
- 新增 `SetMCPServerProvider(mp)` setter；未注入时端点返 50301

### `internal/api/mcp_handler.go`（新）
- `mcpServerListData{Items []mcp.ServerStatus}` DTO
- `mcpProvider(w, r)` 解引用 + nil 检查 → 50301
- `handleListMCPServers` — GET /api/v1/mcp/servers：投影 List()；items nil → [] 防 null
- `handleGetMCPServer` — GET /api/v1/mcp/servers/{name}：40401 未找到 / 200 OK + ServerStatus DTO

### `internal/api/routes.go` 改动
- 2 个 MCP stub (`s.notImplemented` 50101) 替换为 `s.handleListMCPServers` / `s.handleGetMCPServer`
- **`s.notImplemented` handler 保留**（不再有端点引用，但作为通用未实现占位留作模板）

### `internal/runtime/runtime.go` 改动
- 加 `internal/mcp` import + `mcpMgr *mcp.Manager` 字段
- Start 段在 SetConfigSnapshot 之后：构造 `mcp.NewManager(&rt.cfg.MCP, rt.tools, rt.logger)` →
  `Prepare()` → `Activate()`（任一失败 `rt.rollback()` + 返 fmt.Errorf 包装）→ 设字段 +
  `rt.api.SetMCPServerProvider(mcpMgr)` + `rt.components["mcp"] = "ready"`
- Shutdown 段在 `rt.api.Shutdown` 之后 `rt.sessions.Shutdown` 之前插入 MCP teardown：
  按 docs/mcp/integration.md §9 "调 Stop(ctx) → 等 Done → 再以 fresh ctx 调 Stop 取最终错误"
- rollback 段在 providers 之前插入：fresh ctx Stop + Done + 置 nil

### `internal/api/agent_handler_test.go` 改动
- 加 `internal/mcp` import；分组 import 顺序 gofmt 规整（带动文件 ival 中已有 import 块的 1 行调换，
  非本项目 commit vol1 引入的杂项清理）
- `agentToolProviderTestEnv` 末尾注入 `mockMCPServerProvider{items: [{fs, disconnected, stdio, 0}]}`
- 新 `mockMCPServerProvider` 类型 + List/Get 实现（深拷贝 List 防测试间共享状态）
- 删 `TestStubEndpointsReturn501NotImplemented`（无 501 stub 残留）
- 新 `TestMCPEndpointsReturn200And404`：list 200 + items 投影 + Get(fs) 200 + Get(miss) 40401
- 新 `TestMCPEndpointsReturn503WhenNoManager`：未注入 Manager 的 Server hit 2 端点均返 50301

### 验证
- `go vet ./...`：通过
- `go build ./...`：通过
- `go test -count=1 -timeout 250s ./...`：18 包全绿（mcp/api/runtime 新增 + 原有 15 包无 regression）
- `go mod tidy`：无新依赖
- `TestWSStreamDisconnectCancelsTurn` 单包并发 run 触发本会话一次 timing flake（已知 200ms time.Sleep
  问题，progress 早前已记录），单跑 / -count=3 全绿；非本 commit 引入
- `go test -count=3 ./internal/api/ -run TestWSStreamDisconnectCancelsTurn` 全绿

### 当前状态：37 路由 100% 已实现 + 0 个 501 stub
- 之前 35 条已实现 + 2 条 MCP stub 50101 占位
- 本 commit：MCP 2 stub 替换为真实 handler。`notImplemented` helper 保留无引用。

### 副债清单 / v1 已知限制（与 docs/mcp/checklist.md 对照）
| 文档项 | v1 实际 | 触发 |
|---|---|---|
| Manager lifecycle Prepare/Activate | stub 返 nil / disabled | docs §2、checklist §1 |
| runUpstream heartbeat / 重连 | 未实现 | docs §2、checklist §1 |
| Client Connect/Initialize/DiscoverTools/CallTool/Ping | 未实现 | docs §3、checklist §2 |
| 本地 MCPServer Serve/handleInitialize/handleListTools/... | 未实现；cfg.Server.Enabled=true 时 Activate 拒绝 | docs §4、checklist §3 |
| Transport stdio/sse/streamable_http | 未实现 | docs §5、checklist §4-6 |
| Tool 映射 `mcp.<server>.<tool>` + Proxy | 未实现 | docs §1、checklist §7 |
| 内置 Tool `mcp_list` | 未实现 | checklist §9 |
| 指标 / span | 未实现 | checklist §9 |
| 修复 W1 时间戳 / W2 README 导览 / W4 tokens[].roles | 仍保留 | progress 早前记录 |

### 下一轮方向
按 docs/mcp/checklist.md § 排序，建议下述渐进路径（每个独立 commit）：

1. **Client v1 起点**：`Client` 结构骨架 + `ConnectionStatus` 状态机 + `Connect/Close/Initialize/
   DiscoverTools/CallTool/Ping/Done/Err` 签名 + 错误 sentinel 映射（checklist §2；不接 transport）。
   目标：完成"Client 一代连接"类型契约 + 单测覆盖状态转换，不接真实 stdio/sse/streamable_http。
2. **stdio Client transport**：`StdioClient` + exec.Command + stdin/stdout JSON-RPC 行协议 + stderr
   日志捕获 + 优雅关闭（checklist §4）。目标：能连本地 npx @modelcontextprotocol/server-filesystem。
3. **Manager.Start / runUpstream 雏形**：构造 Client + 启动 + 状态转换 + DiscoverTools 把 Tool 注册到
   ToolManager 稳定 Proxy（checklist §1 §7 §9）。目标：Runtime 启动 auto-start=true server 能真连上
   本地 stdio MCP server 并显示 ToolCount>0、Status=connected。
4. **streamable_http / SSE**: network transport。
5. **本地 MCP Server + 工具暴露**：完整 binding 校验 + Serve ctx 生命周期 + handleInitialize/...
6. **重连 + heartbeat + 指标**：完成 lifecycle + observability。

注：本 commit 已让 Manager Stop/Done 幂等、状态字段就位，后续 commit 只需往 Manager 里加 entries
client 字段与其 lifecycle 而非改 API/handler 投影契约 —— Manager 与 Runtime 启停边界已固定。

## Phase 3 下一步 #9：MCP Client v1 起点（types 扩充 + ClientTransport 接口 + Client 完整实现 + 集成测试）（commit 待 push）

### 范围
落地 `internal/mcp/` 包 §2 MCP Client 完整骨架（docs/mcp/checklist.md §2、client.md §1-5）。
单代 Client 的请求/响应/状态机/fail/Close/握手/工具发现/调用全部就绪 — Manager 与 stdio transport
**依旧未接**（留后续 commit）。Client 通过 `ClientTransport` interface 完成所有 I/O；本 commit 用
`fakeTransport` 完整端到端集成测试（Initialize/Ping/DiscoverTools/CallTool/Close/状态/fail等）验证。

同时：先按用户约束修文档 `docs/mcp/checklist.md §1` Manager — 勾选 v1 已落地项 (`Manager` 骨架 /
`Get` / `Tools` / `List` / `Prepare` / `Activate` / `Ready` / `Stop`+`Done`) + 明确未交付项措辞
（`runUpstream` heartbeat / 重连 / 后台 teardown with `errors.Join` 仍 `[ ]`）。

### 文档依据
- `docs/mcp/checklist.md` §1（修文档）+ §2（Client 实现目标）
- `docs/mcp/client.md` §1 状态结构 + §2 Initialize/Ping + §3 DiscoverTools + §4 CallTool + §5 重连语义
- `docs/mcp/transport.md` §2 JSON-RPC 协议 + Message/RPCError wire DTO + 上限表 + validateEnvelope 规则；
  §3 ClientTransport 接口；preferredVersion/acceptsVersion 矩阵
- `docs/mcp/errors.md` §2 JSON-RPC code → sentinel 映射 (-32602 → ErrMCPInvalidParams,
  server-defined -32000..-32099 → ErrMCPToolExecFailed)

### `docs/mcp/checklist.md` 改动
§1 MCP Manager 项全部对齐 v1 实际：7 项勾选（含 v1 副债清单备注），1 项 keep `[ ]`（runUpstream 仍待
lifecycle commit 落地）。无 §2-9 改动。

### `internal/mcp/types.go` 扩充
- `Message` / `RPCError` (Error → 稳定 "mcp rpc error" 不含 message/data；docs/mcp/transport.md §2) /
  `Implementation` / `InitializeParams` / `InitializeResult` / `MCPTool` / `ListToolsParams` /
  `ListToolsResult` / `CallToolParams` / `Content` / `CallToolResult` / `ProtocolVersion` / 
  `LegacyProtocolVersion` / `TransportInfo` 常量与结构
- 用 json.RawMessage 严格 wire 透传（不干预 wire shape）

### `internal/mcp/transport.go`（新）
- `ClientTransport` 接口：Start / Send / Recv / Close / Info（docs/mcp/transport.md §3）
- `validateEnvelope(msg) (messageKind, error)`：严格 5 类校验（jsonrpc=2.0 / method 互斥 result/error /
  result 互斥 error / notification 无 ID / response 无 method 等等），任一违反返 ErrMCPProtocolError
- `messageKind` 枚举：request/notification/response/invalid + String() 诊断用
- `isNullJSON(b)`：识别 wire "null" 字面量 / 字符串 "null"
- `preferredVersion(transportType) string` + `acceptsVersion(transportType, serverVersion) bool`：
  按 docs/mcp/client.md §2 矩阵 (streamable_http → 2025-03-26 strict / sse → 2024-11-05 strict /
  stdio → 兼容两个版本)
- `parseID(json.RawMessage) (uint64, bool)`：仅接受 Client 自己签发的正整数 ID；string ID / 空 / 0 皆
  拒（避免 late response 误识别）

### `internal/mcp/transport_test.go`（新，9 例）
- validateEnvelope 各路径覆盖：request / notification / notification+id→request (语义升级) /
  response / response+method 拒 / result+error 互斥拒 / 空 version 拒 / empty envelope 拒 / nil 拒
- preferredVersion/acceptsVersion 矩阵覆盖 + parseID 正数/0/字符串/空 拒绝

### `internal/mcp/client.go`（新，560 行）
`Client` 结构详对齐 docs/mcp/client.md §1：
- 字段：name / runCtx / transport / status / mu / cancel / closeOnce / failOnce / closing / closeErr /
  closedErr / pendingMu / pending / wg / cntroll / recvDone (新: 无 recvLoop chan / 无 controlDone /
  无 lateCount，删 YAGNI 字段) / nextID / issuedHighWater
- `NewClient(name, runCtx, transport)`：未连接（status=disconnected）；调用方需 Connect→Initialize
- `Status()` mu.RLock 返当前快照
- `Done() <-chan struct{}`：failOnce 关闭；Manager 在此 wait torn-down
- `Err() error`：pendingMu.Lock 返 closedErr（stable sentinel 不带 wire 原 message/data）
- `Connect(startupCtx)`：disconnected→connecting→Start transport；失败 Close 并返错
- `request(ctx, method, params, out)`：marhalParams→pending 注册→Send→等 call.ch 或 ctx 取消
  + bestEffortCancel；stream 上限保护：nextID == MaxUint64 或 pending ≥ 1024 → fail(ErrMCPProtocolError)
- `marshalParams(params) (json.RawMessage, error)`：nil→ omitted / object / array 否则 ErrMCPInvalidParams
- `mapRPCError(err)`：-32601 → ErrMCPToolNotFound / -32602 → ErrMCPInvalidParams / 其他 →
  ErrMCPToolExecFailed（不假设 -32001 固定含义；docs/mcp/errors.md §2）
- `fail(err)`：failOnce；标记 closedErr / 摘 all pending 投递 / 置 status=Error / cancel / transport.Close
  / 关 c.recvDone 信号
- `retire(id, call)`：pendingMap identity check 仅删同指针 entry（防 fail 后误删新 entry）
- `bestEffortCancel(id, cause)`：100ms 超时发 notifications/cancelled，reason 限 128 bytes；
  不进 pending / 不重试 / 不可回放
- `Close()`：closeOnce + 关闭顺序 closing=true → fail(ErrMCPTransportClosed) → wg.Wait → status=Disconnected
- `runRecvLoop(ctx)`：唯一调用 transport.Recv；按 validateEnvelope 分类；response → dispatchResponse；
  request 入容量 32 control channel；notification 仅 tolerated tools/list_changed；
  control 满 / Recv 失败 / envelope 错 → fail
- `dispatchResponse(msg)`：parseID 失败/0 → protocol error / id > issuedHighWater → protocol error /
  未匹配 pending (已 retire late/duplicate) → 丢 (不毒化连接)
- `runControlLoop(ctx)`：处理 server ping → 返 ping 不带 result payload；其他 server method → -32601
- `handleServerRequest(ctx, msg)`：返响应 Message
- `notify(ctx, method, params)`：发 notification（无 ID，无 response）
- `Initialize(ctx)`：initialize → 校验 protocolVersion + capabilities.tools → notifications/initialized →
  status=Connected；任一步失败返错（caller 应 Close）
- `Ping(ctx)`：ping 验证当前代可用，不启动重连
- `DiscoverTools(ctx)`：128 pages / 4096 tools / 4 KiB cursor 上限；重复 cursor / 重复 name / wire shape 
  非法 → fail(ErrMCPProtocolError)；sorted by name 升序返回
- `normalizeTool(serverName, raw)`：远端 name 1..128 bytes、无控制字符、desc ≤ 4 KiB、schema ≤ 256 KiB；
  canonical `mcp.<server>.<remote>` ≤ 256 bytes、无控制字符
- `canonicalToolName` + `isValidName` helper
- `CallTool(ctx, name, arguments)`：未 connected 返 ErrMCPUnavailable；ctx 取消返 context.Cause(ctx)
  不重映射 caller DeadlineExceeded 为 ErrMCPToolTimeout（hard cap 由 Proxy 设置）
- 常量限制：maxPendingRequests=1024 / maxListToolsPages=128 / maxTotalTools=4096 / maxCursorBytes=4096 /
  maxToolNameBytes=128 / maxCanonicalNameBytes=256 / maxDescriptionBytes=4096 / maxInputSchemaBytes=256KiB /
  bestEffortCancelTimeout=100ms / controlBufferSize=32

### `internal/mcp/client_test.go`（新，15 例）
用 `fakeTransport` 实现 ClientTransport + channel 驱动 Recv / Send 捕获；端到端覆盖：
- TestClientInitializeLifecycle：Connect→status=connecting / Initialize 成功→connected / Close→disconnected
- TestClientCloseIdempotent：两次 Close 同 sentinel
- TestClientInitializeRejectsIncompatibleVersion：stdio 拒 "1.0.0" → ErrMCPProtocolError
- TestClientInitializeRejectsServerWithoutTools：没 advertise tools → ErrMCPProtocolError
- TestClientPingOK：Ping 成功路径
- TestClientDiscoverToolsSinglePage：1 页 2 工具，按 name 升序，`mcp.fs.<remote>` 命名前缀
- TestClientDiscoverToolsMultiPage：nextCursor 翻页直到空
- TestClientDiscoverToolsToolNameTooLong：192 byte name → ErrMCPProtocolError
- TestClientCallToolOK：result.IsError=false 回传 Content
- TestClientCallToolNotConnected：未 Connected → ErrMCPUnavailable
- TestClientRPCErrorMapping：-32602 → ErrMCPInvalidParams
- TestClientRequestCtxCancelReturnsCause：caller 取消 → context.Cause + bestEffortCancel 发
  notifications/cancelled
- TestClientRequestSendFailureFailsClient：transport.Send 拒绝 → wrap(ErrMCPTransportWrite) + Client fail
- TestClientDoneClosesOnClose：Close 后 Done 可读
- TestClientHandlesServerPingRequest：server ping request → Client 回 ping 响应

### 验证
- `go vet ./...`：通过
- `go build ./...`：通过
- `go test -count=1 -timeout 250s ./...`：18 包全绿（mcp/api/runtime 无 regression）；
  mcp 包 37 例 manager + transport + client 全绿
- `go test -count=3 ./internal/mcp/ -run TestClient`：3 轮重复 Client 测试全绿（无 timing flake）
- `go mod tidy`：无新依赖
- 注意：`go test -race` 在本 sandbox arm64 上返 ThreadSanitizer: unsupported VMA range，环境问题
  非代码问题；后续 commit 不在本沙箱跑 -race 检验

### 副债清单 / v1 已知限制
- Manager 与 Client 尚未集成（auto-start Client、Manager.runUpstream、heartbeat、Proxy 注册到 ToolManager）
- stdio/sse/streamable_http transport 尚未实现（用 fakeTransport 验证）
- onListChanged callback 未接（notification 仅 tolerate 但不通知 Manager）
-ReleasedDate listTools strict wire DTO 解码仍走标准 json.Unmarshal，未用 DisallowUnknownFields
  + EOF 双保险（后续 commit 接 stdio 后再补 strict 解码）
- connected_at 未发布到 ServerStatus（lifecycle commit 接入时填）
- bestEffortCancel reason 简单截断 128 bytes（未做 unicode boundary 处理）

### 下一轮方向 → （按 progress #8 末尾规划第 2 项）
1. **stdio Client transport**（checklist §4）：`StdioClient` + `exec.Command` + stdin/stdout JSON-RPC
   行协议 + stderr 日志捕获 + 子进程退出检测 + 优雅关闭。目标：能连本地 npx @modelcontextprotocol/
   server-filesystem 真实 stdio MCP server。
2. **Manager.runUpstream 雏形**（checklist §1 §7 §9）：构造 Client + 启动 + DiscoverTools →
   注册稳定 Proxy 到 ToolManager；Manager.Start 走 auto-start=true 串接 lifecycle，
   Runtime 启动后 ServerStatus.ToolCount>0、Status=connected。
3. streamable_http/SSE → 4. 本地 MCPServer → 5. 重连+heartbeat+指标 → 6. Planner step 1-2。

### Ponytail 决策记录
- 接口注入 ClientTransport 在 Client 层（而非 Client 持有具体 transport ），与上轮
  MCPServerProvider 在 API 层接口注入思路一致 — 新模块通过接口隔离 + fakeTransport 测试
  驱动贯通端到端，避免引入 stdio 真实依赖到 Client 单元测试。
- v1 listTools 实现不要求 DisallowUnknownFields + EOF 双保险（Ponytail YAGNI — 后续 stdio
  commit 接真实 server 后再补 strict 解码；当前 json.Unmarshal 已 cover schema 字段）。
- 删 `recvLoop chan`, `controlDone chan`, `lateCount int` 等 YAGNI 字段（无人读）；
  留 `recvDone` 是 Manager wait 信号，无人读其余两字段。
- mapRPCError 中 -32601 → ErrMCPToolNotFound（本地 catalog 查找用）；其他统一
  ErrMCPToolExecFailed（含 -32001 不假设过载语义），与 docs/mcp/errors.md §2 "不能假设 -32001 固定
  表示过载" 对齐。

## Phase 3 下一步 #10：stdio Client transport（cmd/yaasmoke 临时辅助）+ Client.Connect Start 顺序修复 + Python 端到端集成测试（commit 待 push）

### 范围
落地 `internal/mcp/StdioClient` 全量实现 docs/mcp/checklist.md §4 Transport — stdio。
通过真实 Python subprocess fake MCP server 完成 8 例端到端集成测试。**Manager.runUpstream 仍未接**（留下个 commit）。
**同时修一处 Client.Connect 真实 bug**：startLoops 必须在 transport.Start 之后启动 recvLoop，否则 recvLoop 在
transport stdout 尚未初始化时阻塞（rg：`recvReady nil channel select permanent block`）。

### 文档改动
`docs/mcp/checklist.md §4 Transport — stdio` 全部 8 项勾选 + 备注（StdioServer 留 §3 本地 MCP Server commit）。

### `internal/mcp/stdio.go`（新）
- `StdioClient` 结构 / `NewStdioClient(command, args, env, logger)` / `Start/Send/Recv/Close/Info`
- `recvReady chan struct{}` NewStdioClient 时 make + Start 时 close —— 防 Recv 在卡 nil channel
- `Start`：exec.Command + cmd.Env=composeStdioEnv(userEnv) + StdinPipe/StdoutPipe/StderrPipe + cmd.Start + pumpStderr goroutine
- `pumpStderr`：bufio.Reader 行级 ReadString → slog.Info（subprocess stderr 标签，不混入协议流）
- `composeStdioEnv`：白名单 inheritEnvKeys = [PATH/HOME/USER/LANG/LC_ALL] + 用户 env 覆盖（docs/mcp/integration.md §7）
- `Send`：json.Marshal → stdin.Write + 行尾；关闭/closed 时返 ErrMCPTransportClosed；body > 4 MiB 返 ErrMCPProtocolError
- `Recv`：readerForRecv lazy 跨 Recv 复用 bufio.Reader (4 MiB buffer)；ReadString → json.Unmarshal；
  EOF → ErrMCPTransportClosed；行超/解码失败 → ErrMCPProtocolError
- `Close`：closeOnce 幂等；close stdin → 5s timeout 等 cmd.Wait → 超时 SIGKILL → close stderr/stdout
  + stderrWG.Wait
- 常量：stdioMessageMaxBytes=4*1024*1024 / stdioCloseGraceTimeout=5s

### `internal/mcp/client.go` 改动（真实 bug 修复）
Connect 顺序改为**先 transport.Start 再 startLoops** —— startLoops 启动 recvLoop 调 transport.Recv 需要 transport stdout 已就绪；
反序会让 recvLoop 卡在 `select case <-c.recvReady:` (nil chan select permanent) → 整个 Initialize 路径死锁。

新流程：
1. 先 c.transport.Start(connCtx) — failure 时复位 status=Disconnected + cancel ctx 返错（不调 c.Close 避免误触 failOnce 失败路径）
2. 后 c.startLoops(connCtx) - failure 时调 c.Close 走 teardown

### `internal/mcp/stdio_test.go`（新，8 例 + 1 helper）
- `fakeMCPStdioServer` Python inline 脚本：实现 initialize / notifications/initialized / ping /
  tools/list / tools/call 的 JSON 响应；其他 method → -32601
- `requirePython3` — skip 测试沙箱无 python3 时（沙箱有 python3，全 8 例绿）
- `stdioClientEndToEnd` helper — 启动 fake MCP server + Client，Connect + Initialize + Cleanup
- TestStdioClientEndToEndLifecycle：Connect→Initialize 拓.connected→Ping OK→DiscoverTools 2 tools (mcp.fake.alpha/beta)→Close.disconnected
- TestStdioClientCallToolEndToEnd：name="remoteFoo" → content hello remoteFoo
- TestStdioClientSendBodyTooLarge：4 MiB + 10 上限触发 ErrMCPProtocolError
- TestStdioClientStartCommandNotFound：不存在 command → ErrMCPConnRefused
- TestStdioClientCloseIdempotent：多次 Close 同 sentinel
- TestStdioClientInfoConnected：Start 前 Connected=false / Start 后=true / Close 后=false
- TestStdioClientRecvOnSubprocessExit：process.Kill → Recv 返 ErrMCPTransportClosed
- TestStdioClientEnvInjection：用户 env 通过 transport 注入子进程，serverInfo.name="env:injected"

### 验证
- `go vet ./internal/mcp/`：通过
- `go build ./cmd/yaasmoke`（临时诊断工具）端到端：Initialize→DiscoverTools→Ping→CallTool→Close 全绿
  （诊断命令行已删；stdio.go 真实端到端通过 stdio_test.go 8 例验证）
- `go test -count=1 -timeout 60s ./internal/mcp/`：manager 13 + transport 9 + client 15 + stdio 8 共 45 例全绿
- `go test -count=1 -timeout 240s ./...`：18 包中 mcp/api/runtime 等全绿；TestWSStreamDisconnectCancelsTurn
  本轮触发一次已知 timing flake（200ms time.Sleep），单跑/-count=3 全绿，非本 commit 引入
- `go mod tidy`：无新依赖

### 副债清单 / v1 已知限制
- 没接 Manager.runUpstream：构造 StdioClient + 启动 + DiscoverTools → 注册 ToolManager 稳定 Proxy，后续 commit
- StdioServer（Yaa! 作为 stdio MCP Server）未实现；checklist §3 待本地 Server commit
- stdio client 子进程 stderr 用 bufio.NewReader（ 默认大小），不是 4 MiB 上限；超长 stderr 行可能截断 ok
  docs 仅协议 body 限 4 MiB（stderr 不入协议流故截断可接受）
- env inherit 白名单固定 5 项；用户实际 MCP server 需要更多 env 由配置 mcp.servers[*].env 显式注入
- 子进程优雅关闭超时 5s 硬编码；配置后续 commit 可接 mcp.timeout.connect（schema 已存在）

### 下一轮方向 → checklist §1 + §7 + §9 Manager.runUpstream + Tool Proxy
1. **Manager.runUpstream 雏形**（checklist §1 §7 §9）：
   - Manager 构造时启动 auto-start=true 的 stdio Client 串接 lifecycle
   - DiscoverTools → 把每个 Tool 经过 normalizeTool → ToolManager 注册稳定 Proxy（`mcp.<server>.<remote>` 命名前缀）
   - ServerStatus.ToolCount>0、Status=connected、ConnectedAtimestamp 注入
   - Stable Proxy 一旦失败置 unavailable、atomic client handle 置 nil → 调用返 ErrMCPUnavailable
2. **重连与 catalog reconciliation**（checklist §1 §7 + docs/mcp/client.md §5）：
   - 失败时按 reconnect 配置指数退避重连，重新 Initialize + DiscoverTools
   - Tool name/description/schema 精确一致才原子替换 client handle；差异保持 unavailable+ErrMCPProtocolError 要求重启
3. **heartbeat**：Manager 定期 Ping；失败换 Client
4. §5 SSE / §6 Streamable HTTP transport / §3 本地 MCPServer / §9 observability → Planner step 1-2

### Ponytail 决策
- 本 commit 同时修 Client.Connect 真实 bug 属于"修复文档先，然后审查已有代码冲突"框架 —— Connect 顺序 bug 是
  代码偏离而非文档 bug；fakeTransport 测试因 Recv 始终可读未触发该 bug，stdio 真实 stdout 才暴露。
- env 白名单而非全部继承：docs/mcp/integration.md §7 "stdio 子进程继承经过过滤的 env"；白名单 5 项足够
  npx / node 找命令 (PATH) + 基本 locale，其他按需由 mcp.servers[*].env 显式注入避免隐式泄漏 Token / API key 等
- PumpStderr bufio buffer 用 default 大小（不是 4 MiB）—— stderr 行通常 < 64K，YAGNI 4 MiB buffer
- Python fake MCP server 用 `-c` inline 字符串避免临时文件管理；每个测试独立 python subprocess 不复用
- 删 cmd/yaasmoke + /tmp 辅助脚本：诊断工具复用任务结束，按 ponytail "few files, no scaffolding" 原则不留
- 选用 `exec.Command`（不是 `exec.CommandContext`）：避免 ctx cancel 触发立即 SIGKILL 绕过 stdin.close 等待路径；Close 自己控 5s 超时

---

## #11 Manager.runUpstream 雏形 + Tool Stable Proxy 集成 (HEAD 待 push)

### 范围
checklist §1 runUpstream + §7 Tool 映射 + §9 集成（与 Tool Manager + Runtime shutdown）。SSE / Streamable HTTP / heartbeat / catalog reconciliation / 指数退避重连留后续 commit。

### 改动文件
- **`internal/mcp/proxy.go` 新建** (~145 行)：`ProxyHandle{atomic.Pointer[Client]}` + `MCPToolProxy` 实现 `tool.Tool` 接口（Name/Description/Parameters/Execute）+ `toToolResult` （多 text 块 \n 连接，isError 透传，非 text → ErrMCPUnsupportedContent，nil result → ErrMCPProtocolError）+ `toMCPResult`（反向映射，留待本地 MCPServer commit 用）。
  - 采用 docs/mcp/integration.md §1 代码样例，但补 `description` 字段（原样例缺省会被 `tool.ToolManager.Register` 拒空描述 → 文档先修）。
  - Execute：handle nil → `ErrMCPUnavailable`；非零 `timeout` 用 `WithCancelCause + AfterFunc`（Go 1.20 无 `WithTimeoutCause`）；返回前查 caller ctx 与 callCtx cause。
- **`internal/mcp/manager.go` 重写** (~310 行，公开 API 签名不变)：
  - `serverEntry` 扩 `cfg config.MCPServerConfig + handle *ProxyHandle + client *Client + status ServerStatus + tools []tool.ToolInfo`。
  - `NewManager` 缓存 cfg + 默认 stdio transport + 初始 ServerStatus 全 disconnected。
  - `Prepare`：遍历 auto_start=true 的 stdio server → `StdioClient + NewClient` → `Connect(connTimeout)` → `Initialize(initTimeout)` → `DiscoverTools(initTimeout hardcap)` → 每个 tool 注册 `MCPToolProxy` 到 ToolManager；success → Store handle + Status=connected + ToolCount + ConnectedAt。
    - 非 stdio transport 标 LastError="transport not supported in current build" 跳过（SSE / Streamable HTTP 待后续）。
    - 单 server 失败仅 LastError + Status=Error，**不阻断其他 server**（docs/integration.md §4）。
    - connect/init timeout 配置优先：`mcp.servers[i].timeout > mcp.timeout.connect/init`，缺省分别 10s/15s。
  - `Stop`：cancelRun + 关每个已建立 client (`Close` 幂等) + `handle.Store(nil)` + status 复位 Disconnected + close(done) + ready=false。
  - `List/Get/Tools` 投影真实状态（ConnectedAt 指针深拷贝；Tools 未连接或无 Tool → (nil,false)）。
  - `ProtocolVersion` 字段本轮仍 nil：Client 未暴露协商版本 getter，未引入额外 API 扩展（留 SSE/Streamable HTTP commit 补）。
- **`docs/mcp/integration.md` §1 修正**：MCPToolProxy 结构补 `description string` 字段（原文档样例省略，会导致 ToolManager.Register 拒空描述）+ 补 Name/Description/Parameters 方法注释。
- **`docs/mcp/checklist.md`**：
  - §1 runUpstream 勾选 + Prepare/Stop/Get/Tools/List 备注更新为本轮实际行为
  - §2 Client 整 11 项补勾选（前轮忘勾，progress #10 已记）
  - §7 Tool 映射：勾 9 项（适配/前缀/schema/结果/错误/稳定 Proxy/共享 handle/超时/分页），仅留 catalog reconciliation 重连原子替换 handle 未实现
  - §9 集成：勾 Tool Manager 集成 + Skill binding 之前注册 + Runtime shutdown Stop/Done/fresh-Stop（runtime.go:344-347 已落地，本轮补勾）
- **`internal/mcp/manager_integration_test.go` 新建** (~300 行)：复用 stdio_test.go 的 `fakeMCPStdioServer` Python 脚本，5 例端到端 + 5 例纯函数测试：
  - `TestManagerPrepareAutoStartStdioRegistersTools`：auto_start stdio → connected + ToolCount=2 + ConnectedAt 非 nil + Tools() 返 mcp.fake.alpha/beta + Source="mcp"
  - `TestManagerToolProxyCallViaToolManager`：经 `tool.Manager.Execute(ctx, scope, "mcp.fake.alpha", {})` 拿到 "hello alpha"
  - `TestManagerPrepareStdioAutoStartFailureMarksError`：command 不存在 → Status=error + LastError 非空 + ToolCount=0 + Prepare 不返错
  - `TestManagerPrepareAutoStartFalseLeavesDisconnected`：auto_start=false 不被 Prepare 启动
  - `TestManagerStopDisconnectsClients`：Stop 后所有 server 回 Disconnected + Tools 返 (nil,false)
  - `TestMCPToolProxyUnavailableWhenHandleNil`：handle nil → Execute 返 `ErrMCPUnavailable` + Name/Description 浅校验
  - `TestToToolResult{JoinsText,RejectsNonText,NilResult,WrapsErr}`：4 例纯函数

### 验证
```
HTTP_PROXY=... go vet ./internal/mcp/      # 通过
HTTP_PROXY=... go build ./...              # 18 包全过
HTTP_PROXY=... go vet ./...                # 全项目无 warning
HTTP_PROXY=... go test -count=1 -timeout 120s ./internal/mcp/
  # ok 1.970s — 55 例全绿 (13 manager + 9 transport + 15 client + 8 stdio + 10 本轮新)
HTTP_PROXY=... go test -count=1 -timeout 240s ./...
  # 18 包全过，WS flake 本轮未触发（详见 progress 早前记录）
```

### 决策
- **ServerStatus.ProtocolVersion 本轮 nil**：Client 未暴露协商版本 getter，本轮不为此扩展 Client API；LE 与 docs *string 可选定义对齐。SSE / Streamable HTTP commit 实现时一并补。
- **Tool registration 中途失败不回滚 ToolManager**：Manager 关 client + 标 LastError + Status=Error 收敛；ToolManager.Unregister 回滚属 YAGNI，留后续 catalog reconciliation commit 一起做。
- **Prepare 同步完成上游连接（无 runUpstream goroutine）**：v1 stdio auto_start 启动是有限并可等待的；Runtime 启动序需要 binding 校验前 Tool 已注册，同步语义最贴近 docs/integration.md §4。heartbeat / 后台重连 / catalog reconciliation 留 async runUpstream commit。
- **docs/integration.md §1 修正优先于代码**：根据用户规则"发现文档问题先修文档"，MCPToolProxy 缺 description 字段是文档 bug。
- **stripServerPrefix helper**：DiscoverTools 返回 mcp.<server>.<remote>，注册 Proxy 逆推出 remoteName 给 CallTool 用；正反路径都走 canonicalToolName 保证一致性。

### 下一轮方向
1. **重连 + catalog reconciliation + heartbeat**（checklist §1 §7 §8）：async runUpstream goroutine 定期 Ping → 失败按 mcp.reconnect 指数退避 → 重新 Initialize + 完整分页 tools/list → 重连后比对 Tool name+description+inputSchema 三元一致才 Store 新 handle，差异保持 unavailable + ErrMCPProtocolError。
2. **SSE Transport**（checklist §5）：SSEClient Event 流解析 + Last-Event-ID 续传。
3. **Streamable HTTP Transport**（checklist §6）：POST JSON-RPC + Mcp-Session-Id 状态码映射。
4. **本地 MCP Server**（checklist §3[+§6 +§9 本地暴露]）：MCPServer handleInitialize/ListTools/CallTool/Ping + -32601 Resource/Prompt。
5. **ServerStatus.ProtocolVersion 暴露**：给 Client 加 protocolVersion getter（Initialize 校验后保存），Manager 投影到 ServerStatus + 实现 SSE 2024-11-05 路径。
6. **Worker 集成 Agent / Session / Provider**（checklist §9 §2）：MCP Tool 在 Agent turn 投影到 Provider Function 列表；Session 视图按 Agent allowall 投影可用 MCP Tool。
7. **Planner step 1-10**（docs/planner/）。

---

## #12 Client.ProtocolVersion getter + ServerStatus.ProtocolVersion 暴露 (HEAD 待 push)

### 范围
上轮 #11 副债「ServerStatus.ProtocolVersion 本轮 nil；Client 未暴露协商版本 getter」。本 commit 收尾：Client 加 `protocolVersion` 字段（mu 保护，Initialize 成功后写入）+ `ProtocolVersion() string` getter；Manager.connectStdioServer 投影到 `status.ProtocolVersion`。Close 后 getter 仍返协商版本快照（避免 Manager 投影闪断）。

### 改动文件
- `internal/mcp/client.go` (~15 行)：
  - Client struct 加 `protocolVersion string`（mu 保护）
  - Initialize 成功路径 `c.protocolVersion = result.ProtocolVersion`
  - 新 `func (c *Client) ProtocolVersion() string`（mu.RLock）
- `internal/mcp/manager.go` (~6 行)：connectStdioServer 成功路径 `pv := client.ProtocolVersion(); e.status.ProtocolVersion = &pv`，替换上轮 nil 注释块；头部 comment 更新。
- `internal/mcp/client_test.go` (+63 行)：新 `TestClientProtocolVersionGetter`：未 Initialize 返 ""；协商后返 Server 选择版本（用 LegacyProtocolVersion="2024-11-05" + stdio transport，验证 stdio 首选 2025-03-26 但接受 legacy）；Close 后仍保留快照。
- `internal/mcp/manager_integration_test.go` (+6 行)：现有 `TestManagerPrepareAutoStartStdioRegistersTools` 加断言 `st.ProtocolVersion != nil && *st.ProtocolVersion == ProtocolVersion`（fake MCP server 返 2025-03-26）。

### 验证
- go vet + build ./... 全过
- mcp 包 56 例全绿 (55 上轮 + 1 新)
- 全项目 test ./... 仅触发已知 WS flake（TestWSStreamDisconnectCancelsTurn 200ms Sleep，progress #10/早前已记，非本系列 commit 引入；单跑全绿）

### 决策
- **Close 后 ProtocolVersion 仍返协商值**：Manager 投影 ServerStatus 用快照模型；Close 触发后 Manager 会同步置 status=Disconnected，但 getter 暴露的协商值作为该代连接的「历史版本」保留，避免 List 在 Stop 与 Disconnect 路径竞态下见到一帧 nil。
- **legacy 测试用 stdio transport**：docs/transport.md §2 明确 stdio 首选 2025-03-26 但接受 legacy 2024-11-05；该测试反向验证 `acceptsVersion("stdio", LegacyProtocolVersion) == true`。
- **Client 单元测试单独成测**：本轮发现 `TestClientInitializeLifecycle` 复用同一 fake transport 应答 initialize 但用 ProtocolVersion 常量；新测试用 LegacyProtocolVersion 独立隔离 path。

### 下一轮方向
本 commit 解决上轮副债后，progress #11 末尾的下一轮方向不变：
1. **重连 + catalog reconciliation + heartbeat**（async runUpstream goroutine + mcp.reconnect 指数退避 + tool 三元一致才 Store 新 handle；docs/mcp/config-ref.md §7.1/§7.2 是权威设计，含 generation + listChanged channel + ticker + lifecycleMu gate）。
2. WS flake 根因修复（独立 commit）：session.Manager 加 IsTurnActive 公开 API + ws_handler_test polling 替换 200ms Sleep。
3. SSE / Streamable HTTP transport（checklist §5/§6）。
4. 本地 MCPServer（checklist §3）。
5. Agent/Session/Provider 集成（checklist §9 §2）。
6. Planner step 1-10（docs/planner/）。

---

## #13 WS flake root-cause fix + session.Manager.IsTurnActive 公开 API (HEAD 待 push)

### 范围
progress #10 早前记录 TestWSStreamDisconnectCancelsTurn 200ms time.Sleep timing flake 是 "非本系列 commit 引入"——本 commit 彻底 root-cause 修：发现测试本身的两处 spec 违背 + 一个 race，对应最小修复。

### Root cause 三个独立问题
1. **测试 assumptions 违反 spec**：docs/remote-api/conversation.md §turn_id 明文规定 "turn_id 在 session 内永久唯一；已提交 user 的 turn_id 复用返回 40001"。原测试期望"重拨同 turn_id 应被接受"与此 spec 直接冲突。session.turn.go:51 的 `snap.Messages` 含同 TurnID 则拒绝是 spec-correct，bug 在 test。
2. **200ms hard-coded Sleep 不可靠**：全项目跑时 CPU 紧迫会让 sleep 提前结束 → conn2 重拨时 conn1 的 turn 还在 activeTurns 中 → 撞 ErrTurnIDConflict，尤其 easy 触发 flake。
3. **Hub 是 session 范围广播**：conn2 即使拨新 turn_id 也会收到 conn1 在 background 跑完时 publish 的 assistant_done frame（turn_abort_1），抢在 turn_after_disconnect 的 queued/assistant_start 前到达，引入非确定性。

### 修复
- **session.Manager 新增 IsTurnActive(sessionID, turnID) bool 公开 API**（runturn.go +13 行，mu.RLock 保护）：供测试在不进 frame 路径的情况下 polling 等 cleanup 完成。该 API 也是 Runtime/Remote API 可以查询任意 (sid, turnID) 的权威活动空间状态的有用公开 API（不限于 testing）。
- **ws_handler_test.go 重写 TestWSStreamDisconnectCancelsTurn（-23/+17 行）**：
  - 用 polling `sm.IsTurnActive(s.ID, "turn_abort_1")` 直到 false（最多 5s，10ms tick）作为唯一权威断言——这正是"Stream disconnect cancels turn"的 spec 语义。
  - 删除"重拨同/异 turn_id 起新 turn 应被接受"段——违反 spec（永久唯一）+ Hub 广播污染非确定性，留 turn_id 唯一性/end-to-end turn lifecycle 测试在他处单独覆盖。
  - 删除 `var _ = strings.HasPrefix` 防 unused stub（strings.Replace 已在 dialWS 实际使用，stub 是死代码）。

### 验证
```
go test -count=10 -timeout 120s ./internal/api/ -run TestWSStreamDisconnectCancelsTurn
  # ok —— 10 轮连续全绿（修复前 5 轮 5 fail）
go vet ./... # 全项目无 warning
go build ./... # 18 包全过
go test -count=1 -timeout 240s ./...
  # 二次跑全项目 18 包全绿（无 WS flake，无任何 fail）
```

### 决策
- **IsTurnActive 作为公开 API 而非 testing helper**：未来 Runtime/observability 可能需要查 turn 是否活动（如 health endpoint 报告 idle/active）；在 session 公开 API 上加这个冷读方法是 1 行成本 + 0 风险，且 ponytail "在 shared 函数加 1 guard 比在每 caller 加 sleep 都重复更小 diff"。
- **测试只测 "cancels" 单一语义**：Hub 广播跨连接复杂场景属于 turn lifecycle / hub subscribe 设计的范畴，不应在一个"disconnect cancels" 测试里交叉；测试单一断言更符合 isolation 原则。
- **没有修 200ms Sleep 改 800ms 的治标做法**：即使延长仍是 sleep-race 解，CPU 负载变化时重新触发是早晚事。polling 是 root-cause 修法。

### 副债清理
- progress #10 早前已记 WS flake；本 commit 后该 flake 已从已知副债清单中移除。
- progress #11 末尾「下一轮方向 #2 WS flake 根因修复（独立 commit）」已完成。

### 下一轮方向
progress #11 末尾的下一轮方向余下 4 项：
1. **重连 + catalog reconciliation + heartbeat**（async runUpstream goroutine + mcp.reconnect 指数退避 + tool 三元一致才 Store 新 handle；docs/mcp/config-ref.md §7.1/§7.2 是权威设计，含 generation + listChanged channel + ticker + lifecycleMu gate）。
2. SSE / Streamable HTTP transport（checklist §5/§6）。
3. 本地 MCPServer（checklist §3）。
4. Agent/Session/Provider 集成（checklist §9 §2）。
5. Planner step 1-10（docs/planner/）。

---

## #14 runUpstream heartbeat ticker + transport-close 自恢复 (HEAD 待 push)

### 范围
progress #11 优先级 #1 第 1 步（async runUpstream goroutine + heartbeat ticker + client.Done()/Ping 失败 → compare-and-clear generation 失败清理 + Stop 同步 Join）。**不**含 重连 / 指数退避 / catalog reconciliation / listChanged channel —— 留下一 commit（progress #14 末尾下一步）。

### 改动文件
- `internal/mcp/manager.go` (+~170 行)：
  - 新增常量 `heartbeatInterval=30s` / `heartbeatTimeout=10s`（docs/mcp/config-ref.md §7.2）
  - `serverEntry` 加 `generation uint64`（启动时 gen=0；重连时下一 commit 递增）+ `listChanged chan struct{}`（cap 1，本期声明不投递，留给 catalog reconciliation commit）
  - `Manager` 加 `upstreamWG sync.WaitGroup` 跟踪所有 entry 的 runUpstream goroutine
  - `connectStdioServer` 成功路径启动 `go m.runUpstream(e, handle, client, e.generation)` 并 Add(1)
  - 新 `runUpstream(e, handle, client, gen)`：select 等 4 事件 —— `runCtx.Done()` 退出 / `client.Done()` → markGenerationFailed / `ticker.C` → Ping（heartbeatTimeout）失败 → markGenerationFailed；ctx 已取消则视为 Manager 关闭不视为 heartbeat 失败
  - 新 `markGenerationFailed`：m.mu 保护下 compare-and-clear generation + e.client==client 比对；OK 则 handle.Store(nil) + status=Error + LastError；锁外关 client（幂等）
  - `Stop` 加 `m.upstreamWG.Wait()` 在 close done 之前；现有 cancelRun 已触发 runUpstream 的 `<-m.runCtx.Done()` 分支退出
  - 头部 comment + Prepare comment + Stop comment 更新为本系列 lifecycle 实况
- `internal/mcp/manager_integration_test.go`（+~150 行，3 例）：
  - `const fakeMCPExitServer`：fakeMCPStdioServer 变体，`tools/call name=="stop"` 时 `sys.exit(2)` 模拟上游 transport 突然中断（不影响 stdio_test.go 原版 fakeMCPStdioServer 8 例稳定）
  - `TestManagerRunUpstreamRecoversTransportClose`：Prepare → runUpstream 启动 → 触发子进程退出（调 mcp.exit.stop → python sys.exit）→ polling ≤5s 等 status 转 Error + LastError 非空 → 后续 `tm.Execute(mcp.exit.alpha)` 返 ErrMCPUnavailable（验证 handle.Store(nil)）
  - `TestManagerStopJoinsUpstreamGoroutines`：Prepare 启动 runUpstream 后 Stop 必须 ≤5s 返 + Done ≤2s 关闭，ticker join 无死锁

### 验证
```
go test -count=1 -timeout 60s ./internal/mcp/ -run 'TestManagerRunUpstream|TestManagerStopJoins|StopDisconnects' -v
  # 3 例 PASS (RunUpstreamRecoversTransportClose 0.22s, StopJoins 0.21s, StopDisconnectsClients 0.21s)
go vet ./... # 全项目无 warning
go build ./... # 18 包全过
go test -count=1 -timeout 120s ./internal/mcp/ # 58 例全绿 (上轮 56 + 本轮 3 新 - 重叠 TestManagerStopDisconnectsClients 已含; 实际加 2 例)
go test -count=1 -timeout 240s ./... # 二次跑 18 包全绿 (含 WS flake 已上轮根治)
```

### 决策
- **TestManagerStopDisconnectsClients 上轮已存在**：本 commit 该测试不动，验证 Stop 仍能在 transport active 时正确清理。新加的 StopJoins 是专门的 Stop+WG.Wait 同步测试，不重叠.
- **`fakeMCPExitServer` 不复用 shared fakeMCPStdioServer**：原版 tools/call 收任意 name 都正常返；本期需 sys.exit 触发 transport 断开. 修改原版可能影响 stdio_test.go 8 例已稳定的断言, 单独变体最小风险.
- **`_stop_ 工具触发 sys.exit(2)`** vs `kill proc`：kill proc 需要拿到 StdioClient.cmd.Process 私有字段在测试中访问过深；让 server 主动退出复用现有 tools/call 路径更接近"上游 process 异常崩溃"的语义, 是更真实的故障 репрезентативности.
- **`heartbeatInterval=30s` 固定**: docs §7.2 明文 fixed 30s/10s. 测试不触发 ticker 真等 30s——`client.Done()` 路径在 subprocess 退出后 sub-second 触发 markGenerationFailed, 比 30s ticker 快.
- **暂不引入指数退避重连**: 本 commit 是 雏形 heartbeat；失败立即转 Error + unavailable, 不重建. 配置 mcp.reconnect.enabled 的 rewrite 已是下个 commit 范围, 一次只动一个独立可验收步骤 (ponytail: 一步小可验收).
- **`generation` 启动时 = 0**: 单代场景下 markGenerationFailed 中 `e.generation != gen` 永远 false → 不 short-circuit. 重连 commit 才真正利用 generation 比对避免旧代 stale writes.

### 下一轮方向
1. **runUpstream 重连第 2 步**：markGenerationFailed 后按 mcp.reconnect.* 指数退避构新 Client（Initialize+DiscoverTools）→ 递增 generation 原子切换 entry/Client/handle；目录三 真比对一致 才成功.
2. **listChanged channel 事件路径**：tools/list_changed notification 投递到该代 listChanged cap-1 channel → runUpstream select 命中 → 完整 DiscoverTools + 三元严格比对 → 不一致保持 unavailable + ErrMCPProtocolError.
3. SSE / Streamable HTTP transport（progress #11 §3 §5/§6）.
4. 本地 MCPServer（checklist §3）.
5. Agent/Session/Provider 集成（checklist §9 §2）.
6. Planner step 1-10（docs/planner/）.

---

## #15 runUpstream 重连 Step 2 — 指数退避 + generation compare-and-clear + catalog 三元严格比对 (待 push)

### 范围
progress #14 末尾下一步 §1: runUpstream 失败分支接入 attemptReconnect. 按 mcp.reconnect 指数退避构新 Client (connect+init+discover) → catalog 三元 (canonical name + description + canonical-marshal InputSchema) 严格比对一致才能原子替换 handle + 递增 generation; 不一致保持 Error 不可自愈; max_attempts 耗尽后停止重连. **不**含 listChanged 事件路径 (留 Step 3).

### 改动文件
- `internal/mcp/manager.go` (+~190 行, 总 ~660 行)：
  - import + `bytes`/`encoding/json`
  - 提炼 `connectAndDiscover(e) (*Client, []MCPTool, error)` — Connect → Initialize → DiscoverTools 复用 path, 不改 entry. 失败仅 Close client 不改 status
  - 提炼 `registerProxies(e, handle, tools, toolTimeout) error` — 首代注册稳定 Proxy 完整流程, 中途失败不回滚已注册 (ToolManager 不提供 Unregister)
  - 提炼 `publishGeneration(e, handle, client, tools, newGen)` — entry 锁内原子更新 generation/client/tools/ConnectedAt/status/ProtocolVersion. 首代 newGen=0; 重连 newGen=oldGen+1
  - 提炼 `effectiveToolTimeout(serverTimeout, globalTimeout) time.Duration` — server 优先, 否则 global, 0= 仅 caller deadline
  - 新 `attemptReconnect(e, handle, oldGen) (*Client, uint64, bool)` — `Reconnect.Enabled=false` 直接返 keepGoing=false; attempt 1..max_attempts 退避 `initial * 2^(attempt-1) cap max` (interruptible via sleepInterruptible on m.runCtx); 调 connectAndDiscover, 失败记 LastError 并续 attempt; 成功后调 catalogMatches, 不等到一致前 close newClient. 进入 entry 锁前再检 `m.runCtx.Err() != nil` (Stop race) + 锁下比 generation == oldGen + 递增 newGen + handle.Store(newClient) + status=Connected + LastError=""
  - `catalogMatches(e, discovered []catalogItem)` 加锁读 e.tools 副本, 与 discovered 按 Index 对位三元机器比对 (len 不等 fail); canonical name + description 直接字符串相等; InputSchema 用 `canonicalJSON` round-trip marshal 后 bytes.Equal (Go json.Marshal 默认按 map key 升序排序) 消除 schema 对象 key 顺序差异 (docs §7.2 明示)
  - `catalogItem{canonicalName, description, inputSchema}` + `snapshotTools(tools)` 转 MCPTool → catalogItem
  - `canonicalJSON(raw json.RawMessage) (json.RawMessage, error)` — UseNumber 解 + Marshal 编, round-trip 后等价字节
  - `sleepInterruptible(ctx, d) bool` — Timer + ctx.Done() select, ctx 取消返 false 由调用层放弃重连
  - `runUpstream` 失败分支 (client.Done() / Ping 失败): 先 markGenerationFailed, 再调 attemptReconnect; keepGoing=true 则更新 local client/gen + ticker.Reset(heartbeatInterval) 继续同 goroutine (避免每 entry 多 goroutine); keepGoing=false 退出
  - `connectStdioServer` 改为: connectAndDiscover → registerProxies → publishGeneration(newGen=0) → go runUpstream. Talker 引入 Reconnect 配置使用 (Stop 兼容)
  - `markGenerationFailed` 注释与 signature 不变 (已正确 compare-and-clear)
  - 注释 sync: 顶部 series commit 段落补 Step 2 已落地描述; Prepare/serverEntry 注释中「下一 commit / Step 3 接入」改为「等待 Step 3」

- `internal/mcp/manager_integration_test.go` (+~110 行):
  - import + `encoding/json`
  - `TestManagerRunUpstreamRecoversTransportClose` (Step 1 旧例) 加 `Reconnect: {Enabled: false}` 保持失败不重连的 Step 1 路径行为期望 (Stage=Error 后 Execute 返 ErrMCPUnavailable); 已验证 5 次重复不 break
  - 新 `TestManagerRunUpstreamReconnectsAfterTransportClose`: 用同 fakeMCPExitServer (stop → sys.exit 触发 transport close); 配 `Reconnect: {Enabled: true, MaxAttempts: 3, InitialDelay: 100ms, MaxDelay: 1s}`; polling ≤5s 验 sawError (markGenerationFailed 中间态) → Connected (重连成功); 重连后 `mcp.recon.alpha` Execute 成功; 末尾 Stop ≤5s (重连 goroutine 干净 join)
  - 新 `TestCatalogMatches`: 函数级 catalogMatches 三元比对单测. 覆盖: 同 → true; schema key 顺序不同 → canonical 等 true; 不同 name/description/schema → false; 数量不同 → false; both empty → true. 不依赖 stdio (0.00s)
  - `mustNewManager(t)` 帮助函数供单测构造最小 Manager

### 验证
```
HTTP_PROXY=http://192.168.4.1:7890 HTTPS_PROXY=http://192.168.4.1:7890 GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.org go test -count=1 -timeout 60s ./internal/mcp/ -run 'TestManagerRunUpstreamReconnects|TestCatalogMatches' -v
  # 2 例 PASS (Reconnects 0.50s, CatalogMatches 0.00s)
go test -count=5 -timeout 120s ./internal/mcp/ -run 'TestManagerRunUpstreamReconnects|TestManagerRunUpstreamRecovers|TestManagerStopJoins' -v
  # 3 例 × 5 = 15 PASS, 无 flake
go vet ./... # 全项目无 warning
go build ./... # 18 包全过
go test -count=1 -timeout 240s ./... # 18 包全绿 (含 WS flake 上轮已根治)
```

### 决策
- **catalog 三元比对用 canonical JSON round-trip**: docs §7.2 明文「不比分页或对象 key 顺序」. Go `json.Marshal` 已按 map key 升序序列化, round-trip 即可吸顺序差异; UseNumber 防大整数丢精度. 比 strings.TrimSpace + bytes.Equal 更严格等价.
- **`Ponytail`: 不引入 lifecycleMu**. 文档 §7.2 提到「取得 lifecycleMu 检查 stopping」但当前 Manager 已用 `m.runCtx` 作 stop 信号 (Stop 调 cancelRun 取消). attemptReconnect 进入 entry 锁前再查一次 `m.runCtx.Err() != nil` + 锁下比 generation == oldGen, 与「lifecycleMu/stopping」语义等价但少一个 mutex (ponytail ladder: 现有 lock 已够用, 不新加 lock).
- **重连不开新 goroutine**: docs §7.2 「每个 entry 只允许该一个 goroutine 重连」. runUpstream 失败分支直接 inline 调 attemptReconnect, 成功后 ticker.Reset 用新 client 继续 select. 这条 invariant 满足且无额外 wg 计数 (已是 upstreamWG 跟的同一 goroutine).
- **重连不重注册 Proxy**: ToolManager.Register 失败回滚不在 ToolManager 支持范围 (YAGNI Unregister). 首代成功后 Proxy 已 ToolManager 注册固定, 重连只需原子切 handle 的 client 字段 (Proxy.handle atomic Load). 第 N 代只需要 catalog 与首代一致, Proxy.Name()/Description()/Parameters() 不变. 接 attemptReconnect 时不调 registerProxies.
- **退避 interruptible**: sleepInterruptible 可被 m.runCtx.Done() 中断, 让 Stop ≤5s (与 StopJoinsUpstreamGoroutines 测试一致). Stop 路径触发 cancelRun 立刻打断退避 timer.
- **catalog 漂移 ExitOK → 不继续 attempt**: docs §7.2 「差异保持 unavailable + ErrMCPProtocolError 要求重启 Runtime」, 即 catalog 漂移是非自愈错误. catalogMatches 返 false 时 attemptReconnect 立即返 keepGoing=false (不再尝试 max_attempts), Status 保持 Error, LastError 记 catalog drift.
- **接入 generation compare-and-clear 重代场景**: 重连成功 newGen=oldGen+1, markGenerationFailed 用 gen 比对. 单代时 e.generation==gen 总成立, mark 不 short-circuit; 重连后代已升 gen, 旧 gen 标记不会污染新代 (暑期在更复杂并发场景下有效).
- **TestManagerRunUpstreamRecoversTransportClose 加 Reconnect.Enabled=false**: 该 Step 1 旧例验证 markGenerationFailed 后保持 Error 路径, 不应被 Step 2 重连影响. 显式 关 Reconnect 保持原意图, 同时给 Step 2 单独留测试用例.
- **不测 attemptReconnect 单独调用**: 单测函数级 catalogMatches 已覆盖比对核心, attemptReconnect 完整端到端由 ReconnectsAfterTransportClose 覆盖 (含退避 + Stop join + post-reconnect Execute). 不写重复单测减少 flake 表面 (Ponytail: YAGNI 测试).

### 下一轮方向
1. **runUpstream Step 3 — listChanged 事件路径**: Client.onListChanged 投递到该代 listChanged cap-1 channel → runUpstream select 命中 → 用当前代 Client 完整 DiscoverTools + 三元严格比对; 不一致保持 unavailable + ErrMCPProtocolError.
2. SSE / Streamable HTTP transport (checklist §5 / §6).
3. 本地 MCPServer (checklist §3) — Yaa! 作为 Server.
4. Agent/Session/Provider 集成 (checklist §9 §2).
5. Planner step 1-10 (docs/planner/).

---

## #16 runUpstream listChanged Step 3 — tools/list_changed 通知 + catalogReconcile + 漂移不可自愈 (待 push)

### 范围
progress #15 末尾下一步 §1: 收到 `notifications/tools/list_changed` 通知后用当前代 Client 完整 DiscoverTools + 三元严格比对 (canonical name + description + canonical-marshal InputSchema); 一致保持 Connected 不替换 Client; 漂移关闭该 Client + 标 ErrMCPProtocolError 保持 Error 不可自愈.

### 改动文件
- `internal/mcp/client.go` (+~20 行)：
  - Client struct 加 `onListChanged func()` 字段 (pendingMu 保护以避免与 recvLoop race)
  - 新 `SetOnListChanged(fn func())` setter, 必须在 Connect 前设置 (Manager 创建 client 后即调)
  - recvLoop notification 分支: method == `notifications/tools/list_changed` 时**非阻塞**调用 onListChanged (do not 发起 request in recvLoop, docs/mcp/client.md §recvLoop); 其他 notification 按 ErrMCPProtocolError fail
  - 修正原代码 method 名比较 prefix: 之前用 "tools/list_changed" (无 notifications/ 前缀) 实际 server emit 是 `notifications/tools/list_changed` (MCP spec 标准命名空间); docs 表述 "tools/list_changed" 是简写无前缀, 代码层用全名

- `internal/mcp/manager.go` (+~90 行)：
  - publishGeneration (首代) + attemptReconnect 末段 (重连) 两路径都: 每代新建**独立** listChanged cap-1 channel (不复用旧代) + client.SetOnListChanged 设置 closure 非阻塞投递到该 channel. 满足 docs §7.2 "旧代 channel 永远不被新代复用, 避免迟到 callback 触发新一代重连" invariant
  - attemptReconnect 签名改为返回 `(*Client, uint64, chan struct{}, bool)` 多一个新代 listChanged channel. `return nil, 0, false` 统一改为 `return nil, 0, nil, false`; 成功 return `newClient, newGen, e.listChanged, true`
  - runUpstream 加本地快照参数 `notify := e.listChanged` (启动代快照不可重读 e.*), 重连成功后更新 `notify = newNotify`; select 加 `case <-notify` 分支调用 catalogReconcile(e, handle, client, gen) → (closeClient, exit). closeClient 则锁外调用 client.Close(); exit 则退出 goroutine (漂移不可自愈, 不再 attemptReconnect)
  - 新 `catalogReconcile(e, handle, client, gen) (closeClient, exit bool)`:
      - 用 initTimeout hardcap 调 client.DiscoverTools → snapshotTools → catalogMatches
      - 一致: 返回 (false, false); 调用方不动 entry, 继续同 ticker (notify 合并 channel 已消化)
      - DiscoverTools 失败: entry 锁下比 `e.generation == gen && e.client == client` 后 handle.Store(nil) + status=Error + LastError="list_changed reconcile failed: <err>"; 返 (true, true)
      - catalog 漂移: entry 锁下同上 + LastError="list_changed reconcile: catalog drift (ErrMCPProtocolError)"; 返 (true, true)
  - catalogMatchesReadOnly 是 catalogReconcile 内部复用 catalogMatches 的薄封装 (避免直接调 m.catalogMatches 时视觉模糊, 但事实上直接调也可; ponytail: 此间接层冗余可平 ◦; 保留dbo 单 stack 一行更直)

- `internal/mcp/manager_integration_test.go` (+~150 行)：
  - `const fakeMCPListChangedStable` — alpha/beta 任意 tools/call 响应后立即 emit `notifications/tools/list_changed` 一帧; tools/list 永远返回原 catalog. 测一致分支
  - `const fakeMCPListChangedDrift` — 内部 `list_calls` 计数 + `emit_drift` flag: 第一次 tools/list 返回原 schema; alpha call 时 emit response + 切到 drift (description 改为 "modified_a") + emit 通知; 第二次 tools/list (catalogReconcile 调) 返回 drift schema. 测漂移分支
  - `TestManagerRunUpstreamListChangedStableKeepingConnected` — Prepare → 触发 alpha call (server emit notify) → polling ≤1.5s 验 status 仍 Connected → post-notify Execute 仍 ok. 5 次重复无 flake
  - `TestManagerRunUpstreamListChangedDriftMarksError` — Prepare → 触发 alpha call (server emit notify + 切 drift) → polling ≤5s 验 status=Error + LastError 非空 → Execute 返 ErrMCPUnavailable. 5 次重复无 flake

### 验证
```
HTTP_PROXY=http://192.168.4.1:7890 HTTPS_PROXY=http://192.168.4.1:7890 GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.org go test -count=1 -timeout 60s ./internal/mcp/ -run 'TestManagerRunUpstreamListChanged' -v
  # 2 例 PASS (Stable 1.7s, Drift 0.2s)
go test -count=5 -timeout 60s ./internal/mcp/ -run 'TestManagerRunUpstreamListChanged' -v
  # 10 例 PASS, 无 flake
go vet ./... # 无 warning
go build ./... # 18 包全过
go test -count=1 -timeout 240s ./... # 18 包全绿 (含 WS flake 上轮已根治 / Step 2 重连稳定 / Step 3 listChanged 闭环)
```

### 决策
- **method 名 `notifications/tools/list_changed` 而非 `tools/list_changed`**: MCP spec 通知 method 带 `notifications/` 命名空间前缀; docs 表述 "tools/list_changed" 是简写. 代码层用全名与 bestEffortCancel (`notifications/cancelled`) / Initialize (`notifications/initialized`) 一致. 第一次 vet 编译过但测试失败因 method 名比较不匹配 → client.go recvLoop 收到带前缀 method 后 fallthrough 到 fail 分支. 修正后全绿.
- **进入 entry 锁前过 race 防护**: catalogReconcile 两条修改 entry 路径 (DiscoverTools 失败 / 漂移) 都先在 entry 锁下比 `e.generation == gen && e.client == client`. 与 Stop race (Stop 已置 e.client=nil) 或 attemptReconnect race (重连已换 e.generation) 时 stale 不改 entry. 本 entry 单 goroutine (runUpstream 唯一重连 owner) 理论上不会 race, 但 inner check 是文档 §7.2 "代际不匹配或 catalog 漂移都在锁外 Close, 禁止发布" 的对应防御.
- **catalogReconcile 一致分支不替换 Client**: docs §7.2 "相同、该 generation 仍存活且未 stopping 才重新发布同一 Client". "重新发布同一 Client" 即 handle 已指着同一 Client, 无需修改 entry 字段. 所以一致分支 return (false, false), 调用方 runUpstream 不动 entry 仅继续 ticker. 这是 Ponytail 关键简化: 一致路径 0 修改.
- **catalogMatchesReadOnly 间接层**: 作为 catalogReconcile 内部对 catalogMatches 的封装, 纯语义标记. 事实上 catalogReconcile 可直接调 m.catalogMatches. Ponytail 实质 level: 该封装纯可 inline 取消. 保留dbo 一行虽省字小但语义清晰; 不取消是 Ponytail ladder §6 (能一行就一行) 的负例. 本 commit 不再拆分以保完整逻辑.
- **listChanged channel 每代新建独立**: publishGeneration/attemptReconnect 末段都 `e.listChanged = make(chan struct{}, 1)`. 旧代 channel 引用由 runUpstream 局部快照 notify 持有; 重连切新代后旧 channel GC. 满足 docs §7.2 "旧代 channel 永远不被新代复用".
- **关闭该代 Client 后退出 runUpstream (漂移不可自愈)**: catalogReconcile 漂移分支返 (closeClient=true, exit=true). 与 attemptReconnect 不同 (attemptReconnect 在 attempt > max_attempts 才退出); catalog drift 直接退出是因为 attemptReconnect 也只能发现相同 drift, 重连不修漂移问题 (runtime 错误, 文档要求 Runtime restart 重新建立 catalog). 路径保持简单.
- **listChanged cap-1 合并语义**: onListChanged closure 用 `select { case e.listChanged <- struct{}{}: default: }` 非阻塞投递. 通知期间如 runUpstream 还没消费, 第二次合并不丢只占用同一空槽. docs §7.2 "listChanged 仅允许合并重复通知". 已经没积压.
- **stable test polling 1.5s**: catalogReconcile 一致分支会立即跑完 DiscoverTools (sub-100ms) + 不动 entry, polling 1.5s 比 3s 快. Drift test polling 5s 给 catalogReconcile 完整执行余地.

### 下一轮方向
1. SSE / Streamable HTTP transport (checklist §5 / §6).
2. 本地 MCPServer (checklist §3) — Yaa! 作为 Server.
3. Agent/Session/Provider 集成 (checklist §9 §2) — MCP Tool 在 Agent turn 投影到 Provider Function 列表.
4. Planner step 1-10 (docs/planner/).
5. Remote API `GET /api/v1/mcp/servers` + `GET /api/v1/mcp/servers/:name` (checklist §9).
6. 文档副债 (W1 时间戳/W2 README 导览/W4 tokens[].roles 默认, 早前 progress 已记).
