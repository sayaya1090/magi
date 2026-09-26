package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// A save that breaks a plugin is a hot reload that fails, and the person editing it is the one who
// needs to read why. The watcher used to discard Reload's error.
func TestAFailedHotReloadIsLogged(t *testing.T) {
	dir := writePlugin(t,
		`name="echo"`+"\n"+`capabilities=["tool"]`,
		`magi.register_tool{name="echo", description="echo", execute=function(a) return a.msg end}`,
	)
	var mu sync.Mutex
	var lines []string
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), Logf: func(s string) {
		mu.Lock()
		lines = append(lines, s)
		mu.Unlock()
	}})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := h.Watch(ctx); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte("this is not lua ((("), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, l := range lines {
			if strings.Contains(l, "[echo] hot reload") {
				mu.Unlock()
				return
			}
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("a broken save produced no hot-reload line; logged: %q", lines)
}
