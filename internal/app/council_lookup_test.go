package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// evPrompt / evToolCall / evToolResult build the minimal event.Event values that the
// council-gate evidence helpers (turnToolEvidence, unverifiedLookup) consume.
func evPrompt() event.Event { return event.Event{Type: event.TypePromptSubmitted} }

func evToolCall(callID, name string) event.Event {
	d, _ := json.Marshal(event.PartAppendedData{Part: session.Part{
		Kind:     session.PartToolCall,
		ToolCall: &session.ToolCall{CallID: callID, Name: name},
	}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

func evToolResult(callID, content string, isErr bool) event.Event {
	c, _ := json.Marshal(content)
	d, _ := json.Marshal(event.PartAppendedData{Part: session.Part{
		Kind:       session.PartToolResult,
		ToolResult: &session.ToolResult{CallID: callID, Content: c, IsError: isErr},
	}})
	return event.Event{Type: event.TypePartAppended, Data: d}
}

// TestUnverifiedLookup pins the N14 detector contract: fire only when a knowledge
// lookup failed this turn AND nothing recovered it, so the council is warned that a
// premise the deliverable rests on may be an unverified guess.
func TestUnverifiedLookup(t *testing.T) {
	fail := func(id string) []event.Event {
		return []event.Event{evToolCall(id, "websearch"), evToolResult(id, "search (duckduckgo) failed: x509: certificate signed by unknown authority", true)}
	}
	ok := func(id, tool string) []event.Event {
		return []event.Event{evToolCall(id, tool), evToolResult(id, "some result", false)}
	}
	concat := func(groups ...[]event.Event) []event.Event {
		var out []event.Event
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}

	cases := []struct {
		name string
		evs  []event.Event
		want bool // want a non-empty signal
	}{
		{
			name: "all lookups fail, none recovered → fire",
			evs:  concat([]event.Event{evPrompt()}, fail("w1"), fail("w2"), ok("b1", "bash")),
			want: true,
		},
		{
			name: "failed lookup then a later lookup SUCCEEDS → recovered, silent",
			evs:  concat([]event.Event{evPrompt()}, fail("w1"), ok("w2", "websearch")),
			want: false,
		},
		{
			name: "webfetch failure also counts",
			evs: concat([]event.Event{evPrompt()},
				[]event.Event{evToolCall("f1", "webfetch"), evToolResult("f1", "fetch failed: x509", true)}),
			want: true,
		},
		{
			name: "no lookup at all → silent",
			evs:  concat([]event.Event{evPrompt()}, ok("b1", "bash"), ok("r1", "read")),
			want: false,
		},
		{
			name: "successful lookup only → silent",
			evs:  concat([]event.Event{evPrompt()}, ok("w1", "websearch")),
			want: false,
		},
		{
			name: "failure was a PRIOR turn; new prompt resets → silent",
			evs:  concat([]event.Event{evPrompt()}, fail("w1"), []event.Event{evPrompt()}, ok("b1", "bash")),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unverifiedLookup(tc.evs)
			if (got != "") != tc.want {
				t.Fatalf("unverifiedLookup fire=%v, want %v\ndetail=%q", got != "", tc.want, got)
			}
			if tc.want {
				if !strings.Contains(got, "UNVERIFIED") {
					t.Errorf("signal detail should flag the fact as UNVERIFIED, got %q", got)
				}
				if !strings.Contains(got, "websearch") && !strings.Contains(got, "webfetch") {
					t.Errorf("signal detail should name the failed lookup tool, got %q", got)
				}
			}
		})
	}
}
