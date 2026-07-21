package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// toolCallEv builds a PartAppended tool-call event for childWaitMajority.
func toolCallEv(name, command string) event.Event {
	args := []byte(`{}`)
	if command != "" {
		args, _ = json.Marshal(map[string]string{"command": command})
	}
	d, _ := json.Marshal(event.PartAppendedData{
		MessageID: "m_x", Role: session.RoleAssistant,
		Part: session.Part{ID: "p_x", Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: "c_x", Name: name, Args: args}},
	})
	return event.Event{Type: event.TypePartAppended, Actor: event.Actor{Kind: event.ActorAgent, ID: "default"}, Data: d}
}

// childWaitMajority: a subagent whose recent calls are mostly waits/polls (the boot-wait case)
// reads as waiting, not churn; a working or churning subagent does not.
func TestChildWaitMajority(t *testing.T) {
	// bash_output polling a background job + a sleep-poll → wait-dominated.
	waiting := []event.Event{
		toolCallEv("bash_output", ""),
		toolCallEv("bash", "sleep 5"),
		toolCallEv("bash_output", ""),
	}
	if !childWaitMajority(waiting, 8) {
		t.Error("a poll/sleep-dominated window must read as waiting")
	}
	// wait_for is a poll too.
	if !childWaitMajority([]event.Event{toolCallEv("wait_for", ""), toolCallEv("wait_for", "")}, 8) {
		t.Error("wait_for calls must read as waiting")
	}
	// Real work (edits, varied builds) → NOT a wait, judge as usual.
	working := []event.Event{
		toolCallEv("edit", ""),
		toolCallEv("bash", "make world"),
		toolCallEv("read", ""),
		toolCallEv("bash", "gcc -c foo.c"),
	}
	if childWaitMajority(working, 8) {
		t.Error("a working window must NOT read as waiting")
	}
	// A lone poll is too little evidence — don't extend on one call.
	if childWaitMajority([]event.Event{toolCallEv("bash_output", "")}, 8) {
		t.Error("a single call must not trigger the wait majority")
	}
	// Mixed but poll-minority (one poll among real work) → not a wait.
	mixed := []event.Event{toolCallEv("bash_output", ""), toolCallEv("edit", ""), toolCallEv("bash", "make")}
	if childWaitMajority(mixed, 8) {
		t.Error("a poll-minority window must not read as waiting")
	}
}

func TestSubagentWaitLeaseDefault(t *testing.T) {
	if !subagentWaitLeaseEnabled() {
		t.Fatal("default must be ON")
	}
	t.Setenv("MAGI_SUBAGENT_WAIT_LEASE", "0")
	if subagentWaitLeaseEnabled() {
		t.Error("=0 must disable")
	}
}
