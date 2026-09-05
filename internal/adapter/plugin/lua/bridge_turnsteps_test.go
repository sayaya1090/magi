package lua

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// turn_steps hands a plugin tool the turn's own log — names, decoded args, failure — and
// answers only inside a tool call, with what the host gives it.
func TestTurnStepsHandsThePluginTheTurnsCalls(t *testing.T) {
	_, tool := spawnPlugin(t, `
    local steps, err = magi.turn_steps()
    if not steps then return "ERR " .. tostring(err) end
    local out = {}
    for _, s in ipairs(steps) do
      out[#out+1] = s.name .. "|" .. tostring(s.failed) .. "|" .. tostring(s.args.slides and #s.args.slides or "-")
    end
    return table.concat(out, "\n")`)
	env := port.ToolEnv{TurnSteps: func(context.Context) ([]port.ChildStep, error) {
		return []port.ChildStep{
			{Name: "add_slides", Args: json.RawMessage(`{"slides":[{},{},{}]}`)},
			{Name: "render_slide", Args: json.RawMessage(`{"slide":1}`), Failed: true, Output: "no such slide"},
		}, nil
	}}
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	_ = json.Unmarshal(res.Content, &got)
	if got != "add_slides|false|3\nrender_slide|true|-" {
		t.Errorf("plugin saw %q", got)
	}
	res, _ = tool.Execute(context.Background(), json.RawMessage(`{}`), port.ToolEnv{})
	_ = json.Unmarshal(res.Content, &got)
	if !strings.HasPrefix(got, "ERR turn_steps: only available inside a tool call") {
		t.Errorf("without a host-provided TurnSteps the bridge must say so, got %q", got)
	}
}

// The shipped landing plugin, loaded from the repo: `land` refuses a turn that made more
// slides than it looked at, and accepts once every page was rendered. This is the rule the
// two 2026-09-04 deck runs broke (skills said "render each finished page", renders were 0).
func TestLandingLandRequiresARenderPerPageMade(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "landing"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "init.lua")); err != nil {
		t.Skip("plugins/landing not beside this tree")
	}
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, ContextReg: &fakeContextReg{},
		Notify: func(string, string) {}, Logf: func(string) {}})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load landing: %v", err)
	}
	land, ok := sink.Get("land")
	if !ok {
		t.Fatal("landing registered no `land` tool")
	}
	call := func(steps []port.ChildStep) (string, bool) {
		env := port.ToolEnv{TurnSteps: func(context.Context) ([]port.ChildStep, error) { return steps, nil }}
		res, err := land.Execute(context.Background(),
			json.RawMessage(`{"did":["슬라이드 1~3 을 만들었습니다"],"verified":"read_slide 로 되읽음"}`), env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		_ = json.Unmarshal(res.Content, &s)
		return s, res.IsError
	}
	made := port.ChildStep{Name: "add_slides", Args: json.RawMessage(`{"slides":[{},{},{}]}`)}
	render := port.ChildStep{Name: "render_slide", Args: json.RawMessage(`{"slide":1}`)}
	if msg, isErr := call([]port.ChildStep{made, render}); !isErr || !strings.Contains(msg, "3장") || !strings.Contains(msg, "1번") {
		t.Errorf("3 made, 1 rendered must be refused naming both counts, got err=%v %q", isErr, msg)
	}
	if msg, isErr := call([]port.ChildStep{made, render, render, render}); isErr {
		t.Errorf("3 made, 3 rendered must land, got %q", msg)
	}
	// A failed render is not a look; a failed add is not a page.
	failedRender := port.ChildStep{Name: "render_slide", Failed: true}
	if _, isErr := call([]port.ChildStep{made, render, render, failedRender}); !isErr {
		t.Error("a failed render_slide must not count as having seen the page")
	}
	if msg, isErr := call([]port.ChildStep{{Name: "add_slides", Failed: true}}); isErr {
		t.Errorf("a failed add_slides made no page, so nothing to render, got %q", msg)
	}
	// No slides made this turn (an edit-only turn): the rule is silent.
	if msg, isErr := call([]port.ChildStep{{Name: "set_text"}}); isErr {
		t.Errorf("a turn that made no slide is not held to renders, got %q", msg)
	}
	// A title-wrap ⚠ the deck tool attached stays until a later set_text on that slide's title
	// answers without one (live 2026-09-05: seven ⚠ ignored, "titles on one line" declared).
	madeWarned := port.ChildStep{Name: "add_slides", Args: json.RawMessage(`{"slides":[{},{},{}]}`),
		Output: `{"made":3,"slides":[{"slide":2,"notes":["⚠ 제목이 2줄로 접힐 수 있습니다(40자·44pt·자리 폭 828pt)"]},{"slide":3,"notes":[]},{"slide":4,"notes":["⚠ 제목이 2줄로 접힐 수 있습니다"]}]}`}
	if msg, isErr := call([]port.ChildStep{madeWarned, render, render, render}); !isErr || !strings.Contains(msg, "남은 장: 2, 4") {
		t.Errorf("unresolved title ⚠ must refuse naming the slides, got err=%v %q", isErr, msg)
	}
	fixed2 := port.ChildStep{Name: "set_text", Args: json.RawMessage(`{"slide":2,"placeholder":"title","text":"짧은 제목"}`), Output: `{"slide_id":"s2","shape_id":"t","text":"짧은 제목"}`}
	stillLong4 := port.ChildStep{Name: "set_text", Args: json.RawMessage(`{"slide":4,"placeholder":"title","text":"여전히 긴"}`), Output: `{"slide_id":"s4","shape_id":"t","text":"x","note":"⚠ 제목이 2줄로 접힐 수 있습니다"}`}
	if msg, isErr := call([]port.ChildStep{madeWarned, render, render, render, fixed2, stillLong4}); !isErr || !strings.Contains(msg, "남은 장: 4") || strings.Contains(msg, "2, 4") {
		t.Errorf("a set_text answering without ⚠ clears its slide, one answering with ⚠ keeps it, got err=%v %q", isErr, msg)
	}
	fixed4 := port.ChildStep{Name: "set_text", Args: json.RawMessage(`{"slide":4,"placeholder":"title","text":"짧게"}`), Output: `{"slide_id":"s4","shape_id":"t","text":"짧게"}`}
	if msg, isErr := call([]port.ChildStep{madeWarned, render, render, render, fixed2, fixed4}); isErr {
		t.Errorf("all title ⚠ answered → lands, got %q", msg)
	}
}

