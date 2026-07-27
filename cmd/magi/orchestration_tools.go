package main

import "github.com/sayaya1090/magi/internal/adapter/tool/builtin"

// registerOrchestrationTools adds the multi-agent orchestration tools (D9 bundled policy).
// The set itself lives beside the registry, so every caller that builds a working one gets the
// same tools and no copy can quietly fall behind.
func registerOrchestrationTools(reg *builtin.Registry, headless bool) {
	builtin.RegisterOrchestration(reg, headless)
}
