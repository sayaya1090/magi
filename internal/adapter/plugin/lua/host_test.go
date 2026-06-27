package lua

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// writePlugin creates a plugin dir with the given manifest and init.lua.
func writePlugin(t *testing.T, manifest, initLua string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(initLua), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func execTool(t *testing.T, tool port.Tool, args string, workdir string) (string, bool) {
	t.Helper()
	res, err := tool.Execute(context.Background(), json.RawMessage(args), port.ToolEnv{Workdir: workdir})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	return s, res.IsError
}

// Load registers a plugin's tool into the shared registry (F-PLUGIN).
func TestLoadRegistersTool(t *testing.T) {
	dir := writePlugin(t,
		`name="echo"`+"\n"+`capabilities=["tool"]`,
		`magi.register_tool{name="echo", description="echo", execute=function(a) return a.msg end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)

	info, err := h.Load(context.Background(), dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if info.Name != "echo" {
		t.Errorf("info.Name=%q want echo", info.Name)
	}
	tool, ok := reg.Get("echo")
	if !ok {
		t.Fatal("tool 'echo' not registered")
	}
	got, isErr := execTool(t, tool, `{"msg":"hi"}`, t.TempDir())
	if isErr || got != "hi" {
		t.Errorf("echo returned %q err=%v, want hi", got, isErr)
	}
}

// A bridge call without the declared permission is denied (F-PLUGIN sandbox).
func TestPermissionEnforced(t *testing.T) {
	// Plugin declares NO fs permission but tries to read a file.
	dir := writePlugin(t,
		`name="peek"`+"\n"+`capabilities=["tool"]`,
		`magi.register_tool{name="peek", execute=function(a)
		   local c, err = magi.read_file(a.path)
		   if c == nil then return err, true end
		   return c
		 end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	os.WriteFile(filepath.Join(wd, "secret.txt"), []byte("top secret"), 0o644)

	tool, _ := reg.Get("peek")
	got, isErr := execTool(t, tool, `{"path":"secret.txt"}`, wd)
	if !isErr {
		t.Fatalf("expected permission error, got success: %q", got)
	}
	if got == "" {
		t.Errorf("expected a denial message, got empty")
	}
}

// With the permission granted, the same read succeeds.
func TestPermissionGranted(t *testing.T) {
	dir := writePlugin(t,
		`name="peek2"`+"\n"+`capabilities=["tool"]`+"\n"+`permissions=["fs:read:."]`,
		`magi.register_tool{name="peek2", execute=function(a)
		   local c, err = magi.read_file(a.path)
		   if c == nil then return err, true end
		   return c
		 end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	os.WriteFile(filepath.Join(wd, "ok.txt"), []byte("hello"), 0o644)

	tool, _ := reg.Get("peek2")
	got, isErr := execTool(t, tool, `{"path":"ok.txt"}`, wd)
	if isErr || got != "hello" {
		t.Errorf("got %q err=%v, want hello", got, isErr)
	}
}

// Reload picks up edits to the plugin source, swapping the registered tool.
func TestHotReload(t *testing.T) {
	dir := writePlugin(t,
		`name="ver"`+"\n"+`capabilities=["tool"]`,
		`magi.register_tool{name="ver", execute=function() return "v1" end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	tool, _ := reg.Get("ver")
	if got, _ := execTool(t, tool, `{}`, t.TempDir()); got != "v1" {
		t.Fatalf("before reload: %q want v1", got)
	}

	// Edit the plugin and reload.
	os.WriteFile(filepath.Join(dir, "init.lua"),
		[]byte(`magi.register_tool{name="ver", execute=function() return "v2" end}`), 0o644)
	if err := h.Reload("ver"); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	tool2, _ := reg.Get("ver")
	if got, _ := execTool(t, tool2, `{}`, t.TempDir()); got != "v2" {
		t.Errorf("after reload: %q want v2", got)
	}
}

// Unload removes the plugin's tools from the registry.
func TestUnload(t *testing.T) {
	dir := writePlugin(t,
		`name="temp"`+"\n"+`capabilities=["tool"]`,
		`magi.register_tool{name="temp", execute=function() return "x" end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)
	h.Load(context.Background(), dir)
	if _, ok := reg.Get("temp"); !ok {
		t.Fatal("tool should be registered")
	}
	if err := h.Unload("temp"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("temp"); ok {
		t.Error("tool should be gone after unload")
	}
}

// The dangerous stdlib is not reachable from plugin code (sandbox).
func TestSandboxBlocksOS(t *testing.T) {
	dir := writePlugin(t,
		`name="evil"`+"\n"+`capabilities=["tool"]`,
		`magi.register_tool{name="evil", execute=function()
		   if os == nil then return "no-os" end
		   return "has-os"
		 end}`,
	)
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	tool, _ := reg.Get("evil")
	got, _ := execTool(t, tool, `{}`, t.TempDir())
	if got != "no-os" {
		t.Errorf("sandbox: os should be nil in plugin, got %q", got)
	}
}

// The shipped example plugin loads and works.
func TestExamplePlugin(t *testing.T) {
	dir := "../../../../plugins/examples/wordcount"
	if _, err := os.Stat(dir); err != nil {
		t.Skip("example plugin not present")
	}
	reg := builtin.NewRegistry()
	h := NewHost(reg, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load example: %v", err)
	}
	wd := t.TempDir()
	os.WriteFile(filepath.Join(wd, "f.txt"), []byte("one two three"), 0o644)
	tool, ok := reg.Get("wordcount")
	if !ok {
		t.Fatal("wordcount tool not registered")
	}
	got, isErr := execTool(t, tool, `{"path":"f.txt"}`, wd)
	if isErr {
		t.Fatalf("wordcount error: %s", got)
	}
	if got != "f.txt has 3 words" {
		t.Errorf("got %q want 'f.txt has 3 words'", got)
	}
}
