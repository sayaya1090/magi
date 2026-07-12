package main

import (
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/config"
)

// subagentTimeoutFrom: env wins over config, both parse Go durations, junk and
// non-positive values fall through, all-empty → 0 (app default).
func TestSubagentTimeoutFrom(t *testing.T) {
	t.Setenv("MAGI_SUBAGENT_TIMEOUT", "")
	if got := subagentTimeoutFrom(config.OrchestrationConfig{}); got != 0 {
		t.Fatalf("unset: want 0, got %v", got)
	}
	if got := subagentTimeoutFrom(config.OrchestrationConfig{SubagentTimeout: "90s"}); got != 90*time.Second {
		t.Fatalf("config: want 90s, got %v", got)
	}
	if got := subagentTimeoutFrom(config.OrchestrationConfig{SubagentTimeout: "banana"}); got != 0 {
		t.Fatalf("junk config: want 0, got %v", got)
	}
	if got := subagentTimeoutFrom(config.OrchestrationConfig{SubagentTimeout: "-5m"}); got != 0 {
		t.Fatalf("negative config: want 0, got %v", got)
	}
	t.Setenv("MAGI_SUBAGENT_TIMEOUT", "2m")
	if got := subagentTimeoutFrom(config.OrchestrationConfig{SubagentTimeout: "90s"}); got != 2*time.Minute {
		t.Fatalf("env should win over config: want 2m, got %v", got)
	}
	t.Setenv("MAGI_SUBAGENT_TIMEOUT", "junk")
	if got := subagentTimeoutFrom(config.OrchestrationConfig{SubagentTimeout: "90s"}); got != 90*time.Second {
		t.Fatalf("junk env should fall through to config: want 90s, got %v", got)
	}
}
