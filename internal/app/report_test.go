package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// reportLLM makes the subagent call the report tool with a fixed status, then
// (if the loop ever asked again, which it must NOT) would emit more tool calls.
type reportLLM struct{ status string }

func (f reportLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 4)
	// If we ever see a tool result, the loop didn't terminate on report — emit a
	// loud marker so the test can catch it.
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolResult {
				ch <- port.ProviderEvent{Type: port.ProviderText, Text: "LOOPED_AFTER_REPORT"}
				ch <- port.ProviderEvent{Type: port.ProviderFinish}
				close(ch)
				return ch, nil
			}
		}
	}
	ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
		CallID: "c_report", Name: "report",
		Args: []byte(`{"summary":"all good","status":"` + f.status + `","details":"line one"}`),
	}}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func newReportApp(t *testing.T, status string) (*App, session.Session) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Report{})
	a := New(store, reportLLM{status: status}, reg, bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"worker": {Name: "worker", Tools: []string{"read", "report"}}},
	})
	return a, session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}
}

// A subagent that calls report terminates with the report as its result, leading
// with the STATUS line — and does NOT loop afterwards.
func TestSubagentReportTerminates(t *testing.T) {
	a, parent := newReportApp(t, "done")
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "do it"})
	if res.Err != "" {
		t.Fatalf("unexpected err: %s", res.Err)
	}
	if strings.Contains(res.Text, "LOOPED_AFTER_REPORT") {
		t.Fatal("loop continued after report (report did not terminate the turn)")
	}
	if !strings.Contains(res.Text, "STATUS: DONE") || !strings.Contains(res.Text, "all good") {
		t.Errorf("expected report text with STATUS + summary, got %q", res.Text)
	}
	if !strings.Contains(res.Text, "line one") {
		t.Errorf("expected details included, got %q", res.Text)
	}
}

// textThenReportLLM writes the answer as a normal (streamed) message, then calls
// report with NO summary — the streamed text becomes the result.
type textThenReportLLM struct{}

func (textThenReportLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 4)
	for _, m := range r.Messages {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolResult {
				ch <- port.ProviderEvent{Type: port.ProviderText, Text: "LOOPED_AFTER_REPORT"}
				ch <- port.ProviderEvent{Type: port.ProviderFinish}
				close(ch)
				return ch, nil
			}
		}
	}
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "my streamed review"}
	ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
		CallID: "c_report", Name: "report", Args: []byte(`{"status":"done"}`),
	}}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// When the answer was written as a streamed message, report (no summary) returns
// that text as the result — output appears live, not buried in tool args.
func TestSubagentReportUsesStreamedText(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	reg := builtin.Default()
	reg.Register(builtin.Report{})
	a := New(store, textThenReportLLM{}, reg, bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"worker": {Name: "worker", Tools: []string{"read", "report"}}},
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "review"})
	if strings.Contains(res.Text, "LOOPED_AFTER_REPORT") {
		t.Fatal("loop continued after report")
	}
	if !strings.Contains(res.Text, "STATUS: DONE") || !strings.Contains(res.Text, "my streamed review") {
		t.Errorf("expected streamed text used as result, got %q", res.Text)
	}
}

// fragmentThenReportLLM finishes the first turn with a stray mid-thought (no
// report), then — once nudged — files a proper report. Models that explore a lot
// and trail off otherwise strand the orchestrator with a fragment.
type fragmentThenReportLLM struct {
	mu sync.Mutex
	n  int
}

func (f *fragmentThenReportLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := f.n
	f.n++
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 4)
	if n == 0 {
		ch <- port.ProviderEvent{Type: port.ProviderText, Text: "Now let me check the diff..."}
		ch <- port.ProviderEvent{Type: port.ProviderFinish}
		close(ch)
		return ch, nil
	}
	ch <- port.ProviderEvent{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{
		CallID: "c_report", Name: "report", Args: []byte(`{"summary":"findings: ok","status":"done"}`),
	}}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

// A subagent that trails off without reporting is nudged to deliver via report,
// so the result is the real findings — not the stray fragment.
func TestSubagentNudgedToReportWhenTrailingOff(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	reg := builtin.Default()
	reg.Register(builtin.Report{})
	a := New(store, &fragmentThenReportLLM{}, reg, bus.New(), nil, Config{
		Permission: "allow",
		Agents:     map[string]AgentSpec{"worker": {Name: "worker", Tools: []string{"read", "report"}}},
	})
	parent := session.Session{ID: "s_parent", Workdir: t.TempDir(), Agent: "default"}
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "review"})
	if !strings.Contains(res.Text, "findings: ok") {
		t.Errorf("expected the reported findings, got %q", res.Text)
	}
	if strings.Contains(res.Text, "let me check the diff") {
		t.Errorf("result should be the report, not the stray fragment: %q", res.Text)
	}
}

// A blocked report surfaces its status so the orchestrator won't read it as done.
func TestSubagentReportBlockedStatus(t *testing.T) {
	a, parent := newReportApp(t, "blocked")
	res := a.spawn(context.Background(), parent, 0, port.SpawnRequest{Agent: "worker", Prompt: "do it"})
	if !strings.Contains(res.Text, "STATUS: BLOCKED") {
		t.Errorf("expected BLOCKED status surfaced, got %q", res.Text)
	}
}