// With a council in the run, landing registers no `land` tool — one door — and its gates run
// on the declaration instead, through RunDeclarationGates, with the same measurements.
func TestLandingBecomesADeclarationGateWhenTheCouncilOwnsTheDoor(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "plugins", "landing"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "init.lua")); err != nil {
		t.Skip("plugins/landing not beside this tree")
	}
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, ContextReg: &fakeContextReg{},
		Notify: func(string, string) {}, Logf: func(string) {}, Runtime: RuntimeInfo{CouncilEnabled: true}})
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load landing: %v", err)
	}
	if _, ok := sink.Get("land"); ok {
		t.Fatal("with a council, landing must not register a second door (`land`)")
	}
	gate := func(steps []port.ChildStep) []string {
		return h.RunDeclarationGates(context.Background(),
			port.ToolEnv{TurnSteps: func(context.Context) ([]port.ChildStep, error) { return steps, nil }})
	}
	made := port.ChildStep{Name: "add_slides", Args: json.RawMessage(`{"slides":[{},{},{}]}`)}
	render := port.ChildStep{Name: "render_slide"}
	if why := gate([]port.ChildStep{made, render}); len(why) != 1 || !strings.Contains(why[0], "3장") || !strings.Contains(why[0], "1번") {
		t.Errorf("3 made, 1 rendered must be refused at the declaration: %v", why)
	}
	if why := gate([]port.ChildStep{made, render, render, render}); len(why) != 0 {
		t.Errorf("3 made, 3 rendered passes the gate: %v", why)
	}
	if why := gate(nil); len(why) != 0 {
		t.Errorf("an empty turn passes: %v", why)
	}
}

// A gate that raises is logged and passes — a broken gate must not lock a turn shut.
func TestDeclarationGateErrorsPass(t *testing.T) {
	sink := builtin.NewRegistry()
	h := NewHostWithConfig(HostConfig{ToolSink: sink, Logf: func(string) {}})
	dir := writePlugin(t, "name=\"gatey\"\ncapabilities=[\"tool\"]\n", `
magi.register_declaration_gate{ check = function() error("boom") end }
magi.register_declaration_gate{ check = function() return "no: " .. #magi.turn_steps() .. " steps" end }
`)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	why := h.RunDeclarationGates(context.Background(), port.ToolEnv{TurnSteps: func(context.Context) ([]port.ChildStep, error) {
		return []port.ChildStep{{Name: "x"}, {Name: "y"}}, nil
	}})
	if len(why) != 1 || why[0] != "no: 2 steps" {
		t.Errorf("the erroring gate passes, the refusing one refuses with turn_steps in reach: %v", why)
	}
}
