package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// What the caller gets back is what the child DID, not what it said it did.
//
// A child that ran the build and watched it fail and a child that never ran it can close with the
// same sentence, and a loop deciding whether to go another round cannot tell them apart from the
// text. The footprint is the calls themselves, in order, with the failing output verbatim.
func TestTheFootprintCarriesTheCallsAndTheFailureVerbatim(t *testing.T) {
	llm := &footprintLLM{}
	a, parent, _ := spawnApp(t, llm)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}

	spawn, steps, _, _, _ := a.spawnFnFor(0, parent, actor, "c1", "looper")
	res, err := spawn(context.Background(), port.SpawnSpec{Prompt: "build it"})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	got, err := steps(context.Background(), res.SessionID)
	if err != nil {
		t.Fatalf("child steps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the child's two tool calls, got %d: %+v", len(got), got)
	}

	// In the order the child ran them, with the arguments it actually sent.
	if got[0].Name != "read" || got[1].Name != "list" {
		t.Errorf("calls came back as %s,%s — want read,list", got[0].Name, got[1].Name)
	}
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(got[0].Args, &args) != nil || args.Path != missingPath {
		t.Errorf("the arguments did not survive: %s", got[0].Args)
	}

	// The failure is marked AND its output is what the tool produced, unaltered. The other decoder
	// in this package flattens newlines for a one-line council row; a stack trace put through that
	// is no longer the raw output, so this path must not share it.
	if !got[0].Failed {
		t.Error("the failing call was not marked failed")
	}
	raw := rawResultFor(t, a, res.SessionID, "r1")
	if got[0].Output != raw {
		t.Errorf("the failure output was altered:\n got %q\nwant %q", got[0].Output, raw)
	}
	if got[0].OutputBytes != len(raw) {
		t.Errorf("OutputBytes = %d, want %d", got[0].OutputBytes, len(raw))
	}
	if got[0].Output == "" {
		t.Fatal("the failing call carried no output at all — there is nothing for a loop to read")
	}

	// The succeeding call carries its size but not its bytes: that is the difference between a
	// footprint and the whole log, and successful output is the bulk of the log.
	if got[1].Failed {
		t.Error("the succeeding call was marked failed")
	}
	if got[1].Output != "" {
		t.Errorf("a succeeding call carried its output: %q", got[1].Output)
	}
	if got[1].OutputBytes <= 0 {
		t.Error("a succeeding call must still say how much output there was")
	}
}

// The reader answers for THIS call's children and nothing else. A plugin that could name any
// session id could read another agent's log, and spawning gives it no reason to.
func TestTheFootprintRefusesASessionThisCallDidNotSpawn(t *testing.T) {
	a, parent, _ := spawnApp(t, &footprintLLM{})
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}

	spawnA, stepsA, _, _, _ := a.spawnFnFor(0, parent, actor, "c1", "looper")
	resA, err := spawnA(context.Background(), port.SpawnSpec{Prompt: "one"})
	if err != nil {
		t.Fatal(err)
	}
	// A DIFFERENT tool call. Its reader must not answer for the first call's child, and the parent
	// session is not its child either.
	_, stepsB, _, _, _ := a.spawnFnFor(0, parent, actor, "c2", "looper")
	for _, sid := range []string{resA.SessionID, string(parent.ID)} {
		if _, err := stepsB(context.Background(), sid); err == nil {
			t.Errorf("a second tool call read %s, which it did not spawn", sid)
		}
	}
	// And the call that DID spawn it still can.
	if _, err := stepsA(context.Background(), resA.SessionID); err != nil {
		t.Errorf("the call that spawned the child cannot read it: %v", err)
	}
}

// A tool call cannot spawn forever. Each child is bounded, but nothing counted how many a plugin
// starts, and one tool call is one step to the parent loop however long it runs.
func TestOneToolCallCannotSpawnForever(t *testing.T) {
	old := spawnCallStepBudget
	spawnCallStepBudget = 3 // the shipped budget would need hundreds of child runs to reach
	t.Cleanup(func() { spawnCallStepBudget = old })

	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")

	var lastErr error
	spawned := 0
	for i := 0; i < 20; i++ { // a loop that never stops asking
		if _, err := spawn(context.Background(), port.SpawnSpec{Prompt: "again"}); err != nil {
			lastErr = err
			break
		}
		spawned++
	}
	if lastErr == nil {
		t.Fatalf("twenty spawns in one tool call were all allowed (budget %d)", spawnCallStepBudget)
	}
	if spawned > spawnCallStepBudget {
		t.Errorf("%d children ran on a %d-step budget", spawned, spawnCallStepBudget)
	}
	// The refusal says which bound and where it stands, so a plugin can tell a budget from a crash
	// and stop instead of asking again.
	for _, want := range []string{"budget", "3"} {
		if !strings.Contains(lastErr.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, lastErr)
		}
	}
	// A separate tool call starts fresh — the budget is per call, not per app.
	_ = spawned
	spawn2, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c2", "looper")
	if _, err := spawn2(context.Background(), port.SpawnSpec{Prompt: "new call"}); err != nil {
		t.Errorf("a new tool call inherited the previous call's exhausted budget: %v", err)
	}
}

