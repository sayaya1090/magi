package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// TestGrepSymlinkJail guards O15: WalkDir hands grep symlinked FILES as entries,
// and os.ReadFile follows them. A symlink inside the workdir that points outside
// must not leak that file's contents, while a symlink to an in-workdir file is
// still searched.
func TestGrepSymlinkJail(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	os.MkdirAll(work, 0o755)
	os.MkdirAll(outside, 0o755)

	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("TOPSECRET_TOKEN=abc\n"), 0o644)
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(work, "escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// An in-workdir file and an in-workdir symlink to it — legitimately searchable.
	os.WriteFile(filepath.Join(work, "real.txt"), []byte("TOPSECRET_TOKEN=inside\n"), 0o644)
	os.Symlink(filepath.Join(work, "real.txt"), filepath.Join(work, "inlink"))

	env := port.ToolEnv{Workdir: work}
	res, _ := Grep{}.Execute(context.Background(),
		json.RawMessage(`{"pattern":"TOPSECRET_TOKEN","path":"."}`), env)
	out := resultText(t, res)

	if strings.Contains(out, "escape:") {
		t.Errorf("grep leaked an out-of-workdir file through a symlink: %q", out)
	}
	if !strings.Contains(out, "real.txt") {
		t.Errorf("grep should still match the in-workdir file: %q", out)
	}
}
