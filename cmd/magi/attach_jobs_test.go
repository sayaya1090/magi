package main

import (
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/session"
)

// jobbingEngine is a promptEngine that is also running things beside the turn.
type jobbingEngine struct {
	*promptEngine
	mu   sync.Mutex
	asks int
	bg   []app.BackgroundJob
	kids []app.SubagentJob
	tail string
}

func (j *jobbingEngine) BackgroundJobs() []app.BackgroundJob {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.asks++ // once per reply: the daemon assembles the whole answer from one call to each
	return j.bg
}
func (j *jobbingEngine) BackgroundTail(string, int) string { return j.tail }
func (j *jobbingEngine) SubagentJobs() []app.SubagentJob   { return j.kids }

func (j *jobbingEngine) asked() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.asks
}

// The strip along the bottom is empty in an attached window, and it should not be.
//
// A background command is a PID the daemon is waiting on; the child register is the one thing a
// session log cannot tell you, because a log does not know it is over. Both live in the memory of
// the process that started the work, so a viewer holding its own App read its own empty registers
// — and the one place a five-minute build or a spawned child is visible while it runs showed
// nothing at all in every window except the one that started it.
func TestTheStripSeesWorkRunningInTheOtherProcess(t *testing.T) {
	began := time.Now().Add(-90 * time.Second).UTC().Truncate(time.Second)
	eng := &jobbingEngine{
		promptEngine: &promptEngine{},
		bg: []app.BackgroundJob{{
			ID: "bg_1", Command: "go build ./...", Running: true, Started: began}},
		kids: []app.SubagentJob{{
			ID: "s_child", Tool: "spawn", Task: "read the failing test", Running: true,
			Steps: 4, Started: began}},
		tail: "ok  	github.com/sayaya1090/magi/internal/app",
	}
	a := attached{c: serveEngine(t, eng), seen: &jobsSeen{sid: session.SessionID("s_1")}}

	jobs := a.BackgroundJobs()
	if len(jobs) != 1 || jobs[0].ID != "bg_1" || jobs[0].Command != "go build ./..." {
		t.Fatalf("the background command did not cross: %+v", jobs)
	}
	if !jobs[0].Running {
		t.Error("a running command arrived finished")
	}
	if !jobs[0].Started.Equal(began) {
		t.Errorf("it started at %v, want %v", jobs[0].Started, began)
	}
	if got := a.BackgroundTail("bg_1", 8<<10); got != eng.tail {
		t.Errorf("its output reads %q", got)
	}

	kids := a.SubagentJobs()
	if len(kids) != 1 || kids[0].ID != "s_child" || kids[0].Task != "read the failing test" {
		t.Fatalf("the child did not cross: %+v", kids)
	}
	if !kids[0].Running || kids[0].Steps != 4 {
		t.Errorf("the child arrived as %+v", kids[0])
	}

	// Three calls, one reply. The strip asks all of these on every tick and once per job, so a
	// round trip each would be a socket call per job per 700ms.
	if n := eng.asked(); n > 1 {
		t.Errorf("one tick of the strip made %d requests", n)
	}
}

// A dropped poll keeps the last picture rather than emptying the strip.
//
// "The daemon did not answer" is not the news that everything finished. The view says the daemon is
// gone by its own means; the strip going blank would say the build ended, which is a different and
// false thing.
func TestALostPollDoesNotEmptyTheStrip(t *testing.T) {
	eng := &jobbingEngine{
		promptEngine: &promptEngine{},
		bg:           []app.BackgroundJob{{ID: "bg_1", Command: "sleep 600", Running: true}},
	}
	cl := serveEngine(t, eng)
	a := attached{c: cl, seen: &jobsSeen{sid: session.SessionID("s_1")}}
	if len(a.BackgroundJobs()) != 1 {
		t.Fatal("nothing crossed to begin with")
	}
	cl.Close()
	a.seen.mu.Lock()
	a.seen.at = time.Now().Add(-time.Hour) // force the next call to ask
	a.seen.mu.Unlock()
	if got := a.BackgroundJobs(); len(got) != 1 {
		t.Errorf("a failed poll emptied the strip: %+v", got)
	}
}
