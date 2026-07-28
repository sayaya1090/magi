package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/config"
	corecouncil "github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func TestProfileModels(t *testing.T) {
	got := profileModels(map[string]config.LLMProfile{
		"a": {Model: "m1"},
		"b": {Model: "m2"},
	})
	if got["a"] != "m1" || got["b"] != "m2" {
		t.Errorf("profileModels = %v", got)
	}
	if profileModels(nil) != nil {
		t.Error("no profiles should yield nil")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("n>=len should be unchanged, got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("truncate = %q, want hello…", got)
	}
	if got := truncate("exact", 5); got != "exact" {
		t.Errorf("n==len should be unchanged, got %q", got)
	}
	// Multibyte content (each CJK rune is 3 bytes) must not be split into invalid
	// UTF-8 — the byte budget backs up to a rune boundary.
	got := truncate("안녕하세요", 7) // 7 lands mid-rune (3-byte runes)
	if !utf8.ValidString(got) {
		t.Errorf("truncate produced invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "…") || len(got) > 7+len("…") {
		t.Errorf("multibyte truncate = %q, want valid prefix within budget + …", got)
	}
}

// renderText turns each fact event into a human-readable line; verify the formats
// for text, tool call/result (✓/✗), council convened/decided, and errors (to errw).
func TestRenderText(t *testing.T) {
	mk := func(typ event.Type, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Type: typ, Data: b}
	}
	render := func(e event.Event) (string, string) {
		var out, errw bytes.Buffer
		renderText(&out, &errw, e)
		return out.String(), errw.String()
	}

	// assistant text
	if out, _ := render(mk(event.TypePartAppended, event.PartAppendedData{
		Part: session.Part{Kind: session.PartText, Text: "hi there"}})); !strings.Contains(out, "hi there") {
		t.Errorf("text not rendered: %q", out)
	}
	// tool call
	if out, _ := render(mk(event.TypePartAppended, event.PartAppendedData{
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read", Args: json.RawMessage(`{"path":"x"}`)}}})); !strings.Contains(out, "⚙ read") {
		t.Errorf("tool call not rendered: %q", out)
	}
	// tool result ok / error glyphs
	if out, _ := render(mk(event.TypePartAppended, event.PartAppendedData{
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{Content: json.RawMessage(`"done"`)}}})); !strings.Contains(out, "✓") {
		t.Errorf("ok result should show ✓: %q", out)
	}
	if out, _ := render(mk(event.TypePartAppended, event.PartAppendedData{
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{Content: json.RawMessage(`"boom"`), IsError: true}}})); !strings.Contains(out, "✗") {
		t.Errorf("error result should show ✗: %q", out)
	}
	// council convened, with signals appended
	if out, _ := render(mk(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar"}, Rule: "majority", Signals: []string{"verify: pass"}})); !strings.Contains(out, "council round 1") || !strings.Contains(out, "majority") || !strings.Contains(out, "verify: pass") {
		t.Errorf("convened (with signals) not rendered: %q", out)
	}
	// council decided
	if out, _ := render(mk(event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 2, Decision: "done", Tally: corecouncil.Breakdown{Done: 3, Continue: 0}})); !strings.Contains(out, "round 2: done") || !strings.Contains(out, "3 done") {
		t.Errorf("decided not rendered: %q", out)
	}
	// decided with a gate Note (forced finish) shows the note
	if out, _ := render(mk(event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 3, Decision: "done", Note: "round cap"})); !strings.Contains(out, "(round cap)") {
		t.Errorf("decided note not rendered: %q", out)
	}
	// decided=continue with feedback shows the → continue marker
	if out, _ := render(mk(event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 4, Decision: "continue", Feedback: "do X"})); !strings.Contains(out, "→ continue") {
		t.Errorf("decided continue marker not rendered: %q", out)
	}
	// error goes to errw, not out
	out, errw := render(mk(event.TypeError, event.ErrorData{Message: "kaboom"}))
	if !strings.Contains(errw, "kaboom") {
		t.Errorf("error should go to errw: %q", errw)
	}
	if strings.Contains(out, "kaboom") {
		t.Errorf("error must not go to stdout: %q", out)
	}
}
