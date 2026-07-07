package builtin

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// parseAstGrepStream must never surface a match whose file resolves outside the
// workdir. ast-grep walks the tree in an external process the in-code symlink guard
// can't reach, so the parser is the last line enforcing the jail invariant that
// grep/findcontext already hold: out-of-jail paths are dropped, not returned as an
// absolute path with their snippet.
func TestAstGrepStreamDropsOutsidePath(t *testing.T) {
	workdir := filepath.FromSlash("/work")
	outside := filepath.ToSlash(filepath.FromSlash("/etc/secret.go"))
	line := `{"file":"` + outside + `","text":"const Token = \"TOPSECRET\"","lines":"const Token = \"TOPSECRET\"","range":{"start":{"line":4}}}` + "\n"

	ms := parseAstGrepStream([]byte(line), workdir)
	if len(ms) != 0 {
		t.Fatalf("outside-workdir match must be dropped; got %+v", ms)
	}
}

// In-workdir matches survive and come back as clean workdir-relative slash paths.
func TestAstGrepStreamKeepsInsidePath(t *testing.T) {
	workdir := filepath.FromSlash("/work")
	inside := filepath.ToSlash(filepath.Join(workdir, "pkg", "a.go"))
	line := `{"file":"` + inside + `","text":"x","lines":"x","range":{"start":{"line":0}}}` + "\n"

	ms := parseAstGrepStream([]byte(line), workdir)
	if len(ms) != 1 {
		t.Fatalf("in-workdir match should survive; got %+v", ms)
	}
	if ms[0].File != "pkg/a.go" {
		t.Errorf("path should be workdir-relative slash form; got %q", ms[0].File)
	}
	if ms[0].Line != 1 {
		t.Errorf("line should be 1-based (0→1); got %d", ms[0].Line)
	}
}

// A relative file path (should ast-grep ever emit one) is resolved against the
// workdir rather than mistaken for an escape.
func TestAstGrepStreamRelativePathKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix path fixture")
	}
	workdir := filepath.FromSlash("/work")
	line := `{"file":"sub/b.go","text":"y","lines":"y","range":{"start":{"line":2}}}` + "\n"

	ms := parseAstGrepStream([]byte(line), workdir)
	if len(ms) != 1 || ms[0].File != "sub/b.go" {
		t.Fatalf("relative in-workdir path should be kept as-is; got %+v", ms)
	}
	if strings.HasPrefix(ms[0].File, "..") {
		t.Errorf("must not look like an escape; got %q", ms[0].File)
	}
}
