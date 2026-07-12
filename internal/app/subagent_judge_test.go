package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// judgedLLM plays both roles: child calls (no supervisor system prompt) hang
// until cancelled; judge calls (the supervisor system prompt) answer from a
// scripted verdict list, so a test can drive EXTEND→KILL sequences.
type judgedLLM struct {
	mu         sync.Mutex
	verdicts   []string // consumed per judge call; empty → "KILL"
	childCalls int
	judgeCalls int
}

func (f *judgedLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	isJudge := strings.Contains(r.System, "execution supervisor")
	var verdict string
	if isJudge {
		f.judgeCalls++
		verdict = "KILL"
		if len(f.verdicts) > 0 {
			verdict, f.verdicts = f.verdicts[0], f.verdicts[1:]
		}
	} else {
		f.childCalls++
	}
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		if isJudge {
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: verdict}
			ch <- port.ProviderEvent{Type: port.ProviderFinish}
			return
		}
		<-ctx.Done() // child hangs (event-quiet; stall watchdog disabled in tests)
	}()
	return ch, nil
}

func (f *judgedLLM) counts() (child, judge int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.childCalls, f.judgeCalls
}

func newJudgeApp(t *testing.T, llm port.LLMProvider, base time.Duration) (*App, session.Session) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{
		Permission:      "allow",
		Agents:          map[string]AgentSpec{"worker": {Name: "worker"}},
		SubagentStall:   time.Hour, // isolate the lease path from the stall watchdog
		SubagentTimeout: base,
	})
	// One attempt only: lease decisions, not restarts, are under test. Set after
	// New — withDefaults treats a zero MaxRestarts as unset and raises it to 2.
	a.cfg.SubagentMaxRestarts = 0
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default",
		Model: session.ModelRef{Provider: "openai", Model: "m"}}
	return a, parent
}

// A KILL verdict at lease expiry ends the attempt like the old hard cap: the
// judge deliberated once and the child was cancelled right after.
func TestLeaseKillVerdictEndsAttempt(t *testing.T) {
	llm := &judgedLLM{verdicts: []string{"KILL"}}
	a, parent := newJudgeApp(t, llm, 300*time.Millisecond)

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err == "" || !strings.Contains(res.Err, "lease expired") {
		t.Fatalf("kill verdict must end the attempt with a lease error, got %q", res.Err)
	}
	if _, judges := llm.counts(); judges != 1 {
		t.Fatalf("exactly one judgment expected, got %d", judges)
	}
}

// An EXTEND verdict grants base/2 more; the next expiry judges again and a KILL
// then ends it. Total runtime must show the extension actually happened.
func TestLeaseExtendThenKill(t *testing.T) {
	llm := &judgedLLM{verdicts: []string{"EXTEND", "KILL"}}
	a, parent := newJudgeApp(t, llm, 400*time.Millisecond)

	start := time.Now()
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	elapsed := time.Since(start)
	if res.Err == "" || !strings.Contains(res.Err, "lease expired") {
		t.Fatalf("want lease-expired end after the extension, got %q", res.Err)
	}
	if _, judges := llm.counts(); judges != 2 {
		t.Fatalf("want 2 judgments (extend, then kill), got %d", judges)
	}
	// initial 400ms + extension 200ms: anything comfortably past the initial
	// lease proves the extension was honored (CI-tolerant lower bound).
	if elapsed < 550*time.Millisecond {
		t.Fatalf("attempt ended at %v — the EXTEND verdict was not honored", elapsed)
	}
}

// The absolute backstop caps everything: perpetual EXTEND verdicts cannot push
// an attempt past base×subagentCapMaxFactor, and once the backstop is spent the
// expiry kills WITHOUT consulting the judge again.
func TestLeaseBackstopOverridesExtend(t *testing.T) {
	llm := &judgedLLM{verdicts: []string{"EXTEND", "EXTEND", "EXTEND", "EXTEND", "EXTEND", "EXTEND", "EXTEND", "EXTEND"}}
	base := 200 * time.Millisecond
	a, parent := newJudgeApp(t, llm, base)

	start := time.Now()
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	elapsed := time.Since(start)
	if res.Err == "" {
		t.Fatal("backstop must end the attempt")
	}
	// backstop = 3×base = 600ms; generous ceiling for CI scheduling noise.
	if elapsed > 3*time.Second {
		t.Fatalf("attempt ran %v — the backstop did not bound perpetual EXTENDs", elapsed)
	}
}

