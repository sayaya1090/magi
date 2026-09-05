package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// normEq compares two strings for equality after collapsing every run of whitespace to a single space
// (and trimming) — it is what the termination gate's idle-resubmit short-circuit uses to decide the
// agent reprinted "the same answer". A wrong normalization would either finish UNVERIFIED prematurely
// (false equal) or burn a council round re-deliberating an identical reply (false unequal), so lock it.
func TestClipLine(t *testing.T) {
	if got := clipLine("short", 10); got != "short" {
		t.Errorf("clipLine under the limit must be unchanged, got %q", got)
	}
	if got := clipLine("hello", 5); got != "hello" {
		t.Errorf("clipLine at exactly the limit must be unchanged, got %q", got)
	}
	if got := clipLine("hello world", 5); got != "hello…" {
		t.Errorf("clipLine over the limit must clip + ellipsis, got %q", got)
	}
	// "héllo": h(1 byte) é(2 bytes: 0xC3 0xA9) l l o. Cutting at byte 2 lands inside é, so it must back
	// up to byte 1 (the rune boundary) → "h…", never a split "h\xC3…".
	if got := clipLine("héllo", 2); got != "h…" {
		t.Errorf("clipLine must not split a multibyte rune, got %q", got)
	}
}

// The council judges against the skills the agent read, so their bodies must reach it whole:
// every `skill` result of the session (not just this turn), latest reading per skill, error
// results and other tools ignored, and a clip that names what did not fit.
func TestGuidanceReadCollectsSkillBodies(t *testing.T) {
	call := func(id, name string, args string) event.Event {
		d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(args)}}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	result := func(id, text string, isErr bool) event.Event {
		c, _ := json.Marshal(text)
		d, _ := json.Marshal(event.PartAppendedData{Role: session.RoleTool, Part: session.Part{
			Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: id, Content: c, IsError: isErr}}})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	prompt := event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser}}
	evs := []event.Event{
		call("c1", "skill", `{"name":"deck-design"}`), result("c1", "# deck-design v1\nrender each page", false),
		call("c2", "read", `{"path":"x"}`), result("c2", "file body that is NOT guidance", false),
		prompt, // a new turn does not forget a skill read earlier
		call("c3", "skill", `{"name":"research"}`), result("c3", "", true), // refused skill: no body
		call("c4", "skill", `{"name":"deck-design"}`), result("c4", "# deck-design v2\nrender each page", false),
		call("c5", "skill", `{"name":"visual-deck"}`), result("c5", "visual rules", false),
	}
	got := guidanceRead(evs, 1000, 10000)
	for _, want := range []string{"## skill deck-design\n# deck-design v2", "## skill visual-deck\nvisual rules"} {
		if !strings.Contains(got, want) {
			t.Errorf("guidance must carry %q, got:\n%s", want, got)
		}
	}
	for _, no := range []string{"v1", "NOT guidance", "## skill research"} {
		if strings.Contains(got, no) {
			t.Errorf("guidance must not carry %q (superseded / other tool / refused), got:\n%s", no, got)
		}
	}
	if strings.Index(got, "deck-design") > strings.Index(got, "visual-deck") {
		t.Errorf("skills must keep first-read order, got:\n%s", got)
	}
	if guidanceRead(evs[1:2], 1000, 10000) != "" {
		t.Error("a result whose call was not a skill must produce no guidance")
	}
	// Caps: the per-skill clip says how long the skill really is; a skill that does not fit
	// the total is named, not silently dropped.
	clipped := guidanceRead(evs, 12, 10000)
	if !strings.Contains(clipped, "[clipped: skill deck-design is 37 bytes]") {
		t.Errorf("per-skill clip must name the skill and its size, got:\n%s", clipped)
	}
	tight := guidanceRead(evs, 1000, 60)
	if !strings.Contains(tight, "did not fit: visual-deck") || strings.Contains(tight, "visual rules") {
		t.Errorf("a skill over the total cap must be named as omitted, got:\n%s", tight)
	}
}
