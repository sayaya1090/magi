package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// promptEngine is a daemon whose only interesting property is what it claims to be blocked on.
type promptEngine struct {
	mu   sync.Mutex
	ask  *app.Ask
	answ []string
}

func (p *promptEngine) set(a *app.Ask) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ask = a
}

func (p *promptEngine) Waiting(session.SessionID) (app.Ask, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ask == nil {
		return app.Ask{}, false
	}
	return *p.ask, true
}

func (p *promptEngine) Submit(context.Context, command.SubmitPrompt) error { return nil }
func (p *promptEngine) Steer(context.Context, command.SubmitPrompt) error  { return nil }
func (p *promptEngine) Interrupt(context.Context, command.Interrupt) error { return nil }
func (p *promptEngine) RespondQuestion(_ context.Context, c command.RespondQuestion) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answ = append(p.answ, "answer:"+c.CallID+":"+c.Answer)
	p.ask = nil
	return nil
}

func (p *promptEngine) RespondPermission(_ context.Context, c command.RespondPermission) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.answ = append(p.answ, "permission:"+c.CallID+":"+c.Decision)
	p.ask = nil
	return nil
}

// serveEngine runs a daemon on a short socket path and returns a client attached to it.
func serveEngine(t *testing.T, eng daemon.Engine) *daemon.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "attachp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, eng, sock) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, derr := net.Dial("unix", sock); derr == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cl, err := daemon.Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	return cl
}

// The attached terminal has to build the prompt itself: the daemon's transient events never leave
// its process, so there is nothing to forward. What must come out is the SAME payload the engine
// would have published in-process — above all the same call id, because that is what an answer is
// addressed to. A prompt rendered from a summary is a prompt nobody can reply to.
func TestTheAttachedTerminalRebuildsThePrompt(t *testing.T) {
	eng := &promptEngine{}
	a := attached{c: serveEngine(t, eng)}
	sid := session.SessionID("s_1")

	// Nothing pending: nothing is drawn, and the state is "cleared" rather than "unknown".
	if _, _, drawing, cleared, _ := a.pendingPrompt(sid, ""); drawing || !cleared {
		t.Errorf("with no prompt pending: drawing=%v cleared=%v, want false/true", drawing, cleared)
	}

	args := json.RawMessage(`{"command":"rm -rf build"}`)
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Args: args,
		Reason: "destructive command detected", Since: time.Now()})

	ev, id, drawing, _, _ := a.pendingPrompt(sid, "")
	if !drawing {
		t.Fatal("a pending permission prompt was not drawn")
	}
	if id != "call_7" {
		t.Errorf("prompt id %q, want call_7", id)
	}
	if ev.Type != event.TypePermissionRequested {
		t.Errorf("event type %q, want %q", ev.Type, event.TypePermissionRequested)
	}
	var d event.PermissionRequestedData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		t.Fatalf("the synthesised event does not parse as the real one: %v", err)
	}
	if d.CallID != "call_7" {
		t.Errorf("call id %q — an answer would go nowhere", d.CallID)
	}
	if d.Name != "bash" || string(d.Args) != string(args) {
		t.Errorf("the prompt lost what is being decided: name=%q args=%s", d.Name, d.Args)
	}
	if d.Reason != "destructive command detected" {
		t.Errorf("the policy's reason did not survive: %q", d.Reason)
	}

	// Drawn once. Polling four times a second must not stack four modals over one question.
	if _, gotID, drawing, cleared, _ := a.pendingPrompt(sid, "call_7"); drawing || cleared || gotID != "call_7" {
		t.Errorf("the same prompt was drawn again (drawing=%v cleared=%v id=%q)", drawing, cleared, gotID)
	}

	// A question carries its picks, or the modal offers nothing to pick.
	eng.set(&app.Ask{ID: "q1#1", Kind: "question", What: "which branch?",
		Options: []string{"main", "release"}, Since: time.Now()})
	ev, _, drawing, _, _ = a.pendingPrompt(sid, "call_7")
	if !drawing || ev.Type != event.TypeQuestionRequested {
		t.Fatalf("a question came through as %v (drawing=%v)", ev.Type, drawing)
	}
	var q event.QuestionRequestedData
	if err := json.Unmarshal(ev.Data, &q); err != nil {
		t.Fatal(err)
	}
	if q.CallID != "q1#1" || q.Question != "which branch?" || len(q.Options) != 2 {
		t.Errorf("the question lost something: %+v", q)
	}
}

