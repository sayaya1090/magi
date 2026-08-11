package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// Headless runs omit the human-in-the-loop tools — nothing can answer them, and an unusable tool
// still costs the model weight on every request.
func TestRegisterOrchestrationToolsHeadless(t *testing.T) {
	has := func(reg *builtin.Registry, name string) bool {
		_, ok := reg.Get(name)
		return ok
	}
	interactiveOnly := []string{"ask_user", "route_interjection"}

	headlessReg := builtin.NewRegistry()
	registerOrchestrationTools(headlessReg, true)
	for _, n := range interactiveOnly {
		if has(headlessReg, n) {
			t.Errorf("headless must omit human-in-the-loop tool %q", n)
		}
	}
	if n := len(headlessReg.List()); n != 0 {
		t.Errorf("every tool this registers needs a human; headless should get none, got %d", n)
	}

	interactiveReg := builtin.NewRegistry()
	registerOrchestrationTools(interactiveReg, false)
	for _, n := range interactiveOnly {
		if !has(interactiveReg, n) {
			t.Errorf("interactive must register %q", n)
		}
	}
}

// A run with no council must not be offered the council tool: every call it could make would come
// back as "no council is configured", which costs a step to learn and teaches nothing.
func TestCouncilToolWithdrawnWithoutACouncil(t *testing.T) {
	reg := builtin.Default()
	if _, ok := reg.Get("council"); !ok {
		t.Fatal("setup: the default registry should carry the council tool")
	}
	applyCouncilAvailability(reg, true)
	if _, ok := reg.Get("council"); !ok {
		t.Error("a configured council must keep the tool")
	}
	applyCouncilAvailability(reg, false)
	if _, ok := reg.Get("council"); ok {
		t.Error("with no council the tool must not be offered")
	}
	// The name is still vocabulary policy code recognises, whether or not it is offered.
	if !builtin.KnownNames()["council"] {
		t.Error("withdrawing the tool must not remove the name from KnownNames")
	}
}

// Whether a question can be asked follows whether anybody can answer, not whether there is a UI.
//
// A daemon is headless and is the run most likely to need a person: it works while nobody watches.
// Withholding ask_user there took the tool away from exactly the case the decision report was built
// for — and the socket, the console and the terminal were all already able to carry the answer.
func TestAskingIsAvailableWhereverSomebodyCanAnswer(t *testing.T) {
	for _, c := range []struct {
		what               string
		headless, canReply bool
		want               bool // want: nobody can answer, so the tools stay off
	}{
		{"the terminal", false, false, false},
		{"a daemon somebody can attach to", true, true, false},
		{"a -p run", true, false, true},
	} {
		if got := nobodyCanAnswer(c.headless, c.canReply); got != c.want {
			t.Errorf("%s: nobodyCanAnswer = %v, want %v", c.what, got, c.want)
		}
	}

	// And the flag reaches the registry, which is the half that decides anything.
	reg := builtin.NewRegistry()
	registerOrchestrationTools(reg, nobodyCanAnswer(true, true))
	if _, ok := reg.Get("ask_user"); !ok {
		t.Error("a daemon that can be answered is not offered ask_user")
	}
}

// Whether anybody can be asked is a property of the process, not of the mode it started in.
//
// This was read off the startup permission mode, and the mode changes at runtime — the terminal
// cycles it, the console has a control for it. A daemon started on the default "allow" and
// switched to "ask" afterwards therefore ran with Interactive false under a policy that says ask,
// which is the one combination that resolves every gated call by policy WITHOUT asking: writing,
// commands and network all refused instantly, no prompt anywhere. It also left the daemon without
// ask_user or route_interjection while the interjection machinery went on naming the second one.
func TestADaemonCanBeAskedWhateverModeItStartedIn(t *testing.T) {
	for _, mode := range []string{"allow", "ask", "auto", "deny"} {
		answerable := answerableRun(true, mode)
		if nobodyCanAnswer(true, answerable) {
			t.Errorf("started on %q, a daemon reports that nobody can be asked", mode)
		}
		reg := builtin.NewRegistry()
		registerOrchestrationTools(reg, nobodyCanAnswer(true, answerable))
		for _, want := range []string{"ask_user", "route_interjection"} {
			if _, ok := reg.Get(want); !ok {
				t.Errorf("started on %q, a daemon has no %s", mode, want)
			}
		}
	}
	// A -p run with nothing able to attach still gets neither: the prompt would block on a
	// decision that cannot come, and an unusable tool is weight on every request.
	if !nobodyCanAnswer(true, false) {
		t.Error("a headless run with no daemon claims somebody can be asked")
	}
}
