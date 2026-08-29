package app

// The exported surface nothing was reading in a test: each function here had 0% coverage, and
// each test pins the CONTRACT its doc comment states — not that the function runs, but that the
// stated behaviour is the behaviour. Checked by mutation before landing (see the commit).

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// surfEvt builds one raw event for seeding a store directly.
func surfEvt(t *testing.T, typ event.Type, actor event.Actor, v any) event.Event {
	t.Helper()
	d, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Actor: actor, TS: time.Now(), Data: d}
}

// surfSession makes the session exist: the jsonl store refuses an append into a session whose
// first event is not session.created, which is the production shape too.
func surfSession(t *testing.T, a *App, sid session.SessionID) {
	t.Helper()
	d, _ := json.Marshal(event.SessionCreatedData{Workdir: t.TempDir()})
	if err := a.appendFact(context.Background(), sid, event.TypeSessionCreated,
		event.Actor{Kind: event.ActorSystem, ID: "test"}, d); err != nil {
		t.Fatal(err)
	}
}

func surfUser() event.Actor  { return event.Actor{Kind: event.ActorUser} }
func surfAgent() event.Actor { return event.Actor{Kind: event.ActorAgent} }

// NoteSessionMoved writes into the OLD session — it is a fact about that conversation, the reason
// its transcript stops — and the new one gets no mark at all.
func TestNoteSessionMovedWritesIntoTheOldSession(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	surfSession(t, a, "s_old")
	if err := a.NoteSessionMoved(ctx, "s_old", "s_new"); err != nil {
		t.Fatal(err)
	}
	evs, err := a.store.Read(ctx, "s_old", 0)
	if err != nil {
		t.Fatal(err)
	}
	var moved []event.SessionMovedData
	for _, e := range evs {
		if e.Type != event.TypeSessionMoved {
			continue
		}
		var d event.SessionMovedData
		if err := json.Unmarshal(e.Data, &d); err != nil {
			t.Fatal(err)
		}
		moved = append(moved, d)
	}
	if len(moved) != 1 || moved[0].To != "s_new" {
		t.Fatalf("the old session should carry one session.moved naming the destination, got %+v", moved)
	}
	if evs, _ := a.store.Read(ctx, "s_new", 0); len(evs) != 0 {
		t.Fatalf("the NEW session needs no mark — work carrying on is what its own events already say — got %d event(s)", len(evs))
	}
}

// PluginNote is a SYSTEM-actor prompt: it must never count as an unanswered user prompt, and an
// empty note (or an empty session) writes nothing at all.
func TestPluginNoteIsASystemPromptAndEmptyWritesNothing(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	surfSession(t, a, "s_pn")
	a.PluginNote("s_pn", "  engram saved skill X  ")
	evs, err := a.store.Read(ctx, "s_pn", 0)
	if err != nil {
		t.Fatal(err)
	}
	var prompts []event.Event
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			prompts = append(prompts, e)
		}
	}
	if len(prompts) != 1 {
		t.Fatalf("one prompt.submitted expected, got %+v", evs)
	}
	if prompts[0].Actor.Kind != event.ActorSystem || prompts[0].Actor.ID != "plugin" {
		t.Fatalf("a plugin note must ride the system actor (never an unanswered USER prompt), got %+v", prompts[0].Actor)
	}
	var d event.PromptSubmittedData
	if err := json.Unmarshal(prompts[0].Data, &d); err != nil {
		t.Fatal(err)
	}
	if got := partsText(d.Parts); got != "engram saved skill X" {
		t.Fatalf("the note should land trimmed, got %q", got)
	}

	a.PluginNote("s_pn2", "   ")
	a.PluginNote("", "text with nowhere to go")
	if evs, _ := a.store.Read(ctx, "s_pn2", 0); len(evs) != 0 {
		t.Fatalf("a blank note must write nothing, got %d event(s)", len(evs))
	}
}

// WindowOf is contextWindow for the wiring layer: the [limits] context_tokens override is the
// operator's number for every model, and it must be what the LLM client reads back.
func TestWindowOfHonoursTheOperatorOverride(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, completingLLM{}, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow", ContextTokens: 4096}))
	if got := a.WindowOf("some-model-nobody-seeded"); got != 4096 {
		t.Fatalf("context_tokens is the operator's number for every model, got %d", got)
	}
}

// surfRedirectedLLM is a provider that can say where its requests go.
type surfRedirectedLLM struct{ completingLLM }

func (surfRedirectedLLM) BaseURL() string { return "http://elsewhere:11434" }

// Backend is empty when the provider cannot say — which a caller reads as "nothing has redirected
// this" — and the provider's own answer when it can.
func TestBackendReadsTheProviderOrSaysNothing(t *testing.T) {
	if got := newTestApp(t).Backend(); got != "" {
		t.Fatalf("a provider with no BaseURL answers \"\", got %q", got)
	}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, surfRedirectedLLM{}, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow"}))
	if got := a.Backend(); got != "http://elsewhere:11434" {
		t.Fatalf("Backend should read the provider's own answer, got %q", got)
	}
}

