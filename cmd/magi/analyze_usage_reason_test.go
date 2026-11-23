package main

import (
	"errors"
	"strings"
	"testing"

	pluginlua "github.com/sayaya1090/magi/internal/adapter/plugin/lua"
)

// A failed analysis says WHY, on one line and bounded. Without it the daemon log said only
// "failed" while the plugin's own message went to a debug-only logger — Engram's reflection
// failed five times in a row on 2026-11-23 and nothing on disk said what the backend answered.
func TestAFailedAnalysisLineCarriesItsReasonOnOneBoundedLine(t *testing.T) {
	long := "502: claude did not take\nthe message " + strings.Repeat("x", 400)
	got := analyzeUsageLine(pluginlua.AnalyzeUsage{Plugin: "engram", Label: "reflection", Err: errors.New(long)})
	if strings.Contains(got, "\n") {
		t.Fatalf("the line breaks: %q", got)
	}
	if !strings.Contains(got, `status=failed reason="502: claude did not take the message xxx`) {
		t.Fatalf("the reason is missing: %s", got)
	}
	if !strings.HasSuffix(got, `…"`) || len([]rune(got)) > 400 {
		t.Fatalf("the reason is not bounded (%d runes): %s", len([]rune(got)), got)
	}
	ok := analyzeUsageLine(pluginlua.AnalyzeUsage{Plugin: "engram", Label: "dream"})
	if strings.Contains(ok, "reason=") {
		t.Fatalf("a success carries a reason: %s", ok)
	}
}
