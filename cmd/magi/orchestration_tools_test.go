package main

import (
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// Headless runs omit the human-in-the-loop tools (nothing can answer them) but keep the
// orchestrator-internal ones; an interactive run registers everything.
func TestRegisterOrchestrationToolsHeadless(t *testing.T) {
	has := func(reg *builtin.Registry, name string) bool {
		_, ok := reg.Get(name)
		return ok
	}
	always := []string{"task", "ask", "report", "resolveconcern", "cancel_dispatch", "replan"}
	interactiveOnly := []string{"ask_user", "route_interjection"}

	headlessReg := builtin.NewRegistry()
	registerOrchestrationTools(headlessReg, true)
	for _, n := range always {
		if !has(headlessReg, n) {
			t.Errorf("headless must still register %q", n)
		}
	}
	for _, n := range interactiveOnly {
		if has(headlessReg, n) {
			t.Errorf("headless must omit human-in-the-loop tool %q", n)
		}
	}

	interactiveReg := builtin.NewRegistry()
	registerOrchestrationTools(interactiveReg, false)
	for _, n := range append(always, interactiveOnly...) {
		if !has(interactiveReg, n) {
			t.Errorf("interactive must register %q", n)
		}
	}
}
