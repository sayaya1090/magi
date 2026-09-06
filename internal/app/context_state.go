package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// What is in a companion's head right now, and what was folded away to make room.
//
// ContextView already answers this for the terminal, but it answers it as a paragraph of text: the
// numbers are formatted into a string by the same function that computes them, so a second surface
// can only re-derive or scrape. This is the same reading, structured — and it is the reading a
// SUPERVISOR needs rather than the operator's: not "how full is it" alone, but how many times this
// session has already had its history summarised away, and which topics went with it.
//
// # Why compaction is the thing to show
//
// A compaction is the one moment a companion silently stops knowing something. The summary keeps
// the gist and the shards keep the detail on disk, but between them sits a lossy step nobody is
// told about — and a companion that has compacted four times in one session is one whose earlier
// reasoning a person should not assume is still there. That is a supervision fact, and until now
// it existed only inside the log.
//
// # Derived, like everything else here
//
// From events already on disk: the compaction facts carry their own before/after sizes, and the
// last turn's usage carries the provider's real prompt count. No new event, so this answers for
// sessions that ran before it was written.
type ContextState struct {
	Model string `json:"model,omitempty"`
	// Window is the model's context window in tokens, 0 when this process does not know it. A
	// console loads no model config of its own, so 0 is common and means "no percentage can be
	// honestly shown" — not "unlimited".
	Window int `json:"window"`
	// Used is the size of what would be sent now: the provider's own prompt count from the last
	// finished turn when the log has one, else a ~4-chars-per-token estimate over the rebuilt
	// transcript. Estimated says which, because a person deciding whether to compact should know
	// whether they are looking at a measurement or arithmetic.
	Used      int  `json:"used"`
	Estimated bool `json:"estimated"`
	Messages  int  `json:"messages"`

	// Cached is how much of the last measured prompt the backend served from its own cache, and
	// CacheReported whether it said anything about a cache at all. The distinction is the whole
	// point: 0 means the cache missed, silence means this backend does not report it, and a screen
	// that drew both as 0% would report a working cache as broken. Measured on the default local
	// backend: it reports nothing, so this is usually silence.
	Cached        int  `json:"cached,omitempty"`
	CacheReported bool `json:"cacheReported,omitempty"`

	Compactions int       `json:"compactions"`
	Shed        int       `json:"shed"` // tokens the compactions freed, summed
	LastAt      time.Time `json:"lastAt,omitzero"`
	LastBefore  int       `json:"lastBefore,omitempty"`
	LastAfter   int       `json:"lastAfter,omitempty"`
	// Topics are the last compaction's shards: the file-shaped subjects whose full detail is off
	// in the log and can be pulled back with recall_context. They are what "the detail is not
	// lost" means concretely, and naming them is the difference between that claim and a promise.
	Topics []string `json:"topics,omitempty"`

	// Parts is what the window is filled WITH. Used answers how full; this answers with what, and
	// the two answer different questions about the same companion. A screen that shows only a
	// total invites the wrong move: somebody looking at a nearly-full bar reaches for the
	// conversation, and on this harness the conversation is routinely the small half.
	Parts ContextParts `json:"parts,omitzero"`
}

