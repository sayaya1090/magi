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
