package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestExtractRunnableAnchor(t *testing.T) {
	t.Run("lifts the first fenced block", func(t *testing.T) {
		task := "Write a filter.\n\nUsage:\n```\npython solve.py < in.txt\n```\nDone."
		if got := extractRunnableAnchor(task); got != "python solve.py < in.txt" {
			t.Fatalf("want the fenced command, got %q", got)
		}
	})
	t.Run("no fence yields empty", func(t *testing.T) {
		if got := extractRunnableAnchor("just prose, no code block at all"); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})
	t.Run("skips an empty fence", func(t *testing.T) {
		// A bare ``` ``` pair carries nothing runnable — must not return "".
		if got := extractRunnableAnchor("x\n```\n```\ny"); got != "" {
			t.Fatalf("empty fence must yield empty, got %q", got)
		}
	})
	t.Run("truncates an oversized block", func(t *testing.T) {
		big := strings.Repeat("x", anchorMaxBytes+500)
		got := extractRunnableAnchor("```\n" + big + "\n```")
		if len(got) != anchorMaxBytes {
			t.Fatalf("want %d bytes, got %d", anchorMaxBytes, len(got))
		}
	})
}

// bashCallEvent synthesizes the PartAppended event the execute path writes for one bash tool
// call, so sessionExercisedCheck can be tested without a live subagent.
func bashCallEvent(cmd string) event.Event {
	args, _ := json.Marshal(map[string]string{"command": cmd})
	d, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{
			Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{
				CallID: "c_" + cmd, Name: "bash", Args: args,
			},
		},
	})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func TestSessionExercisedCheck(t *testing.T) {
	cases := []struct {
		name string
		cmds []string
		want bool
	}{
		{"runs a program", []string{"cat out.txt", "python out.txt"}, true},
		{"path-qualified run", []string{"./run.sh"}, true},
		{"only inspects", []string{"cat out.txt", "ls -la", "test -f out.txt"}, false},
		{"no bash at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var evs []event.Event
			for _, c := range tc.cmds {
				evs = append(evs, bashCallEvent(c))
			}
			if got := sessionExercisedCheck(evs); got != tc.want {
				t.Fatalf("sessionExercisedCheck(%v) = %v, want %v", tc.cmds, got, tc.want)
			}
		})
	}
}

// deliverableRunnable is the structural basis of the UNRUNNABLE verdict (D4): a change set made
// only of data/config/doc files has nothing to independently run, while any code/script/unknown/
// no-extension file makes it runnable. The bias is toward runnable, so a real program is never
// mislabeled "nothing to run".
func TestDeliverableRunnable(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  bool
	}{
		{"no change is runnable (safe default)", nil, true},
		{"only docs/data is not runnable", []string{"README.md", "config.yaml", "data.csv"}, false},
		{"a code file makes it runnable", []string{"README.md", "solve.py"}, true},
		{"no extension is runnable (script/binary)", []string{"Makefile"}, true},
		{"unknown extension is runnable", []string{"thing.xyz"}, true},
		{"a lone go file is runnable", []string{"main.go"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newRunGuard()
			for _, p := range tc.paths {
				g.recordChange(p, "", "x")
			}
			if got := g.deliverableRunnable(); got != tc.want {
				t.Fatalf("deliverableRunnable(%v) = %v, want %v", tc.paths, got, tc.want)
			}
		})
	}
}

func TestVerdictTierEnabledFlag(t *testing.T) {
	if !verdictTierEnabled() {
		t.Fatal("verdict tier is ON by default")
	}
	t.Setenv("MAGI_VERDICT_TIER", "off")
	if verdictTierEnabled() {
		t.Fatal("MAGI_VERDICT_TIER=off must disable the tier")
	}
}

// verdictTierLLM: the tester reports PASS but runs NO command — a vacuous pass. The main agent
// writes one deliverable then keeps finishing. With the tier on, that vacuous PASS must be
// demoted so the deliverable never lands as verified; with it off, the bare PASS opens the gate.
type verdictTierLLM struct {
	mu sync.Mutex
}

func (f *verdictTierLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	prompt := promptText(r)
	var evs []port.ProviderEvent
	switch {
	case strings.Contains(prompt, "Independently VERIFY"): // tester: claims PASS, runs nothing
		evs = []port.ProviderEvent{
			{Type: port.ProviderText, Text: "the file is present and looks right.\nVERDICT: " + verdictPass},
			{Type: port.ProviderFinish},
		}
	case strings.Contains(prompt, "Independently REVIEW"):
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "looks fine."}, {Type: port.ProviderFinish}}
	default: // main agent
		if countWriteCalls(r) == 0 {
			evs = []port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_w", Name: "write", Args: []byte(`{"path":"out.txt","content":"v1"}`)}},
				{Type: port.ProviderFinish},
			}
		} else {
			evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done."}, {Type: port.ProviderFinish}}
		}
	}
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// unrunnableLLM writes ONE non-executable deliverable (a .md doc) and then always says "done";
// the tester can only ever return BLOCKED (there is nothing it can run). This is the genuine
// UNRUNNABLE shape: no independent PASS is even possible, so the honest outcome is "delivered but
// unverified by execution — nothing to run", reached without nagging the agent to run a doc.
type unrunnableLLM struct {
	mu sync.Mutex
}

