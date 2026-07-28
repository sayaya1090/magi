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

// The viewer's contract, against real processes: a job appears while it runs, its tail is readable
// without consuming what the agent has not read, and it can be stopped the same way bash_kill stops
// it. The consuming half is the one that matters — bash_output advances an offset, and a viewer
// sharing that offset would take output the agent never sees.
func TestBackgroundJobsAreWatchableWithoutConsuming(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{
		"command":    `for i in 1 2 3 4 5; do echo "line $i"; sleep 0.2; done`,
		"background": true,
	})
	res, err := (Bash{}).Execute(context.Background(), raw, env)
	if err != nil || res.IsError {
		t.Fatalf("start: err=%v res=%s", err, res.Content)
	}

	jobs := ListBackgroundJobs()
	if len(jobs) == 0 {
		t.Fatal("a started job must appear in the registry")
	}
	j := jobs[len(jobs)-1]
	if !strings.Contains(j.Command, "line $i") {
		t.Errorf("the job must carry the command it runs, got %q", j.Command)
	}
	if !j.Running {
		t.Error("a job that just started is running")
	}

	// Let it produce something, then read the tail twice: watching is idempotent.
	waitFor(t, func() bool { return strings.Contains(TailBackgroundJob(j.ID, 4096), "line 1") })
	first := TailBackgroundJob(j.ID, 4096)
	if second := TailBackgroundJob(j.ID, 4096); !strings.HasPrefix(second, first[:min(len(first), 6)]) {
		t.Errorf("a second look must show the same output, not the next slice:\n%q\n%q", first, second)
	}

	// The agent's own read still returns everything from the beginning — the viewer took none of it.
	outRaw, _ := json.Marshal(map[string]string{"id": j.ID})
	out, err := (BashOutput{}).Execute(context.Background(), outRaw, env)
	if err != nil || out.IsError {
		t.Fatalf("bash_output: err=%v res=%s", err, out.Content)
	}
	var text string
	_ = json.Unmarshal(out.Content, &text)
	if !strings.Contains(text, "line 1") {
		t.Errorf("watching consumed the agent's output — it no longer sees line 1:\n%s", text)
	}

	if !KillBackgroundJob(j.ID) {
		t.Fatal("a live job must be stoppable")
	}
	waitFor(t, func() bool {
		for _, k := range ListBackgroundJobs() {
			if k.ID == j.ID {
				return k.Killed || !k.Running
			}
		}
		return false
	})
	if KillBackgroundJob("bg_nope") {
		t.Error("stopping a job that does not exist must report that it did not")
	}
}

// A tail longer than the cap is cut at a line boundary, so the pane never opens on half a line.
func TestBackgroundTailStartsOnAWholeLine(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{
		"command":    `for i in $(seq 1 200); do echo "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa $i"; done`,
		"background": true,
	})
	if res, err := (Bash{}).Execute(context.Background(), raw, env); err != nil || res.IsError {
		t.Fatalf("start: err=%v res=%s", err, res.Content)
	}
	jobs := ListBackgroundJobs()
	j := jobs[len(jobs)-1]
	waitFor(t, func() bool { return strings.Contains(TailBackgroundJob(j.ID, 200), "200") })

	tail := TailBackgroundJob(j.ID, 200)
	if tail == "" {
		t.Fatal("expected a tail")
	}
	if strings.HasPrefix(tail, "aaaa") && !strings.HasPrefix(tail, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ") {
		t.Errorf("the tail must start on a whole line, got %q", tail[:min(40, len(tail))])
	}
	KillBackgroundJob(j.ID)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition never became true within 5s")
}
