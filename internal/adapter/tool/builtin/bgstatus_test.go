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

// waitReaped blocks until the reaper has marked the job done.
func waitReaped(t *testing.T, p *bgProc) {
	t.Helper()
	for i := 0; i < 200; i++ {
		p.mu.Lock()
		done := p.done
		p.mu.Unlock()
		if done {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the job never reached done")
}

// The status header answers "what happened to this job", and it read `done` before `killed` — the
// same ordering BashKill was fixed for, in the sibling function that was not. The reaper sets done
// on a killed job too, so a few milliseconds after magi stopped something, polling it reported
//
//	[bg_1 exited -1]
//
// which is wrong twice over: the job did not exit, magi killed it; and -1 is not an exit code, it
// is Go's ExitCode() placeholder for a process that never returned through main. The killed branch
// was correct and effectively unreachable.
func TestKilledJobDoesNotReportAnExitItNeverMade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups and signal exits differ on Windows")
	}
	env := port.ToolEnv{Workdir: t.TempDir()}
	header := func(id string) string {
		res, err := BashOutput{}.Execute(context.Background(), json.RawMessage(`{"id":`+mustJSON(id)+`}`), env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			s = string(res.Content)
		}
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[:i]
		}
		return s
	}

	// magi kills it: the answer must stay "killed" after the reaper runs, not become an exit.
	p, err := bg.start("s-test", env.Workdir, t.TempDir(), port.SandboxSpec{}, "sleep 30", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := header(p.id); !strings.Contains(got, "running") {
		t.Errorf("a live job is running: %s", got)
	}
	if _, err := (BashKill{}).Execute(context.Background(), json.RawMessage(`{"id":`+mustJSON(p.id)+`}`), env); err != nil {
		t.Fatal(err)
	}
	waitReaped(t, p)
	got := header(p.id)
	if !strings.Contains(got, "killed") {
		t.Errorf("magi stopped this job; the header must say so: %s", got)
	}
	if strings.Contains(got, "exited") {
		t.Errorf("it did not exit — magi killed it: %s", got)
	}
	if strings.Contains(got, "-1") {
		t.Errorf("-1 is not an exit code: %s", got)
	}

	// A job that ended on its own keeps its real code.
	q, err := bg.start("s-test", env.Workdir, t.TempDir(), port.SandboxSpec{}, "exit 3", false)
	if err != nil {
		t.Fatal(err)
	}
	waitReaped(t, q)
	if got := header(q.id); !strings.Contains(got, "exited 3") {
		t.Errorf("a real exit code survives: %s", got)
	}

	// Terminated by something OTHER than magi: killed is false, and the exit magi holds is the
	// same -1 placeholder. It must not be printed as a status the program chose.
	r, err := bg.start("s-test", env.Workdir, t.TempDir(), port.SandboxSpec{}, "sleep 30", false)
	if err != nil {
		t.Fatal(err)
	}
	_ = killGroup(r.pid) // not through BashKill, so `killed` stays false
	waitReaped(t, r)
	got = header(r.id)
	if strings.Contains(got, "exited -1") {
		t.Errorf("-1 is not something a program returns: %s", got)
	}
	if !strings.Contains(got, "terminated by a signal") {
		t.Errorf("say what magi can tell — it ended without an exit code: %s", got)
	}
}