// ContextParts breaks the context into the pieces a person can actually act on.
//
// # These are estimates even when Used is measured
//
// Used prefers the provider's own prompt count. Nothing reports a BREAKDOWN, so these are the
// chars/4 estimate the compactor sizes with, and they will not sum to a measured Used. They are
// honest as proportions and dishonest as totals, which is why the screen draws them as a share of
// their own sum and says the reading is an estimate.
//
// # Zero means "not known", not "empty"
//
// System and Tools come from what the session has FROZEN. A session that has not run a step has
// frozen neither, so both are 0 — and that is different from a companion whose system prompt is
// genuinely empty, which does not happen. Callers must not draw a 0 here as a measurement.
//
// It is a conversion of event.PromptShape rather than an alias so this package's wire shape can
// change without editing a fact already written to thousands of logs. The conversion is checked by
// the compiler, so a field added to one and not the other stops the build rather than going out as
// a silent zero.
type ContextParts struct {
	// System is the assembled system prompt: identity, workdir, memory, skills.
	System int `json:"system,omitempty"`
	// Tools is the tool catalog that rides on every single request — names, descriptions and
	// JSON schemas. Measured at ~6-7k tokens on the default roster, which is more than most
	// conversations ever reach, and the reason this breakdown is worth drawing at all.
	Tools int `json:"tools,omitempty"`
	// Talk is what the person and the companion actually said to each other.
	Talk int `json:"talk,omitempty"`
	// Calls is the tool calls: names and arguments.
	Calls int `json:"calls,omitempty"`
	// Results is what the tools answered — file contents, command output. The part that grows
	// without anybody deciding it should.
	Results int `json:"results,omitempty"`
}

// Sum is what the parts add up to, which is the denominator for their shares. It is not Used: Used
// may be the provider's real count and these are always the estimate.
func (p ContextParts) Sum() int { return p.System + p.Tools + p.Talk + p.Calls + p.Results }

// scaledTo keeps the proportions and changes the size: the five pieces are multiplied so they sum
// to total. The remainder of integer rounding lands on the largest piece, so the sum is exact. A
// make-up with nothing in it stays empty — there are no proportions to keep.
func (p ContextParts) scaledTo(total int) ContextParts {
	sum := p.Sum()
	if sum == 0 || total <= 0 || sum == total {
		return p
	}
	sc := func(v int) int { return int(int64(v) * int64(total) / int64(sum)) }
	out := ContextParts{System: sc(p.System), Tools: sc(p.Tools), Talk: sc(p.Talk), Calls: sc(p.Calls), Results: sc(p.Results)}
	rest := total - out.Sum()
	big := &out.System
	for _, c := range []*int{&out.Tools, &out.Talk, &out.Calls, &out.Results} {
		if *c > *big {
			big = c
		}
	}
	*big += rest
	return out
}

// scaledAround is scaledTo with the fixed pieces pinned: when fixed (the measured size of system +
// tools) is known and fits under total, those two keep their mutual proportion and sum to fixed,
// and the three conversation pieces share what is left. Without a measurement, or with a total
// smaller than the measurement (a fold just emptied the window and the count is the estimate),
// it is plain proportional scaling.
func (p ContextParts) scaledAround(fixed, total int) ContextParts {
	if fixed <= 0 || fixed >= total {
		return p.scaledTo(total)
	}
	head := ContextParts{System: p.System, Tools: p.Tools}.scaledTo(fixed)
	tail := ContextParts{Talk: p.Talk, Calls: p.Calls, Results: p.Results}
	rest := total - fixed
	if tail.Sum() == 0 {
		tail.Talk = rest
	} else {
		tail = tail.scaledTo(rest)
	}
	return ContextParts{System: head.System, Tools: head.Tools, Talk: tail.Talk, Calls: tail.Calls, Results: tail.Results}
}

// partsOf is the five token counts out of a recorded shape. Window rides on the same fact but is
// not one of the parts — it is what they are measured AGAINST, and adding it to the sum would put
// the whole context window inside the bar that shows how full the context window is.
func partsOf(sh event.PromptShape) ContextParts {
	return ContextParts{System: sh.System, Tools: sh.Tools, Talk: sh.Talk,
		Calls: sh.Calls, Results: sh.Results}
}

