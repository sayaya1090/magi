package main

import "github.com/sayaya1090/magi/internal/adapter/tool/builtin"

// registerOrchestrationTools adds the multi-agent orchestration tools (D9 bundled policy).
// The set itself lives beside the registry, so every caller that builds a working one gets the
// same tools and no copy can quietly fall behind.
func registerOrchestrationTools(reg *builtin.Registry, headless bool) {
	builtin.RegisterOrchestration(reg, headless)
}

// applyCouncilAvailability withdraws the council tool when this run has no council.
//
// With none configured there is nobody to ask and nobody to declare completion to, so the tool can
// only ever answer "no council is configured for this run". Advertising a tool that will refuse
// every call is how an agent spends steps discovering a door is painted on — the same drift the
// permission allowlist had, where the volatile prompt advertised tools the allowlist would deny and
// the model called them until the loop guard killed the run. The name stays in KnownNames, because
// policy code elsewhere still has to recognise it; only the offer goes away.
func applyCouncilAvailability(reg *builtin.Registry, available bool) {
	if !available {
		reg.Unregister("council")
	}
}
