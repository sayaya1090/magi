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

// TestFindContextSymlinkJail guards O16: findcontext walks the tree and reads
// (and ranks a snippet of) each file. A symlink inside the workdir pointing
// outside must not leak that file's snippet; an in-workdir target is still ranked.
func TestFindContextSymlinkJail(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	outside := filepath.Join(root, "outside")
	os.MkdirAll(work, 0o755)
	os.MkdirAll(outside, 0o755)

	os.WriteFile(filepath.Join(outside, "credentials.txt"),
		[]byte("aws_secret_access_key = LEAKED_XYZ\n"), 0o644)
	if err := os.Symlink(filepath.Join(outside, "credentials.txt"),
		filepath.Join(work, "credentials")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	os.WriteFile(filepath.Join(work, "aws_credentials.txt"),
		[]byte("aws_secret_access_key = inside\n"), 0o644)

	env := port.ToolEnv{Workdir: work}
	res, _ := FindContext{}.Execute(context.Background(),
		json.RawMessage(`{"query":"credentials aws secret"}`), env)
	out := resultText(t, res)

	if strings.Contains(out, "LEAKED_XYZ") {
		t.Errorf("findcontext leaked an out-of-workdir snippet through a symlink: %q", out)
	}
	if !strings.Contains(out, "aws_credentials.txt") {
		t.Errorf("findcontext should still rank the in-workdir file: %q", out)
	}
}