// A failed status is not "nothing pending".
//
// Clearing the marker on an error means the next poll redraws a prompt that is already on screen —
// one dropped read turning into two modals over the same question. Unknown must leave the screen
// exactly as it is.
func TestAFailedStatusChangesNothingOnScreen(t *testing.T) {
	eng := &promptEngine{}
	cl := serveEngine(t, eng)
	a := attached{c: cl}
	sid := session.SessionID("s_1")
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Since: time.Now()})
	if _, _, drawing, _, _ := a.pendingPrompt(sid, ""); !drawing {
		t.Fatal("the prompt was not drawn to begin with")
	}

	cl.Close() // the daemon is gone, or the connection dropped
	_, id, drawing, cleared, reachable := a.pendingPrompt(sid, "call_7")
	if drawing {
		t.Error("a failed status redrew the prompt")
	}
	if cleared {
		t.Error("a failed status was treated as 'nothing pending' — the next poll would draw it twice")
	}
	if id != "call_7" {
		t.Errorf("a failed status forgot which prompt was on screen (%q)", id)
	}
	// And it says the daemon is out of reach, which is what puts the notice on screen.
	if reachable {
		t.Error("a failed status reported the daemon as reachable — nothing would tell the user")
	}
}

// And when it IS answered, the marker clears — so a later prompt is not swallowed by the memory of
// the one before it, even if the id repeats.
func TestAnAnsweredPromptClearsTheMarker(t *testing.T) {
	eng := &promptEngine{}
	a := attached{c: serveEngine(t, eng)}
	sid := session.SessionID("s_1")
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Since: time.Now()})
	if _, _, drawing, _, _ := a.pendingPrompt(sid, ""); !drawing {
		t.Fatal("not drawn")
	}
	if err := a.RespondPermission(context.Background(), command.RespondPermission{
		SessionID: sid, CallID: "call_7", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, cleared, _ := a.pendingPrompt(sid, "call_7"); !cleared {
		t.Fatal("the marker did not clear after the prompt was answered")
	}
	// Same id again — a fresh prompt, and it must reach the screen.
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Since: time.Now()})
	if _, _, drawing, _, _ := a.pendingPrompt(sid, ""); !drawing {
		t.Error("a new prompt reusing the id was swallowed")
	}
}

// A daemon that dies must not leave the screen looking alive.
//
// The transcript comes from the log, which stays readable after the engine is gone — so the view
// keeps rendering the last thing that happened and nothing says the process driving it has stopped.
// The only way to find out was to type something and watch it fail. The poll that notices says so
// instead, once, in the transcript where the record is.
func TestTheScreenIsToldWhenTheDaemonGoesAway(t *testing.T) {
	eng := &promptEngine{}
	cl := serveEngine(t, eng)
	a := attached{c: cl}
	sid := session.SessionID("s_1")

	if _, _, _, _, reachable := a.pendingPrompt(sid, ""); !reachable {
		t.Fatal("a live daemon was reported unreachable")
	}
	cl.Close()
	_, _, _, _, reachable := a.pendingPrompt(sid, "")
	if reachable {
		t.Fatal("a dead daemon was reported reachable — nothing would tell the user")
	}

	ev := daemonLostEvent(sid)
	if ev.Type != event.TypeError {
		t.Errorf("the notice is a %q; the transcript shows errors", ev.Type)
	}
	var d event.ErrorData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"stopped answering", "magi --daemon"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("the notice does not say %q: %q", want, d.Message)
		}
	}
	if ev.Seq != 0 {
		t.Error("the notice carries a sequence number, as though it came from the log")
	}
}
