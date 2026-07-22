package tui

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A subagent's permission request must surface in the shared modal tagged with the CHILD sid, so
// the user's decision routes back to the child's blocked call — not the main turn. Before this,
// child permission events reached only the pane transcript and the worker hung until its lease.
func TestSurfaceChildPromptRoutesToChildSID(t *testing.T) {
	m := &Model{sid: "s_main"}
	child := session.SessionID("s_child")
	d, _ := json.Marshal(event.PermissionRequestedData{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"ssh host ls"}`), Reason: "egress"})
	m.surfaceChildPrompt(child, event.Event{Type: event.TypePermissionRequested, Data: d})

	if m.perm == nil {
		t.Fatal("child permission must surface a modal")
	}
	if m.perm.sid != child {
		t.Errorf("modal sid = %q, want the child sid %q (so respond routes to the child)", m.perm.sid, child)
	}
	if m.perm.callID != "c1" || m.perm.name != "bash" {
		t.Errorf("modal fields = %+v", m.perm)
	}

	// A non-permission child event must NOT hijack the modal.
	m.perm = nil
	m.surfaceChildPrompt(child, event.Event{Type: event.TypePartDelta})
	if m.perm != nil {
		t.Error("a non-permission child event must not open the modal")
	}
}
