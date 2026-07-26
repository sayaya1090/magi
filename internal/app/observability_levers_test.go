package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// clipCheckOutput keeps the head of a check's output and bounds it: the verdict-deciding line is at
// the start for the short outputs a check should print, and the fact is written on every gate cycle.
func TestClipCheckOutput(t *testing.T) {
	if got := clipCheckOutput("  1.2.3\n\n"); got != "1.2.3" {
		t.Errorf("surrounding whitespace must go: %q", got)
	}
	if got := clipCheckOutput(""); got != "" {
		t.Errorf("empty stays empty, got %q", got)
	}
	long := strings.Repeat("x", stepCheckRecordCap*2)
	got := clipCheckOutput(long)
	if len(got) > stepCheckRecordCap+40 { // clipLine may add an elision marker
		t.Errorf("output not bounded: %d chars", len(got))
	}
	if !strings.HasPrefix(got, "xxx") {
		t.Errorf("the HEAD must be kept, got %q", got[:20])
	}
}

// A recorded check must carry the output it was judged on and the pattern it was judged against.
// Step + command + verdict alone cannot distinguish "the world was wrong" from "the check could
// never match what the command prints", which is exactly the call a failure analysis has to make.
func TestStepCheckFactCarriesOutputAndExpect(t *testing.T) {
	a, sid := newPlannerApp(t, Config{})
	c := council.DeliverableCheck{Step: "1", Deliverable: "the tool reports its version",
		Command: "printversion", Expect: `^9\.8\.7$`}
	a.emitStepCheck(context.Background(), sid, c, 0, false, "9.8.6\n")

	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var d event.StepCheckData
	found := false
	for _, e := range evs {
		if e.Type == event.TypeStepCheck {
			if json.Unmarshal(e.Data, &d) == nil {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no step.check fact recorded")
	}
	if d.Output != "9.8.6" {
		t.Errorf("Output = %q, want the trimmed captured output", d.Output)
	}
	if d.Expect != `^9\.8\.7$` {
		t.Errorf("Expect = %q, want the pattern it was matched against", d.Expect)
	}
	if d.Pass {
		t.Error("verdict must be preserved")
	}
}

// An EMPTY execution note must say which pass came up empty. Mining is best-effort, but the note is
// the run's only record of the literals a grader checks verbatim, so silence made "nothing to mine"
// and "the distill never parsed" identical in the log — on the one task where record formats
// mattered most, the note was simply absent with no trace of why.
func TestSpecMineReportsWhyTheNoteIsEmpty(t *testing.T) {
	cases := []struct {
		name, reply, want string
	}{
		{"analysis empty", "", "analysis pass returned nothing"},
		{"nothing to mine", "NONE", "found nothing to mine"},
		{"distill unparseable", "a real analysis with content", "distill pass did not parse"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newOrchApp(t, &gateLLM{text: c.reply}, Config{Permission: "allow", MaxAgents: 10})
			s := parentSession(t.TempDir())
			sub := watchProgress(t, a, s.ID)
			if note := a.elicitSpecMine(context.Background(), AgentSpec{Name: "planner"}, s, "do the thing"); note != "" {
				t.Fatalf("this reply must not produce a note, got %q", note)
			}
			if got := sub.notes("spec-mine"); !strings.Contains(got, "no execution note") || !strings.Contains(got, c.want) {
				t.Errorf("want a reason naming %q, got:\n%s", c.want, got)
			}
		})
	}
}

// A tool name that differs from a REGISTERED one only in separators/case must be named back, and
// the reply must never deny a tool the same reply lists. It used to append "there is no todo/plan
// tool" unconditionally while todowrite was registered and listed — the model was told, in one
// message, both that the tool exists and that it does not.
func TestNearestToolName(t *testing.T) {
	names := []string{"bash", "todowrite", "bash_output", "report"}
	cases := []struct{ called, want string }{
		{"todo_write", "todowrite"},   // the observed miss
		{"TodoWrite", "todowrite"},    // case only
		{"todo-write", "todowrite"},   // another separator
		{"bashOutput", "bash_output"}, // camel vs snake
		{"bash", ""},                  // exact hit is not a suggestion
		{"run", ""},                   // NOT fuzzy: never guess a tool the model didn't ask for
		{"finish", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := nearestToolName(c.called, names); got != c.want {
			t.Errorf("nearestToolName(%q) = %q, want %q", c.called, got, c.want)
		}
	}
}
