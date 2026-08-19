package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A compaction folds the AGENT's memory. It must not fold the screen.
//
// The two are different things and the log holds both: compaction never deletes an event, it only
// changes which ones go into the next request. Reading the display off reconstruct handed every
// reader the model's amnesia — reported from a live console, where a fold landed mid-read and the
// scrollback being followed was replaced by a summary of itself, on a machine whose log still had
// every word of it.
func TestTheDisplayKeepsWhatTheAgentForgot(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	say := func(id, text string) int64 {
		seqs, err := a.store.Append(ctx, sid, ctxEvent(t, event.TypePromptSubmitted, event.PromptSubmittedData{
			MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: text}}}))
		if err != nil {
			t.Fatal(err)
		}
		return seqs[len(seqs)-1]
	}
	say("m1", "the FIRST thing I asked")
	boundary := say("m2", "the SECOND thing I asked")
	// The boundary is a REAL sequence number from the log. Left at zero it covers nothing, and the
	// half of this test that checks the model's context still folds would pass without the fold.
	if _, err := a.store.Append(ctx, sid, ctxEvent(t, event.TypeCompaction, event.CompactionData{
		Summary: "we looked at the parser", ReplacesUpToSeq: boundary,
		TokensBefore: 40000, TokensAfter: 9000,
		Shards: []event.ContextShard{{Topic: "internal/parse.go"}}})); err != nil {
		t.Fatal(err)
	}
	say("m3", "the THIRD thing I asked")

	msgs, _, err := a.SessionState(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, m := range msgs {
		for _, p := range m.Parts {
			all.WriteString(p.Text)
			all.WriteString("\n")
		}
	}
	shown := all.String()
	for _, want := range []string{"the FIRST thing I asked", "the SECOND thing I asked", "the THIRD thing I asked"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the display lost %q to a compaction — the log still holds it and the reader was "+
				"in the middle of reading it", want)
		}
	}
	if !strings.Contains(shown, "folded here") {
		t.Error("nothing marks where the fold happened; it is the moment the agent stopped being " +
			"able to remember what is still on the screen above it")
	}

	// And the AGENT's context is unchanged: it still folds, which is the whole point of compacting.
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var forModel strings.Builder
	for _, m := range reconstruct(evs) {
		for _, p := range m.Parts {
			forModel.WriteString(p.Text)
			forModel.WriteString("\n")
		}
	}
	if strings.Contains(forModel.String(), "the FIRST thing I asked") {
		t.Error("the model's context did not fold; compaction exists to shrink what is sent, and a " +
			"display fix that stopped it doing that would have traded the bug for a worse one")
	}
	if !strings.Contains(forModel.String(), "the THIRD thing I asked") {
		t.Error("the model lost what came AFTER the boundary")
	}
}
