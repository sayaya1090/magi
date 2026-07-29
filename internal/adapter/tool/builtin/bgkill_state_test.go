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
	// The same call again has nothing left to do, and must not claim it did. Waited out first:
	// the reaper sets done on a KILLED job too, so this is the state a real second call meets —
	// killed and done at once. Answering from done says "exited on its own" about a process magi
	// stopped, which is the trap this ordering exists to avoid, and a fast second call would skip
	// straight past it.
	waitDone(t, p)
	got := kill(p.id)
	if strings.Contains(got, "killed "+p.id) && !strings.Contains(got, "already") {
		t.Errorf("the second kill performed no act and must not report one: %s", got)
	}
	if !strings.Contains(got, "already killed") {
		t.Errorf("say which case it is, so the agent knows the first one took: %s", got)
	}
	if strings.Contains(got, "on its own") {
		t.Errorf("magi stopped this process; it did not exit on its own: %s", got)
	}

	// A job that ended on its own before the agent got to it: the exit is the news, not the kill.
	q, err := bg.start(env.Workdir, t.TempDir(), port.SandboxSpec{}, "exit 3", false)
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, q)
	got = kill(q.id)
	if strings.Contains(got, "killed "+q.id) && !strings.Contains(got, "already") {
		t.Errorf("nothing was killed — the process had already exited: %s", got)
	}
	for _, want := range []string{"already exited", "3", "bash_output"} {
		if !strings.Contains(got, want) {
			t.Errorf("a job that finished on its own must report %q: %s", want, got)
		}
	}

	// The graceful-signal path asks the same three states before it acts. Without that it calls
	// signalGroup on a reaped process group, gets ESRCH, and reports a finished job as
	// "signal failed: no such process" — true, but an error for something that is not one.
	r, _ := BashKill{}.Execute(context.Background(),
		json.RawMessage(`{"id":`+strconv.Quote(q.id)+`,"signal":"term"}`), env)
	sg := string(r.Content)
	if r.IsError || strings.Contains(sg, "signal failed") {
		t.Errorf("a job that already finished is not a failed signal: %s", sg)
	}
	if !strings.Contains(sg, "already exited") || !strings.Contains(sg, "nothing to signal") {
		t.Errorf("say the same thing the hard-stop path says, in the signal's words: %s", sg)
	}

	// An id that was never a job is still an error, not a silent success.
	res, _ := BashKill{}.Execute(context.Background(), json.RawMessage(`{"id":"bg_nope"}`), env)
	if !res.IsError {
		t.Errorf("an unknown id is an error: %s", res.Content)
	}
}

// waitDone blocks until the reaper has recorded the process as finished, so a test meets the same
// state a real second call would rather than racing ahead of it.
func waitDone(t *testing.T, p *bgProc) {
	t.Helper()
	for i := 0; i < 300; i++ {
		p.mu.Lock()
		done := p.done
		p.mu.Unlock()
		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never finished", p.id)
}
