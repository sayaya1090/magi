package app

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// subagentChoiceLog is the config file, as far as this test is concerned: it records the choice
// each edit made, and can refuse a named one.
type subagentChoiceLog struct {
	wrote  []string // "name=on" / "name=off", in the order they landed
	refuse string
}

var _ SubagentPersister = (*subagentChoiceLog)(nil)

func (p *subagentChoiceLog) PersistSubagent(name string, pref SubagentPref) error {
	if name == p.refuse {
		return errors.New("the config file is read-only")
	}
	state := "unset"
	if pref.Enabled != nil {
		state = map[bool]string{true: "on", false: "off"}[*pref.Enabled]
	}
	p.wrote = append(p.wrote, name+"="+state)
	return nil
}

func groupApp(t *testing.T, p SubagentPersister) *App {
	t.Helper()
	reg := builtin.Default()
	reg.Register(metaTool{name: "planner", meta: port.ToolMetadata{Subagent: true, Group: "research"}})
	reg.Register(metaTool{name: "scout", meta: port.ToolMetadata{Subagent: true, Group: "research"}})
	reg.Register(metaTool{name: "builder", meta: port.ToolMetadata{Subagent: true, Group: "build"}})
	reg.Register(metaTool{name: "loner", meta: port.ToolMetadata{Subagent: true}})
	return &App{tools: reg, cfg: Config{SubagentPersister: p}}
}

func enabledOf(a *App) map[string]bool {
	out := map[string]bool{}
	for _, s := range a.Subagents() {
		out[s.Name] = s.Enabled
	}
	return out
}

// A group has no state of its own: turning one off writes each of its members. A stored group flag
// could disagree with the members under it and nothing would force them back into step — the header
// a user sees is derived from the members instead, so what the switch changes IS the members.
func TestAGroupSwitchWritesItsMembersAndNobodyElse(t *testing.T) {
	p := &subagentChoiceLog{}
	a := groupApp(t, p)
	if err := a.SetGroupEnabled("research", false); err != nil {
		t.Fatal(err)
	}
	got := enabledOf(a)
	if got["planner"] || got["scout"] {
		t.Errorf("the group was switched off and its members read %v", got)
	}
	if !got["builder"] || !got["loner"] {
		t.Errorf("the switch reached outside its group: %v", got)
	}
	sort.Strings(p.wrote)
	if strings.Join(p.wrote, " ") != "planner=off scout=off" {
		t.Errorf("what was written to the config was %v — a choice that is not persisted is one "+
			"the user makes again after every restart", p.wrote)
	}
	// And back on, which is the case a subagent that ships switched off needs: the config has to be
	// able to say "the user turned this on".
	if err := a.SetGroupEnabled("research", true); err != nil {
		t.Fatal(err)
	}
	if got := enabledOf(a); !got["planner"] || !got["scout"] {
		t.Errorf("the group would not come back on: %v", got)
	}
}

// A group with nobody in it writes nobody. The name comes from a plugin's declaration, so a group
// that no longer exists is a name a saved keybinding or a stale pane can still send.
func TestAGroupNobodyIsInChangesNothing(t *testing.T) {
	p := &subagentChoiceLog{}
	a := groupApp(t, p)
	before := enabledOf(a)
	if err := a.SetGroupEnabled("a group that went away", false); err != nil {
		t.Fatal(err)
	}
	if len(p.wrote) != 0 {
		t.Errorf("an empty group wrote %v", p.wrote)
	}
	for name, on := range enabledOf(a) {
		if on != before[name] {
			t.Errorf("%q changed", name)
		}
	}
	// The ungrouped are their own case, and not a group called "": a switch has to be able to reach
	// them, and it must not reach them by accident from a group whose name went missing.
	if err := a.SetGroupEnabled("", false); err != nil {
		t.Fatal(err)
	}
	if got := enabledOf(a); got["loner"] {
		t.Error("the ungrouped subagent could not be switched off")
	} else if !got["planner"] {
		t.Error("switching the ungrouped off reached into a named group")
	}
}

// One member that will not persist does not decide the group. The rest are still written and the
// switch still reports the failure — a half-applied group that claims success is a header that
// disagrees with its members with nobody told.
func TestOneMemberThatWillNotPersistDoesNotStopTheRest(t *testing.T) {
	p := &subagentChoiceLog{refuse: "planner"}
	a := groupApp(t, p)
	err := a.SetGroupEnabled("research", false)
	if err == nil {
		t.Error("a member that could not be written was reported as a clean switch")
	}
	if len(p.wrote) != 1 || p.wrote[0] != "scout=off" {
		t.Errorf("after the refusal the config was written %v — the members past the failure were "+
			"never attempted", p.wrote)
	}
}
