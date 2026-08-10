package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// recordingEngine is a daemon that remembers what the browser told it to do.
type recordingEngine struct {
	mu   sync.Mutex
	got  []string
	ask  *app.Ask
	fail error
}

func (r *recordingEngine) note(s string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, s)
	return r.fail
}

func (r *recordingEngine) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// Controller is optional on a daemon, and this fake implements it so the console's control calls
// are exercised over the wire rather than against a stub that always says yes.
func (r *recordingEngine) Compact(_ context.Context, c command.Compact) error {
	return r.note("compact:" + string(c.SessionID))
}

func (r *recordingEngine) Rewind(_ context.Context, sid session.SessionID, n int) (int64, error) {
	return 0, r.note("rewind:" + string(sid))
}
func (r *recordingEngine) SetModel(sid session.SessionID, m string) { _ = r.note("model:" + m) }
func (r *recordingEngine) SetPermission(p string)                   { _ = r.note("perm:" + p) }

func (r *recordingEngine) Submit(_ context.Context, c command.SubmitPrompt) error {
	return r.note("submit:" + textOf(c.Parts))
}
func (r *recordingEngine) Steer(_ context.Context, c command.SubmitPrompt) error {
	return r.note("steer:" + textOf(c.Parts))
}
func (r *recordingEngine) Interrupt(context.Context, command.Interrupt) error {
	return r.note("interrupt")
}
func (r *recordingEngine) RespondPermission(_ context.Context, c command.RespondPermission) error {
	return r.note("permission:" + c.CallID + ":" + c.Decision)
}
func (r *recordingEngine) RespondQuestion(_ context.Context, c command.RespondQuestion) error {
	return r.note("answer:" + c.CallID + ":" + c.Answer)
}
func (r *recordingEngine) Waiting(session.SessionID) (app.Ask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ask == nil {
		return app.Ask{}, false
	}
	return *r.ask, true
}