// The budget charges STEPS, not spawns.
//
// Nothing populated SpawnResult.Steps when the budget was written, so the charge fell to its floor
// of one and the "step budget" was really a spawn count — a plugin running one long child could
// spend any amount of model work under a budget it never touched. Here one child does three round
// trips and exhausts a budget of three by itself.
func TestTheBudgetChargesStepsNotSpawns(t *testing.T) {
	old := spawnCallStepBudget
	spawnCallStepBudget = 3
	t.Cleanup(func() { spawnCallStepBudget = old })

	a, parent, _ := spawnApp(t, &footprintLLM{}) // read, list, then answer: three round trips
	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")

	res, err := spawn(context.Background(), port.SpawnSpec{Prompt: "build"})
	if err != nil {
		t.Fatalf("the first spawn was refused: %v", err)
	}
	if res.Steps < 3 {
		t.Errorf("the child ran three round trips but reported Steps=%d — a caller reading this "+
			"cannot see what the child spent", res.Steps)
	}
	// One child spent the whole budget, so the next must be refused. If the charge were per spawn
	// this would be allowed twice more.
	if _, err := spawn(context.Background(), port.SpawnSpec{Prompt: "again"}); err == nil {
		t.Error("a second child ran after the first had already spent the whole budget — " +
			"the budget is counting spawns, not the steps they take")
	}
}

// missingPath is a file that is not there, so the failing call fails for the same reason on every
// machine — a real `make` would depend on what is installed.
const missingPath = "/nonexistent-magi-footprint-probe/build.log"

// rawResultFor returns the tool result the child's log actually holds for a call, so the assertion
// compares against the recorded bytes rather than a string the test made up.
func rawResultFor(t *testing.T, a *App, sid, callID string) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), session.SessionID(sid), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.ToolResult == nil || d.Part.ToolResult.CallID != callID {
			continue
		}
		var str string
		if json.Unmarshal(d.Part.ToolResult.Content, &str) == nil {
			return str
		}
		return string(d.Part.ToolResult.Content)
	}
	t.Fatalf("no tool result for %s in the child log", callID)
	return ""
}

// footprintLLM makes the child run a failing read and then a succeeding list, then answer.
type footprintLLM struct{ n int }

func (f *footprintLLM) StreamChat(_ context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 4)
	switch f.n {
	case 0:
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "r1", Name: "read", Args: json.RawMessage(`{"path":"` + missingPath + `"}`)}}
	case 1:
		ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
			CallID: "l1", Name: "list", Args: json.RawMessage(`{"path":"."}`)}}
	default:
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "could not fix it"}
	}
	f.n++
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A child that stopped can be sent back, IN THE SAME SESSION.
//
// This is the relationship the main agent has with the council, one level down: the child ends by
// declaring, and the caller either accepts or says what is still undone. What makes it worth having
// as a loop here rather than a second spawn is the session — the child keeps everything it has
// already read, and re-gathering that is most of what a fresh child's steps would go to.
func TestAReviewSendsTheChildBackWithoutLosingWhatItRead(t *testing.T) {
	a, parent, _ := spawnApp(t, &footprintLLM{})
	spawn, steps, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")

	var rounds []string
	var reviewedSID string
	res, err := spawn(context.Background(), port.SpawnSpec{
		Prompt: "build it",
		Review: func(round int, text string, spent int, sid string) (string, error) {
			// The review is called BEFORE spawn returns, so a caller holding the result has
			// nothing yet — without this argument the only thing it can read is the child's own
			// closing sentence, which is the account a footprint exists to check.
			if sid == "" {
				t.Error("the review was not told which child it is judging")
			}
			reviewedSID = sid
			rounds = append(rounds, text)
			if round == 1 {
				return "not done: the build still fails", nil
			}
			return "", nil // accept the second ending
		},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("the review was asked %d times, want 2 (one refusal, one acceptance)", len(rounds))
	}

	if reviewedSID != res.SessionID {
		t.Errorf("the review judged %q but the spawn returned %q", reviewedSID, res.SessionID)
	}

	// ONE session across both rounds — that is what keeps the child's context.
	all, err := steps(context.Background(), res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Errorf("the second round started a fresh child: only %d tool calls in the session", len(all))
	}
	// And the refusal reached it verbatim, as a new instruction in that same session.
	if !promptContains(t, a, session.SessionID(res.SessionID), "the build still fails") {
		t.Error("what the review sent back is not in the child's log")
	}
}

// A review that never accepts does not run forever. The step budget is the child's TOTAL and is
// spent down across rounds, and the round count is capped besides.
func TestAReviewThatNeverAcceptsStillEnds(t *testing.T) {
	a, parent, _ := spawnApp(t, &footprintLLM{})
	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")

	asked := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = spawn(context.Background(), port.SpawnSpec{
			Prompt: "build it",
			Review: func(int, string, int, string) (string, error) { asked++; return "still not done", nil },
		})
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("a review that never accepts did not terminate")
	}
	if asked > spawnMaxRounds {
		t.Errorf("the review was asked %d times on a cap of %d", asked, spawnMaxRounds)
	}
	if asked == 0 {
		t.Error("the review was never asked at all")
	}
}

// A review that ERRORS is reported, not read as acceptance. "It did not answer" and "it said yes"
// are different facts, and a caller told the second when the first happened cannot tell.
func TestAFailingReviewIsReportedNotTakenAsYes(t *testing.T) {
	a, parent, _ := spawnApp(t, &footprintLLM{})
	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "looper")

	res, err := spawn(context.Background(), port.SpawnSpec{
		Prompt: "build it",
		Review: func(int, string, int, string) (string, error) { return "", errTestReview },
	})
	if err != nil {
		t.Fatalf("spawn itself failed: %v", err)
	}
	if res.Err == "" {
		t.Fatal("a review that errored was reported as a clean finish")
	}
	if !strings.Contains(res.Err, "review") {
		t.Errorf("the failure does not say the review was what broke: %q", res.Err)
	}
}

var errTestReview = errors.New("the reviewer blew up")
