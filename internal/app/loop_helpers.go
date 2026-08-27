package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
	"github.com/sayaya1090/magi/internal/port"
)

// Pure event/message extraction and formatting helpers used by the run loop: locating the
// most recent genuine user prompt (vs. injected feedback), enumerating user prompts across a
// turn, and rendering the experience/todo blocks. No App state; split out of loop.go for
// cohesion. Behavior unchanged.

// lastUserPromptText returns the text of the most recent GENUINE user prompt
// (Actor.Kind == user), skipping council/hook/auto injections (which are recorded
// as user-role prompts but authored by the system). Used for the language lock so
// injected English feedback can't unlock the user's language.
func lastUserPromptText(evs []event.Event) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypePromptSubmitted && evs[i].Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(evs[i].Data, &d) == nil {
				return partsText(d.Parts)
			}
		}
	}
	return ""
}

// userPrompt is a genuine user prompt with the id of the event that carried it, so the
// interjection detector can mask that exact event while the message stays queued.
type userPrompt struct {
	MsgID string
	Text  string
}

// seedPromptIdx returns the index (in userPromptEntries order) of the genuine user
// prompt that SEEDS the current top-level turn: the first user prompt not already
// answered by an assistant reply, skipping any that is DEFERRED — queued as an
// interjection, or left queued by a process that died (the hydrated abandoned set). It is meaningful only at step 0, where the current
// turn has produced no output yet — so any assistant (ActorAgent) part in the log
// belongs to a PREVIOUS turn, and the first user prompt after the last such part is
// this turn's seed. Later user prompts are mid-turn interjections that piled up before
// execution began. Returns -1 when the log has no genuine user prompt (e.g. a subagent
// session, whose seed is authored by ActorAgent).
// deferred is the same mask hasUnansweredPrompt and liveEvents use, and it has to be the same
// here or the three disagree about one prompt. They did: a deferral that outlived its process
// stayed masked, so it could not START a run — and then the next unrelated prompt started one and
// this function handed it the deferred question as the turn's task. The turn was ABOUT a question
// filtered out of the model's own context, with the council judging completion against text the
// agent could not read, and the prompt the user had just typed queued behind it as an
// interjection. Reported from a real session: quit mid-question, resume, ask something new, and
// the old question is answered first.
func seedPromptIdx(evs []event.Event, deferred map[string]bool) int {
	abandoned := abandonedPromptIDs(evs) // prompts whose turn was cancelled — resolved, never a seed
	ui := -1                             // running index into userPromptEntries order
	lastAnswered := -1                   // highest user-prompt index a prior assistant reply covered
	for _, e := range evs {
		switch {
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			ui++
			// A cancelled (abandoned) prompt counts as resolved: skip it so the next
			// genuine prompt seeds the turn instead of the dead one.
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil && (abandoned[d.MessageID] || deferred[d.MessageID]) {
				lastAnswered = ui
			}
		case e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent:
			if ui >= 0 {
				lastAnswered = ui
			}
		}
	}
	if ui < 0 {
		return -1
	}
	seed := lastAnswered + 1
	if seed > ui {
		seed = ui // defensive: everything already answered → treat the latest as seed
	}
	return seed
}

// hasUnansweredPrompt reports whether a genuine (ActorUser) prompt exists that no
// assistant reply has yet covered, that was not abandoned, and that is NOT a queued
// interjection (deferred set) — the STRICT question seedPromptIdx blurs (it defaults
// to the last prompt even when all are answered). Used at the run goroutine's exit
// window to catch a prompt that landed mid-council (buried behind the approved answer)
// so it re-runs instead of being stranded. DEFERRED prompts are excluded on purpose:
// a queued interjection is already owned by the dedicated drain path (triage), so
// counting it here too would make the generic re-run fire in a loop — the "consecutive
// requests keep getting recalled" bug — instead of letting the queue drain it once.
func hasUnansweredPrompt(evs []event.Event, deferred map[string]bool) bool {
	abandoned := abandonedPromptIDs(evs)
	ui, lastOpen := -1, -1 // lastOpen = highest index of a still-open (unanswered, non-deferred) prompt
	answeredThrough := -1  // highest prompt index a later agent reply covered
	for _, e := range evs {
		switch {
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			ui++
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil {
				continue
			}
			if abandoned[d.MessageID] || deferred[d.MessageID] {
				continue // resolved (cancelled) or spoken-for (queued) → never "open"
			}
			lastOpen = ui
		case e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent:
			if ui >= 0 {
				answeredThrough = ui
			}
		}
	}
	return lastOpen > answeredThrough // an open prompt with no later agent reply
}

