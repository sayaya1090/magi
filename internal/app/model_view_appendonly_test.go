package app

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The model view is APPEND-ONLY. What a person reads and what the model is sent are two different
// renderings of one log (rebuild's `whole` flag), and only the person's may reorder, decorate or
// fold — because a prompt cache matches from the start, so rewriting anything already sent throws
// away every token after it.
//
// This has now been rediscovered twice, each time from a bill rather than from a test:
//
//	bc3f7a99  repeat-collapse restubbed an already-sent result. Measured on a paid backend:
//	          cache_read ZERO across every call of the run, $2.68 over 8 calls.
//	8b5f8c47  the per-step block moved out of the system prompt to keep IT stable, and what was
//	          left was called small without anyone measuring it.
//
// The first of those was fixed by collapsing forward and then, when it turned out to match only a
// byte-identical triple, removed outright — but a guard on one deleted function would have gone
// with it. This pins the PROPERTY, over the whole model view, so the next thing that edits history
// in place fails here rather than on an invoice.
//
// Two exceptions are sanctioned, both deliberate trades recorded as facts in the log: compaction
// (a fold replaces older turns with a summary) and result elision (one bulky, digested, recent
// tool result becomes a stub — the cut that costs the prefix least). This grows a log containing
// neither.
func TestModelViewOnlyEverAppends(t *testing.T) {
	part := func(role session.Role, p session.Part) event.Event {
		d, _ := json.Marshal(event.PartAppendedData{Role: role, Part: p})
		return event.Event{Type: event.TypePartAppended, Data: d}
	}
	call := func(id, name, args string) event.Event {
		return part(session.RoleAssistant, session.Part{
			Kind:     session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: id, Name: name, Args: json.RawMessage(args)},
		})
	}
	result := func(id, out string) event.Event {
		return part(session.RoleTool, session.Part{
			Kind:       session.PartToolResult,
			ToolResult: &session.ToolResult{CallID: id, Content: json.RawMessage(out)},
		})
	}

	// The view as buildStepRequest actually assembles it.
	view := func(evs []event.Event) []string { return render(reconstruct(evs)) }

	evs := []event.Event{userPromptEvt(t, "m1", "fix the build")}
	prev := view(evs)
	for step := 0; step < 12; step++ {
		id := "c" + string(rune('a'+step))
		// The same call and the same failure over and over: the shape that made collapse rewrite
		// history, and the shape a stuck model actually produces.
		evs = append(evs, call(id, "bash", `{"command":"make"}`), result(id, `"exit 1 FAIL"`))
		now := view(evs)
		if len(now) < len(prev) {
			t.Fatalf("step %d: the model view SHRANK, %d entries → %d", step, len(prev), len(now))
		}
		for i := range prev {
			if now[i] != prev[i] {
				t.Fatalf("step %d: entry %d was rewritten after it had been sent\n  was: %q\n  now: %q",
					step, i, prev[i], now[i])
			}
		}
		prev = now
	}
}

// render flattens the model view to one comparable string per message part, which is the
// granularity a prompt cache actually sees.
func render(msgs []session.Message) []string {
	var out []string
	for _, m := range msgs {
		for _, p := range m.Parts {
			s := string(m.Role) + "|" + string(p.Kind) + "|" + p.Text
			if p.ToolCall != nil {
				s += "|" + p.ToolCall.Name + "|" + string(p.ToolCall.Args)
			}
			if p.ToolResult != nil {
				s += "|" + string(p.ToolResult.Content)
			}
			out = append(out, s)
		}
	}
	return out
}