// With judging disabled the lease expiry kills immediately — the pre-lease
// elastic-cap behavior, with no LLM judgment call at all.
func TestLeaseJudgeDisabledKillsAtCap(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_JUDGE", "off")
	llm := &judgedLLM{verdicts: []string{"EXTEND"}} // would extend if consulted
	a, parent := newJudgeApp(t, llm, 300*time.Millisecond)

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err == "" || !strings.Contains(res.Err, "lease expired") {
		t.Fatalf("disabled judge must kill at the cap, got %q", res.Err)
	}
	if _, judges := llm.counts(); judges != 0 {
		t.Fatalf("disabled judge must not be consulted, got %d calls", judges)
	}
}

// slowJudgeLLM: the child finishes ON ITS OWN shortly after the lease expires,
// while the judge is still deliberating. The completed outcome must win over
// the KILL verdict — a finished attempt is never discarded.
type slowJudgeLLM struct{ mu sync.Mutex }

func (f *slowJudgeLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	isJudge := strings.Contains(r.System, "execution supervisor")
	ch := make(chan port.ProviderEvent)
	go func() {
		defer close(ch)
		if isJudge {
			select { // deliberate slowly, then vote KILL
			case <-time.After(600 * time.Millisecond):
			case <-ctx.Done():
				return
			}
			ch <- port.ProviderEvent{Type: port.ProviderText, Text: "KILL"}
			ch <- port.ProviderEvent{Type: port.ProviderFinish}
			return
		}
		select { // child: finish legitimately just after the lease expires
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return
		}
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "recovered late but done"}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
	}()
	return ch, nil
}

func TestLeaseChildFinishDuringJudgeWins(t *testing.T) {
	a, parent := newJudgeApp(t, &slowJudgeLLM{}, 300*time.Millisecond)

	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "go"})
	if res.Err != "" {
		t.Fatalf("a child that finished during the judge window must not be killed: %s", res.Err)
	}
	if !strings.Contains(res.Text, "recovered late but done") {
		t.Fatalf("the completed outcome must be returned, got %q", res.Text)
	}
}

// childToolDigest: renders name+args oldest-first, collapses consecutive
// duplicates with a repeat count, keeps only the newest k, and returns "" for a
// call-free transcript.
func TestChildToolDigest(t *testing.T) {
	mk := func(name, args string) event.Event {
		d := event.PartAppendedData{Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: name, Args: []byte(args)},
		}}
		b, _ := json.Marshal(d)
		return event.Event{Type: event.TypePartAppended, Data: b}
	}
	evs := []event.Event{
		mk("bash", `{"command":"ls"}`),
		mk("bash", `{"command":"echo done"}`),
		mk("bash", `{"command":"echo done"}`),
		mk("bash", `{"command":"echo done"}`),
		mk("read", `{"path":"a.txt"}`),
	}
	got := childToolDigest(evs, 12)
	if !strings.Contains(got, "repeated 2 more times") {
		t.Errorf("consecutive duplicates must be collapsed with a count:\n%s", got)
	}
	if !strings.Contains(got, `- bash {"command":"ls"}`) || !strings.Contains(got, `- read {"path":"a.txt"}`) {
		t.Errorf("digest must keep distinct calls oldest-first:\n%s", got)
	}
	if childToolDigest(nil, 12) != "" {
		t.Error("no tool calls must yield an empty digest")
	}
	// k-window: only the newest k calls are shown.
	if d := childToolDigest(evs, 1); strings.Contains(d, "ls") || !strings.Contains(d, "a.txt") {
		t.Errorf("k=1 must keep only the newest call:\n%s", d)
	}
}
