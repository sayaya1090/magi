package app

import (
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A worker runs in a FRESH session: the only thing it knows is the prompt written for it. Everything
// the parent established — what earlier steps produced, what the review council could not settle,
// what a read-only exploration found in the real repository — lives in the PARENT's window and
// reaches the worker only if something copies it in. Each of those was added one at a time, at the
// call site, as `if x != "" { brief = brief + "\n\n" + x }`; and because delegate and refine each
// assembled their own list, the two hand-offs drifted apart silently — a block added to one was
// simply absent from the other, with nothing in either place to say so.
//
// So the set is a LIST, in one place, and every hand-off renders the same list in the same order.
// Adding a block is adding an entry; a hand-off cannot forget one, because it no longer names them.
//
// Two properties every block must hold, both learned the hard way:
//
//   - VERBATIM and AFTER curation. The curator paraphrases, and a paraphrased path or identifier is
//     the defect these blocks exist to prevent (the shared ledger was moved here for exactly that).
//   - Silent when empty. A header with nothing under it reads to a weak model as "this is empty on
//     purpose", which is a claim none of these can make.

// workerContextBlock is one named block of parent context appended to a worker's prompt. The name is
// for the test that holds this list against the hand-offs, and for reading a diff.
type workerContextBlock struct {
	name   string
	render func(a *App, sid session.SessionID) string
}

// workerContextBlocks is the whole set, in prompt order: what exists already, what the repository
// really contains, then the caveat the council left open. Facts first, warnings last — a worker that
// stops reading early should still have the identifiers it must not invent.
var workerContextBlocks = []workerContextBlock{
	{"ledger", func(a *App, sid session.SessionID) string { return renderLedger(a.ledgerOf(sid)) }},
	{"specmine", func(a *App, sid session.SessionID) string { return specMineWorkerBrief(a.cachedSpecMine(sid)) }},
	{"concern", func(a *App, sid session.SessionID) string { return concernBrief(a.cachedConcern(sid)) }},
}

// workerContext renders every block for one session, in order, blank-line separated. Empty when no
// block has anything to say — the caller appends nothing rather than an empty header.
func (a *App) workerContext(sid session.SessionID) string {
	var out []string
	for _, blk := range workerContextBlocks {
		if s := strings.TrimSpace(blk.render(a, sid)); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n\n")
}

// withWorkerContext appends the parent-context blocks to a brief that has already been through
// curation. Returns the brief unchanged when there is nothing to add.
func (a *App) withWorkerContext(sid session.SessionID, brief string) string {
	c := a.workerContext(sid)
	if c == "" {
		return strings.TrimSpace(brief)
	}
	return strings.TrimSpace(strings.TrimSpace(brief) + "\n\n" + c)
}

// specMineWorkerBrief formats the mined contract — the request's identifiers/types and the real
// signatures/paths a read-only exploration found in this repository — as a VERBATIM block for a
// worker's prompt.
//
// It was, until this block existed, the one piece of mined context that reached everybody EXCEPT
// the agent that writes the code. injectSpecMineNote stores it for the termination council and
// appends it to the parent session, which serves the planner, the check-author, and a step run
// INLINE — but a delegated step runs in a fresh session whose whole context is its brief, and the
// curator cannot forward what it never sees (its input is the mechanical brief, not the session).
// So the pass whose entire purpose is to hand real paths and signatures to whoever implements the
// step was invisible to precisely that agent whenever the step was delegated.
//
// Clipped generously rather than summarized: the block's value is the exact spelling of a name, and
// clipSpec cuts with a marker so a truncated tail cannot be mistaken for the whole of it.
//
// The block carries TWO provenances and used to present them as one. A ⟨tagged⟩ line is distilled
// from the request's text before anything in the workspace has been opened; the rest was read out of
// the real files. Under a single header, and a closing clause that demoted only "a file's sample
// contents", the pre-observation guess read as the fixed part and the observation read as the soft
// part — the authority exactly inverted. Observed live: three ⟨hard⟩ lines predicting one
// transformation of a data file sat directly above a read-only pass's reading of the actual bytes
// describing a completely different one, and the guess was the line labelled "match it verbatim".
// The tag legend made it worse by being absent: it is written into the note the PARENT session gets,
// so a worker saw `⟨hard⟩` with nothing anywhere in its window defining it.
func specMineWorkerBrief(mined string) string {
	mined = strings.TrimSpace(mined)
	if mined == "" || !workerSpecMineEnabled() {
		return ""
	}
	return "── MINED CONTRACT ──\nTwo kinds of line below, and they do NOT carry the same weight.\n" +
		"A ⟨tagged⟩ line was distilled from the REQUEST's own text, before any file here was opened: " +
		"⟨hard⟩ = a value the request itself pins — match it verbatim; ⟨example⟩ = a sample input→output — " +
		"reproduce that behavior; ⟨semantic⟩ = a described behavior — satisfy its MEANING, not a spelling; " +
		"⟨unconstrained⟩ = the request does NOT pin this, so choose freely and assert nothing about it. " +
		"What such a line says about what a file already CONTAINS is a prediction — nothing had read that " +
		"file when it was written.\n" +
		"Every other line was read out of THIS repository by a read-only pass. Reuse a path or identifier " +
		"from it verbatim; do not invent an alternative spelling for one that is named.\n" +
		"Where the two disagree about a file's actual content, the pass that OPENED the file wins over the " +
		"line that predicted it — and if your own read disagrees with both, TRUST THE FILE.\n\n" +
		clipSpec(mined, 4000)
}
