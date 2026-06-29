package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	corecouncil "github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// applyAgentModels: a route naming a profile routes that agent to the profile's
// backend AND model; any other value is a bare model on the default backend; an
// unknown agent or empty value is ignored.
func TestApplyAgentModels(t *testing.T) {
	agents := map[string]app.AgentSpec{
		"coder":   {Name: "coder"},
		"explore": {Name: "explore"},
		"writer":  {Name: "writer"},
	}
	profiles := map[string]config.LLMProfile{"fast": {Model: "qwen3-coder:30b"}}
	routes := map[string]string{
		"coder":   "fast",     // profile route
		"explore": "gpt-4o",   // bare model
		"writer":  "",         // empty → skip
		"ghost":   "anything", // unknown agent → skip
	}
	applyAgentModels(agents, routes, profiles)

	if a := agents["coder"]; a.Provider != "fast" || a.Model.Model != "qwen3-coder:30b" || a.Model.Provider != "openai" {
		t.Errorf("coder routed wrong: provider=%q model=%+v", a.Provider, a.Model)
	}
	if a := agents["explore"]; a.Provider != "" || a.Model.Model != "gpt-4o" {
		t.Errorf("explore bare model wrong: provider=%q model=%+v", a.Provider, a.Model)
	}
	if a := agents["writer"]; a.Model.Model != "" || a.Provider != "" {
		t.Errorf("empty route should not change writer: %+v", a)
	}
	if _, ok := agents["ghost"]; ok {
		t.Error("unknown agent must not be created")
	}
}

// A profile route with no model pins only the provider (the agent keeps inheriting
// the session model for that backend).
func TestApplyAgentModelsProfileNoModel(t *testing.T) {
	agents := map[string]app.AgentSpec{"coder": {Name: "coder"}}
	profiles := map[string]config.LLMProfile{"local": {BaseURL: "http://x"}} // no Model
	applyAgentModels(agents, map[string]string{"coder": "local"}, profiles)
	a := agents["coder"]
	if a.Provider != "local" {
		t.Errorf("provider should be set to the profile, got %q", a.Provider)
	}
	if a.Model.Model != "" {
		t.Errorf("no profile model → agent model should stay empty, got %q", a.Model.Model)
	}
}

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
	// council convened
	if out, _ := render(mk(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar"}, Rule: "majority"})); !strings.Contains(out, "council round 1") || !strings.Contains(out, "majority") {
		t.Errorf("convened not rendered: %q", out)
	}
	// council decided
	if out, _ := render(mk(event.TypeCouncilDecided, event.CouncilDecidedData{
		Round: 2, Decision: "done", Tally: corecouncil.Breakdown{Done: 3, Continue: 0}})); !strings.Contains(out, "round 2: done") || !strings.Contains(out, "3 done") {
		t.Errorf("decided not rendered: %q", out)
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
