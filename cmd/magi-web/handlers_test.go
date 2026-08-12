package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
)

// recordingEngine is a daemon that remembers what the browser told it to do.
type recordingEngine struct {
	mu    sync.Mutex
	got   []string
	ask   *app.Ask
	doing string
	fail  error
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
func (r *recordingEngine) Permission() string                       { return "auto" }

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
func (r *recordingEngine) Doing(session.SessionID) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.doing, r.doing != ""
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
	// A call and its result are ONE row now. Split across two, the question a reader has — did that
	// work — could only be answered by finding the row below it and opening it.
	if strings.Join(kinds, ",") != "thinking,tool" {
		t.Fatalf("rows came out as %v", kinds)
	}
	if rows[1].Ok == nil {
		t.Fatal("the call does not say how it ended")
	}
	if *rows[1].Ok {
		t.Error("a failed call is reported as having worked")
	}
	// A failure is what somebody opens the row for, so it is what the body holds.
	if !strings.Contains(rows[1].Out, "FAIL") {
		t.Errorf("the failure's output is not on the row: %q", rows[1].Out)
	}
	// And keeping the output did not cost the arguments: what a tool was asked and what it said
	// are two facts, and the first attempt at this overwrote one with the other.
	if !strings.Contains(rows[1].Args, "go test ./...") {
		t.Errorf("pairing the result lost the arguments: %q", rows[1].Args)
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
}

// A result whose call is not in the transcript still draws. Compaction can take the call away, and
// a result with nowhere to attach must not vanish with it.
func TestAnOrphanedToolResultStillDraws(t *testing.T) {
	rows := renderMessages([]session.Message{{
		Role: session.RoleTool,
		Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: "gone", Content: json.RawMessage(`"output"`)}}},
	}})
	if len(rows) != 1 || rows[0].Who != "result" {
		t.Fatalf("an orphaned result came out as %+v", rows)
	}
}

// The last prompt is marked while nothing has answered it.
//
// Whether a prompt is being worked on is a fact about that prompt, and every row looked the same
// whether it had been answered an hour ago or was the one being thought about now.
func TestTheUnansweredPromptIsMarked(t *testing.T) {
	ask := session.Message{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}}
	reply := session.Message{Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartText, Text: "done"}}}

	waiting := markPending(renderMessages([]session.Message{ask}))
	if len(waiting) != 1 || !waiting[0].Pending {
		t.Errorf("a prompt with nothing after it is not marked: %+v", waiting)
	}
	answered := markPending(renderMessages([]session.Message{ask, reply}))
	for _, r := range answered {
		if r.Pending {
			t.Errorf("an answered prompt is marked as pending: %+v", answered)
		}
	}
	// And an earlier prompt is not marked just because a later one is.
	two := markPending(renderMessages([]session.Message{ask, reply, ask}))
	if two[0].Pending {
		t.Error("an older, answered prompt was marked pending")
	}
	if !two[len(two)-1].Pending {
		t.Error("the newest unanswered prompt is not marked")
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
		// The member is NOT in the text: the gutter beside the row is their name already, and
		// having it in both read "Melchior ✓ Melchior: done".
		"council:✓ done", "council:the council says done — 3 done, 0 continue of 3",
		"user:second ask", "assistant:second answer",
		// "reject", not "continue": from this council a continue is a rejection, and the raw word
		// read as progress.
		"council:✗ reject",
	}
	// …and the name is still on the row, where the gutter reads it from.
	for _, r := range got {
		if r.Who == "council" && r.Text == "✓ done" && r.Member != "Melchior" {
			t.Errorf("the vote lost the member it belongs to: %+v", r)
		}
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

// A council "continue" is a rejection, and says so.
//
// It is the gate on ending the turn: the work cannot proceed past it. The page printed the raw word
// in a neutral colour, which reads as progress — the opposite of what the vote means. The terminal
// has said "reject" since it drew its first verdict.
func TestAContinueVoteReadsAsTheRejectionItIs(t *testing.T) {
	got := councilText(app.CouncilMark{Member: "Casper", Decision: "continue", Why: "the tests do not run"})
	if strings.Contains(got, "continue") {
		t.Errorf("a rejection is still worded as %q", got)
	}
	if !strings.Contains(got, "reject") || !strings.Contains(got, "✗") {
		t.Errorf("a rejection does not read as one: %q", got)
	}
	// And an approval is not dressed up as one.
	ok := councilText(app.CouncilMark{Member: "Melchior", Decision: "done"})
	if !strings.Contains(ok, "✓") || strings.Contains(ok, "reject") {
		t.Errorf("an approval reads as %q", ok)
	}
}

// The call at the end with no result is the one running now.
//
// Ok is nil for a call that is in flight and also for one whose result never came — an interrupted
// turn, a compaction that took the result away. Only the trailing one is marked, because a
// heartbeat on a call that stopped hours ago is a lie about what the machine is doing.
func TestOnlyTheTrailingUnfinishedCallIsMarkedRunning(t *testing.T) {
	call := func(id string) session.Part {
		return session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: "bash"}}
	}
	result := func(id string) session.Part {
		return session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: json.RawMessage(`"ok"`)}}
	}
	// One finished call, then one still open.
	rows := markPending(renderMessages([]session.Message{{
		Role: session.RoleAssistant, Parts: []session.Part{call("a"), result("a"), call("b")},
	}}))
	if len(rows) != 2 {
		t.Fatalf("drew %d rows, want 2 (each call is one row)", len(rows))
	}
	if rows[0].Pending {
		t.Error("a finished call is marked as running")
	}
	if !rows[1].Pending {
		t.Error("the open call at the end is not marked as running")
	}

	// An open call with something after it is over, however it ended.
	stranded := markPending(renderMessages([]session.Message{
		{Role: session.RoleAssistant, Parts: []session.Part{call("a")}},
		{Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartText, Text: "moving on"}}},
	}))
	for _, r := range stranded {
		if r.Pending {
			t.Errorf("a call with work after it is still marked running: %+v", stranded)
		}
	}
}

