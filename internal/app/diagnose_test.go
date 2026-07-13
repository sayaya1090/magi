package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

func TestPathArg(t *testing.T) {
	if got := pathArg(json.RawMessage(`{"path":"a/b.go"}`)); got != "a/b.go" {
		t.Errorf("pathArg = %q", got)
	}
	if got := pathArg(json.RawMessage(`not json`)); got != "" {
		t.Errorf("bad args → empty path, got %q", got)
	}
}

func TestAppendToContent(t *testing.T) {
	// A JSON-string content gets the extra text appended, still a JSON string.
	out := appendToContent(json.RawMessage(`"result"`), "\n[note]")
	var s string
	if err := json.Unmarshal(out, &s); err != nil || s != "result\n[note]" {
		t.Errorf("appendToContent = %s (decoded %q, err %v)", out, s, err)
	}
}

// diagnose runs gofmt -e on a Go file and reports a syntax error (the post-edit
// self-correction feedback documented in the manual).
func TestDiagnoseGoSyntaxError(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{})
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "bad.go"), []byte("package x\n\nfunc {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := a.diagnose(context.Background(), wd, "bad.go")
	if strings.TrimSpace(out) == "" {
		t.Error("gofmt should report a syntax error for a malformed Go file")
	}

	// A gofmt-clean file produces no syntax diagnostic.
	if err := os.WriteFile(filepath.Join(wd, "ok.go"), []byte("package x\n\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// (go vet may not run cleanly outside a module; the gofmt-clean path returns no
	// syntax error, which is what we assert here.)
	if out, _ := a.diagnose(context.Background(), wd, "ok.go"); strings.Contains(out, "expected") {
		t.Errorf("clean file should have no syntax error, got %q", out)
	}
}