func (f *unrunnableLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	prompt := promptText(r)
	var evs []port.ProviderEvent
	switch {
	case strings.Contains(prompt, "Independently VERIFY"): // tester: nothing runnable to confirm
		evs = []port.ProviderEvent{
			{Type: port.ProviderText, Text: "there is nothing here I can run.\nVERDICT: " + verdictBlocked},
			{Type: port.ProviderFinish},
		}
	case strings.Contains(prompt, "Independently REVIEW"):
		evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "the doc reads fine."}, {Type: port.ProviderFinish}}
	default: // main agent: write a doc once, then insist done
		if countWriteCalls(r) == 0 {
			evs = []port.ProviderEvent{
				{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_w", Name: "write", Args: []byte(`{"path":"NOTES.md","content":"# Notes\nsome text"}`)}},
				{Type: port.ProviderFinish},
			}
		} else {
			evs = []port.ProviderEvent{{Type: port.ProviderText, Text: "done."}, {Type: port.ProviderFinish}}
		}
	}
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// turnFinishedReason returns the Reason string off the single TurnFinished event.
func turnFinishedReason(t *testing.T, evs []event.Event) string {
	t.Helper()
	for _, e := range evs {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				t.Fatalf("unmarshal TurnFinishedData: %v", err)
			}
			return d.Reason
		}
	}
	t.Fatalf("no TurnFinished event; types %v", typesOf(evs))
	return ""
}

// D4: a change set of only non-executable files (a .md doc) has nothing to independently run, so
// the turn lands honestly UNVERIFIED with a "nothing to run" reason — and WITHOUT the "actually
// run it" evidence push, which is meaningless for a doc. The pass is never forced; the file stays
// on disk. This is the 4th tier value (UNRUNNABLE) of the same structural verdict as D2.
func TestUnrunnableLandsHonestReasonWithoutPush(t *testing.T) {
	a, wd := newApp(t, &unrunnableLLM{}, verdictTierConfig())
	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected a TurnFinished; types %v", typesOf(evs))
	}
	if !unv {
		t.Error("a non-executable deliverable never independently run must land UNVERIFIED")
	}
	if r := turnFinishedReason(t, evs); !strings.Contains(r, "non-executable") {
		t.Errorf("want an UNRUNNABLE 'non-executable' reason, got %q", r)
	}
	if n := countLoopPushes(evs); n != 0 {
		t.Errorf("nothing is runnable, so the gate must not push 'run it'; got %d loop pushes", n)
	}
}

// The same run with the tier OFF: D4 is disabled, so the deliverable takes the generic
// execution-evidence path — one "actually run it" push, then a generic unverified reason (no
// UNRUNNABLE label). Pins that the flag is what selects the honest no-push path above.
func TestUnrunnableDisabledTakesGenericEvidencePath(t *testing.T) {
	t.Setenv("MAGI_VERDICT_TIER", "0")
	a, wd := newApp(t, &unrunnableLLM{}, verdictTierConfig())
	evs := submitAndDrain(t, a, wd)

	if unv, ok := turnFinishedUnverified(t, evs); !ok || !unv {
		t.Fatalf("still expected an UNVERIFIED TurnFinished; unv=%v ok=%v", unv, ok)
	}
	if r := turnFinishedReason(t, evs); strings.Contains(r, "non-executable") {
		t.Errorf("with the tier off there is no UNRUNNABLE label; got %q", r)
	}
	if n := countLoopPushes(evs); n != 1 {
		t.Errorf("the disabled path should push 'run it' exactly once; got %d", n)
	}
}

// No council here (Council nil) so the verdict tier is the ONLY thing that can keep the gate
// closed — isolating D2 from the council/D1 paths.
func verdictTierConfig() Config {
	return Config{
		Permission: "allow",
		ReviewGate: true,
		Agents: map[string]AgentSpec{
			"tester":   {Name: "tester", Tools: []string{"read", "bash"}},
			"reviewer": {Name: "reviewer", Tools: []string{"read"}},
		},
	}
}

// A tester PASS backed by no real run is vacuous: with the tier ON it is demoted to INCONCLUSIVE,
// the fresh-evidence gate never opens, and the turn lands honestly UNVERIFIED. It is never forced
// to pass — the deliverable stays on disk, just labeled unverified.
func TestVerdictTierDemotesVacuousPass(t *testing.T) {
	a, wd := newApp(t, &verdictTierLLM{}, verdictTierConfig())
	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected a TurnFinished; types %v", typesOf(evs))
	}
	if !unv {
		t.Error("a PASS the tester never actually ran must not land as verified")
	}
}

// The same run with the tier OFF: the bare PASS is taken at face value, opening the gate and
// landing the turn as verified. This pins that the demotion (not some other gate) is what changes
// the outcome above.
func TestVerdictTierDisabledTrustsBarePass(t *testing.T) {
	t.Setenv("MAGI_VERDICT_TIER", "0")
	a, wd := newApp(t, &verdictTierLLM{}, verdictTierConfig())
	evs := submitAndDrain(t, a, wd)

	unv, ok := turnFinishedUnverified(t, evs)
	if !ok {
		t.Fatalf("expected a TurnFinished; types %v", typesOf(evs))
	}
	if unv {
		t.Error("with the tier disabled the tester's bare PASS should verify the turn")
	}
}