// abandonedPromptIDs returns the set of user-prompt MessageIDs marked abandoned (their
// turn was cancelled before answering — TypePromptAbandoned). seedPromptIdx uses it to
// skip a cancelled prompt so it cannot seed a later, unrelated turn.
func abandonedPromptIDs(evs []event.Event) map[string]bool {
	var out map[string]bool
	for _, e := range evs {
		if e.Type == event.TypePromptAbandoned {
			var d event.PromptAbandonedData
			if json.Unmarshal(e.Data, &d) == nil && d.MsgID != "" {
				if out == nil {
					out = map[string]bool{}
				}
				out[d.MsgID] = true
			}
		}
	}
	return out
}

// unansweredUserPromptIDs returns every genuine (ActorUser) prompt in the log that no assistant
// reply has covered and that was not already abandoned — the full set a cancel must drain, INCLUDING
// a prompt Steer'd into the log a moment before the interrupt that the loop never detected into the
// in-memory queue. Without it, that undetected prompt stayed a live seed and ran ahead of the user's
// next, newer request.
// userPromptIDsNotAbandoned lists every user prompt still standing in the log, in order —
// unlike unansweredUserPromptIDs it does not ask whether a turn has since finished, because the
// caller is asking "did the person say something new", not "is anything owed a turn". A
// turn.finished written by a turn that never read the prompt answers nothing.
func userPromptIDsNotAbandoned(evs []event.Event) []string {
	abandoned := abandonedPromptIDs(evs)
	var out []string
	seen := map[string]bool{}
	for _, e := range evs {
		if e.Type != event.TypePromptSubmitted || e.Actor.Kind != event.ActorUser {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) != nil || d.MessageID == "" || abandoned[d.MessageID] || seen[d.MessageID] {
			continue
		}
		out = append(out, d.MessageID)
		seen[d.MessageID] = true
	}
	return out
}

func unansweredUserPromptIDs(evs []event.Event) []string {
	abandoned := abandonedPromptIDs(evs)
	var open []string         // ids still unanswered, in order
	seen := map[string]bool{} // dedupe
	for _, e := range evs {
		switch {
		case e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser:
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) != nil || d.MessageID == "" || abandoned[d.MessageID] || seen[d.MessageID] {
				continue
			}
			open = append(open, d.MessageID)
			seen[d.MessageID] = true
		case e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent:
			// An assistant reply answers everything open before it.
			open = open[:0]
		}
	}
	return open
}

// userPromptEntries returns every genuine user (ActorUser) prompt in log order, each
// paired with its PromptSubmitted MessageID.
func userPromptEntries(evs []event.Event) []userPrompt {
	var out []userPrompt
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				out = append(out, userPrompt{MsgID: d.MessageID, Text: partsText(d.Parts)})
			}
		}
	}
	return out
}

// currentTaskText is the query for push-side shard hints: the most recent genuine
// user prompt (what the user asked) joined with the latest assistant message (what
// the agent is doing right now). Using both means a hint can fire on the file the
// agent just started editing, not only on words from the opening prompt — the case
// where a weak model has drifted onto a sub-task and most needs the recalled detail.
func currentTaskText(evs []event.Event) string {
	prompt := lastUserPromptText(evs)
	var last string
	msgs := reconstruct(evs)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == session.RoleAssistant {
			var b strings.Builder
			for _, p := range msgs[i].Parts {
				if p.Kind == session.PartText {
					b.WriteString(p.Text + " ")
				}
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				last = s
				break
			}
		}
	}
	return strings.TrimSpace(prompt + " " + last)
}

// taskSeedText returns the most recent prompt that IS a task — the user's request at the top
// level, or the seed a parent agent authored for a subagent — and never one of magi's own.
//
// "The task" was read off reconstruct()'s messages, and reconstruct labels EVERY prompt.submitted
// RoleUser regardless of who wrote it. magi writes several: the stall nudge, an orchestrator
// re-prompt, a hook message, a permission note, a plugin note. So the newest of those became the
// answer. Measured:
//
//	user "fix the GC bug in shared_heap.c" + a stall nudge  → "You've run many steps without…"
//	subagent seed "SUBTASK: port the parser" + a stall nudge → "You've run many steps without…"
//
// The caller that hurts most is the nudge itself: it ends with "Re-read the original task:" and
// then quotes this, so a second nudge handed the agent the FIRST nudge as its task — and for a
// subagent, where turnTask is empty and this fallback is the only path, the real seed was gone.
//
// lastUserPromptText above is strictly ActorUser, which is right for "did the user speak" but
// drops a subagent's seed. The line that matters is authorship: anything magi injected is not the
// task, and everything else is.
func taskSeedText(evs []event.Event) string {
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Type != event.TypePromptSubmitted || e.Actor.Kind == event.ActorSystem {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(e.Data, &d) == nil {
			if t := strings.TrimSpace(partsText(d.Parts)); t != "" {
				return t
			}
		}
	}
	return ""
}

