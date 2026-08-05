package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// subTool is a plugin-shaped subagent: it declares itself one, names its group, and describes what
// it does — all three the way register_tool passes them through.
type subTool struct {
	name, desc, group string
}

func (t subTool) Name() string            { return t.name }
func (t subTool) Description() string     { return t.desc }
func (t subTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t subTool) Meta() port.ToolMetadata {
	return port.ToolMetadata{Subagent: true, Group: t.group}
}
func (t subTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

// subagentModel is a Model whose registry holds three declared subagents, two of them grouped.
func subagentModel(t *testing.T) Model {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(subTool{name: "security_reviewer", desc: "보안 렌즈로 diff를 훑고 취약점만 보고", group: "review"})
	reg.Register(subTool{name: "perf_reviewer", desc: "핫패스만 보고 회귀를 짚는다", group: "review"})
	reg.Register(subTool{name: "test_writer", desc: "실패하는 테스트를 먼저 쓰고 통과시킨다"})
	a := app.New(store, stubLLM{}, reg, bus.New(), nil, app.Config{Permission: "allow"})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return New(context.Background(), a, nil, sid, "m", t.TempDir(), true, "")
}

// The list shows what a plugin registered: name AND what it does. A name alone does not tell a
// user what they are switching off.
func TestTheSubagentListShowsNamesAndDescriptions(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 100, 40
	m.openSubagents()

	plain := ansi.Strip(m.subagentsView())
	for _, want := range []string{"security_reviewer", "보안 렌즈", "test_writer", "실패하는 테스트"} {
		if !strings.Contains(plain, want) {
			t.Errorf("%q missing from the list:\n%s", want, plain)
		}
	}
	// Grouped under the plugin's own label, ungrouped last.
	gi, ui := strings.Index(plain, "review"), strings.Index(plain, "(ungrouped)")
	if gi < 0 || ui < 0 || gi > ui {
		t.Errorf("groups should lead and (ungrouped) trail:\n%s", plain)
	}
}

// A group header has NO state of its own — its box is computed from the members every draw. A
// stored group flag could disagree with the rows under it and nothing would force them back into
// step, which is the shape of most of what this tree has been shedding.
func TestTheGroupBoxIsDerivedFromItsMembers(t *testing.T) {
	if got := groupBox(0, 3); got != "[ ]" {
		t.Errorf("none on = %q, want [ ]", got)
	}
	if got := groupBox(3, 3); got != "[✓]" {
		t.Errorf("all on = %q, want [✓]", got)
	}
	if got := groupBox(1, 3); got != "[~]" {
		t.Errorf("some on = %q, want [~]", got)
	}
	if got := groupBox(0, 0); got != "[ ]" {
		t.Errorf("an empty group = %q, want [ ]", got)
	}

	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 100, 40
	m.openSubagents()

	// Turn one member of "review" off; the header must follow to mixed without anything storing it.
	for i, r := range m.subagentList {
		if r.kind == subRowAgent && r.group == "review" {
			m.subSel = i
			break
		}
	}
	m.toggleSubagentRow()
	on, total := m.groupState("review")
	if on == total {
		t.Fatalf("the toggle did not take: %d/%d", on, total)
	}
	if got := groupBox(on, total); got != "[~]" {
		t.Errorf("a partly-on group should read mixed, got %q", got)
	}
}

// Toggling the header is a BULK ACTION over the members: any off → all on, all on → all off.
func TestTogglingAGroupHeaderMovesEveryMember(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 100, 40
	m.openSubagents()

	hdr := -1
	for i, r := range m.subagentList {
		if r.kind == subRowGroup && r.group == "review" {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		t.Fatal("no review group header")
	}
	m.subSel = hdr
	m.toggleSubagentRow() // all on → all off
	if on, total := m.groupState("review"); on != 0 {
		t.Errorf("after toggling a full group, %d/%d are still on", on, total)
	}
	m.toggleSubagentRow() // all off → all on
	if on, total := m.groupState("review"); on != total {
		t.Errorf("after toggling an empty group, only %d/%d came on", on, total)
	}
}

// The view fits the terminal it is drawn in: bounded in width like every row here, and windowed in
// height like every other list, with a marker saying what is outside the window.
func TestTheSubagentListFitsTheTerminal(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	for _, wh := range [][2]int{{40, 10}, {60, 8}, {100, 40}, {30, 6}} {
		m.width, m.height = wh[0], wh[1]
		m.openSubagents()
		out := m.subagentsView()
		for i, line := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(line); lw > m.width {
				t.Errorf("%dx%d line %d = %d cells: %q", m.width, m.height, i, lw, ansi.Strip(line))
			}
		}
		// A window so short that modalRoom goes negative cannot be satisfied — one row is the
		// floor for any view at all — so the requirement there is that it sheds down to that one
		// row rather than drawing the whole list.
		room, h := m.modalRoom(), lipgloss.Height(out)
		if want := max(1, room); h > want {
			t.Errorf("%dx%d: the view is %d rows in %d of room", m.width, m.height, h, want)
		}
	}
}

// On/off and the model are edited on the same row, from the same screen. Space toggles, enter takes
// the model. Splitting them — the flag here, the model in the plugin's own config section — would
// have meant two places to go for one subagent.
func TestTheModelIsEditedOnTheSameRow(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 120, 40
	m.openSubagents()

	// Land on a subagent row, not a header.
	for i, r := range m.subagentList {
		if r.kind == subRowAgent {
			m.subSel = i
			break
		}
	}
	name := m.subagentList[m.subSel].info.Name

	m.handleSubagentKey("enter")
	if !m.subEditing {
		t.Fatal("enter on a subagent row should start editing its model")
	}
	for _, k := range strings.Split("big-model", "") {
		m.handleSubagentKey(k)
	}
	// What is being typed shows on the row, so the edit is visible where it lands.
	if plain := ansi.Strip(m.subagentsView()); !strings.Contains(plain, "big-model") {
		t.Errorf("the model being typed is not on the row:\n%s", plain)
	}
	m.handleSubagentKey("enter")
	if m.subEditing {
		t.Error("enter should apply and leave the field")
	}
	for _, s := range m.app.Subagents() {
		if s.Name == name && s.Model != "big-model" {
			t.Errorf("%s kept model %q", name, s.Model)
		}
	}

	// Empty CLEARS it — the way back to "whatever the plugin asked for" without having to remember
	// what that was.
	m.handleSubagentKey("enter")
	for range "big-model" {
		m.handleSubagentKey("backspace")
	}
	m.handleSubagentKey("enter")
	for _, s := range m.app.Subagents() {
		if s.Name == name && s.Model != "" {
			t.Errorf("an empty field should clear the override, got %q", s.Model)
		}
	}

	// esc abandons rather than applies.
	m.handleSubagentKey("enter")
	m.handleSubagentKey("x")
	m.handleSubagentKey("esc")
	for _, s := range m.app.Subagents() {
		if s.Name == name && s.Model != "" {
			t.Errorf("esc should abandon the edit, got %q", s.Model)
		}
	}
}

// A group header has no model to set, so enter must not open a field on one.
func TestEnterOnAGroupHeaderStartsNoEdit(t *testing.T) {
	mm := subagentModel(t)
	m := &mm
	m.width, m.height = 120, 40
	m.openSubagents()
	for i, r := range m.subagentList {
		if r.kind == subRowGroup {
			m.subSel = i
			break
		}
	}
	m.handleSubagentKey("enter")
	if m.subEditing {
		t.Error("a group header has nothing to point at a model")
	}
}