// An asset is cached and revalidated, not frozen for a day.
//
// They were served immutable for 24h on the reasoning that they change with a release — and the
// consequence was that they change with a release and nobody sees it. An upgraded console served
// its new language pack to a browser that went on using yesterday's, so a label added in the same
// build rendered as its own dotted key. Twice, while working on this page, that read as a bug in
// the page rather than as a stale file.
func TestAnAssetIsRevalidatedRatherThanFrozen(t *testing.T) {
	f := newFleetFixture(t)
	w := get(t, f.srv.asset, "/i18n/language.en.json")
	if w.Code != 200 {
		t.Fatalf("the pack answered %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); strings.Contains(cc, "max-age=") {
		t.Errorf("the pack is frozen in the browser for %q", cc)
	}
	tag := w.Header().Get("ETag")
	if tag == "" {
		t.Fatal("nothing to revalidate against — no ETag")
	}
	// And the revalidation is cheap: the same bytes come back as a 304 with no body.
	r := httptest.NewRequest(http.MethodGet, "/i18n/language.en.json", nil)
	r.Header.Set("If-None-Match", tag)
	again := httptest.NewRecorder()
	f.srv.asset(again, r)
	if again.Code != http.StatusNotModified {
		t.Errorf("an unchanged asset answered %d rather than 304", again.Code)
	}
	if again.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes", again.Body.Len())
	}
}

// A call that WORKED carries what it answered, not only the ones that failed.
//
// Only failures used to travel, on the reasoning that a success is noise and the arguments are the
// more useful thing to keep. The row folds, so a success costs nothing until somebody opens it —
// and when they opened one they got the arguments they had just read in the summary line, again,
// with the answer nowhere. "What did the grep find" is most of why anybody opens a tool call.
func TestASuccessfulCallCarriesItsOutput(t *testing.T) {
	rows := renderMessages([]session.Message{{
		Role: session.RoleAssistant,
		Parts: []session.Part{
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "c1", Name: "grep", Args: json.RawMessage(`{"pattern":"empty-state"}`)}},
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
				CallID: "c1", Content: json.RawMessage(`"src/list.tsx\nsrc/table.tsx"`)}},
		},
	}})
	if len(rows) != 1 {
		t.Fatalf("rows came out as %+v", rows)
	}
	if rows[0].Ok == nil || !*rows[0].Ok {
		t.Fatal("a call that worked is not reported as having worked")
	}
	if !strings.Contains(rows[0].Out, "src/list.tsx") {
		t.Errorf("what it answered did not travel: %+v", rows[0])
	}
	// And what it was asked is still there: the two are different questions and the row holds both.
	if !strings.Contains(rows[0].Args, "empty-state") {
		t.Errorf("what it was asked was lost: %+v", rows[0])
	}
}

