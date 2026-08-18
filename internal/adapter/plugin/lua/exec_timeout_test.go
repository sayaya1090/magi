package lua

import (
	"testing"
	"time"
)

// The 60s exec bound was sized for probes, and the first plugin whose whole purpose is running a
// slow CLI (a model backend) met it as a wall: an Opus turn is not a sixty-second command. The
// manifest is where that need is declared — next to the exec: permission an auditor already reads.
func TestTheManifestCanWidenTheExecBoundWithinTheClamp(t *testing.T) {
	quiet := func(string) {}
	for raw, want := range map[string]time.Duration{
		"5m":   5 * time.Minute,
		"90s":  90 * time.Second,
		"":     0,                // absent: the bridge default stands
		"1ms":  time.Second,      // floor — 0 must not come to mean "no exec"
		"300m": 10 * time.Minute, // cap — a typo must not become an unkillable hang
	} {
		if got := manifestExecTimeout(raw, quiet, "p"); got != want {
			t.Errorf("exec_timeout %q resolved to %v, want %v", raw, got, want)
		}
	}
	// Unreadable is fallback PLUS a log line: a silently ignored declaration is how a plugin
	// ships believing it has five minutes and dies at sixty seconds in production.
	said := ""
	if got := manifestExecTimeout("soon", func(s string) { said = s }, "p"); got != 0 {
		t.Errorf("an unreadable exec_timeout resolved to %v instead of the default", got)
	}
	if said == "" {
		t.Error("an unreadable exec_timeout was ignored without a word")
	}
}
