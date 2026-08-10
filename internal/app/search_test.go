package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// writeTurns puts a session on disk with the given prompts, each answered by one line of assistant
// text. Straight to the store: this exercises the reader, and driving a model to produce the log
// would make the fixture the model's shape rather than the one the test needs.
func writeTurns(t *testing.T, a *App, workdir string, prompts ...string) session.SessionID {
	t.Helper()
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: workdir,
		Actor:   event.Actor{Kind: event.ActorUser, ID: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range prompts {
		if err := a.appendPrompt(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: p}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "test"},
		}); err != nil {
			t.Fatal(err)
		}
		a.appendPart(ctx, sid, event.Actor{Kind: event.ActorAgent, ID: "test"},
			fmt.Sprintf("m_reply%d", i), session.RoleAssistant,
			session.Part{Kind: session.PartText, Text: fmt.Sprintf("answering %d", i)})
	}
	return sid
}

// TestALongSessionDoesNotWinEveryQuery is the reason turns are the unit of search.
//
// rank.ByIDF scores on which query words are PRESENT, weighted by rarity — no term frequency, no
// length normalisation. Under whole-session documents, a session long enough to contain every word
// contains every query, and it comes back first for all of them. In this workspace the largest log
// is tens of megabytes: it would be the answer to everything.
//
// Break the granularity — make buildTurns emit one document per session — and this test fails. That
// is what it is for.
func TestALongSessionDoesNotWinEveryQuery(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})

	// The precise session goes in FIRST, so that listing order works against the answer.
	//
	// This matters, and the first version of this test did it the other way round and passed with
	// whole-session documents: under that granularity both sessions carry both words, both score
	// identically, and the tie broke on position — which happened to favour the right one. The test
	// was measuring the sort's stability, not the ranking. Now the wrong answer is the one listing
	// order would pick, so only the ranking can produce the right one.
	want := writeTurns(t, a, wd, "the dehydration handler drops a shard on the floor")

	// A rambling session that mentions everything, including both search words — but in separate
	// turns, which is exactly the case whole-session documents cannot tell apart from the above.
	var long []string
	for i := range 40 {
		long = append(long, fmt.Sprintf(
			"turn %d about deployment pipelines and caching and templates and migrations", i))
	}
	long = append(long, "somewhere in here we also mentioned dehydration once")
	long = append(long, "and separately, in another turn entirely, the handler was slow")
	writeTurns(t, a, wd, long...)

	out, err := a.SearchSessions(context.Background(), wd, "dehydration handler")
	if err != nil {
		t.Fatal(err)
	}
	first := firstSessionIn(t, out)
	if first != want {
		t.Errorf("the long session outranked the precise one.\nwanted %s first, got %s\n\n%s",
			want, first, out)
	}
}

// firstSessionIn pulls the first session id out of a rendered search answer.
func firstSessionIn(t *testing.T, out string) session.SessionID {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		for _, f := range strings.Split(line, " · ") {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(f, "s_") && !strings.Contains(f, "#") {
				return session.SessionID(f)
			}
		}
	}
	t.Fatalf("no session id in the answer:\n%s", out)
	return ""
}

func TestASearchFindsTheTurnAndSaysHowToReadIt(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid := writeTurns(t, a, wd,
		"set up the postgres container",
		"the vacuum threshold needs raising on the events table",
		"write the changelog")

	out, err := a.SearchSessions(context.Background(), wd, "vacuum threshold")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, string(sid)) {
		t.Fatalf("the session is not in the answer:\n%s", out)
	}
	if !strings.Contains(out, "vacuum threshold needs raising") {
		t.Errorf("the matching turn is not shown:\n%s", out)
	}
	// The other two turns of the same session are not what was asked for.
	if strings.Contains(out, "write the changelog") {
		t.Errorf("an unrelated turn from the same session came along:\n%s", out)
	}
	// Every hit carries the id that reads it back.
	if !strings.Contains(out, string(sid)+"#") {
		t.Errorf("no turn id to open:\n%s", out)
	}
	// And it says which kind of search this was, so a caller can interpret an empty answer.
	if !strings.Contains(out, "by wording") {
		t.Errorf("the answer does not say how it searched:\n%s", out)
	}
}

func TestOpeningATurnGivesItWholeAndOnlyIt(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid := writeTurns(t, a, wd, "first thing", "the middle thing", "last thing")

	out, err := a.SearchSessions(context.Background(), wd, "middle")
	if err != nil {
		t.Fatal(err)
	}
	ref := turnRefIn(t, out)

	whole, err := a.OpenTurn(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(whole, "the middle thing") {
		t.Errorf("the turn is missing its own prompt:\n%s", whole)
	}
	if !strings.Contains(whole, "answering 1") {
		t.Errorf("the turn is missing the reply:\n%s", whole)
	}
	// A turn ends where the next one begins. Without that boundary, opening the first turn of a
	// long session would hand back the whole session.
	if strings.Contains(whole, "last thing") {
		t.Errorf("the next turn leaked in:\n%s", whole)
	}
	if strings.Contains(whole, "first thing") {
		t.Errorf("the previous turn leaked in:\n%s", whole)
	}
	_ = sid
}

func turnRefIn(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		for _, f := range strings.Fields(line) {
			if strings.HasPrefix(f, "s_") && strings.Contains(f, "#") {
				return f
			}
		}
	}
	t.Fatalf("no turn id in:\n%s", out)
	return ""
}

