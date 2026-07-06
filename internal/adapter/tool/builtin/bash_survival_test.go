//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A child backgrounded with `&` in a synchronous bash call must OUTLIVE the call.
// Before the file-backed capture, os/exec wired stdout/stderr to a pipe the child
// inherited; when the tool's Wait closed the read end, the child died by SIGPIPE
// (and blocked ~WaitDelay first). Regression guard: the tool returns promptly and
// the detached child still runs long enough afterward to create its marker.
func TestBashBackgroundChildSurvives(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")
	env := port.ToolEnv{Workdir: dir}

	started := time.Now()
	r, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"(sleep 0.4; touch `+marker+`) &"}`), env)
	if r.IsError {
		t.Fatalf("bash errored: %s", resultText(t, r))
	}
	// The `&` child must not hold the call open (no pipe drain, no WaitDelay).
	if d := time.Since(started); d > time.Second {
		t.Errorf("synchronous call blocked on background child: %v", d)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("marker already exists; test child ran too early to be meaningful")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return // child survived the tool call and wrote its marker
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background child died with the bash call: marker never created")
}
