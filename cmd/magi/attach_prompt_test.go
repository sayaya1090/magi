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
	mu    sync.Mutex
	ask   *app.Ask
	doing string
	user  string
	answ  []string
}

func (p *promptEngine) Doing(session.SessionID) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doing, p.doing != ""
}

func (p *promptEngine) setDoing(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.doing = s
}

// UserLabel makes this engine one that knows what to call the person — the optional interface the
// daemon asserts for when it assembles a status.
func (p *promptEngine) UserLabel(session.SessionID) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.user
}

func (p *promptEngine) setUser(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.user = s
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
	// Cancelled AND waited for: the goroutine writes into a store rooted in a t.TempDir()
	// created earlier, so a cancel that only asks it to stop leaves a write racing the removal.
	// CI reports that as "TempDir RemoveAll cleanup: directory not empty".
	var running sync.WaitGroup
	running.Add(1)
	t.Cleanup(func() { cancel(); running.Wait() })
	go func() { defer running.Done(); _ = daemon.Serve(ctx, eng, sock) }()

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
	if p := a.pendingPrompt(sid, ""); p.drawing || !p.cleared {
		t.Errorf("with no prompt pending: drawing=%v cleared=%v, want false/true", p.drawing, p.cleared)
	}

	args := json.RawMessage(`{"command":"rm -rf build"}`)
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Args: args,
		Reason: "destructive command detected", Since: time.Now()})

	p := a.pendingPrompt(sid, "")
	ev, id, drawing := p.ev, p.id, p.drawing
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
	if p := a.pendingPrompt(sid, "call_7"); p.drawing || p.cleared || p.id != "call_7" {
		t.Errorf("the same prompt was drawn again (drawing=%v cleared=%v id=%q)", p.drawing, p.cleared, p.id)
	}

	// A question carries its picks, or the modal offers nothing to pick.
	eng.set(&app.Ask{ID: "q1#1", Kind: "question", What: "which branch?",
		Options: []string{"main", "release"}, Since: time.Now()})
	p = a.pendingPrompt(sid, "call_7")
	ev, drawing = p.ev, p.drawing
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
	if p := a.pendingPrompt(sid, ""); !p.drawing {
		t.Fatal("the prompt was not drawn to begin with")
	}

	cl.Close() // the daemon is gone, or the connection dropped
	p := a.pendingPrompt(sid, "call_7")
	id, drawing, cleared, reachable := p.id, p.drawing, p.cleared, p.reachable
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
	if p := a.pendingPrompt(sid, ""); !p.drawing {
		t.Fatal("not drawn")
	}
	if err := a.RespondPermission(context.Background(), command.RespondPermission{
		SessionID: sid, CallID: "call_7", Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if p := a.pendingPrompt(sid, "call_7"); !p.cleared {
		t.Fatal("the marker did not clear after the prompt was answered")
	}
	// Same id again — a fresh prompt, and it must reach the screen.
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Since: time.Now()})
	if p := a.pendingPrompt(sid, ""); !p.drawing {
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

	if p := a.pendingPrompt(sid, ""); !p.reachable {
		t.Fatal("a live daemon was reported unreachable")
	}
	cl.Close()
	reachable := a.pendingPrompt(sid, "").reachable
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

// The live note crosses too, and on the same reply.
//
// `⏳ …` has been in the renderer since wait_for was written, fed by a transient event — which goes
// to the engine's bus, in the engine's process. An ATTACHED terminal is a different process, so in
// that view the line could never once have appeared: the one view most likely to be open on a
// twenty-minute wait was the one that showed nothing about it.
func TestTheAttachedTerminalIsToldWhatIsBeingWaitedOn(t *testing.T) {
	eng := &promptEngine{}
	a := attached{c: serveEngine(t, eng)}
	sid := session.SessionID("s_1")

	if p := a.pendingPrompt(sid, ""); p.doing != "" {
		t.Fatalf("a quiet daemon reported %q", p.doing)
	}

	eng.setDoing("check 6, 4m12s elapsed, still running")
	p := a.pendingPrompt(sid, "")
	doing, reachable := p.doing, p.reachable
	if !reachable {
		t.Fatal("the daemon went unreachable")
	}
	if doing != "check 6, 4m12s elapsed, still running" {
		t.Fatalf("the note did not cross: %q", doing)
	}

	// And it arrives as the event the screen already knows how to draw — the same synthesis the
	// prompt gets, so there is one renderer and not two.
	ev := progressEvent(sid, doing)
	if ev.Type != event.TypeToolProgress {
		t.Fatalf("event type %q, want %q", ev.Type, event.TypeToolProgress)
	}
	var d event.ToolProgressData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		t.Fatalf("the synthesised event does not parse as the real one: %v", err)
	}
	if d.Text != doing {
		t.Errorf("the note lost its words: %q", d.Text)
	}
}

// A prompt and a note arrive together, because they are one question asked at one moment.
//
// Two exchanges could return a permission prompt and a progress note taken half a second apart —
// a state the daemon was never in. The specific wrong picture: a screen showing "waiting for you"
// and "still running" at once, which are contradictory answers to "should I do something".
func TestThePromptAndTheNoteComeFromOneAnswer(t *testing.T) {
	eng := &promptEngine{}
	a := attached{c: serveEngine(t, eng)}
	sid := session.SessionID("s_1")
	eng.set(&app.Ask{ID: "call_7", Kind: "permission", What: "bash", Since: time.Now()})
	eng.setDoing("still running")

	p := a.pendingPrompt(sid, "")
	ev, id, doing, drawing := p.ev, p.id, p.doing, p.drawing
	if !drawing || id != "call_7" || ev.Type != event.TypePermissionRequested {
		t.Fatalf("the prompt did not come through: drawing=%v id=%q type=%q", drawing, id, ev.Type)
	}
	if doing != "still running" {
		t.Errorf("the note was dropped when a prompt was pending: %q", doing)
	}
}

