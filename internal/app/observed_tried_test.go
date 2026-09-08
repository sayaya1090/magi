package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

func triedCall(id, name, args string) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{
		Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(args)},
	}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func triedResult(id, text string, isErr, advisory bool) event.Event {
	c, _ := json.Marshal(text)
	d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleTool, Part: session.Part{
		Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: isErr, Advisory: advisory},
	}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func triedDenied(id string) event.Event {
	d, _ := json.Marshal(event.PermissionDecidedData{CallID: id, Decision: "deny"})
	return event.Event{Type: event.TypePermissionDecided, Data: d}
}

// An edit that RAN and did not take is a fact the record has to keep.
//
// It used to be dropped entirely, and dropping it is how a turn spends itself on the same wrong
// anchor: the reason a write failed lives in the tool result, which compaction takes away, and this
// block is what survives. `changed` still omits it — nothing is on disk — so it goes on its own
// line, which is also how a reader tells the two apart.
func TestARunWhoseEditFailedSaysSo(t *testing.T) {
	o := observeEvents([]event.Event{
		triedCall("c1", "edit", `{"path":"a.go","old":"nope","new":"x"}`),
		triedResult("c1", "old_string not found in a.go", true, false),
	}, nil)
	got := o.render()
	if !strings.Contains(got, "tried to write and it did not take: a.go") {
		t.Errorf("the record does not say the edit failed:\n%s", got)
	}
	if strings.Contains(got, "changed: a.go") {
		t.Errorf("a failed edit was reported as a change:\n%s", got)
	}
	if !strings.Contains(got, "changed: nothing") {
		t.Errorf("the record should still say nothing changed:\n%s", got)
	}
}

// A run whose every write failed has no commands and nothing changed — and that is the run most
// worth telling. The record used to render as empty, so nothing reached the reader at all.
func TestAFailOnlyRunStillRenders(t *testing.T) {
	o := observeEvents([]event.Event{
		triedCall("c1", "edit", `{"path":"a.go","old":"nope","new":"x"}`),
		triedResult("c1", "old_string not found", true, false),
	}, nil)
	if o.render() == "" {
		t.Error("a run that only failed to write rendered nothing")
	}
}

// Refused is not attempted. A call denied before it ran did not try anything, so putting it here
// would tell the reader to narrow an anchor that was never tested — the same defect the refused
// ledger test exists for, one line down.
func TestARefusedEditIsNotAnAttempt(t *testing.T) {
	o := observeEvents([]event.Event{
		triedCall("c1", "write", `{"path":"a.go","content":"x"}`), triedDenied("c1"),
		triedResult("c1", `write is unavailable in this headless run: permission mode "deny" cannot approve it`, true, false),
	}, nil)
	if got := o.render(); strings.Contains(got, "tried to write") {
		t.Errorf("a refused call was counted as an attempt:\n%s", got)
	}
}

// Counted per path, so a reader sees WHERE the run is stuck rather than only that something failed.
func TestRepeatedFailuresOnOnePathAreCounted(t *testing.T) {
	var evs []event.Event
	for _, id := range []string{"c1", "c2", "c3"} {
		evs = append(evs, triedCall(id, "edit", `{"path":"a.go","old":"nope","new":"x"}`),
			triedResult(id, "old_string not found", true, false))
	}
	if got := observeEvents(evs, nil).render(); !strings.Contains(got, "a.go ×3") {
		t.Errorf("three failures on one path are not counted:\n%s", got)
	}
}

// A landed write that then failed its diagnostics is IsError AND Advisory — the file DID change. It
// belongs in `changed`, and reporting it as an attempt as well would tell the reader to try again.
func TestALandedWriteThatFailedDiagnosticsIsNotAnAttempt(t *testing.T) {
	o := observeEvents([]event.Event{
		triedCall("c1", "write", `{"path":"a.go","content":"x"}`),
		triedResult("c1", "[diagnostics] a.go:1 undefined: x", true, true),
	}, nil)
	got := o.render()
	if !strings.Contains(got, "changed: a.go") {
		t.Errorf("a landed write was not recorded as a change:\n%s", got)
	}
	if strings.Contains(got, "tried to write") {
		t.Errorf("a landed write was also reported as a failed attempt:\n%s", got)
	}
}