func textOf(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// liveDaemon starts a real daemon behind the fixture's socket and publishes its record, so the
// handlers go over the wire the way they do in the browser.
func (f *fleetFixture) liveDaemon(t *testing.T, workdir, sid string, eng daemon.Engine) string {
	return f.liveDaemonAs(t, workdir, sid, eng, daemon.Identity{})
}

// liveDaemonAs is liveDaemon for a companion that declares what it is called and what it is for.
func (f *fleetFixture) liveDaemonAs(t *testing.T, workdir, sid string, eng daemon.Engine, id daemon.Identity) string {
	t.Helper()
	sock := f.cfgDir + "/daemon-" + sid + ".sock"
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, eng, sock) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cl, err := daemon.Dial(sock); err == nil {
			cl.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	unpublish, err := daemon.Publish(sock, workdir, sid, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpublish)
	return sock
}

// get is post's twin for the checks that a state-changing route refuses to be a link.
func get(t *testing.T, h http.HandlerFunc, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func post(t *testing.T, s *server, h http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

// The browser answers a prompt in a daemon it is not attached to, addressed by call id.
//
// The prompt itself never reaches this process — it is transient and belongs to the daemon's bus —
// so the id travelling with the status is the whole mechanism. A viewer that can show a pending
// permission and not grant it has stopped somewhere worse than not showing it at all.
func TestAnsweringAPromptOverTheSocket(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{ask: &app.Ask{ID: "call_7", Kind: "permission", What: "bash"}}
	sock := f.liveDaemon(t, wd, "asking", eng)
	f.session("asking", wd, "build it", 1, false)

	q := "?d=" + url.QueryEscape(sock)
	if w := post(t, f.srv, f.srv.answer, "/answer"+q, url.Values{
		"call": {"call_7"}, "kind": {"permission"}, "text": {"always"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/answer replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 1 || got[0] != "permission:call_7:always" {
		t.Errorf("the daemon received %v, want one permission:call_7:always", got)
	}

	// A question goes to the other call, with the text as the answer.
	if w := post(t, f.srv, f.srv.answer, "/answer"+q, url.Values{
		"call": {"q1#1"}, "kind": {"question"}, "text": {"main"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/answer replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 2 || got[1] != "answer:q1#1:main" {
		t.Errorf("the daemon received %v, want answer:q1#1:main second", got)
	}

	// A missing or unknown kind is refused, not defaulted. The two answers travel the same shape:
	// defaulted to permission, a question's answer becomes a decision string the core does not
	// recognise, which reads as "not allow" — so the tool is denied and the page says it worked.
	for _, bad := range []url.Values{
		{"call": {"call_7"}, "text": {"allow"}},                         // no kind
		{"call": {"call_7"}, "kind": {"guess"}, "text": {"allow"}},      // not a kind
		{"call": {"call_7"}, "kind": {"permission"}, "text": {"maybe"}}, // not a decision
		{"call": {"call_7"}, "kind": {"permission"}, "text": {"main"}},  // a question's answer
	} {
		if w := post(t, f.srv, f.srv.answer, "/answer"+q, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%v replied %d, want 400", bad, w.Code)
		}
	}
	if got := eng.seen(); len(got) != 2 {
		t.Errorf("a malformed answer reached the engine: %v", got)
	}

	// No call id is refused rather than guessed at: an answer with nowhere to go would be reported
	// as delivered and silently dropped.
	if w := post(t, f.srv, f.srv.answer, "/answer"+q, url.Values{
		"kind": {"permission"}, "text": {"allow"}}); w.Code != http.StatusBadRequest {
		t.Errorf("an answer with no call id replied %d, want 400", w.Code)
	}
	// And a GET does nothing: this is a state change, and a link that grants a permission is a
	// permission granted by a page preloader.
	r := httptest.NewRequest(http.MethodGet, "/answer"+q+"&call=call_7&text=allow", nil)
	w := httptest.NewRecorder()
	f.srv.answer(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /answer replied %d, want 405", w.Code)
	}
	if got := eng.seen(); len(got) != 2 {
		t.Errorf("a GET reached the engine: %v", got)
	}
}

// The composer STEERS. A daemon is usually mid-turn, and Submit queues a new turn behind the one
// running — so the sentence you typed to redirect the work arrives after the work is over.
func TestTheComposerSteersRatherThanQueues(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "busy", eng)
	f.session("busy", wd, "a long job", 5, false)

	q := "?d=" + url.QueryEscape(sock)
	if w := post(t, f.srv, f.srv.submit, "/submit"+q, url.Values{"text": {"actually, use the other API"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/submit replied %d: %s", w.Code, w.Body.String())
	}
	got := eng.seen()
	if len(got) != 1 || !strings.HasPrefix(got[0], "steer:") {
		t.Fatalf("the daemon received %v, want a steer", got)
	}
	if !strings.Contains(got[0], "actually, use the other API") {
		t.Errorf("the text did not arrive intact: %q", got[0])
	}
	// Empty input is refused before it reaches anybody.
	if w := post(t, f.srv, f.srv.submit, "/submit"+q, url.Values{"text": {"   "}}); w.Code != http.StatusBadRequest {
		t.Errorf("an empty steer replied %d, want 400", w.Code)
	}
	if w := post(t, f.srv, f.srv.interrupt, "/interrupt"+q, nil); w.Code != http.StatusNoContent {
		t.Fatalf("/interrupt replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 2 || got[1] != "interrupt" {
		t.Errorf("the daemon received %v, want an interrupt second", got)
	}
}

// A daemon that restarted leaves this process holding a socket nobody reads. The first write after
// that fails on the connection rather than on anything the user did, so it is retried once on a
// fresh one — otherwise every viewer needs a page reload after every daemon restart.
func TestAWriteSurvivesADaemonRestart(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "restarted", eng)
	f.session("restarted", wd, "hello", 1, false)
	q := "?d=" + url.QueryEscape(sock)

	if w := post(t, f.srv, f.srv.submit, "/submit"+q, url.Values{"text": {"first"}}); w.Code != http.StatusNoContent {
		t.Fatalf("first steer replied %d: %s", w.Code, w.Body.String())
	}
	// Break the cached connection the way a restart does, leaving the daemon itself running.
	f.srv.mu.Lock()
	for _, c := range f.srv.clients {
		c.Close()
	}
	f.srv.mu.Unlock()

	if w := post(t, f.srv, f.srv.submit, "/submit"+q, url.Values{"text": {"second"}}); w.Code != http.StatusNoContent {
		t.Fatalf("the steer after the connection dropped replied %d: %s", w.Code, w.Body.String())
	}
	got := eng.seen()
	if len(got) != 2 || !strings.Contains(got[1], "second") {
		t.Errorf("the daemon received %v, want both steers", got)
	}
}

// The transcript stream is the page's whole view of a run. It must open on what already happened
// (a viewer that starts late is the normal case) and then follow the log as another process writes
// it — the defect that made this page look live while showing one frozen frame.
func TestTheEventStreamOpensOnThePastAndFollows(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "streamed", true)
	f.session("streamed", wd, "first prompt", 1, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/events?d="+url.QueryEscape(sock), nil).WithContext(ctx)
	w := newStreamRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.srv.events(w, r) }()

	if !w.waitFor(t, "first prompt", 3*time.Second) {
		t.Fatalf("the stream never carried the backlog: %s", w.body())
	}
	// Something else writes to the log; the open stream must show it without reconnecting.
	f.session("streamed", wd, "a second thing entirely", 0, false)
	if !w.waitFor(t, "a second thing entirely", 3*time.Second) {
		t.Fatalf("the stream froze at the frame it opened with: %s", w.body())
	}
	// Frames are server-sent events, not raw JSON: a page reading these with EventSource needs the
	// data: prefix and the blank-line terminator.
	if !strings.Contains(w.body(), "data: [") || !strings.Contains(w.body(), "\n\n") {
		t.Errorf("the stream is not SSE-framed: %.200s", w.body())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("the stream did not stop when the client went away")
	}
}

// streamRecorder is an http.ResponseWriter that a test can read while the handler is still writing.
type streamRecorder struct {
	mu  sync.Mutex
	buf strings.Builder
	hdr http.Header
}

func newStreamRecorder() *streamRecorder { return &streamRecorder{hdr: http.Header{}} }

func (s *streamRecorder) Header() http.Header { return s.hdr }
func (s *streamRecorder) WriteHeader(int)     {}
func (s *streamRecorder) Flush()              {}
func (s *streamRecorder) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(b)
}

func (s *streamRecorder) body() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *streamRecorder) waitFor(t *testing.T, want string, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if strings.Contains(s.body(), want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// The rows the page draws come from the log, and a tool call keeps its arguments: what a tool was
// asked to DO is most of what a watcher wants, and the name alone was not enough on the terminal's
// pane strip either.
func TestTranscriptRowsKeepWhatWasAsked(t *testing.T) {
	rows := renderMessages([]session.Message{{
		Role: session.RoleAssistant,
		Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "thinking about it"},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`)}},
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
				CallID: "c1", Content: json.RawMessage(`"FAIL"`), IsError: true}},
			{Kind: session.PartText, Text: "  "},
		},
	}})
	var kinds []string
	for _, r := range rows {
		kinds = append(kinds, r.Who)
	}
	if strings.Join(kinds, ",") != "thinking,tool,failed" {
		t.Fatalf("rows came out as %v", kinds)
	}
	// The arguments moved from Text to their own field. Carried apart so the page can fold a call
	// behind a summary naming the tool without taking the name back out of a string this file has
	// just put it into — the assertion is the same one, on the field that now holds it.
	if rows[1].Tool != "bash" {
		t.Errorf("the tool row does not name the tool: %q", rows[1].Tool)
	}
	if !strings.Contains(rows[1].Args, "go test ./...") {
		t.Errorf("the tool row lost its arguments: %q", rows[1].Args)
	}
	if !strings.Contains(rows[2].Text, "FAIL") {
		t.Errorf("the failed result lost its output: %q", rows[2].Text)
	}
}

// Sending to one agent must not wait on the others.
//
// Resolving the target used to list the whole fleet, and listing dials every daemon to see who is
// alive. So a steer typed into a healthy agent paid for a wedged neighbour before it was sent — the
// cost landing on the one action where somebody is watching the cursor. Resolving is a lookup in
// the published records; liveness is a question only the dashboard asks.
func TestSendingToOneAgentDoesNotWaitOnTheOthers(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "healthy", eng)
	f.session("healthy", wd, "hello", 1, false)

	// Three neighbours that accept and never answer — the shape that costs a probe its full bound.
	for i := 0; i < 3; i++ {
		name := "wedged" + string(rune('a'+i))
		p := filepath.Join(f.cfgDir, "daemon-"+name+".sock")
		ln, err := net.Listen("unix", p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ln.Close() })
		go func() {
			var held []net.Conn
			for {
				c, aerr := ln.Accept()
				if aerr != nil {
					for _, h := range held {
						h.Close()
					}
					return
				}
				held = append(held, c)
			}
		}()
		unpublish, err := daemon.Publish(p, wd, "s_"+name, daemon.Identity{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(unpublish)
	}

	start := time.Now()
	w := post(t, f.srv, f.srv.submit, "/submit?d="+url.QueryEscape(sock), url.Values{"text": {"go on"}})
	took := time.Since(start)
	if w.Code != http.StatusNoContent {
		t.Fatalf("/submit replied %d: %s", w.Code, w.Body.String())
	}
	if took > 500*time.Millisecond {
		t.Errorf("one steer took %s with three wedged neighbours — it is probing them", took)
	}
	if got := eng.seen(); len(got) != 1 {
		t.Errorf("the healthy daemon received %v", got)
	}
}

// The context panel says a companion is nearly full; this is the lever beside the reading.
//
// The daemon has accepted `compact` since it was written and the TUI calls it — the gap was that
// nothing on the console did, so a supervisor could see the problem and had to open a terminal.
func TestTheConsoleCanFoldACompanionsHistory(t *testing.T) {
	f := newFleetFixture(t)
	eng := &recordingEngine{}
	wd := shortTempDir(t)
	sock := f.liveDaemon(t, wd, "api", eng)

	if w := post(t, f.srv, f.srv.compact, "/compact?d="+url.QueryEscape(sock), nil); w.Code != http.StatusNoContent {
		t.Fatalf("compacting answered %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 1 || !strings.HasPrefix(got[0], "compact") {
		t.Errorf("the daemon was told %v", got)
	}
	// A GET does not fold anything: it changes the session.
	if w := get(t, f.srv.compact, "/compact?d="+url.QueryEscape(sock)); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /compact answered %d", w.Code)
	}
	if got := eng.seen(); len(got) != 1 {
		t.Errorf("a GET reached the daemon: %v", got)
	}
}

// The two part kinds that were being dropped on the floor.
//
// renderMessages named four of them and fell through the rest, and a part kind a switch does not
// name is not an empty row — it is a row that never existed, with nothing anywhere saying so. An
// image the agent produced and an error that ended a turn both reached the log and neither ever
// reached the page.
func TestNoPartKindIsSilentlyDropped(t *testing.T) {
	rows := renderMessages([]session.Message{{
		Role: session.RoleAssistant,
		Parts: []session.Part{
			{Kind: session.PartImage, Image: &session.ImageRef{Path: "/tmp/plot.png", MIME: "image/png"}},
			{Kind: session.PartError, Err: "the provider closed the stream"},
		},
	}})
	var kinds []string
	for _, r := range rows {
		kinds = append(kinds, r.Who)
	}
	if strings.Join(kinds, ",") != "image,error" {
		t.Fatalf("rows came out as %v — a part reached the log and not the page", kinds)
	}
	if !strings.Contains(rows[0].Text, "plot.png") {
		t.Errorf("the image row does not say which image: %q", rows[0].Text)
	}
	if !strings.Contains(rows[1].Text, "closed the stream") {
		t.Errorf("the error row lost the reason: %q", rows[1].Text)
	}
}

// Every kind renderMessages can emit has somewhere to be drawn.
//
// A row with a class the stylesheet has never heard of is not a visible bug — it renders, in the
// default colour, looking like an assistant reply. Both new kinds arrived that way.
func TestEveryRowKindHasAStyle(t *testing.T) {
	for _, kind := range []string{"user", "assistant", "thinking", "tool", "result", "failed", "image", "error"} {
		if !strings.Contains(indexHTML, ".row."+kind+" ") {
			t.Errorf("a %q row has no style; it draws as an assistant reply", kind)
		}
	}
}

// The council goes back where it happened, not at the end.
//
// Appending is right until a session has a second turn, which is every session anybody keeps —
// and then round one's votes appear after round two's work, saying the members approved something
// they never saw. Each mark names the message it followed, so this is a splice.
func TestTheCouncilIsSplicedWhereItVoted(t *testing.T) {
	rows := []line{
		{Who: "user", Text: "first ask", msg: "m1"},
		{Who: "assistant", Text: "first answer", msg: "m2"},
		{Who: "user", Text: "second ask", msg: "m3"},
		{Who: "assistant", Text: "second answer", msg: "m4"},
	}
	marks := []app.CouncilMark{
		{After: "m2", Round: 1, Member: "Melchior", Decision: "done"},
		{After: "m2", Round: 1, Decision: "done", Tally: "3 done, 0 continue of 3"},
		{After: "m4", Round: 1, Member: "Casper", Decision: "continue"},
	}
	got := spliceCouncil(rows, marks)

	var order []string
	for _, r := range got {
		order = append(order, r.Who+":"+strings.SplitN(r.Text, "\n", 2)[0])
	}
	want := []string{
		"user:first ask", "assistant:first answer",
		"council:Melchior: done", "council:the council says done — 3 done, 0 continue of 3",
		"user:second ask", "assistant:second answer",
		"council:Casper: continue",
	}
	if strings.Join(order, " | ") != strings.Join(want, " | ") {
		t.Errorf("spliced as:\n  %s\nwant:\n  %s", strings.Join(order, "\n  "), strings.Join(want, "\n  "))
	}
}

// A mark whose anchor is not in the transcript still shows. A compaction can drop the message a
// vote followed, and a vote that silently vanishes is the state this whole change exists to end.
func TestACouncilMarkWithNoAnchorStillShows(t *testing.T) {
	rows := []line{{Who: "user", Text: "ask", msg: "m1"}}
	got := spliceCouncil(rows, []app.CouncilMark{{After: "gone", Round: 1, Member: "Balthasar", Decision: "abstain"}})
	if len(got) != 2 || got[1].Who != "council" {
		t.Fatalf("the orphaned vote was dropped: %+v", got)
	}
}

// A "done" that cited nothing says so. An empty citation on an approval is the thing somebody
// auditing a run most needs to see, and an absent line is not a statement.
func TestADoneVoteSaysWhenItCitedNothing(t *testing.T) {
	with := councilText(app.CouncilMark{Member: "Melchior", Decision: "done", Cite: "the build passed"})
	if !strings.Contains(with, "rests on: the build passed") {
		t.Errorf("a citation is not shown: %q", with)
	}
	without := councilText(app.CouncilMark{Member: "Melchior", Decision: "done"})
	if !strings.Contains(without, "nothing cited") {
		t.Errorf("an approval resting on nothing does not say so: %q", without)
	}
	// A "continue" carries no such claim, so it is not annotated either way.
	cont := councilText(app.CouncilMark{Member: "Casper", Decision: "continue"})
	if strings.Contains(cont, "rests on") {
		t.Errorf("a continue was annotated with a citation line: %q", cont)
	}
}