// A result too big for the wire keeps its END, which is where the answer is.
//
// Clipped from the front alone, a two-hundred-kilobyte build log arrived as the first eight
// kilobytes — the part where everything was still going fine — and the failure that made somebody
// open the row was exactly what got dropped.
func TestABigResultKeepsTheEndAndSaysWhatWentMissing(t *testing.T) {
	big := `"` + strings.Repeat("compiling…\\n", 3000) + `FAILED: 3 tests"`
	rows := renderMessages([]session.Message{{
		Role: session.RoleAssistant,
		Parts: []session.Part{
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"go test ./..."}`)}},
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
				CallID: "c1", Content: json.RawMessage(big)}},
		},
	}})
	if len(rows) != 1 {
		t.Fatalf("rows came out as %+v", rows)
	}
	out := rows[0].Out
	if len(out) >= len(big) {
		t.Fatalf("nothing was elided: %d of %d", len(out), len(big))
	}
	if !strings.Contains(out, "FAILED: 3 tests") {
		t.Errorf("the end went, which is the reason anybody opens the row: …%q", out[max(0, len(out)-60):])
	}
	if !strings.Contains(out, "bytes omitted") {
		t.Errorf("it does not say how much it dropped")
	}
}

// The approval mode is readable and settable from the console.
//
// It decides whether a companion stops for permission at all, and it could only be changed at the
// terminal it was started from. Somebody watching a blocked companion from a phone could answer the
// one prompt in front of them and not the rule that produced it — and the console could not even
// say which mode was on, so "why is this asking me" had no answer on the screen that showed it.
func TestTheApprovalModeIsReadAndSetOverTheSocket(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "minding", eng)
	f.session("minding", wd, "watch it", 1, false)

	// Read: the mode travels with the rest of the companion's facts, so the page can show which of
	// the four is on rather than offering four buttons and no state.
	var seen string
	list, err := fleet.ListCached(context.Background(), f.srv.reader, f.srv.cfgDir, f.srv.here, &f.srv.fleetCache)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.Socket == sock {
			seen = a.Permission
		}
	}
	if seen != "auto" {
		t.Errorf("the fleet row says the mode is %q, want the daemon's auto", seen)
	}

	q := "?d=" + url.QueryEscape(sock)
	if w := post(t, f.srv, f.srv.permission, "/permission"+q, url.Values{"mode": {"ask"}}); w.Code != http.StatusNoContent {
		t.Fatalf("/permission replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 1 || got[0] != "perm:ask" {
		t.Errorf("the daemon received %v, want one perm:ask", got)
	}

	// A mode the core does not know is refused here rather than forwarded. SetPermission takes a
	// string and ignores what it does not recognise, so a typo would answer 204 and change nothing.
	if w := post(t, f.srv, f.srv.permission, "/permission"+q, url.Values{"mode": {"whenever"}}); w.Code != http.StatusBadRequest {
		t.Errorf("an unknown mode answered %d", w.Code)
	}
	if got := eng.seen(); len(got) != 1 {
		t.Errorf("the daemon was told about it anyway: %v", got)
	}
}

// The document is revalidated too, not only the assets under it.
//
// It carries the whole front end, so a browser holding yesterday's copy is a person looking at
// yesterday's console — reporting bugs that were fixed and not seeing controls that exist. With no
// Cache-Control at all a browser applies its own heuristic, which is the worst of both: sometimes
// stale, never predictably.
func TestThePageIsRevalidatedRatherThanHeuristicallyCached(t *testing.T) {
	f := newFleetFixture(t)
	w := get(t, f.srv.page, "/")
	if w.Code != 200 {
		t.Fatalf("the page answered %d", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc == "" || strings.Contains(cc, "max-age=") {
		t.Errorf("the document's freshness is left to the browser to guess: %q", cc)
	}
	tag := w.Header().Get("ETag")
	if tag == "" {
		t.Fatal("nothing to revalidate against — no ETag")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("If-None-Match", tag)
	again := httptest.NewRecorder()
	f.srv.page(again, r)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("an unchanged page answered %d with %d bytes", again.Code, again.Body.Len())
	}
}

// Every row a message produces carries that message's time.
//
// The page can only draw what crosses, and the rows are what crosses. A turn's reasoning, its
// calls and its answer all belong to one moment; the log records the moment once, on the message.
func TestEveryRowOfAMessageCarriesItsTime(t *testing.T) {
	at := time.Date(2026, 8, 11, 4, 5, 0, 0, time.UTC)
	rows := renderMessages([]session.Message{{
		ID: "m1", Role: session.RoleAssistant, At: at,
		Parts: []session.Part{
			{Kind: session.PartReasoning, Text: "thinking about it"},
			{Kind: session.PartText, Text: "here is what I found"},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read", CallID: "c1", Args: []byte(`{}`)}},
		},
	}, {
		ID: "m2", Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "from an older log"}},
	}})
	if len(rows) != 4 {
		t.Fatalf("rendered %d rows", len(rows))
	}
	for i, r := range rows[:3] {
		if r.At != at.Format(time.RFC3339) {
			t.Errorf("row %d (%s) is stamped %q, want %q", i, r.Who, r.At, at.Format(time.RFC3339))
		}
	}
	// A message with no recorded time says nothing rather than 1970: the page draws an empty stamp
	// as no stamp, and a wrong one would be worse than a missing one.
	if rows[3].At != "" {
		t.Errorf("an unstamped message came through as %q", rows[3].At)
	}
}

// A note magi wrote says WHICH part of magi wrote it.
//
// The orchestrator's nudge, a planner's note and a mined spec all reach the log as "system", and
// the terminal has told them apart since they existed — it draws "⟳ orchestrator note:". The
// rebuild dropped the actor, so every other reader had one word for all of them, and "magi said
// something to itself" is not the fact anybody needs.
func TestASystemRowSaysWhichPartOfMagiWroteIt(t *testing.T) {
	rows := renderMessages([]session.Message{
		{ID: "m1", Role: session.RoleSystem, Author: "orchestrator",
			Parts: []session.Part{{Kind: session.PartText, Text: "you stopped without saying you are finished"}}},
		{ID: "m2", Role: session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: "carry on then"}}},
	})
	if len(rows) != 2 {
		t.Fatalf("rendered %d rows", len(rows))
	}
	if rows[0].By != "orchestrator" {
		t.Errorf("the note is attributed to %q", rows[0].By)
	}
	// Nothing is attributed to a mechanism when a person spoke.
	if rows[1].By != "" {
		t.Errorf("what the person said is attributed to %q", rows[1].By)
	}
}

// An edit crosses as the change it makes, built where the terminal's diff is built.
//
// The page could not do this for itself without a second implementation of a thing that already
// exists and is tested — and the page already knows how to colour a diff, so what was missing was
// the diff, not the drawing.
func TestAnEditCrossesAsItsDiff(t *testing.T) {
	call := func(name, args string, failed bool) []line {
		res := &session.ToolResult{CallID: "c1", Content: []byte("done"), IsError: failed}
		return renderMessages([]session.Message{{
			ID: "m1", Role: session.RoleAssistant,
			Parts: []session.Part{{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				Name: name, CallID: "c1", Args: []byte(args)}}},
		}, {
			ID: "m2", Role: session.RoleTool,
			Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: res}},
		}})
	}
	rows := call("edit", `{"path":"a.go","old":"one\ntwo","new":"one\nthree"}`, false)
	if len(rows) != 1 {
		t.Fatalf("rendered %d rows", len(rows))
	}
	if !strings.Contains(rows[0].Diff, "-two") || !strings.Contains(rows[0].Diff, "+three") {
		t.Errorf("the change reads %q", rows[0].Diff)
	}
	// A write is its content, added.
	if got := call("write", `{"path":"a.go","content":"hello"}`, false); !strings.Contains(got[0].Diff, "+hello") {
		t.Errorf("a write's change reads %q", got[0].Diff)
	}
	// A call that FAILED describes a change that never happened, and drawing it would show the
	// file as it is not.
	if got := call("edit", `{"path":"a.go","old":"one","new":"two"}`, true); got[0].Diff != "" {
		t.Errorf("a refused edit still came through as a diff: %q", got[0].Diff)
	}
	// Anything else is not an edit and has no diff to show.
	if got := call("bash", `{"command":"ls"}`, false); got[0].Diff != "" {
		t.Errorf("a command came through with a diff: %q", got[0].Diff)
	}
}

// A prompt nothing will ever answer says so, and does not wear a spinner.
//
// It was cancelled, or a later request swallowed it — the log records that in its own event, and
// the terminal has drawn the note since prompts could be cancelled. Without it a cancelled request
// reads as a question the companion ignored, and being the last row it also got the "working on
// it" mark: a spinner over work that will never happen.
func TestAnAbandonedPromptSaysSoAndDoesNotSpin(t *testing.T) {
	rows := markPending(renderMessages([]session.Message{
		{ID: "m1", Role: session.RoleUser, Abandoned: true,
			Parts: []session.Part{{Kind: session.PartText, Text: "never mind this one"}}},
	}))
	if len(rows) != 1 {
		t.Fatalf("rendered %d rows", len(rows))
	}
	if !rows[0].Abandoned {
		t.Error("the row does not carry what the log said about it")
	}
	if rows[0].Pending {
		t.Error("a request that will never be answered is marked as being worked on")
	}
	// One that was NOT abandoned still is: this is a distinction, not a blanket.
	live := markPending(renderMessages([]session.Message{
		{ID: "m2", Role: session.RoleUser,
			Parts: []session.Part{{Kind: session.PartText, Text: "do this one"}}},
	}))
	if !live[0].Pending {
		t.Error("an unanswered live prompt lost its mark")
	}
}

// A file that was written and then linted is not a write that failed.
//
// A post-edit hook or a language server attaches its complaint to the result and marks it an
// error, deliberately: that is what makes the agent read it instead of moving on. Every screen
// drew its outcome glyph from the same field, so the row said ✗ over a file that was on disk —
// reported live, with the model treating it as done and both windows saying it had failed.
func TestAFileWrittenAndThenLintedIsNotAFailedWrite(t *testing.T) {
	row := func(res *session.ToolResult) line {
		rows := renderMessages([]session.Message{{
			ID: "m1", Role: session.RoleAssistant,
			Parts: []session.Part{{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				Name: "write", CallID: "c1", Args: []byte(`{"path":"hello.py","content":"print(1)"}`)}}},
		}, {
			ID: "m2", Role: session.RoleTool,
			Parts: []session.Part{{Kind: session.PartToolResult, ToolResult: res}},
		}})
		if len(rows) != 1 {
			t.Fatalf("rendered %d rows", len(rows))
		}
		return rows[0]
	}

	noted := row(&session.ToolResult{CallID: "c1", IsError: true, Advisory: true,
		Content: []byte(`"wrote 22 bytes to hello.py\n\n[diagnostics]\nPython: unused import"`)})
	if !noted.Note {
		t.Error("the row does not carry that the work happened")
	}
	if noted.Ok == nil || *noted.Ok {
		t.Error("it is still an error for the agent, which is what makes it read the diagnostic")
	}
	// And the change it made is worth drawing, because it was made.
	if noted.Diff == "" {
		t.Error("a write that landed shows no diff")
	}

	// A refusal is a different thing and keeps saying so: nothing was written, so there is nothing
	// to draw and nothing to soften.
	refused := row(&session.ToolResult{CallID: "c1", IsError: true,
		Content: []byte(`"refused: you said no"`)})
	if refused.Note {
		t.Error("a refusal came through as work that happened")
	}
	if refused.Diff != "" {
		t.Errorf("a refused write drew a change that never happened: %q", refused.Diff)
	}
}

// The shape a report must take is readable and writable from the console.
//
// The sections are a contract — ask_user refuses a report with one missing — and the only way to
// change them was to write a markdown file into a workspace. The person the report is FOR is the
// one who knows what belongs in it, and they are the one looking at this page.
func TestTheReportFormatIsReadAndWrittenFromTheConsole(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.liveDaemon(t, wd, "shaping", &recordingEngine{})
	f.session("shaping", wd, "decide something", 1, false)
	q := "?d=" + url.QueryEscape(sock)

	// With nothing written anywhere, the built-in default is what the agent is held to, and that
	// is what the page must show — not an empty card implying there is no contract.
	var got reportFormat
	w := get(t, f.srv.reportFormat, "/report-format"+q)
	if w.Code != 200 {
		t.Fatalf("reading the format answered %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.From != "default" || len(got.Sections) != len(report.Default) {
		t.Fatalf("with nothing written the page is told %+v", got)
	}

	// Written: to the WORKSPACE, where the agent's own loader reads it — a console can hold
	// companions from several projects and one edit must not re-shape all of them.
	if w := post(t, f.srv, f.srv.reportFormat, "/report-format"+q, url.Values{
		"key":    {"tried", "risk"},
		"prompt": {"what you ran", "what breaks if this is wrong"},
	}); w.Code != http.StatusNoContent {
		t.Fatalf("writing answered %d: %s", w.Code, w.Body.String())
	}
	body, err := os.ReadFile(filepath.Join(wd, ".magi", "skills", "decision-report.md"))
	if err != nil {
		t.Fatalf("nothing was written where the agent reads it: %v", err)
	}
	// Read back by the SAME parser the agent uses, not by looking for the strings just written:
	// what matters is that the file is a contract, not that it contains some text.
	c := report.Parse(string(body))
	if len(c) != 2 || c[0].Key != "tried" || c[1].Key != "risk" {
		t.Fatalf("the file does not parse as the sections that were saved: %+v", c)
	}
	if c[1].Prompt != "what breaks if this is wrong" {
		t.Errorf("a section lost its prompt: %q", c[1].Prompt)
	}

	// And the page now reads it back as the workspace's own.
	w = get(t, f.srv.reportFormat, "/report-format"+q)
	got = reportFormat{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.From != "workspace" || len(got.Sections) != 2 {
		t.Errorf("after writing, the page is told %+v", got)
	}

	// An empty contract is refused: with no sections ask_user would accept a report with nothing
	// in it, which is the state the whole mechanism exists to prevent.
	if w := post(t, f.srv, f.srv.reportFormat, "/report-format"+q, url.Values{"key": {"  "}}); w.Code != http.StatusBadRequest {
		t.Errorf("a report with no sections was accepted (%d)", w.Code)
	}
	// So are two sections with one name: Fill writes by key, so the second would silently take the
	// first one's place and somebody would be missing a section they wrote.
	if w := post(t, f.srv, f.srv.reportFormat, "/report-format"+q, url.Values{
		"key": {"tried", "tried"}, "prompt": {"a", "b"}}); w.Code != http.StatusBadRequest {
		t.Errorf("two sections with one name were accepted (%d)", w.Code)
	}
}

// Resume returns whatever the daemon decides — this fake records the ask and can refuse.
func (r *recordingEngine) Resume(_ context.Context, sid session.SessionID) error {
	return r.note("resume:" + string(sid))
}

// The console asks the DAEMON to move, and carries its refusal back unchanged.
//
// Which conversation a companion is in is the daemon's answer, not this process's: only it knows
// whether a turn is running and which sessions are its own. A console that pre-judged either would
// be a second truth, and the one people would see first.
func TestMovingACompanionToAnotherConversationAsksTheDaemon(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "mover", eng)
	f.session("mover", wd, "hello", 1, false)
	q := "?d=" + url.QueryEscape(sock)

	if w := post(t, f.srv, f.srv.resume, "/resume"+q, url.Values{"session": {"a7"}}); w.Code != http.StatusNoContent {
		t.Fatalf("the move replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 1 || got[0] != "resume:a7" {
		t.Errorf("the daemon was told %v — the conversation asked for must cross unchanged", got)
	}

	// No session named is refused HERE, because there is nothing to ask about.
	if w := post(t, f.srv, f.srv.resume, "/resume"+q, url.Values{}); w.Code != http.StatusBadRequest {
		t.Errorf("a move with no conversation named replied %d", w.Code)
	}

	// And the daemon's own refusal reaches the person, in the daemon's words: "mid-turn" and "not
	// this workspace" are the two they can act on, and a console that flattened them to "failed"
	// would leave somebody pressing the same button.
	eng.fail = errors.New("this companion is mid-turn in a1")
	w := post(t, f.srv, f.srv.resume, "/resume"+q, url.Values{"session": {"a9"}})
	if w.Code == http.StatusNoContent {
		t.Fatal("a refused move was reported as done")
	}
	if !strings.Contains(w.Body.String(), "mid-turn") {
		t.Errorf("the daemon's reason did not reach the page: %q", w.Body.String())
	}
}

// A page watching a COMPANION follows it into the conversation it moves to.
//
// The stream is addressed by companion — that is what `?d=` names — and it used to resolve the
// session once, at open. After a move it polled the conversation the companion had left, for as
// long as the tab stayed open, while everything typed into that page went to the new one. What is
// on screen and what a control reaches must not be two different things.
func TestAWatchingPageFollowsTheCompanionToItsNewConversation(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "wanderer", true)
	f.session("wanderer", wd, "the first conversation", 1, false)
	f.session("elsewhere", wd, "the second conversation", 1, false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/events?d="+url.QueryEscape(sock), nil).WithContext(ctx)
	w := newStreamRecorder()
	done := make(chan struct{})
	go func() { defer close(done); f.srv.events(w, r) }()

	if !w.waitFor(t, "the first conversation", 3*time.Second) {
		t.Fatalf("the stream never opened on the conversation it was pointed at: %s", w.body())
	}
	// The daemon moves, which is a rewrite of the record every reader polls.
	if err := daemon.Moved(sock, "elsewhere"); err != nil {
		t.Fatal(err)
	}
	if !w.waitFor(t, "the second conversation", 5*time.Second) {
		t.Fatalf("the stream stayed on the conversation the companion left: %s", w.body())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Error("the stream did not stop when the client went away")
	}
}

// A companion that answered no is not a machine that could not be reached.
//
// Everything coming back from a daemon was 502. Most of it is the opposite of a gateway failure:
// it was reached, it understood, and it refused — mid-turn, or a conversation that is not in its
// workspace. Reported as 502 a refusal reads as a broken console, and the reason scrolls past as a
// toast on a page that looks disconnected.
func TestARefusalIsNotAGatewayError(t *testing.T) {
	f := newFleetFixture(t)
	eng := &recordingEngine{fail: errors.New("this companion is mid-turn")}
	sock := f.liveDaemon(t, t.TempDir(), "s1", eng)
	w := post(t, f.srv, f.srv.submit, "/submit?d="+url.QueryEscape(sock), url.Values{"text": {"go"}})
	if w.Code != http.StatusConflict {
		t.Errorf("a refusal answered %d, wanted 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "mid-turn") {
		t.Errorf("the companion's own reason did not come back: %s", w.Body.String())
	}
	// And it was asked once. The reconnect exists for a socket the daemon has replaced, and asking
	// a companion that already answered gets the same answer down a second connection.
	var tries int
	for _, s := range eng.seen() {
		// Submit or steer: a companion that is doing something is steered, and which one this
		// fixture takes is not what is being measured.
		if strings.HasPrefix(s, "submit:") || strings.HasPrefix(s, "steer:") {
			tries++
		}
	}
	if tries != 1 {
		t.Errorf("the refusal was asked for %d times: %v", tries, eng.seen())
	}
}

// A companion that is not running says so, rather than naming a socket file.
//
// Turning a daemon off leaves its record behind on purpose — the board still shows what it did and
// its conversations are files — so the console offers everything about it and fails only when
// somebody sends. It failed with "dial unix …: connect: no such file or directory", which is true
// and answers none of what the person is then wondering, chiefly whether the conversation they
// were reading is gone.
func TestAStoppedCompanionSaysSoInWords(t *testing.T) {
	f := newFleetFixture(t)
	wd := namedWorkdir(t, "docs")
	// A record with nothing listening behind it: exactly what a daemon leaves when it exits.
	sock := f.daemonAt(wd, "docs", false)
	w := post(t, f.srv, f.srv.submit, "/submit?d="+url.QueryEscape(sock), url.Values{"text": {"go"}})
	body := w.Body.String()
	if strings.Contains(body, "no such file") || strings.Contains(body, "connect:") {
		t.Errorf("the console answered with the syscall: %s", body)
	}
	if !strings.Contains(body, "not running") || !strings.Contains(body, "on disk") {
		t.Errorf("it does not say the companion is off and its conversations are kept: %s", body)
	}
}
