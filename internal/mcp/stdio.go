package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/exp/slog"
)

// stdioMessageMaxBytes 单行 JSON-RPC body 上限 4 MiB（docs/mcp/transport.md §2）。
// 超过上限立即关闭该连接返 ErrMCPProtocolError。
const stdioMessageMaxBytes = 4 * 1024 * 1024

// stdioCloseGraceTimeout 是 Close 时等待子进程自然退出的超时；
// 超时后 SIGKILL。
const stdioCloseGraceTimeout = 5 * time.Second

// inheritEnvKeys 是子进程从 Runtime 继承的基础环境变量白名单
// （docs/mcp/integration.md §7：stdio 子进程继承经过过滤的 env）。
// 仅保留定位所需的项：PATH 用于命令查找、HOME / USER / LANG 用于 locale。
// ponytail: 白名单而非全部继承；后续若 MCP server 需要更多 env 由配置显式注入。
var inheritEnvKeys = []string{"PATH", "HOME", "USER", "LANG", "LC_ALL"}

// StdioClient 是 stdio transport 的 Client 端实现（docs/mcp/transport.md §3.1）。
// 启动子进程，通过 stdin/stdout JSON-RPC 行分隔通信；stderr 转发到 logger，不混入协议流。
// Close 先关 stdin，等子进程自然退出，超时 SIGKILL。
type StdioClient struct {
	command string
	args    []string
	env     map[string]string
	logger  *slog.Logger

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    io.ReadCloser
	started   bool
	closed    bool
	info      TransportInfo
	stderrWG  sync.WaitGroup
	recvReady chan struct{} // closed 在 Start 后；Recv 在 Close 之前可读
	reader    *bufio.Reader // lazy 跨 Recv 复用，见 readerForRecv
}

// NewStdioClient 构造 StdioClient（不启动进程）。后续 Connect 调用 Start 启动。
// logger 为 nil 时使用 slog.Default()。
func NewStdioClient(command string, args []string, env map[string]string, logger *slog.Logger) *StdioClient {
	if logger == nil {
		logger = slog.Default()
	}
	return &StdioClient{
		command:   command,
		args:      args,
		env:       env,
		logger:    logger,
		info:      TransportInfo{Type: "stdio", Endpoint: command, Connected: false},
		recvReady: make(chan struct{}),
	}
}

// Start 启动子进程并连接 stdin/stdout/stderr。startupCtx 仅约束启动超时；
// 实际进程生命周期至 transport.Close 之前一直运行。
func (c *StdioClient) Start(startupCtx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.started {
		return fmt.Errorf("%w: stdio already started", ErrMCPProtocolError)
	}
	if c.closed {
		return fmt.Errorf("%w: stdio closed", ErrMCPTransportClosed)
	}

	cmd := exec.Command(c.command, c.args...)
	cmd.Env = composeStdioEnv(c.env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdio: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("stdio: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return fmt.Errorf("stdio: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("%w: stdio start %q: %v", ErrMCPConnRefused, c.command, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.started = true
	close(c.recvReady) // 通知 Recv 可以开始读
	c.info = TransportInfo{Type: "stdio", Endpoint: c.command, Connected: true}

	// stderr 转发 goroutine：把子进程 stderr 行按 slog.Info 写，避免阻塞子进程 stderr 写满。
	c.stderrWG.Add(1)
	go c.pumpStderr()
	return nil
}

// composeStdioEnv 按 docs/mcp/integration.md §7 构建 Stdio 子进程 env：
// 从 Runtime 继承白名单（PATH/HOME/USER/LANG/LC_ALL）+ 用户配置 env 注入。
// key 为空字符串的 entry 跳过；与白名单重叠的 key 由用户 env 覆盖。
// 返回 os.Environ 兼容的 []string。
func composeStdioEnv(userEnv map[string]string) []string {
	inherited := make(map[string]string, len(inheritEnvKeys))
	for _, k := range inheritEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			inherited[k] = v
		}
	}
	for k, v := range userEnv {
		if k == "" {
			continue
		}
		inherited[k] = v
	}
	out := make([]string, 0, len(inherited))
	for k, v := range inherited {
		out = append(out, k+"="+v)
	}
	return out
}

// pumpStderr 把子进程 stderr 行级转发到 logger.Info（带 subprocess stderr 标签）。
// 子进程关闭 stderr 后 goroutine 退出；不重新入协议流（docs: stderr 不混入协议）。
func (c *StdioClient) pumpStderr() {
	defer c.stderrWG.Done()
	scanner := bufio.NewReader(c.stderr)
	for {
		line, err := scanner.ReadString('\n')
		if line != "" {
			c.logger.Info("mcp stdio subprocess stderr",
				"server", c.info.Endpoint, "line", strings.TrimRight(line, "\n"))
		}
		if err != nil {
			if err != io.EOF && !errors.Is(err, os.ErrClosed) {
				c.logger.Info("mcp stdio subprocess stderr closed",
					"server", c.info.Endpoint, "err", err.Error())
			}
			return
		}
	}
}

// Send 写单条 JSON-RPC Message：marshal + 行分隔。
// Send 可并发：mu 保护 io 写入防止行交错。
func (c *StdioClient) Send(ctx context.Context, msg *Message) error {
	c.mu.Lock()
	if c.closed || !c.started {
		c.mu.Unlock()
		return fmt.Errorf("%w: stdio not started or closed", ErrMCPTransportClosed)
	}
	stdin := c.stdin
	c.mu.Unlock()

	raw, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("%w: marshal", ErrMCPProtocolError)
	}
	if len(raw)+1 > stdioMessageMaxBytes {
		return fmt.Errorf("%w: send body too long %d", ErrMCPProtocolError, len(raw)+1)
	}

	// 写入 + 行尾；带 ctx 是否已结束检查避免在 closed stdin 上长阻塞。
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if _, werr := stdin.Write(append(raw, '\n')); werr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("%w: stdin write: %v", ErrMCPTransportWrite, werr)
	}
	return nil
}

