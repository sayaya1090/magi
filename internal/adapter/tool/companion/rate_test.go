package companion_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/companion"
	"github.com/sayaya1090/magi/internal/port"
)

func rate(t *testing.T, wd string, args map[string]string) string {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res, err := companion.Rate{}.Execute(context.Background(), b, port.ToolEnv{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		return "ERROR: " + text(t, res)
	}
	return text(t, res)
}

// What an answer was worth is written down where the next choice can read it, and shown with the
// count it rests on.
//
// "Three of four useful" and "one of one useful" are different facts. A percentage hides which one
// is on the screen, and a companion asked once is not a companion with a record.
func TestWhatAnAnswerWasWorthIsRecordedWithItsSampleSize(t *testing.T) {
	wd := t.TempDir()
	if got := companion.Tally(wd); got != "" {
		t.Fatalf("a workspace that has rated nothing says %q", got)
	}
	// Chosen so name order and score order disagree: builder is the worse of the two and sorts
	// first. With the other way round, a list sorted by score would look identical and this test
	// would prove nothing.
	rate(t, wd, map[string]string{"who": "design", "verdict": "good", "why": "named the tokens"})
	rate(t, wd, map[string]string{"who": "design", "verdict": "missed", "why": "answered a different question"})
	rate(t, wd, map[string]string{"who": "design", "verdict": "good", "why": "the contrast table"})
	rate(t, wd, map[string]string{"who": "builder", "verdict": "missed", "why": "never ran it"})

	got := companion.Tally(wd)
	for _, want := range []string{"design — 2 of 3 useful", "builder — 0 of 1 useful"} {
		if !strings.Contains(got, want) {
			t.Errorf("the record does not say %q:\n%s", want, got)
		}
	}
	// Said, not implied: a name that is absent has not been judged badly.
	if !strings.Contains(got, "never been rated") {
		t.Errorf("nothing distinguishes an unrated companion from a bad one:\n%s", got)
	}
	// And it is not a ranking. design is the better of the two and builder is listed first,
	// because b comes before d.
	if strings.Index(got, "builder") > strings.Index(got, "design") {
		t.Errorf("the record is ordered by score, which reads as a recommendation:\n%s", got)
	}
}

// The file is append-only and readable, so it can be committed and read by a person.
func TestTheRecordIsAnAppendOnlyLineEach(t *testing.T) {
	wd := t.TempDir()
	rate(t, wd, map[string]string{"who": "design", "verdict": "good", "why": "first",
		"asked": "name the tokens"})
	first, err := os.ReadFile(filepath.Join(wd, ".magi", "handoffs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	rate(t, wd, map[string]string{"who": "design", "verdict": "missed", "why": "second"})
	after, err := os.ReadFile(filepath.Join(wd, ".magi", "handoffs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(after), string(first)) {
		t.Fatalf("the second verdict rewrote the first:\n%s", after)
	}
	if n := len(strings.Split(strings.TrimSpace(string(after)), "\n")); n != 2 {
		t.Errorf("two verdicts are %d lines", n)
	}
	if !strings.Contains(string(after), "name the tokens") {
		t.Errorf("the record does not say what the verdict was about:\n%s", after)
	}
}

// A verdict with no reason is refused, and a made-up verdict is refused.
//
// A column of names and adjectives is not a record: the one line is what makes it mean anything to
// whoever opens the file later, which includes this agent on another day.
func TestAVerdictNeedsToBeOneOfTwoAndToSayWhy(t *testing.T) {
	wd := t.TempDir()
	if got := rate(t, wd, map[string]string{"who": "design", "verdict": "good"}); !strings.HasPrefix(got, "ERROR") {
		t.Errorf("a verdict with no reason was accepted: %q", got)
	}
	if got := rate(t, wd, map[string]string{"who": "design", "verdict": "excellent", "why": "x"}); !strings.HasPrefix(got, "ERROR") {
		t.Errorf("an invented verdict was accepted: %q", got)
	}
	if got := rate(t, wd, map[string]string{"who": "", "verdict": "good", "why": "x"}); !strings.HasPrefix(got, "ERROR") {
		t.Errorf("a verdict about nobody was accepted: %q", got)
	}
	if companion.Tally(wd) != "" {
		t.Error("a refused verdict was written down anyway")
	}
}

// A corrupt line loses itself and nothing else.
//
// This is a file people are meant to open and edit. Refusing the whole thing over one bad line
// would turn a stray keystroke into a workspace with no record at all.
func TestOneUnreadableLineDoesNotLoseTheRest(t *testing.T) {
	wd := t.TempDir()
	rate(t, wd, map[string]string{"who": "design", "verdict": "good", "why": "first"})
	path := filepath.Join(wd, ".magi", "handoffs.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte("{ this is not json\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	rate(t, wd, map[string]string{"who": "builder", "verdict": "good", "why": "second"})
	got := companion.Tally(wd)
	if !strings.Contains(got, "design") || !strings.Contains(got, "builder") {
		t.Errorf("a bad line took the good ones with it:\n%s", got)
	}
}

// The record is shown only where there is something to choose between.
//
// It exists to inform a choice. With one companion there is no choice, and the paragraph is weight
// in every prompt of every step — the tool description is rebuilt each one, and what a model reads
// while deciding is a cost this tree has measured before.
func TestThePastIsShownOnlyWhenThereIsAChoice(t *testing.T) {
	wd := t.TempDir()
	rate(t, wd, map[string]string{"who": "design", "verdict": "good", "why": "landed"})
	record := func() string { return companion.Tally(wd) }

	alone := companion.Hand{Record: record, Roster: func() (string, int) {
		return "  design [core] — screens", 1
	}}.Description()
	if strings.Contains(alone, "useful") {
		t.Errorf("with one companion the record is carried for nothing:\n%s", alone)
	}
	if !strings.Contains(alone, "design") {
		t.Errorf("the one companion there is went missing:\n%s", alone)
	}

	several := companion.Hand{Record: record, Roster: func() (string, int) {
		return "  builder [core] — builds\n  design [core] — screens", 2
	}}.Description()
	if !strings.Contains(several, "design — 1 of 1 useful") {
		t.Errorf("with a choice to make the record is not in front of it:\n%s", several)
	}
}