// lastUserText returns the text of the most recent user message.
func lastUserText(msgs []session.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == session.RoleUser {
			var b strings.Builder
			for _, p := range msgs[i].Parts {
				if p.Kind == session.PartText {
					b.WriteString(p.Text)
				}
			}
			return b.String()
		}
	}
	return ""
}

// experiencePointer renders the one-line push notice: how many stored memories/skills
// match, pointing the agent at recall_memory to pull the detail. Empty when nothing
// matched, so the section is dropped entirely.
func experiencePointer(nMem, nSkill int) string {
	total := nMem + nSkill
	if total == 0 {
		return ""
	}
	noun := "entry"
	if total != 1 {
		noun = "entries"
	}
	return fmt.Sprintf("%d relevant team memory/skill %s exist — call recall_memory with keywords to read the details.", total, noun)
}

// formatExperienceFull renders the full memory/skill entries for a recall_memory pull
// (as opposed to the one-line push pointer). This is where the detail actually enters
// context, and only when the agent asked for it.
func formatExperienceFull(mems []port.Memory, skills []port.Skill) string {
	var b strings.Builder
	for _, m := range mems {
		b.WriteString("- " + strings.TrimSpace(m.Text) + "\n")
	}
	for _, s := range skills {
		b.WriteString("- skill " + s.Name + ": " + strings.TrimSpace(s.Description) + "\n")
		if body := strings.TrimSpace(s.Body); body != "" {
			b.WriteString("  " + strings.ReplaceAll(body, "\n", "\n  ") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// wikiPointer renders the wiki's two advertisement lines: the index (titles, so the model learns
// the pages exist) and, when the current request matches one, a pointer at that page. Hooks only —
// bodies stay behind recall_memory, so this block's cost does not grow with the wiki.
func wikiPointer(ctx context.Context, w port.WikiStore, q string) string {
	var b strings.Builder
	if idx, err := w.WikiIndex(ctx, 8); err == nil && len(idx) > 0 {
		titles := make([]string, 0, len(idx))
		for _, p := range idx {
			// Clipped: a page title is model-authored (and fleet-synced) text landing in every
			// step's prompt — bounded here so one bloated title cannot tax every turn.
			titles = append(titles, text.Clip(p.Title, 80))
		}
		b.WriteString("wiki pages (update via remember{page:…}, read via recall_memory): " +
			strings.Join(titles, " · "))
	}
	// A few hits, first FRESH one wins: an exact-title match on a stale page leads the ranking by
	// design (a tombstone is still the best answer to its own title), but the pointer's job is to
	// advertise a live page — a stale leader must not silence a fresh runner-up one slot below.
	if hits, err := w.WikiSearch(ctx, q, 3); err == nil {
		for _, p := range hits {
			if p.Stale {
				continue
			}
			sep := ""
			if b.Len() > 0 {
				sep = "\n" // no leading blank line when the index half rendered nothing
			}
			hook := text.Clip(firstNonBlankLine(p.Body), 160)
			b.WriteString(sep + "wiki page likely relevant to this request: [" + text.Clip(p.Title, 80) + "] " + hook +
				" — recall_memory pulls the full page")
			break
		}
	}
	return b.String()
}

func firstNonBlankLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// wikiNeighborNote answers "does a page like this already exist under another title" at the
// moment a page is about to be written — the convergence pressure against near-duplicate pages
// piling up under drifting names. Advisory, never a refusal: a refused write teaches a weak model
// to stop writing, and the write itself is still wanted; what the note buys is the NEXT write
// landing on the existing title.
func (a *App) wikiNeighborNote(ctx context.Context, page string) string {
	w, ok := a.cfg.Experience.(port.WikiStore)
	if !ok {
		return ""
	}
	hits, err := w.WikiSearch(ctx, page, 2)
	if err != nil {
		return ""
	}
	// An UPDATE needs no advice: the page exists under this exact title, the write converges by
	// construction, and nagging every legitimate update about a related sibling teaches the model
	// to ignore the note. The advisory is for the moment a NEW title is minted.
	for _, h := range hits {
		if strings.EqualFold(strings.TrimSpace(h.Title), strings.TrimSpace(page)) {
			return ""
		}
	}
	want := strings.Fields(strings.ToLower(page))
	for _, h := range hits {
		have := map[string]bool{}
		for _, f := range strings.Fields(strings.ToLower(h.Title)) {
			have[f] = true
		}
		n := 0
		for _, f := range want {
			if have[f] {
				n++
			}
		}
		if len(want) > 0 && n*2 >= len(want) { // half the new title's words already name a page
			// Clipped like every other rendering of a synced title (formatWikiPages): this note
			// rides the remember tool's success text into context, and a degenerate foreign
			// title must not ride in whole through the one site the clipping pass missed.
			return "note: page [" + text.Clip(h.Title, 80) + "] looks related — if this is the same topic, " +
				"update that page next time instead of a near-duplicate title"
		}
	}
	return ""
}

// formatWikiPages renders wiki pages for a recall_memory pull. Each carries the trust material a
// reader needs about a CANONICAL claim: who last edited it, when, in which tier — and the stale
// mark with its reason, because "this stopped being true" is an answer, not an omission.
func formatWikiPages(pages []port.WikiPage) string {
	var b strings.Builder
	for _, p := range pages {
		// Every head field clipped, not only the body: a locally-written page is oneLine-folded
		// at the store, but a SYNCED foreign revision's frontmatter arrives as it was written —
		// a degenerate single-line title must not ride into context whole.
		head := "- wiki"
		if p.Tier != "" {
			head += ":" + p.Tier
		}
		head += " [" + text.Clip(p.Title, 80) + "]"
		if p.Stale {
			head += " ⚠STALE"
		}
		meta := text.Clip(strings.TrimSpace(p.Editor), 60)
		if p.Updated != "" {
			if meta != "" {
				meta += ", "
			}
			meta += text.Clip(p.Updated, 40)
		}
		if meta != "" {
			head += " (" + meta + ")"
		}
		b.WriteString(head + "\n")
		// Bounded like the council's evidence items and for the same reason: a page body is
		// model-authored, fleet-synced text with no size ceiling at write time, and an unclipped
		// pull would let one bloated page own the context. The marker says where to get the rest.
		if body := strings.TrimSpace(p.Body); body != "" {
			body = text.ClipWith(body, 2400, "\n…(the rest of this page is not shown — it is longer than a recall carries)")
			b.WriteString("  " + strings.ReplaceAll(body, "\n", "\n  ") + "\n")
		}
		if len(p.Links) > 0 {
			b.WriteString("  related: " + text.Clip(strings.Join(p.Links, ", "), 200) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatTodos renders the plan as a checklist for the system prompt.
func formatTodos(td []session.Todo) string {
	mark := map[string]string{"completed": "[x]", "in_progress": "[~]", "pending": "[ ]", "cancelled": "[✗]"}
	var b strings.Builder
	for i, t := range td {
		if i > 0 {
			b.WriteString("\n")
		}
		m := mark[t.Status]
		if m == "" {
			m = "[ ]"
		}
		b.WriteString(m + " " + t.Content)
	}
	return b.String()
}

// waitSteer names the ways THIS agent can wait on a long-running job, for the nudges that
// steer a polling loop toward blocking. Empty when it holds neither tool.
//
// The nudges exist to redirect an agent that is already stuck, which is the worst possible
// moment to name a tool its allowlist refuses: the redirect becomes a refused call, and the
// agent is stuck in a second way. The default read-only agents (explore/locator) have neither
// of these, and the curated worker's set has bash_output but not wait_for — so a steer written
// as if every agent could do both was wrong for every one of them.
func waitSteer(agent AgentSpec) string {
	switch block, poll := agent.allows("wait_for"), agent.allows("bash_output"); {
	case block && poll:
		return "poll bash_output, or better, block on wait_for so a single call covers the whole wait"
	case block:
		return "block on wait_for so a single call covers the whole wait"
	case poll:
		return "poll bash_output"
	}
	return ""
}

// backgroundWaitAdvice is the stall nudge's "don't restart a long job" clause, ending in a
// space so it splices into the sentence run. It is empty for an agent that cannot run a
// background job in the first place — the default read-only agents were being told to start
// one with bash, poll it with bash_output, and block on wait_for, and they hold none of the
// three, so the whole clause was three instructions that could only be refused.
func backgroundWaitAdvice(agent AgentSpec) string {
	steer := waitSteer(agent)
	if !agent.allows("bash") || steer == "" {
		return ""
	}
	return "If you are WAITING on a long-running install or build: do NOT restart it or launch another " +
		"copy — start it once (bash with background=true), then " + steer + " until it actually finishes. "
}

// coalesceInterjectionText merges a batch of queued interjections into one prompt text: the
// distinct non-empty texts in arrival order, de-duplicated (a re-typed identical question
// collapses to one) and joined by newlines. Merging rather than picking one loses no content —
// if the follow-ups WERE distinct, they all ride into the single answered prompt.
func coalesceInterjectionText(q []pendingInterjection) string {
	seen := map[string]bool{}
	var out []string
	for _, p := range q {
		t := strings.TrimSpace(p.Text)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
