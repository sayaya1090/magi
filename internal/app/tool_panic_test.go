package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// panicTool is a tool whose execution panics, standing in for a defect anywhere under a call.
type panicTool struct{}

func (panicTool) Name() string            { return "boom" }
func (panicTool) Description() string     { return "panics" }
func (panicTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (panicTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	panic("index out of range [62289] with length 62289")
}

// A panic under one tool call used to end the whole run: an off-by-one in the truncation path
// exited magi with code 2 eight calls into a three-hour task, and the task scored 0 with nothing
// attempted. The crash must cost that CALL, not the hours that would have followed it.
//
// Two things it must not do, and both are asserted here: it must not leave the call unanswered —
// the loop waits on a result, and a call with none is the same silent nothing an oversized result
// used to return — and it must not soften the crash into "the tool failed", because a recovered
// panic is still a magi defect somebody has to fix.
func TestAPanicUnderOneToolCallDoesNotEndTheRun(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	a.tools.Register(panicTool{})
	s := a.sessionInfo(context.Background(), sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}

	tc := &session.ToolCall{CallID: "c1", Name: "boom", Args: json.RawMessage(`{}`)}
	// Does not panic out: the deferred recover turns it into this call's failure.
	a.executeTool(context.Background(), s, AgentSpec{Name: "coder"}, 0, actor, tc, newRunGuard(), "")

	// The call is answered — an unanswered call is the failure mode this must not reintroduce.
	txt := allEventText(t, a, sid)
	res := toolResultFor(t, a, sid, "c1")
	if res == nil {
		t.Fatal("the loop waits on a result; a crashed call must still get one")
	}
	if !res.IsError {
		t.Error("a call that crashed did not succeed")
	}
	var body string
	if err := json.Unmarshal(res.Content, &body); err != nil {
		t.Fatalf("result content must be a JSON string: %v", err)
	}
	// The panic value reaches the agent verbatim, not as a generic failure.
	if !strings.Contains(body, "index out of range [62289]") {
		t.Errorf("the agent must be handed what actually happened:\n%s", body)
	}
	if !strings.Contains(body, "defect in magi") {
		t.Errorf("say whose fault it is, so the agent does not hunt its own request:\n%s", body)
	}
	if !strings.Contains(body, "nothing here says whether its work happened") {
		t.Errorf("a half-run call proves nothing either way; say so:\n%s", body)
	}
	// And the operator sees it: the panic and its top frames go to the run's error stream. Matched
	// without the quoted tool name: the event payload is JSON, so the quotes around it are escaped
	// there and a literal match on them never fires.
	if !strings.Contains(txt, "magi panicked while running tool") || !strings.Contains(txt, "boom") {
		t.Errorf("the crash must be recorded, not only handed to the model:\n%s", txt)
	}
	if !strings.Contains(txt, "panicTool") {
		t.Errorf("the frames name where it came from:\n%s", txt)
	}

	// A tool that works is untouched by any of this.
	ok := &session.ToolCall{CallID: "c2", Name: "read", Args: json.RawMessage(`{"path":"nope.txt"}`)}
	a.executeTool(context.Background(), s, AgentSpec{Name: "coder"}, 0, actor, ok, newRunGuard(), "")
	if r := toolResultFor(t, a, sid, "c2"); r == nil {
		t.Error("an ordinary call still gets its result")
	}
}

// firstFrames keeps the top frames and says when it cut.
func TestFirstFramesCuts(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("goroutine 1 [running]:\n")
	for i := 0; i < 40; i++ {
		sb.WriteString("pkg.Func\n\t/path/file.go:1 +0x1\n")
	}
	got := firstFrames(sb.String(), 3)
	if n := strings.Count(got, "pkg.Func"); n != 3 {
		t.Errorf("want 3 frames, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "stack truncated") {
		t.Errorf("say that it was cut:\n%s", got)
	}
	// A short stack is passed through whole, with no cut marker.
	short := "goroutine 1 [running]:\npkg.Func\n\t/path/file.go:1 +0x1\n"
	if out := firstFrames(short, 8); out != short {
		t.Errorf("a stack under the limit is unchanged:\n%q", out)
	}
}

// toolResultFor finds the recorded result for one call id, or nil when the call was never answered.
func toolResultFor(t *testing.T, a *App, sid session.SessionID, callID string) *session.ToolResult {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil &&
			d.Part.ToolResult.CallID == callID {
			return d.Part.ToolResult
		}
	}
	return nil
}

// allEventText is every event's raw payload — what the operator can see, error stream included.
func allEventText(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range evs {
		b.WriteString(string(e.Type))
		b.WriteByte(' ')
		b.Write(e.Data)
		b.WriteByte('\n')
	}
	return b.String()
}
