package app

import (
	"strings"
	"testing"
)

// /routine-trace and /routine-draft read the conversation the command was typed in. They used to read
// whichever conversation last finished a tool call, so a command typed in tab A after tab B had
// worked showed, and drafted from, B's calls.
func TestARoutineCommandReadsTheConversationItWasTypedIn(t *testing.T) {
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.fire("tool_finished", "tool", "grep", "ok", "true", "call_id", "a1", "order_known", "true")
	r.fire("tool_finished", "session", "s_other", "tool", "read", "ok", "true", "call_id", "b1", "order_known", "true")
	out, handled, err := r.host.RunCommand(string(r.sid), "routine-trace", nil)
	if !handled || err != nil {
		t.Fatalf("routine-trace: %v %v", handled, err)
	}
	if !strings.Contains(out, " grep") || strings.Contains(out, " read") {
		t.Fatalf("the trace shown is not this conversation's:\n%s", out)
	}
	_, _, err = r.host.RunCommand("s_other", "routine-draft", []string{"probe", "1-1"})
	if err != nil {
		t.Fatalf("drafting s_other's own call 1 (read): %v", err)
	}
	if _, _, err = r.host.RunCommand(string(r.sid), "routine-draft", []string{"probe2", "2-2"}); err == nil || !strings.Contains(err.Error(), "not all in the trace") {
		t.Fatalf("s_other's call #2 does not exist; a draft for this conversation must not find it: %v", err)
	}
}
