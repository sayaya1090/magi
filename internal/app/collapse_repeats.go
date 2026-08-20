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
// model must see. Every tool_call keeps a paired result, so the wire stays valid for providers
// that check the pairing.
//
// The FIRST occurrence keeps its full content and the later ones are stubbed — not the other way
// round, which is how this shipped and what it cost. Keeping the newest full means that when a
// fourth duplicate arrives, the third (already sent, already cached) is rewritten from full text
// to a stub: the transcript stops being append-only, and every backend with a prompt cache has to
// re-write everything from that point on. Measured on a paid backend, an agent run that collapsed
// repeats read ZERO tokens from cache across every call and paid the full re-write each turn
// ($2.68 over 8 calls); the same run with collapsing off read its history back.
//
// Nothing is lost by the flip, and that is guaranteed by the key rather than argued: the triple is
// name + args + RESULT CONTENT, so every occurrence in a group is byte-identical and which one is
// kept cannot change what the model learns. It arguably reads better this way — the recent end of
// the transcript, which dominates sampling, now carries "you already ran this and it did not
// change" instead of another full copy of the thing being repeated.
func collapseRepeatsEnabled() bool { return envflag.Enabled("MAGI_COLLAPSE_REPEATS", true) }

const collapsedRepeatStub = "[this exact call already ran earlier in this transcript and returned " +
	"the IDENTICAL result — collapsed here; the full output is at its first occurrence above. " +
	"Repeating it again will not change the answer.]"

// collapseRepeatedCalls rewrites msgs so that LATER duplicates of an identical (tool, args,
// result) triple carry a stub instead of the full result. The message list is not reordered and
// nothing is dropped. Later, not earlier: a message that has already been sent is never rewritten,
// which is what keeps a backend's prompt cache alive.
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
		for _, at := range occ[1:] { // all but the FIRST — see the flip note above
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
