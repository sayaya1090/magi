package lua

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// LoadDir walks only what could be a plugin: a missing root is nothing (not an error), and an
// entry without a plugin.toml is passed over in silence.
func TestLoadDirWalksOnlyPlugins(t *testing.T) {
	h := &Host{}
	if loaded, errs := h.LoadDir(context.Background(), filepath.Join(t.TempDir(), "absent")); loaded != nil || errs != nil {
		t.Fatalf("a missing plugins dir is an empty machine, not a failure: (%v, %v)", loaded, errs)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "not-a-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if loaded, errs := h.LoadDir(context.Background(), root); len(loaded) != 0 || len(errs) != 0 {
		t.Fatalf("no plugin.toml, no plugin: (%v, %v)", loaded, errs)
	}
}

// The presence questions answer honestly on a host with nothing loaded.
func TestEmptyHostAnswersHonestly(t *testing.T) {
	h := &Host{}
	if h.Has("engram") {
		t.Fatal("nothing is loaded")
	}
	if h.HasEventHandlers("turn_finished") {
		t.Fatal("nobody is listening")
	}
}

// The lua tool's face and its error shape.
func TestLuaToolFaceAndErrResult(t *testing.T) {
	lt := &luaTool{name: "mcp_x", description: "does x", schema: []byte(`{"type":"object"}`)}
	if lt.Name() != "mcp_x" || lt.Description() != "does x" || string(lt.Schema()) != `{"type":"object"}` {
		t.Fatal("the face is the declaration, verbatim")
	}
	res := errResult("it broke")
	if !res.IsError || string(res.Content) != `"it broke"` {
		t.Fatalf("an error result is marked and carries its words: %+v", res)
	}
}

// Watch stands up its watcher and returns; a cancelled context ends the loop — the host's
// lifetime, not the caller's patience.
func TestWatchStandsUpAndObeysCancel(t *testing.T) {
	h := &Host{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Watch(ctx); err != nil {
		t.Fatalf("an empty host watches nothing and errors on nothing: %v", err)
	}
}
