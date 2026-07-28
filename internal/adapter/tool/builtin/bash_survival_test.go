//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// A background job reports the PIPELINE's exit, which is its last stage's — so `make … | tail`
// says 0 for a build that died. Observed live: a three-hour run polled a backgrounded build and was
// told `[bg_1 exited 0]` while the output carried `make: *** Error 2`. The foreground path has said
// which stage really failed since PIPESTATUS went in; the status header says it too now.
func TestBackgroundStatusNamesTheFailingStage(t *testing.T) {
	dir := t.TempDir()
	env := port.ToolEnv{Workdir: dir, ScratchTmp: dir}

	r, _ := Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"sh -c 'echo boom; exit 2' | tail -1","background":true}`), env)
	if r.IsError {
		t.Fatalf("start errored: %s", resultText(t, r))
	}
	id := regexp.MustCompile(`bg_\d+`).FindString(resultText(t, r))
	if id == "" {
		t.Fatalf("no background id in start result: %s", resultText(t, r))
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := BashOutput{}.Execute(context.Background(),
			json.RawMessage(`{"id":"`+id+`"}`), env)
		txt := resultText(t, out)
		if strings.Contains(txt, "exited") {
			if !strings.Contains(txt, "2 → 0") {
				t.Fatalf("a background pipeline's per-stage statuses must reach its status line: %s", txt)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("background job never finished: %s", txt)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
