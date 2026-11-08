package lua

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	lualib "github.com/yuin/gopher-lua"
)

func loadRealEngram(t *testing.T) *Host {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, ConfigPath: cfg,
		DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()},
	})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	return h
}

// The slot's query runs only when a client asks, so a broken one fails nowhere but a log line.
func TestRealEngramSlotAnswers(t *testing.T) {
	h := loadRealEngram(t)
	slots, err := h.QuerySlots(context.Background(), "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 {
		t.Fatalf("want the engram slot, got %d", len(slots))
	}
	s := slots[0]
	if s.Plugin != "engram" || s.Status == nil || len(s.Items) != 3 || len(s.Actions) != 3 {
		t.Fatalf("unexpected slot: %+v", s)
	}
}

func TestRealEngramSlotActionsReturnTheirOwnWords(t *testing.T) {
	h := loadRealEngram(t)
	for _, a := range []string{"reflect", "flush_facts"} {
		out, err := h.TriggerSlotAction(context.Background(), "engram", "", a)
		if err != nil || out == "" {
			t.Fatalf("%s: out=%q err=%v", a, out, err)
		}
	}
	_, err := h.TriggerSlotAction(context.Background(), "engram", "", "nope")
	if err == nil {
		t.Fatal("an unknown action must be an error")
	}
	if !strings.Contains(err.Error(), "unknown slot action: nope") || strings.Contains(err.Error(), "stack traceback") {
		t.Fatalf("a person should read what the plugin raised, got %q", err)
	}
}

// A plugin busy for minutes (an analyze call holds its lock) must not hold up the knowledge list.
func TestQuerySlotsDoesNotWaitForABusyPlugin(t *testing.T) {
	h := loadRealEngram(t)
	first, err := h.QuerySlots(context.Background(), "knowledge")
	if err != nil || len(first) != 1 {
		t.Fatalf("first query: %v %v", first, err)
	}
	p := h.snapshot()[0]
	p.mu.Lock()
	defer p.mu.Unlock()
	got := make(chan int, 1)
	go func() {
		slots, _ := h.QuerySlots(context.Background(), "knowledge")
		got <- len(slots)
	}()
	select {
	case n := <-got:
		if n != 1 {
			t.Fatalf("a busy plugin's slot should stay listed, got %d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("QuerySlots waited on a busy plugin")
	}
}

func TestEngramPayloadErrorClassification(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "plugins", "engram", "init.lua"))
	if err != nil {
		t.Fatal(err)
	}
	fn := regexp.MustCompile(`(?s)local function is_payload_error\(err\).*?\nend\n`).Find(src)
	if fn == nil {
		t.Fatal("is_payload_error not found")
	}
	L := lualib.NewState()
	defer L.Close()
	if err := L.DoString(string(fn) + "\nreturn is_payload_error"); err != nil {
		t.Fatal(err)
	}
	classify := L.Get(-1)
	L.Pop(1)
	for text, want := range map[string]bool{
		"llm: bad request (status 400): nope":               true,
		"llm: refused (status 400): x":                      true,
		"context length exceeded":                           true,
		"response stopped by safety classifier":             true,
		"timeout after 400ms":                               false,
		"dial tcp 127.0.0.1:11400: connection refused":      false,
		"llm: rate limited (status 429): too many requests": false,
		"llm: server error (status 503): overloaded":        false,
		"connection blocked by firewall":                    false,
		"request 8400 failed":                               false,
	} {
		if err := L.CallByParam(lualib.P{Fn: classify, NRet: 1, Protect: true}, lualib.LString(text)); err != nil {
			t.Fatal(err)
		}
		got := lualib.LVAsBool(L.Get(-1))
		L.Pop(1)
		if got != want {
			t.Errorf("%q: got %v want %v", text, got, want)
		}
	}
}