// ContextStateOf reads one session's context situation.
func (a *App) ContextStateOf(ctx context.Context, sid session.SessionID) (ContextState, error) {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ContextState{}, err
	}
	msgs := reconstruct(evs)
	s := a.sessionInfo(ctx, sid)
	// From the events THIS call already read, not from the cached meta. A reader process caches a
	// session's meta on first sight and has nothing to invalidate it with — the daemon changes the
	// model, writes the fact, and the console kept answering from the snapshot it took before that.
	// The log is the source; this function is already holding it.
	model := s.Model.Model
	if m := modelFromEvents(evs); m != "" {
		model = m
	}
	out := ContextState{Model: model, Window: a.contextWindow(model), Messages: len(msgs)}
	// fixed is the measured size of the two pieces that do not change with the conversation —
	// the system prompt and the tool catalog — taken from the FIRST turn that reported a real
	// count: what the backend billed, less the chars/4 estimate of the little transcript there
	// was. Scaling all five pieces to the total in proportion had the catalog growing with the
	// conversation (31k estimated, 53k drawn, on Excel 2026-09-07): the backend counts JSON
	// schemas heavier than chars/4, and the shortfall was spread over every piece. Measured once,
	// it stays put, and everything the total grows by afterwards is the conversation's.
	fixed := 0

	for _, e := range evs {
		switch e.Type {
		case event.TypeCompaction:
			var d event.CompactionData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			out.Compactions++
			if shed := d.TokensBefore - d.TokensAfter; shed > 0 {
				out.Shed += shed
			}
			out.LastAt, out.LastBefore, out.LastAfter = e.TS, d.TokensBefore, d.TokensAfter
			// A real prompt count measured before the fold describes a context that no longer
			// exists, and it is the LARGER number — carrying it across would report a companion
			// as nearly full at the exact moment it was emptied.
			//
			// The make-up goes with it, for exactly the same reason and one line later than it
			// should have: a fold is what SHRINKS the conversation, so the piece that just changed
			// most is the one this breakdown exists to show. Keeping it would draw the pre-fold
			// bar under a post-fold total — the fold's whole effect, rendered as if it had not
			// happened.
			out.Used, out.Cached, out.CacheReported = 0, 0, false
			out.Parts = ContextParts{}
			out.Topics = out.Topics[:0]
			for _, sh := range d.Shards {
				out.Topics = append(out.Topics, sh.Topic)
			}
		case event.TypeTurnFinished:
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			// From the process that ASSEMBLED the request. A reader replaying the log can measure
			// the conversation and nothing else: the system prompt and the tool catalog are built
			// per session and never written down. Absent on turns recorded before this was, and on
			// the empty finishes a cancel writes — those leave the previous shape standing, which
			// is the last thing this companion actually held.
			if d.Prompt != nil {
				out.Parts = partsOf(*d.Prompt)
				// The window travels with the make-up, and for the same reason: this process may
				// not be the one that knows it. A console reading a log has an empty model
				// registry and no backend prober, so its own answer is always 0 — and 0 means
				// "unknown", which is the state where the screen deliberately draws no gauge.
				if d.Prompt.Window > 0 {
					out.Window = d.Prompt.Window
				}
			}
			// The provider's own count, from the most recent turn that reported one. A zero is
			// not a measurement — several backends omit usage — so it does not displace an
			// earlier real number or the estimate.
			// Measured on RECEIPT, not on send: the context this companion holds after the turn is
			// the prompt it sent plus the answer that came back, and the backend counted both. In
			// alone is what the request cost, one answer short of what the window holds now — a
			// person saw "talk 1" on a companion that had answered them (2026-09-06).
			// And it is the LAST request's count, not the turn's bill. Usage sums In over every
			// step of the turn; a six-step turn's bill is six windows' worth, and a reading that
			// took it for the window drew a 35k context as 221k with a 196k tool catalog (Excel,
			// 2026-09-07). Held is what the window actually held when the turn ended; the bill
			// stands in only for turns recorded before Held was.
			if d.Held != nil && d.Held.In > 0 {
				out.Used = d.Held.In + d.Held.Out
				out.Cached, out.CacheReported = d.Held.Cached, d.Held.CacheReported
				if fixed == 0 && d.Prompt != nil {
					if f := d.Held.In - (d.Prompt.Talk + d.Prompt.Calls + d.Prompt.Results); f > 0 {
						fixed = f
					}
				}
			} else if d.Usage.In > 0 {
				out.Used = d.Usage.In + d.Usage.Out
				out.Cached, out.CacheReported = d.Usage.Cached, d.Usage.CacheReported
			}
		}
	}
	// The three transcript pieces are measured over the conversation AS IT STANDS — what the person
	// and the companion have said so far, including the last answer — not over what the last
	// request carried, which ends one answer earlier. The recorded shape keeps the two pieces a
	// reader cannot measure (system prompt, tool catalog); the transcript it can, and should, read
	// fresh.
	if len(msgs) > 0 {
		var tr event.PromptShape
		measureMessages(&tr, msgs)
		out.Parts.Talk, out.Parts.Calls, out.Parts.Results = tr.Talk, tr.Calls, tr.Results
	}
	// No finished turn has written a make-up yet — a session before its first turn, or one whose
	// fold just emptied the shape. The transcript alone would say "system 0, tools 0", and a person
	// reading the bar asked exactly that: why the system prompt, the skills and the tools were not
	// counted (2026-09-06, the Office pane). They ARE what the next request will carry, and this
	// process can assemble them now without sending anything; a reader over the log cannot.
	if out.Parts.System == 0 && out.Parts.Tools == 0 {
		if parts, ok := a.assembledParts(sid, s, evs, msgs); ok {
			out.Parts = parts
		}
	}
	// **The count is the provider's; the make-up is ours.** The five pieces are character arithmetic
	// (~4 per token) and the total beside them is what the backend actually billed — two rulers, and
	// a band drawn with one under a number from the other never quite met: the segments summed to
	// 20.1k under a "20.0k" total, or fell short of it. A person asked which was true (2026-09-06)
	// and the answer is the received one. So when a real count exists, the make-up keeps its
	// PROPORTIONS and takes the count's SIZE: each piece is scaled so the five add up to what was
	// received. Before any count there is only arithmetic, and Estimated says so.
	if out.Used > 0 {
		out.Parts = out.Parts.scaledAround(fixed, out.Used)
	}
	if out.Used == 0 {
		// The estimate is the whole request, not the transcript.
		//
		// It used to be estimateTokens("", msgs) — an empty system prompt, so the two pieces that
		// dominate a real request were left out of the number every screen shows. Measured on one
		// companion after a single exchange: it reported ~4 tokens where the request carried 8,107.
		// The recorded make-up has all five pieces, so when there is one it IS the estimate; before
		// the first finish there is nothing to read and the transcript is still the best guess.
		if sum := out.Parts.Sum(); sum > 0 {
			out.Used, out.Estimated = sum, true
		} else {
			out.Used, out.Estimated = estimateTokens("", msgs), true
		}
	}
	return out, nil
}

// assembledParts measures what the next request would be made of, from the pieces this process
// would assemble for it: the system prompt as buildStepSystem writes it (project memory, the
// agent's prompt, the environment, the skill list), the tool catalog this agent is advertised, and
// the transcript. Nothing is frozen or recorded by measuring — the skill block is rendered, not
// pinned, and the catalog is read, not cached — so a reading has no effect on the turn that follows.
//
// Only a process that owns a provider assembles requests. A console reading the log has none, and
// its answer stays what the log says.
func (a *App) assembledParts(sid session.SessionID, s session.Session, evs []event.Event, msgs []session.Message) (ContextParts, bool) {
	if a.llm == nil {
		return ContextParts{}, false
	}
	agent := a.agentFor(s)
	sys := a.systemFor(agent, s.Workdir)
	if dir := langDirective(lastUserPromptText(a.liveEvents(sid, evs))); dir != "" {
		sys = dir + "\n\n" + sys
	}
	sys += renderSkillBlock(a.loadSkills(s.Workdir))
	sh := event.PromptShape{System: len(sys) / 4, Tools: toolSpecTokens(a.toolSpecs(sid, agent))}
	measureMessages(&sh, msgs)
	return partsOf(sh), true
}
