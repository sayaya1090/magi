package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// read with offset past EOF returns empty, not an error.
func TestReadOffsetPastEOF(t *testing.T) {
	got, isErr := run(t, Read{}, readArgs{Path: "a.txt", Offset: 99}, func(d string) { writeFile(d, "a.txt", "one\ntwo\n") })
	if isErr {
		t.Errorf("offset past EOF should not error, got %q", got)
	}
}

// edit on a missing file errors (not panic).
func TestEditMissingFile(t *testing.T) {
	if _, isErr := run(t, Edit{}, editArgs{Path: "nope.txt", Old: "a", New: "b"}, nil); !isErr {
		t.Error("edit on missing file should error")
	}
}

// write rejects an absolute path outside the workdir.
func TestWriteAbsoluteEscape(t *testing.T) {
	// An absolute path outside the workdir must be rejected. Use an OS-absolute
	// path (os.TempDir is absolute on Windows too; "/tmp/..." is not).
	outside := filepath.Join(os.TempDir(), "evil-xyz.txt")
	if _, isErr := run(t, Write{}, writeArgs{Path: outside, Content: "x"}, nil); !isErr {
		t.Error("write to absolute path outside workdir should error")
	}
}

// glob with ** matches at the root level (zero intermediate dirs).
func TestGlobDoubleStarRoot(t *testing.T) {
	got, _ := runJSON(t, Glob{}, globArgs{Pattern: "**/*.go"}, func(d string) {
		writeFile(d, "a.go", "")
		writeFile(d, "sub/b.go", "")
	})
	if len(got) != 2 {
		t.Errorf("**/*.go matched %v, want 2", got)
	}
}

// bash honors a timeout.
func TestBashTimeout(t *testing.T) {
	raw, _ := json.Marshal(bashArgs{Command: "sleep 5", Timeout: 1})
	start := time.Now()
	res, _ := Bash{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: t.TempDir()})
	// timeout(1s) + WaitDelay(2s) bounds it well under the command's 5s sleep.
	if time.Since(start) > 4*time.Second {
		t.Error("bash did not honor timeout")
	}
	var out string
	_ = json.Unmarshal(res.Content, &out)
	if !res.IsError || !strings.Contains(out, "timed out") {
		t.Errorf("bash timeout: out=%q isErr=%v", out, res.IsError)
	}
}

// multiedit with a duplicate-but-not-unique old string errors atomically.
func TestMultiEditNotUnique(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "f", "x x")
	raw, _ := json.Marshal(multiEditArgs{Path: "f", Edits: []editHunk{{Old: "x", New: "y"}}})
	res, _ := MultiEdit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if !res.IsError {
		t.Error("multiedit non-unique should error")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f")); string(b) != "x x" {
		t.Errorf("file must be unchanged, got %q", b)
	}
}

// tools that need capabilities fail cleanly when the env lacks them.
func TestToolsWithoutEnvCaps(t *testing.T) {
	for _, tc := range []struct {
		tool port.Tool
		args any
	}{
		{Task{}, taskArgs{Agent: "x", Prompt: "y"}},
		{TodoWrite{}, todoWriteArgs{Todos: nil}},
		{Remember{}, rememberArgs{Text: "x"}},
		{Skill{}, skillArgs{Name: "x"}},
	} {
		raw, _ := json.Marshal(tc.args)
		res, err := tc.tool.Execute(context.Background(), raw, port.ToolEnv{})
		if err != nil {
			t.Errorf("%s returned go error: %v", tc.tool.Name(), err)
		}
		if !res.IsError {
			t.Errorf("%s without env caps should be a tool error", tc.tool.Name())
		}
	}
}

func TestFindContext(t *testing.T) {
	got, isErr := runJSON(t, FindContext{}, findCtxArgs{Query: "authentication login token"}, func(d string) {
		writeFile(d, "auth/login.go", "func Login(token string) {}")
		writeFile(d, "readme.md", "unrelated docs")
		writeFile(d, "util/token.go", "var token = 1")
	})
	if isErr || len(got) == 0 {
		t.Fatalf("findcontext: got %v err=%v", got, isErr)
	}
	top := got[0].(map[string]any)["path"].(string)
	if top != "auth/login.go" {
		t.Errorf("top result=%q want auth/login.go", top)
	}
}
