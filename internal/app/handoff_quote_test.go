package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// The paraphrase defect this tree measured was on the way OUT — a brief rewritten until the graded
// identifier was gone. The same loss is available on the way back: the asker folds the answer into
// its own report, and what a model drops while summarising is precisely what it could not tell was
// load-bearing, because it never saw the workspace the answer came from.
//
// So the note the answer arrives in has to carry the rule, and a digest that makes a later quote
// checkable by someone who does not have the original.
func TestAHandedAnswerArrivesWithItsQuotingRuleAndDigest(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}

	const answer = "token --surface-dim is #8a8a8a, contrast 4.7:1 against surface, measured with axe 4.9"
	e := port.Elsewhere{
		Who: "design", Session: "s_42",
		Request:  "what is the dim surface token",
		AnswerAs: "- token name:\n- hex value:",
	}
	if err := a.deliverHandoff(ctx, sid, actor, e, answer); err != nil {
		t.Fatalf("deliverHandoff: %v", err)
	}

	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	note := ""
	for _, ev := range evs {
		if ev.Type == event.TypePromptSubmitted && strings.Contains(string(ev.Data), "design answered") {
			note = string(ev.Data)
		}
	}
	if note == "" {
		t.Fatal("the answer was never delivered into the asker's log")
	}

	// The answer itself, whole.
	if !strings.Contains(note, "#8a8a8a") || !strings.Contains(note, "axe 4.9") {
		t.Error("the answer must arrive intact — those are exactly the details a summary drops")
	}
	// The rule, in words the model reads before it folds anything in.
	for _, want := range []string{"QUOTE IT AS IT STANDS", "never reword", "digest"} {
		if !strings.Contains(strings.ToUpper(note), strings.ToUpper(want)) {
			t.Errorf("the note must carry the quoting rule; %q is missing", want)
		}
	}
	// The digest, so a later quote can be checked by somebody without the original.
	if d := shortDigest(answer); !strings.Contains(note, d) {
		t.Errorf("the note must carry the answer's digest %s", d)
	}
}

// A digest that changed with every delivery would make the rule uncheckable, and one that ignored
// the answer would make it meaningless. It is a receipt, not a seal: it catches a rewrite, and
// anything that can rewrite the answer can rewrite the line beside it.
func TestTheDigestFollowsTheAnswer(t *testing.T) {
	const a1 = "contrast 4.7:1"
	if shortDigest(a1) != shortDigest(a1) {
		t.Fatal("the same answer must digest the same, or a quote can never be checked")
	}
	if shortDigest(a1) == shortDigest("contrast 4.8:1") {
		t.Fatal("a changed digit must change the digest — that is the whole job")
	}
	if len(shortDigest(a1)) != 12 {
		t.Fatalf("short and fixed-width, so it reads as a receipt: got %q", shortDigest(a1))
	}
}
