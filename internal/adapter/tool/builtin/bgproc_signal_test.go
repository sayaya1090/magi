//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// bash_kill with signal="int" is the Ctrl-C affordance: the process's trap runs,
// its cleanup output lands in bash_output, and nothing is torn down until the
// process exits on its own. The default (no signal) stays the hard stop.
func TestBashKillGracefulInterrupt(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	r, _ := Bash{}.Execute(context.Background(), json.RawMessage(
		`{"command":"trap 'echo CLEANUP_RAN; exit 0' INT TERM; echo READY; while true; do sleep 0.2; done","background":true}`), env)
	out := resultText(t, r)
	if r.IsError {
		t.Fatalf("bg start failed: %s", out)
	}
	i := strings.Index(out, "bg_")
	if i < 0 {
		t.Fatalf("no bg id in start result: %s", out)
	}
	id := out[i:]
	for _, cut := range []string{" ", "\"", "”", "}", ","} {
		if j := strings.Index(id, cut); j > 0 {
			id = id[:j]
		}
	}

	poll := func(want string, deadline time.Duration) string {
		end := time.Now().Add(deadline)
		var last string
		for time.Now().Before(end) {
			r, _ := BashOutput{}.Execute(context.Background(), json.RawMessage(`{"id":"`+id+`"}`), env)
			last = resultText(t, r)
			if strings.Contains(last, want) {
				return last
			}
			time.Sleep(50 * time.Millisecond)
		}
		return last
	}

	// Wait for READY so the trap is installed before we signal.
	if got := poll("READY", 5*time.Second); !strings.Contains(got, "READY") {
		t.Fatalf("bg never printed READY: %s", got)
	}

	r, _ = BashKill{}.Execute(context.Background(),
		json.RawMessage(`{"id":"`+id+`","signal":"int"}`), env)
	out = resultText(t, r)
	if r.IsError || !strings.Contains(out, "SIGINT") || !strings.Contains(out, "bash_output") {
		t.Fatalf("graceful interrupt should report the signal and point at bash_output: %s", out)
	}

	// The trap's cleanup output must be readable afterwards — the whole point.
	if got := poll("CLEANUP_RAN", 5*time.Second); !strings.Contains(got, "CLEANUP_RAN") {
		t.Fatalf("cleanup output must surface after SIGINT, got: %s", got)
	}
}

// An unknown id and a bad signal value stay client errors; default hard stop is
// untouched by the new field.
func TestBashKillSignalEdges(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	r, _ := BashKill{}.Execute(context.Background(),
		json.RawMessage(`{"id":"bg_nope","signal":"int"}`), env)
	if !r.IsError {
		t.Fatalf("unknown id must error: %s", resultText(t, r))
	}

	// A junk signal value falls through to the hard-stop path (signal gate matches
	// only int/term), which still works.
	r, _ = Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"sleep 30","background":true}`), env)
	out := resultText(t, r)
	i := strings.Index(out, "bg_")
	id := out[i:]
	for _, cut := range []string{" ", "\"", "”", "}", ","} {
		if j := strings.Index(id, cut); j > 0 {
			id = id[:j]
		}
	}
	r, _ = BashKill{}.Execute(context.Background(),
		json.RawMessage(`{"id":"`+id+`","signal":"whatever"}`), env)
	if r.IsError || !strings.Contains(resultText(t, r), "killed") {
		t.Fatalf("junk signal should hard-stop as before: %s", resultText(t, r))
	}
}
