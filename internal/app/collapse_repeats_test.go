package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func repeatMsgs(results ...string) []session.Message {
	var msgs []session.Message
	msgs = append(msgs, session.Message{Role: session.RoleUser,
		Parts: []session.Part{{Kind: session.PartText, Text: "fix it"}}})
	for i, r := range results {
		id := string(rune('a' + i))
		msgs = append(msgs,
			session.Message{Role: session.RoleAssistant, Parts: []session.Part{{
				Kind: session.PartToolCall, ToolCall: &session.ToolCall{
					CallID: id, Name: "bash", Args: json.RawMessage(`{"command":"go test"}`)}}}},
			session.Message{Role: session.RoleTool, Parts: []session.Part{{
				Kind: session.PartToolResult, ToolResult: &session.ToolResult{
					CallID: id, Content: json.RawMessage(`"` + r + `"`), IsError: true}}}},
		)
	}
	return msgs
}

func resultTexts(msgs []session.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == session.PartToolResult && p.ToolResult != nil {
				out = append(out, string(p.ToolResult.Content))
			}
		}
	}
	return out
}

// The identical (call, result) pair stacked three deep is the loop's own gravity well: the
// transcript testifies this is what the turn does, and the next step samples more of it. With the
// flag on, older duplicates carry a stub and only the newest keeps the full answer.
func TestIdenticalRepeatsCollapseToTheNewest(t *testing.T) {
	t.Setenv("MAGI_COLLAPSE_REPEATS", "1")
	got := resultTexts(collapseRepeatedCalls(repeatMsgs("exit 1 FAIL", "exit 1 FAIL", "exit 1 FAIL")))
	if len(got) != 3 {
		t.Fatalf("nothing may be dropped, got %d results", len(got))
	}
	for i, r := range got[:2] {
		if !strings.Contains(r, "collapsed") {
			t.Errorf("older duplicate %d kept its full content: %s", i, r)
		}
	}
	if !strings.Contains(got[2], "exit 1 FAIL") {
		t.Errorf("the newest occurrence must keep the full result: %s", got[2])
	}
}

// A repeat that returned something NEW is progress, not a loop — untouched.
func TestADifferingResultIsNotCollapsed(t *testing.T) {
	t.Setenv("MAGI_COLLAPSE_REPEATS", "1")
	got := resultTexts(collapseRepeatedCalls(repeatMsgs("exit 1 FAIL", "exit 0 ok")))
	for _, r := range got {
		if strings.Contains(r, "collapsed") {
			t.Errorf("a differing result was collapsed: %v", got)
		}
	}
}

// Default ON — unset collapses; MAGI_COLLAPSE_REPEATS=0 is the off switch, and it must actually
// switch off.
func TestCollapseIsOnByDefaultAndSwitchable(t *testing.T) {
	t.Setenv("MAGI_COLLAPSE_REPEATS", "")
	got := resultTexts(collapseRepeatedCalls(repeatMsgs("exit 1 FAIL", "exit 1 FAIL")))
	if !strings.Contains(got[0], "collapsed") {
		t.Fatalf("default must collapse an identical repeat: %v", got)
	}
	t.Setenv("MAGI_COLLAPSE_REPEATS", "0")
	got = resultTexts(collapseRepeatedCalls(repeatMsgs("exit 1 FAIL", "exit 1 FAIL")))
	for _, r := range got {
		if strings.Contains(r, "collapsed") {
			t.Fatalf("collapse ran with the flag off: %v", got)
		}
	}
}