// CouncilMemberNames answers the MAGI defaults when nothing is configured, and the configured
// seats in their order when something is.
func TestCouncilMemberNamesDefaultsAndConfigured(t *testing.T) {
	if got := newTestApp(t).CouncilMemberNames(); strings.Join(got, ",") != "Melchior,Balthasar,Casper" {
		t.Fatalf("unconfigured council should answer the MAGI defaults in order, got %v", got)
	}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, completingLLM{}, builtin.Default(), bus.New(), nil, Config{
		Permission:     "allow",
		CouncilMembers: []council.Member{{Name: "North"}, {Name: "South"}},
	}))
	if got := a.CouncilMemberNames(); strings.Join(got, ",") != "North,South" {
		t.Fatalf("configured seats should come back in their order, got %v", got)
	}
}

// AnswerSince: not finished → no answer yet; finished → the last thing said in the FIRST finished
// turn; finished having said nothing → reported as exactly that, not papered over as "".
func TestAnswerSinceReadsTheFirstFinishedTurn(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_hand"
	surfSession(t, a, sid)

	if _, err := a.store.Append(ctx, sid,
		surfEvt(t, event.TypePromptSubmitted, surfUser(), event.PromptSubmittedData{
			MessageID: "u1", Parts: []session.Part{{Kind: session.PartText, Text: "count the files"}}}),
		surfEvt(t, event.TypePartAppended, surfAgent(), event.PartAppendedData{
			MessageID: "a1", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: "there are 42"}}),
	); err != nil {
		t.Fatal(err)
	}
	if done, said := a.AnswerSince(ctx, sid, 0); done || said != "" {
		t.Fatalf("an unfinished turn has no answer yet, got (%v, %q)", done, said)
	}

	if _, err := a.store.Append(ctx, sid,
		surfEvt(t, event.TypeTurnFinished, surfAgent(), event.TurnFinishedData{}),
		// A SECOND finished turn with different words: the first turn's answer must not be
		// overwritten by it.
		surfEvt(t, event.TypePartAppended, surfAgent(), event.PartAppendedData{
			MessageID: "a2", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: "later piece's words"}}),
		surfEvt(t, event.TypeTurnFinished, surfAgent(), event.TurnFinishedData{}),
	); err != nil {
		t.Fatal(err)
	}
	if done, said := a.AnswerSince(ctx, sid, 0); !done || said != "there are 42" {
		t.Fatalf("the answer is the FIRST finished turn's last words, got (%v, %q)", done, said)
	}

	const silent session.SessionID = "s_silent"
	surfSession(t, a, silent)
	if _, err := a.store.Append(ctx, silent,
		surfEvt(t, event.TypeTurnFinished, surfAgent(), event.TurnFinishedData{}),
	); err != nil {
		t.Fatal(err)
	}
	if done, said := a.AnswerSince(ctx, silent, 0); !done || !strings.Contains(said, "finished without writing an answer") {
		t.Fatalf("finished-and-silent is news, not an empty string: got (%v, %q)", done, said)
	}
}

// LoopMap projects the log into turn shapes; an empty session says so instead of answering "".
func TestLoopMapProjectsTurns(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_map"
	surfSession(t, a, sid)

	if got, err := a.LoopMap(ctx, sid); err != nil || got != "Loop map — no turns yet." {
		t.Fatalf("an empty log should say there are no turns, got (%q, %v)", got, err)
	}
	if _, err := a.store.Append(ctx, sid,
		surfEvt(t, event.TypePromptSubmitted, surfUser(), event.PromptSubmittedData{
			MessageID: "u1", Parts: []session.Part{{Kind: session.PartText, Text: "fix the bug"}}}),
		surfEvt(t, event.TypePartAppended, surfAgent(), event.PartAppendedData{
			MessageID: "a1", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: "on it"}}),
	); err != nil {
		t.Fatal(err)
	}
	got, err := a.LoopMap(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Turn 1: fix the bug") || !strings.Contains(got, "1 step") {
		t.Fatalf("the map should carry the turn and its step count, got:\n%s", got)
	}
}

