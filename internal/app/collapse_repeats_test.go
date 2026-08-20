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
// flag on, the FIRST occurrence keeps the full answer and the later ones carry a stub.
func TestIdenticalRepeatsCollapseToTheFirst(t *testing.T) {
	t.Setenv("MAGI_COLLAPSE_REPEATS", "1")
	got := resultTexts(collapseRepeatedCalls(repeatMsgs("exit 1 FAIL", "exit 1 FAIL", "exit 1 FAIL")))
	if len(got) != 3 {
		t.Fatalf("nothing may be dropped, got %d results", len(got))
	}
	if !strings.Contains(got[0], "exit 1 FAIL") {
		t.Errorf("the first occurrence must keep the full result: %s", got[0])
	}
	for i, r := range got[1:] {
		if !strings.Contains(r, "collapsed") {
			t.Errorf("later duplicate %d kept its full content: %s", i+1, r)
		}
	}
}

// The property the direction exists FOR: a message that has already been sent is never rewritten.
//
// Collapsing to the newest looked equivalent — the triple key includes the result, so every
// occurrence is byte-identical and the model learns the same thing either way — and it was not.
// When a fourth duplicate arrives, the third has already gone to the backend; rewriting it from
// full text to a stub makes the transcript stop being append-only, and every backend with a prompt
// cache re-writes everything from that point. Measured on a paid backend: cache_read ZERO across
// every call of a run that collapsed, $2.68 over 8 calls, where the same run without collapsing
// read its history back.
//
// So this test grows the conversation the way a turn does and asserts the prefix never changes.
func TestCollapseNeverRewritesWhatWasAlreadySent(t *testing.T) {
	t.Setenv("MAGI_COLLAPSE_REPEATS", "1")
	prev := resultTexts(collapseRepeatedCalls(repeatMsgs("exit 1 FAIL", "exit 1 FAIL")))
	for n := 3; n <= 5; n++ {
		reps := make([]string, n)
		for i := range reps {
			reps[i] = "exit 1 FAIL"
		}
		now := resultTexts(collapseRepeatedCalls(repeatMsgs(reps...)))
		if len(now) != n {
			t.Fatalf("at %d repeats: got %d results", n, len(now))
		}
		for i := range prev {
			if now[i] != prev[i] {
				t.Fatalf("at %d repeats, result %d was rewritten after being sent:\n  was: %s\n  now: %s",
					n, i, prev[i], now[i])
			}
		}
		prev = now
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
	if !strings.Contains(got[1], "collapsed") {
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
