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
