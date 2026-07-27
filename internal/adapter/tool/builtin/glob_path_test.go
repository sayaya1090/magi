package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func globPathIn(t *testing.T, dir, path, pattern string) ([]string, bool, string) {
	t.Helper()
	raw, _ := json.Marshal(globArgs{Pattern: pattern, Path: path})
	res, err := Glob{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	if err != nil {
		t.Fatalf("Execute returned error (should be in result): %v", err)
	}
	if res.IsError {
		var msg string
		_ = json.Unmarshal(res.Content, &msg)
		return nil, true, msg
	}
	var out []string
	if e := json.Unmarshal(res.Content, &out); e != nil {
		t.Fatalf("result is not a string array: %s", res.Content)
	}
	return out, false, ""
}

// glob was the only tree-walking tool without `path` — read, write, edit, list, lsp, grep and
// astgrep all take one — so a model that had just scoped a grep wrote the glob the same way, and
// encoding/json dropped the field without a word. Both halves of the answer were then wrong in the
// same undetectable way: matches appeared from OUTSIDE the named directory, and a pattern written
// for that directory ("*.h") matched nothing at all, because it was tested against a
// workspace-relative path that still had directories in front of it.
func TestGlobScopesToPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "runtime/caml/mlvalues.h", "x")
	writeFile(dir, "runtime/caml/memory.h", "x")
	writeFile(dir, "runtime/major_gc.c", "x")
	writeFile(dir, "otherlib/stray.h", "x")

	// The shape that answered `[]` while the directory held the files.
	got, isErr, msg := globPathIn(t, dir, "runtime/caml", "*.h")
	if isErr {
		t.Fatalf("errored: %s", msg)
	}
	if strings.Join(got, ",") != "runtime/caml/memory.h,runtime/caml/mlvalues.h" {
		t.Errorf("matched %v, want both headers under the named directory — and workspace-relative, "+
			"so the paths can be handed to another tool unchanged", got)
	}
	// Scoping must EXCLUDE the sibling tree; without `path` this returned it too.
	got, isErr, msg = globPathIn(t, dir, "runtime", "**/*.h")
	if isErr {
		t.Fatalf("errored: %s", msg)
	}
	if strings.Join(got, ",") != "runtime/caml/memory.h,runtime/caml/mlvalues.h" {
		t.Errorf("matched %v, want only the files under runtime/ (otherlib/stray.h is out of scope)", got)
	}
}

// An absolute pattern under an explicit path is anchored to THAT directory, and one pointing
// elsewhere says so — the same rule as the workspace, one level down.
func TestGlobPathAnchorsAndRejects(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "runtime/major_gc.c", "x")
	writeFile(dir, "otherlib/stray.c", "x")

	got, isErr, msg := globPathIn(t, dir, "runtime", dir+"/runtime/*.c")
	if isErr {
		t.Fatalf("errored: %s", msg)
	}
	if strings.Join(got, ",") != "runtime/major_gc.c" {
		t.Errorf("matched %v, want the one file under the scoped directory", got)
	}
	out, isErr, msg := globPathIn(t, dir, "runtime", dir+"/otherlib/*.c")
	if !isErr {
		t.Fatalf("a pattern outside the scoped directory returned %v instead of an error", out)
	}
	if !strings.Contains(msg, "outside the searched directory") {
		t.Errorf("message = %q, want it to name the searched directory", msg)
	}
}

// A path that is not a readable directory must SAY so. WalkDir hands the error to the walk
// function, which skips unreadable entries — so a typo'd directory would otherwise walk nothing
// and return the same `[]` that means "searched, found nothing".
func TestGlobPathThatIsNotADirectorySaysSo(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "runtime/major_gc.c", "x")

	for _, p := range []string{"runtiem", "runtime/major_gc.c"} {
		out, isErr, msg := globPathIn(t, dir, p, "*.c")
		if !isErr {
			t.Fatalf("path %q returned %v instead of an error", p, out)
		}
		if !strings.Contains(msg, "not a directory") && !strings.Contains(msg, "outside") {
			t.Errorf("path %q: message = %q, want it to name the unusable path", p, msg)
		}
	}
}

// An escape attempt is refused by the same jail every other path-taking tool uses.
func TestGlobPathCannotEscapeTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "a.c", "x")

	out, isErr, _ := globPathIn(t, dir, "../..", "*.c")
	if !isErr {
		t.Fatalf("an escaping path returned %v instead of an error", out)
	}
}

// Omitting `path` must behave exactly as before: the whole workspace, workspace-relative results.
func TestGlobWithoutPathIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeFile(dir, "runtime/caml/mlvalues.h", "x")
	writeFile(dir, "otherlib/stray.h", "x")

	got, isErr, msg := globIn(t, dir, "**/*.h")
	if isErr {
		t.Fatalf("errored: %s", msg)
	}
	if strings.Join(got, ",") != "otherlib/stray.h,runtime/caml/mlvalues.h" {
		t.Errorf("matched %v, want every header in the workspace", got)
	}
}
