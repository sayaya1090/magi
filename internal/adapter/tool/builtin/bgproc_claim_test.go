package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// A background job's outcome is claimed exactly once, and only when the job's exit is really the
// job's own verdict. Everything else reports nothing rather than a guess: a job still running has
// no outcome yet, a job the agent KILLED has an exit that judges the kill and not the work, and a
// second claim would let one failure be counted as many by an agent that polls in a loop.
func TestClaimBackgroundOutcome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics")
	}
	dir := t.TempDir()
	start := func(cmd string) string {
		t.Helper()
		args, _ := json.Marshal(map[string]any{"command": cmd, "background": true})
		res, err := Bash{}.Execute(context.Background(), args, port.ToolEnv{Workdir: dir})
		if err != nil || res.IsError {
			t.Fatalf("start %q: %v %s", cmd, err, resultText(t, res))
		}
		out := resultText(t, res)
		i := strings.Index(out, "bg_")
		if i < 0 {
			t.Fatalf("no id in %q", out)
		}
		return strings.Fields(out[i:])[0]
	}
	waitDone := func(id string) {
		t.Helper()
		for i := 0; i < 200; i++ {
			if p := bg.get(id); p != nil {
				p.mu.Lock()
				done := p.done
				p.mu.Unlock()
				if done {
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s never finished", id)
	}

	// A job still running has no outcome to hand back.
	running := start("sleep 30")
	if _, _, ok := ClaimBackgroundOutcome(running); ok {
		t.Error("a running job must not report an outcome")
	}

	// A finished job hands back its OWN command text and exit code — once.
	id := start("sh -c 'exit 7'")
	waitDone(id)
	cmd, exit, ok := ClaimBackgroundOutcome(id)
	if !ok || exit != 7 || !strings.Contains(cmd, "exit 7") {
		t.Fatalf("claim = %q %d %v, want the job's own command and exit 7", cmd, exit, ok)
	}
	if _, _, ok := ClaimBackgroundOutcome(id); ok {
		t.Error("a second claim must report nothing — one job, one outcome")
	}

	// A killed job reports nothing: the agent stopped it, so its exit says nothing about the work.
	killed := start("sleep 30")
	kargs, _ := json.Marshal(map[string]any{"id": killed})
	if _, err := (BashKill{}).Execute(context.Background(), kargs, port.ToolEnv{Workdir: dir}); err != nil {
		t.Fatal(err)
	}
	waitDone(killed)
	if _, _, ok := ClaimBackgroundOutcome(killed); ok {
		t.Error("a killed job must not report an outcome")
	}

	// An unknown id is not an error, just nothing.
	if _, _, ok := ClaimBackgroundOutcome("bg_does_not_exist"); ok {
		t.Error("an unknown id must report nothing")
	}
	kargs, _ = json.Marshal(map[string]any{"id": running})
	_, _ = (BashKill{}).Execute(context.Background(), kargs, port.ToolEnv{Workdir: dir})
}