// The tools a turn used are searchable, because "the run where it used ripgrep" is a thing people
// remember when the words of the conversation have gone. Their ARGUMENTS and RESULTS are not: a
// build's whole stdout in the index would make every session match "error", and results are most of
// a log's bytes.
func TestATurnIsFindableByTheToolsItUsed(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: wd, Actor: event.Actor{Kind: event.ActorUser, ID: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "tidy the imports"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "test"},
	}); err != nil {
		t.Fatal(err)
	}
	a.appendPart(ctx, sid, event.Actor{Kind: event.ActorAgent, ID: "test"}, "m_1", session.RoleAssistant,
		session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: "c1", Name: "portowner", Args: []byte(`{"port":"5432 secretsauce"}`)}})
	a.appendPart(ctx, sid, event.Actor{Kind: event.ActorAgent, ID: "test"}, "m_2", session.RoleTool,
		session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: "c1", Content: []byte(`"quinoa hexadecimal"`)}})

	out, err := a.SearchSessions(ctx, wd, "portowner")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tidy the imports") {
		t.Errorf("a turn is not findable by the tool it ran:\n%s", out)
	}
	// The words in the arguments and the result must not be in the index.
	for _, noise := range []string{"secretsauce", "quinoa"} {
		found, ferr := a.SearchSessions(ctx, wd, noise)
		if ferr != nil {
			t.Fatal(ferr)
		}
		if strings.Contains(found, "tidy the imports") {
			t.Errorf("%q from a tool's arguments or result is in the index:\n%s", noise, found)
		}
	}
}

func TestABadTurnIdIsAnsweredNotThrown(t *testing.T) {
	a, _ := newApp(t, &fakeLLM{}, Config{})
	for _, ref := range []string{"", "nonsense", "s_missing#4", "s_x#notanumber"} {
		out, err := a.OpenTurn(context.Background(), ref)
		if err != nil {
			t.Errorf("OpenTurn(%q) returned an error rather than an answer: %v", ref, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("OpenTurn(%q) said nothing", ref)
		}
	}
}

func TestSearchingAnEmptyWorkspaceSaysSo(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	out, err := a.SearchSessions(context.Background(), wd, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no earlier work") {
		t.Errorf("got %q", out)
	}
	// And a workspace with work but no match says a different thing, so the two can be told apart.
	writeTurns(t, a, wd, "something unrelated")
	out, err = a.SearchSessions(context.Background(), wd, "zzzqqq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing in") {
		t.Errorf("a workspace with no match reads like an empty one: %q", out)
	}
}

// TestNewTurnsShowUpWithoutARestart guards the cache key. Sessions are append-only, so "has this
// changed" is answered by LastActivity — but only if the cache actually consults it.
func TestNewTurnsShowUpWithoutARestart(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid := writeTurns(t, a, wd, "the first thing we discussed")

	if _, err := a.SearchSessions(context.Background(), wd, "first"); err != nil {
		t.Fatal(err)
	}
	// Same session, a new turn, after the cache was warmed.
	ctx := context.Background()
	if err := a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "a quite different subject: kerning"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "test"},
	}); err != nil {
		t.Fatal(err)
	}

	out, err := a.SearchSessions(ctx, wd, "kerning")
	if err != nil {
		t.Fatal(err)
	}
	// Asserted on the SNIPPET, not on the query word. Both answers — the hit and the miss — quote
	// the query back, so looking for "kerning" in the output passed with the cache never
	// invalidating at all. The words that only appear when the turn was actually found are the
	// ones worth checking.
	if !strings.Contains(out, "a quite different subject") {
		t.Errorf("a turn added after the first search is invisible — the cache is not being invalidated:\n%s", out)
	}
}

// A prompt magi injects into a turn of its own accord carries a system actor. Treating one as a
// turn boundary would chop a single piece of work into fragments, and each fragment would rank on
// its own with the context that explains it in a different document.
func TestOnlyAPersonsPromptStartsATurn(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{
		Workdir: wd, Actor: event.Actor{Kind: event.ActorUser, ID: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "port the parser"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "test"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "the council asks you to double check"}},
		Actor:     event.Actor{Kind: event.ActorSystem, ID: "council"},
	}); err != nil {
		t.Fatal(err)
	}

	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := buildTurns(sid, evs); len(got) != 1 {
		t.Errorf("split into %d turns, want 1 — a system injection is part of the turn it landed in", len(got))
	}
}
