package metrics

import (
	"strings"
	"testing"
)

func TestCounterIncAndValue(t *testing.T) {
	c := NewCounter("yaa_test_calls_total", "server", "result")
	c.Inc("a", "success")
	c.Inc("a", "success")
	c.Add(3, "a", "error")
	c.Inc("b", "success")
	if got := c.Value("a", "success"); got != 2 {
		t.Errorf("Counter a/success=%d want 2", got)
	}
	if got := c.Value("a", "error"); got != 3 {
		t.Errorf("Counter a/error=%d want 3", got)
	}
	if got := c.Value("b", "success"); got != 1 {
		t.Errorf("Counter b/success=%d want 1", got)
	}
	if got := c.Value("a", "timeout"); got != 0 {
		t.Errorf("Counter a/timeout=%d want 0", got)
	}
}

func TestCounterLabelLenPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on wrong label count")
		}
	}()
	c := NewCounter("yaa_test_total", "server")
	c.Inc("a", "extra") // label 值数量不匹配, 应 panic
}

func TestGaugeSetAndMod(t *testing.T) {
	g := NewGauge("yaa_test_servers", "status", "transport")
	g.Set(3, "connected", "stdio")
	g.Set(2, "disconnected", "sse")
	g.Inc("connected", "stdio")
	if got := g.Value("connected", "stdio"); got != 4 {
		t.Errorf("Gauge connected/stdio=%d want 4", got)
	}
	if got := g.Value("disconnected", "sse"); got != 2 {
		t.Errorf("Gauge disconnected/sse=%d want 2", got)
	}
}

func TestHistogramObserve(t *testing.T) {
	h := NewHistogram("yaa_test_duration_seconds", "server")
	h.Observe(0.05, "a")
	h.Observe(0.2, "a")
	h.Observe(5, "b")
	if got := h.Count("a"); got != 2 {
		t.Errorf("hist count(a)=%d want 2", got)
	}
	if got := h.SumMilli("a"); got != 250 { // 0.05+0.2 = 0.25s = 250ms
		t.Errorf("hist sumMilli(a)=%d want 250", got)
	}
	if got := h.Count("b"); got != 1 {
		t.Errorf("hist count(b)=%d want 1", got)
	}
}

func TestRegistryWritePrometheus(t *testing.T) {
	r := NewRegistry()
	c := NewCounter("yaa_mcp_reconnects_total", "server", "result")
	c.Inc("fake", "success")
	c.Inc("fake", "error")
	g := NewGauge("yaa_mcp_tools", "server")
	g.Set(2, "fake")
	h := NewHistogram("yaa_mcp_tool_call_duration_seconds", "server", "tool")
	h.Observe(0.3, "fake", "alpha")
	r.MustRegister(c)
	r.MustRegister(g)
	r.MustRegister(h)
	var sb strings.Builder
	r.WritePrometheus(&sb)
	out := sb.String()
	// Counter 行
	if !strings.Contains(out, `yaa_mcp_reconnects_total{server="fake",result="success"} 1`) {
		t.Errorf("missing counter success line in:\n%s", out)
	}
	if !strings.Contains(out, `yaa_mcp_reconnects_total{server="fake",result="error"} 1`) {
		t.Errorf("missing counter error line in:\n%s", out)
	}
	// Gauge 行
	if !strings.Contains(out, `yaa_mcp_tools{server="fake"} 2`) {
		t.Errorf("missing gauge line in:\n%s", out)
	}
	// Histogram bucket 行
	if !strings.Contains(out, `yaa_mcp_tool_call_duration_seconds_bucket{server="fake",tool="alpha",le="0.25"}`) {
		t.Errorf("missing histogram bucket le=0.25 in:\n%s", out)
	}
	if !strings.Contains(out, `yaa_mcp_tool_call_duration_seconds_count{server="fake",tool="alpha"} 1`) {
		t.Errorf("missing histogram count in:\n%s", out)
	}
}

func TestRegistryMustRegisterPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate register")
		}
	}()
	r := NewRegistry()
	c1 := NewCounter("yaa_test_x")
	c2 := NewCounter("yaa_test_x")
	r.MustRegister(c1)
	r.MustRegister(c2)
}