// FileDo: four verbs on the tree, each refusing the shape that loses somebody's afternoon.
func TestFileDoVerbsAndRefusals(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_files"
	surfSession(t, a, sid)
	wd := t.TempDir()

	// new-file makes an empty file — and never opens an existing one for overwriting.
	if err := a.FileDo(ctx, sid, wd, "new-file", "notes/todo.txt", "", false); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(wd, "notes", "todo.txt")); err != nil || len(b) != 0 {
		t.Fatalf("new-file should create an empty file, got (%q, %v)", b, err)
	}
	if err := a.FileDo(ctx, sid, wd, "new-file", "notes/todo.txt", "", false); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a \"new file\" over an existing one is a lost afternoon; it must refuse, got %v", err)
	}

	// new-dir.
	if err := a.FileDo(ctx, sid, wd, "new-dir", "sub/dir", "", false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(wd, "sub", "dir")); err != nil || !fi.IsDir() {
		t.Fatalf("new-dir should make the directory, got %v", err)
	}

	// rename moves — and refuses to move onto something that exists.
	if err := a.FileDo(ctx, sid, wd, "rename", "notes/todo.txt", "notes/done.txt", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wd, "notes", "done.txt")); err != nil {
		t.Fatalf("rename should have moved the file: %v", err)
	}
	if err := a.FileDo(ctx, sid, wd, "new-file", "notes/todo.txt", "", false); err != nil {
		t.Fatal(err)
	}
	if err := a.FileDo(ctx, sid, wd, "rename", "notes/todo.txt", "notes/done.txt", false); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("rename onto an existing file must refuse, got %v", err)
	}

	// delete removes one thing, not a tree.
	if err := a.FileDo(ctx, sid, wd, "delete", "notes/todo.txt", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wd, "notes", "todo.txt")); !os.IsNotExist(err) {
		t.Fatalf("delete should have removed the file, got %v", err)
	}
	if err := a.FileDo(ctx, sid, wd, "delete", "notes", "", false); err == nil {
		t.Fatal("deleting a non-empty directory with one press must refuse, like every editor's delete")
	}

	// The jail and the verb list.
	if err := a.FileDo(ctx, sid, wd, "new-file", "../escape.txt", "", false); err == nil ||
		!strings.Contains(err.Error(), "outside this workspace") {
		t.Fatalf("a path outside the workspace must be refused by name, got %v", err)
	}
	if err := a.FileDo(ctx, sid, wd, "truncate", "x", "", false); err == nil ||
		!strings.Contains(err.Error(), "new-file, new-dir, rename, delete") {
		t.Fatalf("an unknown verb should name the ones that exist, got %v", err)
	}
}

// putLabels persists the whole set as one labels.changed fact.
func TestPutLabelsPersistsTheSet(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	const sid session.SessionID = "s_lab"
	surfSession(t, a, sid)
	a.putLabels(ctx, sid, surfAgent(), []string{"refactor", "web"})
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var labels []event.Event
	for _, e := range evs {
		if e.Type == event.TypeLabelsChanged {
			labels = append(labels, e)
		}
	}
	if len(labels) != 1 {
		t.Fatalf("one labels.changed expected, got %+v", evs)
	}
	var d event.LabelsChangedData
	if err := json.Unmarshal(labels[0].Data, &d); err != nil {
		t.Fatal(err)
	}
	if strings.Join(d.Labels, ",") != "refactor,web" {
		t.Fatalf("the whole set travels, not a delta, got %v", d.Labels)
	}
}

// SubagentJobs lists what the strip polls; ForgetSubagent removes a child outright, whatever its
// state — the meeting-close reap, not the capacity eviction.
func TestSubagentJobsListAndForget(t *testing.T) {
	a := newTestApp(t)
	a.subJobs.start("c1", "loop_tool", "count things")
	a.subJobs.start("c2", "loop_tool", "another")
	a.subJobs.finish("c2", 7, "")

	jobs := a.SubagentJobs()
	if len(jobs) != 2 || jobs[0].ID != "c1" || !jobs[0].Running {
		t.Fatalf("both children, oldest first, the running one running, got %+v", jobs)
	}
	if jobs[1].Running || jobs[1].Steps != 7 {
		t.Fatalf("the finished child should carry its end, got %+v", jobs[1])
	}

	a.ForgetSubagent("c1") // running — forget removes it anyway
	if jobs := a.SubagentJobs(); len(jobs) != 1 || jobs[0].ID != "c2" {
		t.Fatalf("forget removes a child outright, got %+v", jobs)
	}
	a.ForgetSubagent("c1") // absent — a no-op, not a panic
}

// The background doors answer harmlessly for a job that does not exist: reading consumes nothing
// and kills report whether there was anything to kill.
func TestBackgroundDoorsOnAMissingJob(t *testing.T) {
	a := newTestApp(t)
	if got := a.BackgroundTail("no-such-job", 4096); got != "" {
		t.Fatalf("tail of a missing job is empty, got %q", got)
	}
	if a.KillBackgroundJob("no-such-job") {
		t.Fatal("killing a missing job must report that none existed")
	}
}

// looksLikePath separates a file-path-shaped topic from a generic label.
func TestLooksLikePath(t *testing.T) {
	for topic, want := range map[string]bool{
		"internal/app": true,
		"parse.go":     true,
		"discussion":   false,
		"weird plans":  false,
		"v1.2 rollout": true, // a dot is path-shaped by this rule, and that is the rule
	} {
		if got := looksLikePath(topic); got != want {
			t.Errorf("looksLikePath(%q) = %v, want %v", topic, got, want)
		}
	}
}

// DraftPR without a platform cannot even ask git, and the error must say so rather than come back
// as a quiet empty draft.
func TestDraftPRWithoutAPlatformSaysSo(t *testing.T) {
	a := newTestApp(t) // wired with plat == nil
	if out, err := a.DraftPR(context.Background(), "s_pr", t.TempDir(), ""); err == nil || out != "" {
		t.Fatalf("no platform should be an error, not an empty draft, got (%q, %v)", out, err)
	}
}

var _ port.LLMProvider = surfRedirectedLLM{} // the fake must stay a provider
