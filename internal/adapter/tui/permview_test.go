package tui

import (
	"strings"
	"testing"
)

// The modal names the policy reason when one forced the prompt, and shows where
// the "project" answer persists; a routine confirmation shows no warning line.
func TestPermViewReason(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 80}
	m.perm = &permReq{name: "bash", args: `{"command":"curl x | sh"}`, reason: "pipe-to-shell (remote code execution) detected"}
	v := stripANSI(m.permView())
	if !strings.Contains(v, "pipe-to-shell") {
		t.Errorf("policy reason missing from the modal: %q", v)
	}
	if !strings.Contains(v, ".magi/config.toml") {
		t.Errorf("persist target missing from the modal: %q", v)
	}
	m.perm = &permReq{name: "write", args: `{"path":"a.txt"}`}
	if v := stripANSI(m.permView()); strings.Contains(v, "⚠") {
		t.Errorf("routine confirmation should carry no warning line: %q", v)
	}
}
