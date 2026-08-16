package app

import (
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/envflag"
)

// H2 — the repeated-failure attractor. A model that re-runs the same call and gets the same answer
// sees the identical (call, result) pair stack up in its context; on a weaker model that pile is a
// gravity well — the transcript itself testifies that this is what the turn does, and the next
// step samples more of it. Council-side the same shape is already collapsed (evidence
// supersession); this is the MODEL-side twin. Default ON: on a transcript with no identical
// repeats it rewrites nothing at all, so the only turns it changes are the ones already
// circling. MAGI_COLLAPSE_REPEATS=0 turns it off.
//
// Only an identical result is collapsed: the same call returning something NEW is progress the
// model must see. The newest occurrence keeps its full content — current state stays current — and
// the older results are replaced in place, so every tool_call keeps a paired result and the wire
// stays valid for providers that check the pairing.
func collapseRepeatsEnabled() bool { return envflag.Enabled("MAGI_COLLAPSE_REPEATS", true) }

const collapsedRepeatStub = "[this exact call ran again later in this transcript and returned the " +
	"IDENTICAL result — collapsed here; read the latest occurrence below. Repeating it again will " +
	"not change the answer.]"

// collapseRepeatedCalls rewrites msgs so that older duplicates of an identical (tool, args,
// result) triple carry a stub instead of the full result. The message list is not reordered and
// nothing is dropped.
func collapseRepeatedCalls(msgs []session.Message) []session.Message {
	if !collapseRepeatsEnabled() {
		return msgs
	}
	// First pass: what was asked (callID → name+args key), and where each result lives.
	callKey := map[string]string{}
	type resultAt struct{ mi, pi int }
	byTriple := map[string][]resultAt{} // key + result content -> occurrences in order
	for mi, m := range msgs {
		for pi, p := range m.Parts {
			switch {
			case p.Kind == session.PartToolCall && p.ToolCall != nil:
				callKey[p.ToolCall.CallID] = p.ToolCall.Name + "\x00" + string(p.ToolCall.Args)
			case p.Kind == session.PartToolResult && p.ToolResult != nil:
				k, ok := callKey[p.ToolResult.CallID]
				if !ok {
					continue
				}
				triple := k + "\x00" + string(p.ToolResult.Content)
				byTriple[triple] = append(byTriple[triple], resultAt{mi, pi})
			}
		}
	}
	stub, _ := json.Marshal(collapsedRepeatStub)
	out := msgs
	copied := false
	for _, occ := range byTriple {
		if len(occ) < 2 {
			continue
		}
		if !copied {
			// Copy-on-write: messages/parts are shared with reconstruct's output.
			out = make([]session.Message, len(msgs))
			copy(out, msgs)
			copied = true
		}
		for _, at := range occ[:len(occ)-1] { // all but the newest
			parts := make([]session.Part, len(out[at.mi].Parts))
			copy(parts, out[at.mi].Parts)
			tr := *parts[at.pi].ToolResult
			tr.Content = stub
			parts[at.pi].ToolResult = &tr
			out[at.mi].Parts = parts
		}
	}
	return out
}
