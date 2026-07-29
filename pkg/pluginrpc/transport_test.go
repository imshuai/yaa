package pluginrpc

import (
	"net"
	"strings"
	"testing"
)

// TestAllocateAndDialUnixSocket 验证 Unix Socket endpoint 的分配+连接 round-trip.
func TestAllocateAndDialUnixSocket(t *testing.T) {
	// Linux/macOS: Unix Socket 分配→连接
	endpoint, cleanup, err := AllocateLocalEndpoint("test-plugin")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !strings.HasPrefix(endpoint, "unix://") {
		t.Fatalf("expected unix:// prefix, got %s", endpoint)
	}

	// 启动 listener 接受连接
	sockPath := endpoint[len("unix://"):]
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen on %s: %v", sockPath, err)
	}
	defer ln.Close()

	conn, err := DialPlugin(endpoint, "test-plugin")
	if err != nil {
		t.Fatalf("dial %s: %v", endpoint, err)
	}
	conn.Close()
}

// TestDialPluginTCPScheme 验证 DialPlugin 的 tcp:// 解析 (Windows loopback 等价路径).
func TestDialPluginTCPScheme(t *testing.T) {
	// 启动 loopback TCP listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := "tcp://" + ln.Addr().String()

	conn, err := DialPlugin(addr, "test-plugin")
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	conn.Close()
}

// TestDialPluginUnknownScheme 验证未知 scheme 返回 error.
func TestDialPluginUnknownScheme(t *testing.T) {
	_, err := DialPlugin("foo://bar", "test-plugin")
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}