// Recv 从 stdout 读单行 JSON-RPC Message。每个 transport 实例唯一调用者 (Client.recvLoop)。
// 子进程退出（stdout EOF）→ 返 ErrMCPTransportClosed。
// 行超 4 MiB / 解码失败 → 返 ErrMCPProtocolError。
func (c *StdioClient) Recv(ctx context.Context) (*Message, error) {
	// 等待 Start 完成（通常 Client.Connect 先调 Start 后 Recv，应几乎不阻塞）。
	select {
	case <-c.recvReady:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// bufio.NewReaderSize 用于 4 MiB 单行上限：超过返 ErrTooLong 实际是 bufio.ErrBufferFull。
	// 由于每次 Recv 用新 reader 会跨行丢数据，需共享 reader；放在字段上。
	reader := c.readerForRecv()
	line, err := reader.ReadString('\n')
	if err == bufio.ErrFinalToken {
		// 不应触发（仅 Reset 用）；视为 EOF
		return nil, fmt.Errorf("%w: stdout closed", ErrMCPTransportClosed)
	}
	if err == io.EOF && len(line) == 0 {
		// 子进程关闭 stdout（通常因为 stdin 关闭或进程退出）
		return nil, fmt.Errorf("%w: stdout closed", ErrMCPTransportClosed)
	}
	if err != nil && err != io.EOF {
		// bufio buffer full：行超过上限
		if errors.Is(err, bufio.ErrBufferFull) || strings.Contains(err.Error(), "bufio") {
			return nil, fmt.Errorf("%w: recv line too long", ErrMCPProtocolError)
		}
		return nil, fmt.Errorf("%w: stdout read: %v", ErrMCPTransportClosed, err)
	}
	line = strings.TrimRight(line, "\n")
	if len(line) > stdioMessageMaxBytes {
		return nil, fmt.Errorf("%w: recv body too long", ErrMCPProtocolError)
	}
	if len(line) == 0 {
		// 空行视为 stdout 提前关闭
		return nil, fmt.Errorf("%w: empty line", ErrMCPProtocolError)
	}
	var msg Message
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil, fmt.Errorf("%w: decode: %v", ErrMCPProtocolError, err)
	}
	return &msg, nil
}

// readerForRecv 返当前 reader，惰性初始化。
// 跨 Recv 复用避免每次 Recv 重置 bufio 丢已读字节（MCP wire 行分隔）。
func (c *StdioClient) readerForRecv() *bufio.Reader {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reader == nil {
		c.reader = bufio.NewReaderSize(c.stdout, stdioMessageMaxBytes)
	}
	return c.reader
}

// Close 幂等关闭：先 close stdin → 等子进程自然退出（5s 超时）→
// Kill → 收 stdout / stderr goroutine。docs/mcp/transport.md §3.1 / checklist §4：
// 不发送未定义的 shutdown RPC。
func (c *StdioClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	stdin := c.stdin
	stdout := c.stdout
	cmd := c.cmd
	c.info = TransportInfo{Type: "stdio", Endpoint: c.info.Endpoint, Connected: false}
	c.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd == nil {
		// 未启动；无需等 goroutine。
		return nil
	}

	// 等子进程退出：doc 要求 close stdin → 等待退出 → 超时 kill。
	// 实现：goroutine 上 process.Wait，main 用 timer 超时 → Kill。
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case <-waitErr:
		// 子进程自然退出
	case <-time.After(stdioCloseGraceTimeout):
		_ = cmd.Process.Kill()
		<-waitErr
	}

	// 显式关闭 stderr/stdout 让 pumpStderr goroutine 解除 ReadString 阻塞
	// （cmd.Process 退出后 pipe OS 层 TCP-like close，ReadString 返 io.EOF 或 os.ErrClosed）。
	if c.stderr != nil {
		_ = c.stderr.Close()
	}
	_ = stdout.Close()
	c.stderrWG.Wait()

	return nil
}

// Info 返回 TransportInfo 快照（mu 保护）。
func (c *StdioClient) Info() TransportInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.info
}
