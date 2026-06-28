package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// fakeHeadless is a canned headlessApp: Subscribe replays a fixed event slice
// (already closed) and Submit records the prompt, so runHeadless can be driven
// without a real app/LLM.
type fakeHeadless struct {
	events    []event.Event
	subErr    error
	submitErr error
	submitted *command.SubmitPrompt
}

func (f *fakeHeadless) Subscribe(_ context.Context, _ session.SessionID, _ int64) (<-chan event.Event, func(), error) {
	if f.subErr != nil {
		return nil, nil, f.subErr
	}
	ch := make(chan event.Event, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, func() {}, nil
}

func (f *fakeHeadless) Submit(_ context.Context, c command.SubmitPrompt) error {
	if f.submitErr != nil {
		return f.submitErr
	}
	f.submitted = &c
	return nil
}

func partEvent(t *testing.T, p session.Part) event.Event {
	t.Helper()
	b, err := json.Marshal(event.PartAppendedData{Part: p})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypePartAppended, Data: b}
}

func errEvent(t *testing.T, msg string) event.Event {
	t.Helper()
	b, err := json.Marshal(event.ErrorData{Message: msg})
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: event.TypeError, Data: b}
}

// runHeadless in text mode renders each part to out, submits the prompt, and exits
// 0 at TurnFinished.
func TestRunHeadlessText(t *testing.T) {
	f := &fakeHeadless{events: []event.Event{
		partEvent(t, session.Part{Kind: session.PartText, Text: "hello world"}),
		partEvent(t, session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "bash", Args: json.RawMessage(`{"cmd":"ls"}`)}}),
		partEvent(t, session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{Content: json.RawMessage(`"file.txt"`)}}),
		{Type: event.TypeTurnFinished},
	}}
	var out, errw bytes.Buffer
	exit := runHeadless(context.Background(), f, "sid", "do a thing", false, &out, &errw)
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	s := out.String()
	for _, want := range []string{"hello world", "⚙ bash", `{"cmd":"ls"}`, "✓", "file.txt"} {
		if !strings.Contains(s, want) {
			t.Errorf("text output missing %q in:\n%s", want, s)
		}
	}
	if f.submitted == nil || len(f.submitted.Parts) != 1 || f.submitted.Parts[0].Text != "do a thing" {
		t.Errorf("prompt not submitted as expected: %+v", f.submitted)
	}
	if errw.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errw.String())
	}
}

// JSON mode emits one JSON object per event, each decodable back to an Event.
func TestRunHeadlessJSON(t *testing.T) {
	f := &fakeHeadless{events: []event.Event{
		partEvent(t, session.Part{Kind: session.PartText, Text: "hi"}),
		{Type: event.TypeTurnFinished},
	}}
	var out, errw bytes.Buffer
	if exit := runHeadless(context.Background(), f, "sid", "p", true, &out, &errw); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 JSON lines, got %d:\n%s", len(lines), out.String())
	}
	var e event.Event
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Errorf("first line not valid Event JSON: %v", err)
	}
}

// A turn Error event makes runHeadless exit 1 and routes the message to stderr.
func TestRunHeadlessError(t *testing.T) {
	f := &fakeHeadless{events: []event.Event{errEvent(t, "boom")}}
	var out, errw bytes.Buffer
	if exit := runHeadless(context.Background(), f, "sid", "p", false, &out, &errw); exit != 1 {
		t.Errorf("exit = %d, want 1 on error event", exit)
	}
	if !strings.Contains(errw.String(), "boom") {
		t.Errorf("error message not on stderr: %q", errw.String())
	}
}

// Subscribe/Submit failures abort with exit 1 before streaming.
func TestRunHeadlessSetupErrors(t *testing.T) {
	var out, errw bytes.Buffer
	if exit := runHeadless(context.Background(), &fakeHeadless{subErr: errors.New("nosub")}, "s", "p", false, &out, &errw); exit != 1 {
		t.Errorf("subscribe error exit = %d, want 1", exit)
	}
	if !strings.Contains(errw.String(), "subscribe") {
		t.Errorf("stderr missing subscribe error: %q", errw.String())
	}
	errw.Reset()
	if exit := runHeadless(context.Background(), &fakeHeadless{submitErr: errors.New("nosubmit")}, "s", "p", false, &out, &errw); exit != 1 {
		t.Errorf("submit error exit = %d, want 1", exit)
	}
	if !strings.Contains(errw.String(), "submit") {
		t.Errorf("stderr missing submit error: %q", errw.String())
	}
}

func TestResolvePrompt(t *testing.T) {
	// A literal flag value is used verbatim (stdin untouched).
	if got, err := resolvePrompt("hello", strings.NewReader("STDIN")); err != nil || got != "hello" {
		t.Errorf("resolvePrompt(literal) = %q, %v", got, err)
	}
	// "-" reads the full prompt from stdin.
	if got, err := resolvePrompt("-", strings.NewReader("from stdin\nline2")); err != nil || got != "from stdin\nline2" {
		t.Errorf("resolvePrompt(stdin) = %q, %v", got, err)
	}
	// A stdin read error propagates.
	if _, err := resolvePrompt("-", errReader{}); err == nil {
		t.Error("expected stdin read error to propagate")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read fail") }

// mergeStrMap overlays `over` onto `base` (over wins), used to merge a project
// config / hooks over the global one.
func TestMergeStrMap(t *testing.T) {
	base := map[string]string{"a": "1", "b": "2"}
	got := mergeStrMap(base, map[string]string{"b": "X", "c": "3"})
	if got["a"] != "1" || got["b"] != "X" || got["c"] != "3" {
		t.Errorf("merge = %v (over should win, base kept)", got)
	}
	// Empty override returns the base untouched.
	if got := mergeStrMap(map[string]string{"k": "v"}, nil); got["k"] != "v" || len(got) != 1 {
		t.Errorf("empty over should keep base: %v", got)
	}
	// nil base + override allocates a new map.
	if got := mergeStrMap(nil, map[string]string{"k": "v"}); got["k"] != "v" {
		t.Errorf("nil base + over = %v", got)
	}
}

func TestEnvDur(t *testing.T) {
	t.Setenv("MAGI_TEST_DUR", "2s")
	if d := envDur("MAGI_TEST_DUR", time.Second); d != 2*time.Second {
		t.Errorf("envDur parsed = %v, want 2s", d)
	}
	if d := envDur("MAGI_TEST_UNSET_DUR", 5*time.Second); d != 5*time.Second {
		t.Errorf("unset → default, got %v", d)
	}
	t.Setenv("MAGI_TEST_BAD_DUR", "notaduration")
	if d := envDur("MAGI_TEST_BAD_DUR", 7*time.Second); d != 7*time.Second {
		t.Errorf("invalid → default, got %v", d)
	}
}
