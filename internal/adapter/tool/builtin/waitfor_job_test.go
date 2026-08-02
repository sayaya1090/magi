package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func jsonArgs(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func runWait(t *testing.T, args map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := WaitFor{}.Execute(context.Background(), raw,
		port.ToolEnv{Workdir: t.TempDir(), SessionID: session.SessionID("s-wait")})
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if json.Unmarshal(res.Content, &s) != nil {
		s = string(res.Content)
	}
	return s
}

func startJob(t *testing.T, cmd string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"command": cmd, "background": true})
	res, err := Bash{}.Execute(context.Background(), raw,
		port.ToolEnv{Workdir: t.TempDir(), SessionID: session.SessionID("s-wait")})
	if err != nil {
		t.Fatal(err)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	for _, f := range strings.Fields(s) {
		if strings.HasPrefix(f, "bg_") {
			return f
		}
	}
	t.Fatalf("no job id in %q", s)
	return ""
}

// Waiting on a CONDITION that a job's output would satisfy says nothing when the job dies — the
// condition simply keeps failing to the deadline. Waiting on the JOB ends the moment it does, and
// hands back how it ended.
func TestWaitingOnAJobEndsWhenTheJobDoes(t *testing.T) {
	id := startJob(t, "sleep 1; exit 3")
	start := time.Now()
	got := runWait(t, map[string]any{"job": id, "timeout": 60, "interval": 1})
	if el := time.Since(start); el > 20*time.Second {
		t.Errorf("the wait ran %s after a 1-second job", el.Round(time.Second))
	}
	if !strings.Contains(got, "exited 3") {
		t.Errorf("the job's exit is what was asked for:\n%s", got)
	}
	if !strings.Contains(got, "checks") {
		t.Errorf("how long it took should be stated:\n%s", got)
	}
}

// An id that names nothing is answered with what IS running, not with a bare failure.
func TestWaitingOnAJobThatIsNotThereNamesWhatIs(t *testing.T) {
	id := startJob(t, "sleep 30")
	defer func() {
		_, _ = BashKill{}.Execute(context.Background(), jsonArgs(map[string]any{"id": id}), port.ToolEnv{})
	}()
	got := runWait(t, map[string]any{"job": "bg_nope", "timeout": 5})
	if !strings.Contains(got, "no background job bg_nope") {
		t.Errorf("the missing id should be named:\n%s", got)
	}
	if !strings.Contains(got, id) {
		t.Errorf("the job that IS running should be offered:\n%s", got)
	}
}

// A job that outlives the wait is the wait's timeout, not the job's failure, and the message must
// not read as one.
func TestAJobOutlivingTheWaitSaysWhichTimedOut(t *testing.T) {
	id := startJob(t, "sleep 30")
	defer func() {
		_, _ = BashKill{}.Execute(context.Background(), jsonArgs(map[string]any{"id": id}), port.ToolEnv{})
	}()
	got := runWait(t, map[string]any{"job": id, "timeout": 2, "interval": 1})
	if !strings.Contains(got, "the wait timed out, the job did not") {
		t.Errorf("whose timeout it was should be explicit:\n%s", got)
	}
	if !strings.Contains(got, "bash_output") || !strings.Contains(got, "bash_kill") {
		t.Errorf("the ways forward should be named:\n%s", got)
	}
}

// The condition path: when everything that was working when the wait began has stopped, the wait is
// cut short instead of running its full deadline, and says so. Measured motivation: 1800s x 2 spent
// on a condition whose producer had already failed.
func TestAConditionWaitIsCutShortOnceEveryJobHasStopped(t *testing.T) {
	// The real grace is a minute; the behaviour under test is the shortening, not its length.
	prev := jobsGoneGrace
	jobsGoneGrace = 2 * time.Second
	t.Cleanup(func() { jobsGoneGrace = prev })

	startJob(t, "sleep 1")
	start := time.Now()
	got := runWait(t, map[string]any{"condition": "test -f /definitely/not/here", "timeout": 600, "interval": 1})
	el := time.Since(start)
	if el > 30*time.Second {
		t.Errorf("the wait ran %s though nothing was left to produce the file", el.Round(time.Second))
	}
	if !strings.Contains(got, "the wait was cut short") {
		t.Errorf("the shortening must be disclosed:\n%s", got)
	}
	if !strings.Contains(got, "wait again") {
		t.Errorf("magi cannot see what it did not start, and must say so:\n%s", got)
	}
}

// …and with nothing running at all, there is nothing to arm on: the wait behaves exactly as before.
func TestAConditionWaitWithNoJobsIsUnchanged(t *testing.T) {
	// A session of its own, so the package's other jobs are not this wait's business — which is
	// the point of scoping the arming by session in the first place.
	raw, _ := json.Marshal(map[string]any{"condition": "test -f /definitely/not/here", "timeout": 2, "interval": 1})
	res, err := WaitFor{}.Execute(context.Background(), raw,
		port.ToolEnv{Workdir: t.TempDir(), SessionID: session.SessionID("s-quiet")})
	if err != nil {
		t.Fatal(err)
	}
	var got string
	if json.Unmarshal(res.Content, &got) != nil {
		got = string(res.Content)
	}
	if strings.Contains(got, "cut short") {
		t.Errorf("with nothing running there is nothing to conclude from:\n%s", got)
	}
	if !strings.Contains(got, "condition not met") {
		t.Errorf("the ordinary timeout message should stand:\n%s", got)
	}
}
