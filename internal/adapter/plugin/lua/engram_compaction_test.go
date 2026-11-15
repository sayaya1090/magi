package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// Compaction is a core feature that knows nothing of memory. Engram, which does keep things, tells
// the summariser through a compaction advisor which of this session's findings it already saved,
// so the brief points at them instead of restating them.
func TestRealEngramAdvisesCompactionAboutWhatItKept(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, "plugin-data"), 0o700); err != nil {
		t.Fatal(err)
	}
	kept := `{"compaction_kept:s1":"[\"lesson: retry 429 in the harness only\",\"skill: ui_prototype_overlay_capture\"]"}`
	if err := os.WriteFile(filepath.Join(data, "plugin-data", "engram.json"), []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reg := &fakeAdvisorReg{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: reg, ConfigPath: cfg,
		DataDir: data, Runtime: RuntimeInfo{Workdir: t.TempDir()}})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	if len(reg.advisors) != 1 {
		t.Fatalf("engram registered %d compaction advisors, want 1", len(reg.advisors))
	}
	got, err := reg.advisors[0].AdviseCompaction(context.Background(), port.CompactionQuery{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"retry 429 in the harness only", "ui_prototype_overlay_capture", "recall_memory", "one line"} {
		if !strings.Contains(got, want) {
			t.Errorf("advice lacks %q:\n%s", want, got)
		}
	}
	other, _ := reg.advisors[0].AdviseCompaction(context.Background(), port.CompactionQuery{SessionID: "s2"})
	if other != "" {
		t.Errorf("a session engram kept nothing for got advice: %q", other)
	}
}