// The name a plugin gave the person reaches a window in another process.
//
// It is set in the daemon's memory by magi.set_user_label and announced on the daemon's bus, so a
// viewer that attached afterwards never heard it and could not have read it either: the label is
// not in the log, because it is not a record of what happened. The same conversation was headed by
// somebody's name in one window and by "you" in the next, with neither able to say the other
// existed.
func TestTheNameThePersonIsCalledReachesAnAttachedView(t *testing.T) {
	eng := &promptEngine{}
	a := attached{c: serveEngine(t, eng)}
	sid := session.SessionID("s_1")

	if p := a.pendingPrompt(sid, ""); p.user != "" {
		t.Fatalf("a daemon with no label reported %q", p.user)
	}
	eng.setUser("sayaya")
	p := a.pendingPrompt(sid, "")
	if !p.reachable {
		t.Fatal("the daemon went unreachable")
	}
	if p.user != "sayaya" {
		t.Fatalf("the label did not cross: %q", p.user)
	}
	// And it arrives as the event the screen already knows how to read, rather than as a second
	// path into the same field.
	ev := userLabelEvent(sid, p.user)
	if ev.Type != event.TypeUserLabelChanged {
		t.Fatalf("the label was carried as %q", ev.Type)
	}
	var d event.UserLabelData
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.Label != "sayaya" {
		t.Errorf("the event carries %q", d.Label)
	}
}

// The approval mode an attached window shows is the daemon's, not its own idea of it.
//
// The viewer holds a throwaway App, and the header reads the mode off it on every frame. Changed
// from the terminal that owns the run — or now from the console — the daemon moved and the
// attached window went on displaying whatever mode it had started in.
func TestTheModeAnAttachedViewShowsIsTheDaemonsOwn(t *testing.T) {
	eng := &controllableEngine{promptEngine: &promptEngine{}, perm: "deny"}
	a := attached{c: serveEngine(t, eng)}
	if p := a.pendingPrompt(session.SessionID("s_1"), ""); p.perm != "deny" {
		t.Errorf("the poll reports the mode as %q, want the daemon's deny", p.perm)
	}
}

// controllableEngine is a promptEngine that also answers for how it runs.
type controllableEngine struct {
	*promptEngine
	perm string
}

func (c *controllableEngine) Rewind(context.Context, session.SessionID, int) (int64, error) {
	return 0, nil
}
func (c *controllableEngine) Compact(context.Context, command.Compact) error { return nil }
func (c *controllableEngine) SetModel(session.SessionID, string)             {}
func (c *controllableEngine) SetPermission(p string)                         { c.perm = p }
func (c *controllableEngine) Permission() string                             { return c.perm }

// The attached view is told when the prompt it is showing was answered somewhere else.
//
// A permission decision is a fact and reaches a viewer through the log by itself. A question's
// answer is not — it goes straight down a channel to the tool that was waiting — so the only thing
// that says the question is over is the daemon no longer reporting it. That is what this poll
// notices, and until it said so the modal stayed up over a turn that had moved on.
func TestTheViewIsToldWhenSomebodyElseAnswered(t *testing.T) {
	eng := &promptEngine{}
	a := attached{c: serveEngine(t, eng)}
	sid := session.SessionID("s_1")

	eng.set(&app.Ask{ID: "q1", Kind: "question", What: "which surface?", Since: time.Now()})
	p := a.pendingPrompt(sid, "")
	if !p.drawing || p.kind != "question" {
		t.Fatalf("the question was not drawn as one: %+v", p)
	}
	// Answered by the other UI: the daemon stops reporting it.
	eng.set(nil)
	if got := a.pendingPrompt(sid, "q1"); !got.cleared {
		t.Fatal("the poll did not notice the prompt was gone")
	}
	ev := answeredElsewhere(sid, "q1", "question")
	if ev.Type != event.TypeQuestionAnswered {
		t.Errorf("a question ending was carried as %q", ev.Type)
	}
	var qd event.QuestionAnsweredData
	if err := json.Unmarshal(ev.Data, &qd); err != nil || qd.CallID != "q1" {
		t.Errorf("it does not name the question: %+v (%v)", qd, err)
	}

	// A permission ends in the vocabulary a permission has. The decision itself is NOT guessed:
	// this process does not know which way it went, and the transcript gets the real one from the
	// log.
	pv := answeredElsewhere(sid, "call_7", "permission")
	if pv.Type != event.TypePermissionDecided {
		t.Errorf("a permission ending was carried as %q", pv.Type)
	}
	var pd event.PermissionDecidedData
	if err := json.Unmarshal(pv.Data, &pd); err != nil || pd.CallID != "call_7" {
		t.Errorf("it does not name the call: %+v (%v)", pd, err)
	}
	if pd.Decision == "allow" || pd.Decision == "deny" {
		t.Errorf("the viewer invented a decision: %q", pd.Decision)
	}
}
