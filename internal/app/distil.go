package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/envflag"
)

// One question, asked once, at the moment the work is accepted.
//
// A process that ends with the session has nothing to carry, so nothing here mattered. One that
// stays up for weeks solves many problems, and whether the next one starts better than the last
// depends entirely on what survived — which today is whatever the agent happened to save on its
// own. Measured across every recorded run: `remember` appears in 60 of 948 task logs and
// `recall_memory` in 40. Six per cent is not a learning organisation, it is an occasional note.
//
// So the finish seam asks. The council has just accepted, the work is done and the whole turn is
// still in context — the one moment where "what was worth learning here" is both answerable and
// cheap, because everything needed to answer it is already loaded.
//
// WHAT IT DOES NOT DO, and this is the design:
//
//   - It does not write anything itself. magi inventing a memory on the agent's behalf would put
//     text the model never chose into every future retrieval, and nobody could tell later which
//     lessons were learned and which were manufactured. The agent answers by calling `remember`,
//     or by saying there is nothing.
//   - It does not insist. "Nothing worth keeping" is the right answer most of the time and the
//     prompt says so, because a question that expects a yes gets one.
//   - It asks ONCE per turn. The reminder loop beside it is bounded because an unbounded one holds
//     a session open forever; this is bounded for the same reason, and by a flag rather than a
//     count because one ask is all that makes sense.
//
// OFF by default (MAGI_DISTIL=1). It costs one model round trip per completed task, and this tree
// has measured that time-to-first-token is most of its wall clock — so it goes behind a flag and
// gets compared, like every other lever here. The scenario that will measure it needs the store to
// have something in it; that is the whole reason this exists before that scenario.

// distilPrompt is what the agent is asked. It names the store's two shapes, because "save
// something" without them produces a paragraph nobody can retrieve.
const distilPrompt = "The council accepted this work, so the task is done and this turn is ending.\n\n" +
	"Before it does: is there anything here worth knowing at the START of a future task in this " +
	"repository — something you had to discover the hard way, that the code does not say plainly?\n\n" +
	"- A pitfall, a required step, a convention that is not written down: save it with `remember` " +
	"as a MEMORY (scope \"project\" for this repository, \"global\" for anywhere).\n" +
	"- STRUCTURE you mapped — how a system works, what owns what, where a thing lives — that the " +
	"other companions working here need CURRENT: write it to the shared wiki with " +
	"`remember{page:\"<topic>\", …}`. If the wiki index already shows a page for the topic, update " +
	"that page (your text becomes its new full body) rather than minting a near-duplicate title.\n" +
	"- A procedure you would follow again the same way: save it as a SKILL, with a name someone " +
	"would search for.\n\n" +
	"Write it so it is useful to someone who does NOT have this conversation — name the file, the " +
	"command, the exact error. A note that only makes sense next to today's transcript is worth " +
	"nothing tomorrow.\n\n" +
	"Most turns have nothing. If this one does not, say so in one line and stop — an invented " +
	"lesson is worse than no lesson, because it will be retrieved as though it were true."

// distilEnabled reports whether the finish seam asks. See the note above for why it is off.
func distilEnabled() bool { return envflag.Enabled("MAGI_DISTIL", false) }

// askToDistil puts the question to the agent and keeps the turn open for its answer.
//
// Returns false when there is nothing to ask about: the flag is off, the turn is a child's (a child
// answers to the tool that spawned it, and its lessons belong to whatever that tool decides to keep
// — magi asking it directly would put entries in the store that no parent ever saw), the agent has
// no way to save anything, or it has already been asked this turn.
func (a *App) askToDistil(ctx context.Context, tc turnCtx, ts *turnState) bool {
	switch {
	case !distilEnabled(), ts.distilAsked, tc.depth != 0:
		return false
	case a.cfg.Experience == nil:
		return false // nowhere to put an answer; asking would waste a round trip on a dead end
	case !tc.agent.allows("remember"):
		// The same defect the shared-experience pointer had: handing an agent a question it is
		// not permitted to answer, and then refusing the tool when it tries.
		return false
	}
	ts.distilAsked = true
	// Same reason the rating gate does it: this prompt asks for `remember` (or `skill`), and a
	// turn that has declared itself finished drops its tool calls. Off by default, which is why
	// nobody had seen this one fail.
	ts.allowAtFinish("remember", "skill")
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: distilPrompt}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, pd)
	return true
}
