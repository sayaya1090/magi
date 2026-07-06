package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// N4: an invalid glob pattern must surface as an error, not silently match nothing.
func TestGlobBadPatternErrors(t *testing.T) {
	got, isErr := run(t, Glob{}, globArgs{Pattern: "["}, nil)
	if !isErr || !strings.Contains(got, "invalid glob pattern") {
		t.Fatalf("bad glob pattern: got=%q isErr=%v", got, isErr)
	}
}

// N9: an empty glob result marshals to [] (a valid empty array), not null.
func TestGlobEmptyResultIsArray(t *testing.T) {
	raw, _ := json.Marshal(globArgs{Pattern: "nope-*.xyz"})
	res, _ := Glob{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: t.TempDir()})
	if res.IsError || strings.TrimSpace(string(res.Content)) != "[]" {
		t.Fatalf("empty glob: want \"[]\", got %s (isErr=%v)", res.Content, res.IsError)
	}
}

// N5: a pattern that names a dotted segment opts into otherwise-hidden paths.
func TestGlobHiddenOptIn(t *testing.T) {
	got, _ := runJSON(t, Glob{}, globArgs{Pattern: ".github/**/*.yml"}, func(d string) {
		writeFile(d, ".github/workflows/ci.yml", "x")
		writeFile(d, "visible.go", "x")
	})
	if len(got) != 1 || got[0] != ".github/workflows/ci.yml" {
		t.Fatalf("hidden opt-in glob matched %v, want [.github/workflows/ci.yml]", got)
	}
}

// A plain pattern still skips hidden paths (no regression on the noise filter).
func TestGlobPlainSkipsHidden(t *testing.T) {
	got, _ := runJSON(t, Glob{}, globArgs{Pattern: "**/*.yml"}, func(d string) {
		writeFile(d, ".github/workflows/ci.yml", "x")
	})
	if len(got) != 0 {
		t.Fatalf("plain glob should skip hidden paths, matched %v", got)
	}
}

// N7: an empty read path is rejected with a clear message, not "is a directory".
func TestReadEmptyPath(t *testing.T) {
	got, isErr := run(t, Read{}, readArgs{Path: ""}, nil)
	if !isErr || !strings.Contains(got, "path is required") {
		t.Fatalf("empty read path: got=%q isErr=%v", got, isErr)
	}
}

// N6: reading past the end notes the over-page rather than reading as an empty file.
func TestReadOffsetPastEOFNote(t *testing.T) {
	got, isErr := run(t, Read{}, readArgs{Path: "a.txt", Offset: 100}, func(d string) {
		writeFile(d, "a.txt", "l1\nl2\nl3\nl4\n")
	})
	if isErr || !strings.Contains(got, "past end of file") || !strings.Contains(got, "4 lines") {
		t.Fatalf("offset past EOF: got=%q isErr=%v", got, isErr)
	}
}

// N7/N8: an empty write path is rejected, and a real OS error stays jail-relative
// (no absolute workdir path leaked).
func TestWriteEmptyPathAndRelErr(t *testing.T) {
	got, isErr := run(t, Write{}, writeArgs{Path: "", Content: "x"}, nil)
	if !isErr || !strings.Contains(got, "path is required") {
		t.Fatalf("empty write path: got=%q isErr=%v", got, isErr)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(writeArgs{Path: "sub", Content: "x"}) // overwrite a dir
	res, _ := Write{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	msg := string(res.Content)
	if !res.IsError {
		t.Fatal("writing over a directory should error")
	}
	if strings.Contains(msg, dir) {
		t.Fatalf("error leaked the absolute workdir path: %s", msg)
	}
	if !strings.Contains(msg, "sub") {
		t.Fatalf("error should name the relative path: %s", msg)
	}
}

// N12: bash_output/bash_kill with a missing id give a clear "id is required".
func TestBgEmptyIDValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() (session.ToolResult, error)
	}{
		{"bash_output", func() (session.ToolResult, error) {
			return BashOutput{}.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
		}},
		{"bash_kill", func() (session.ToolResult, error) {
			return BashKill{}.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
		}},
	} {
		res, _ := tc.run()
		if !res.IsError || !strings.Contains(string(res.Content), "id is required") {
			t.Fatalf("%s empty id: content=%s isErr=%v", tc.name, res.Content, res.IsError)
		}
	}
}
