// Package pluginrpc transport: Unix Socket / loopback TCP endpoint 管理.
// docs/plugin/loader.md §4: Linux 用 Unix Socket, Windows 用 loopback TCP.
package pluginrpc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

// AllocateLocalEndpoint 分配本地 IPC endpoint 和 cleanup 函数.
// Linux/macOS: 临时目录中的 Unix Socket, 仅当前用户可访问.
// Windows: 随机 loopback TCP 端口 (TODO MVP 留 stub).
func AllocateLocalEndpoint(pluginID string) (endpoint string, cleanup func(), err error) {
	if runtime.GOOS == "windows" {
		// Windows: loopback TCP
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", nil, fmt.Errorf("allocate tcp endpoint: %w", err)
		}
		cleanup = func() { _ = l.Close() }
		return l.Addr().String(), cleanup, nil
	}

	// Unix: 临时目录 Unix Socket
	dir, err := os.MkdirTemp("", "yaa-plugin-")
	if err != nil {
		return "", nil, fmt.Errorf("allocate socket dir: %w", err)
	}
	// dir 仅当前用户可访问 (防止其他用户访问 socket)
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("chmod socket dir: %w", err)
	}
	sockPath := filepath.Join(dir, pluginID+".sock")
	cleanup = func() { _ = os.RemoveAll(dir) }
	return "unix://" + sockPath, cleanup, nil
}

// DialPlugin 连接到 plugin endpoint.
// docs/plugin/loader.md §3: Dial->Handshake->Init->Ready.
func DialPlugin(endpoint, pluginID string) (net.Conn, error) {
	if len(endpoint) > 7 && endpoint[:7] == "unix://" {
		return net.Dial("unix", endpoint[7:])
	}
	// tcp://<host:port>
	if len(endpoint) > 6 && endpoint[:6] == "tcp://" {
		return net.Dial("tcp", endpoint[6:])
	}
	return nil, fmt.Errorf("unknown endpoint scheme: %s", endpoint)
}
