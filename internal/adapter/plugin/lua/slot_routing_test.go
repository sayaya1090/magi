package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

func writeSlotPlugin(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name = \"" + name + "\"\nversion = \"0.0.1\"\nentry = \"init.lua\"\ncapabilities = [\"ui\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func slotBody(ids ...string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString("magi.register_slot{ id = \"" + id + "\", target = \"knowledge\", title = \"" + id + "\",\n")
		b.WriteString("  query = function() return {} end,\n")
		b.WriteString("  execute = function(action) return \"" + id + ":\" .. action end }\n")
	}
	return b.String()
}

func TestSlotActionRunsTheSlotItNames(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, ConfigPath: cfg,
		DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()},
	})
	root := t.TempDir()
	ctx := context.Background()
	for name, ids := range map[string][]string{"alpha": {"one", "two"}, "beta": {"only"}} {
		if _, err := h.Load(ctx, writeSlotPlugin(t, root, name, slotBody(ids...))); err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
	}
	for _, c := range []struct{ plugin, slot, want, wantErr string }{
		{"alpha", "one", "one:ping", ""},
		{"alpha", "two", "two:ping", ""},
		{"beta", "", "only:ping", ""},
		{"alpha", "", "", "name one"},
		{"alpha", "missing", "", "no slot"},
		{"", "", "", "names no plugin"},
		{"", "only", "", "names no plugin"},
	} {
		got, err := h.TriggerSlotAction(ctx, c.plugin, c.slot, "ping")
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || got != "" {
				t.Errorf("%q/%q: got %q err=%v, want error with %q", c.plugin, c.slot, got, err, c.wantErr)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%q/%q: got %q err=%v, want %q", c.plugin, c.slot, got, err, c.want)
		}
	}
}
