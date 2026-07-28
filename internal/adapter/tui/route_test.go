package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// routedTestModel builds a Model whose app has named agents, so the /route editor
// has something to list and edit.
func routedTestModel(t *testing.T) Model {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(store, stubLLM{}, builtin.Default(), bus.New(), nil, app.Config{
		Permission: "allow",
		Model:      session.ModelRef{Provider: "openai", Model: "base"},
		Agents: map[string]app.AgentSpec{
			"explore": {Name: "explore"},
			"coder":   {Name: "coder"},
		},
		ProfileModels: map[string]string{"fast": "gpt-oss:20b"},
	})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return New(context.Background(), a, nil, sid, "base", t.TempDir(), true, "")
}

func keyPress(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: keyCode(s)}
}
func keyCode(s string) rune {
	switch s {
	case "down":
		return tea.KeyDown
	case "up":
		return tea.KeyUp
	case "left":
		return tea.KeyLeft
	case "right":
		return tea.KeyRight
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	case "backspace":
		return tea.KeyBackspace
	case "tab":
		return tea.KeyTab
	case "space":
		return tea.KeySpace
	}
	return 0
}

func typeStr(m *Model, s string) {
	for _, ch := range s {
		m.handleRouteKey(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
}

// Editing the (session) row sets the session default model.
func TestRouteEditorSessionModel(t *testing.T) {
	m := routedTestModel(t)
	m.openRouteEditor() // row 0 = (session)
	m.handleRouteKey(keyPress("enter"))
	typeStr(&m, "new-model")
	m.handleRouteKey(keyPress("enter"))
	if m.model != "new-model" {
		t.Errorf("session model not updated in header: %q", m.model)
	}
}

// The "+ add profile" row opens the multi-field profile form; filling it and
// selecting [save] creates a profile reachable via app.Profiles().
func TestRouteEditorAddsProfile(t *testing.T) {
	m := routedTestModel(t)
	m.openRouteEditor()
	// Navigate to the last row (+ add profile) and open the form.
	for m.routeList[m.routeSel].kind != rowAddProfile {
		m.handleRouteKey(keyPress("down"))
	}
	m.handleRouteKey(keyPress("enter"))
	if m.profileForm == nil || !m.profileForm.isNew {
		t.Fatal("add profile should open a new profile form")
	}
	// Fields: name, base_url, api_key, model, header_key, header_value.
	editField := func(text string) {
		m.handleRouteKey(keyPress("enter")) // begin editing the selected field
		typeStr(&m, text)
		m.handleRouteKey(keyPress("enter")) // commit
	}
	editField("cheap") // name
	m.handleRouteKey(keyPress("down"))
	editField("https://cheap.gw/v1") // base_url
	m.handleRouteKey(keyPress("down"))
	m.handleRouteKey(keyPress("down")) // skip api_key
	editField("gpt-oss:20b")           // model
	// Jump to [save] and save.
	for m.profileForm.sel < len(m.profileForm.fields) {
		m.handleRouteKey(keyPress("down"))
	}
	m.handleRouteKey(keyPress("enter")) // [save]
	if m.profileForm != nil {
		t.Fatal("save should close the form")
	}
	var found *app.ProfileDef
	for _, p := range m.app.Profiles() {
		if p.Name == "cheap" {
			pp := p
			found = &pp
		}
	}
	if found == nil {
		t.Fatalf("profile 'cheap' not created: %+v", m.app.Profiles())
	}
	if found.BaseURL != "https://cheap.gw/v1" || found.Model != "gpt-oss:20b" {
		t.Errorf("profile fields = %+v", *found)
	}
}

// Tab saves the profile form from anywhere (quick submit).
func TestProfileFormTabSaves(t *testing.T) {
	m := routedTestModel(t)
	m.openRouteEditor()
	for m.routeList[m.routeSel].kind != rowAddProfile {
		m.handleRouteKey(keyPress("down"))
	}
	m.handleRouteKey(keyPress("enter")) // open new profile form (sel 0 = name)
	m.handleRouteKey(keyPress("enter")) // edit name
	typeStr(&m, "quick")
	m.handleRouteKey(keyPress("enter"))                 // commit name
	m.handleRouteKey(tea.KeyPressMsg{Code: tea.KeyTab}) // Tab → save
	if m.profileForm != nil {
		t.Fatal("Tab should save and close the form")
	}
	found := false
	for _, p := range m.app.Profiles() {
		if p.Name == "quick" {
			found = true
		}
	}
	if !found {
		t.Errorf("Tab-save did not create the profile: %+v", m.app.Profiles())
	}
}

// Backspace edits the buffer; esc cancels editing without applying.
func TestRouteEditorBackspaceAndCancel(t *testing.T) {
	m := routedTestModel(t)
	m.openRouteEditor()
	m.handleRouteKey(keyPress("enter")) // edit the (session) row
	typeStr(&m, "abc")
	m.handleRouteKey(keyPress("backspace"))
	if m.routeBuf != "ab" {
		t.Fatalf("backspace buffer = %q, want ab", m.routeBuf)
	}
	m.handleRouteKey(keyPress("esc"))
	if m.routeEditing {
		t.Error("esc should exit editing")
	}
	if !m.routing {
		t.Error("esc from editing should keep the editor open (cancel edit, not close)")
	}
	if m.model == "ab" {
		t.Error("cancel must not apply the typed model")
	}
}
