package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A read-only spec cannot write, edit, or run anything, so the "you produced no deliverable — act
// now" nudge names an action its tools forbid. specCanAct is what keeps the nudge off it.
func TestSpecCanAct(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tools []string
		want  bool
	}{
		{"unrestricted (nil allowlist = all tools)", nil, true},
		{"the read-only repository explorer", []string{"read", "grep", "glob", "list", "findcontext"}, false},
		{"read-only plus a writer", []string{"read", "grep", "write"}, true},
		{"read-only plus a shell", []string{"read", "grep", "bash"}, true},
		{"edit only", []string{"edit"}, true},
		{"multiedit only", []string{"multiedit"}, true},
	} {
		if got := specCanAct(AgentSpec{Name: "x", Tools: tc.tools}); got != tc.want {
			t.Errorf("%s: specCanAct = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The explorer's contract is `path — fact` lines, so findings that name no file are empty for this
// pass and earn one re-ask. The detector has to survive prose: the live failure's note was several
// hundred chars of code commentary with `//` comments and line numbers, and named no file at all.
func TestMentionsFilePath(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"a real path", "runtime/major_gc.c — the sweep loop lives here", true},
		{"a bare filename", "the entry point is server.py", true},
		{"a nested path with a dash", "src/some-pkg/parse_input.go — takes an io.Reader", true},
		{"analysis prose with no file", "Looking at the code more carefully:\n" +
			"```c\nif (FREE_HD(hd)) {\n    // ... merge logic ...\n    p += wh * Wosize_hd(hd);\n}\n```\n" +
			"The problem is at line 644. The fix: change it to include the current block. Let me make this fix:", false},
		{"an abbreviation is not a path", "report the facts, e.g. the ones you read", false},
		{"a version is not a path", "the installed dependency is at 1.73.0", false},
		{"empty", "", false},
	} {
		if got := mentionsFilePath(tc.text); got != tc.want {
			t.Errorf("%s: mentionsFilePath = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A guard stop leaves an error event in the child's log but does NOT set SpawnResult.Err, so without
// spawnStoppedBy a cut-off mid-analysis fragment is indistinguishable from a finished answer.
func TestSpawnStoppedBy(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	wd := t.TempDir()
	mk := func(t *testing.T, code, msg string) session.SessionID {
		t.Helper()
		sid := startSession(t, a, wd)
		if code != "" {
			d, _ := json.Marshal(event.ErrorData{Message: msg, Code: code})
			if _, err := a.store.Append(ctx, sid, event.Event{
				SessionID: sid, Type: event.TypeError,
				Actor: event.Actor{Kind: event.ActorSystem, ID: "loop"}, Data: d,
			}); err != nil {
				t.Fatal(err)
			}
		}
		return sid
	}
	for _, tc := range []struct{ code, want string }{
		{"loop_guard", "loop guard"},
		{"stall_guard", "stall guard"},
		{"spin_guard", "spin guard"},
		{"", ""},           // ended on its own
		{"tool_error", ""}, // an ordinary error is not a guard stop
	} {
		sid := mk(t, tc.code, "stopped: ...; recovery: plan-ineligible")
		if got := a.spawnStoppedBy(ctx, sid); got != tc.want {
			t.Errorf("code %q: spawnStoppedBy = %q, want %q", tc.code, got, tc.want)
		}
	}
	if got := a.spawnStoppedBy(ctx, ""); got != "" {
		t.Errorf("empty session id: want \"\", got %q", got)
	}
}
