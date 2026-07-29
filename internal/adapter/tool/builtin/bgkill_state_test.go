package builtin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// bash_kill said "killed <id>" whenever the id existed, whether or not there was anything left to
// kill. Observed live (fix-ocaml-gc, 2026-07-29): two bash_kill calls on the same job, two
// identical "killed bg_5" answers, with nothing to say whether the first had taken.
//
// The two cases it was flattening are both worth knowing, and neither is "killed": the process
// finished on its own between the last poll and the call — the run the agent was about to abandon
// completed, and its output is still there — or an earlier bash_kill already stopped it.
func TestBashKillSaysWhatItActuallyDid(t *testing.T) {
	env := port.ToolEnv{Workdir: t.TempDir()}
	kill := func(id string) string {
		res, err := BashKill{}.Execute(context.Background(),
			json.RawMessage(`{"id":`+strconv.Quote(id)+`}`), env)
		if err != nil {
			t.Fatal(err)
		}
		return string(res.Content)
	}

	// A job that is still running: killed, and said so.
	p, err := bg.start(env.Workdir, t.TempDir(), port.SandboxSpec{}, "sleep 30", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := kill(p.id); !strings.Contains(got, "killed "+p.id) {
		t.Fatalf("a running job is killed and reported as killed: %s", got)
	}
	// The same call again has nothing left to do, and must not claim it did.
	got := kill(p.id)
	if strings.Contains(got, "killed "+p.id) && !strings.Contains(got, "already") {
		t.Errorf("the second kill performed no act and must not report one: %s", got)
	}
	if !strings.Contains(got, "already killed") {
		t.Errorf("say which case it is, so the agent knows the first one took: %s", got)
	}

	// A job that ended on its own before the agent got to it: the exit is the news, not the kill.
	q, err := bg.start(env.Workdir, t.TempDir(), port.SandboxSpec{}, "exit 3", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		q.mu.Lock()
		done := q.done
		q.mu.Unlock()
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	got = kill(q.id)
	if strings.Contains(got, "killed "+q.id) && !strings.Contains(got, "already") {
		t.Errorf("nothing was killed — the process had already exited: %s", got)
	}
	for _, want := range []string{"already exited", "3", "bash_output"} {
		if !strings.Contains(got, want) {
			t.Errorf("a job that finished on its own must report %q: %s", want, got)
		}
	}

	// An id that was never a job is still an error, not a silent success.
	res, _ := BashKill{}.Execute(context.Background(), json.RawMessage(`{"id":"bg_nope"}`), env)
	if !res.IsError {
		t.Errorf("an unknown id is an error: %s", res.Content)
	}
}
