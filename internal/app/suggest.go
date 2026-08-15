package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/rank"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Composer suggestion — the ghost text under the instruction a person is typing to the companion.
//
// Unlike code completion it is not about a file; it is about what this user is likely to ASK next.
// Three things make that guess: what just happened in the conversation (the strongest signal — after
// "3 tests failed" the next instruction is usually "fix them"), what they have typed so far, and how
// THIS person phrases instructions, learned from their past sessions' prompts. The last is why it is
// worth more than a generic model: it is grounded in one person's own words.
//
// The past-prompt corpus is ranked with rank.ByIDF — the one ranking in this tree, whose package
// comment forbids a second — and cached per workdir, rebuilt only for sessions that changed since we
// last read them (their LastActivity moved). So the cold call reads the recent log once and every
// later keystroke pays nothing.

const (
	// maxIndexSessions caps how many recent sessions the corpus is built from. A workspace can have
	// a thousand-plus logs; reading them all on the first pause would be a visible stall, and the
	// person's phrasing is legible from the recent hundreds. Most-recent by LastActivity.
	maxIndexSessions = 300
	// maxExamples is how many past prompts ride into the suggestion as few-shot. Enough to show a
	// style, few enough to keep the prompt (and the latency) small.
	maxExamples = 6
	// snapshotCap bounds the live conversation snapshot fed to the suggester.
	snapshotCap = 2 << 10
)

type workdirPrompts struct {
	perSession map[session.SessionID]sessionPrompts
}

type sessionPrompts struct {
	seen    time.Time // the LastActivity we read this session at; a later one means re-read
	prompts []string
}

// SuggestPrompt returns ghost text continuing the instruction the user is typing in the composer.
// prefix is what they have typed so far. It self-disables (returns "") when composer suggestion is
// off or no composer profile is routed, so the main model is never spent on a keystroke.
func (a *App) SuggestPrompt(ctx context.Context, sid session.SessionID, prefix string) (string, error) {
	profile := a.cfg.Autocomplete.ComposerProfile
	if !a.cfg.Autocomplete.ComposerOn() || profile == "" {
		return "", nil
	}
	s := a.sessionInfo(ctx, sid)
	evs, _ := a.store.Read(ctx, sid, 0)

	// What just happened, clipped: the run state magi already renders each turn is exactly the "what
	// did the agent just do / find / break" the next instruction usually responds to.
	snapshot := clipTail(a.runState(evs), snapshotCap)
	if strings.TrimSpace(prefix) == "" && strings.TrimSpace(snapshot) == "" {
		return "", nil
	}

	// Few-shot from this person's phrasing. The query is what they have typed (or, on an empty
	// composer, what just happened) so the examples pulled are the ones like the instruction forming.
	var examples []string
	if a.cfg.Autocomplete.CrossSessionOn() {
		q := prefix
		if strings.TrimSpace(q) == "" {
			q = snapshot
		}
		docs := a.pastPrompts(ctx, s.Workdir)
		for _, h := range topHits(rank.ByIDF(q, docs), maxExamples) {
			examples = append(examples, docs[h.Index])
		}
	}

	var b strings.Builder
	if snapshot != "" {
		b.WriteString("## What just happened in this session\n")
		b.WriteString(snapshot)
		b.WriteString("\n\n")
	}
	if len(examples) > 0 {
		b.WriteString("## How this user tends to phrase instructions (their own past requests)\n")
		for _, ex := range examples {
			b.WriteString("- " + exampleLine(ex) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("## The instruction the user is typing now (continue it)\n")
	b.WriteString(prefix)

	system := "You suggest how the user is about to finish the instruction they are typing to a coding " +
		"agent. Use what just happened and the way this user phrases things. Output ONLY the text that " +
		"continues from where they stopped typing — no preamble, no quotation marks, no restating what " +
		"they already typed. If the instruction already reads as complete, or you cannot tell what they " +
		"mean, output nothing at all."
	out, err := a.complete(ctx, profile, a.completeModel(profile, s.Model.Model), system, b.String())
	if err != nil {
		return "", err
	}
	return strings.Trim(out, "`\n \""), nil
}

// pastPrompts returns this workspace's past user prompts, most-recent sessions first, from the cache
// — reading only the sessions whose LastActivity moved since last time. Cron-origin sessions are
// skipped: an unattended job's prompt is not this person's phrasing.
func (a *App) pastPrompts(ctx context.Context, workdir string) []string {
	metas, err := a.store.ListSessions(ctx, workdir)
	if err != nil {
		return nil
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].LastActivity.After(metas[j].LastActivity) })
	if len(metas) > maxIndexSessions {
		metas = metas[:maxIndexSessions]
	}

	a.promptMu.Lock()
	if a.promptIndex == nil {
		a.promptIndex = map[string]*workdirPrompts{}
	}
	wp := a.promptIndex[workdir]
	if wp == nil {
		wp = &workdirPrompts{perSession: map[session.SessionID]sessionPrompts{}}
		a.promptIndex[workdir] = wp
	}
	// Which sessions are stale (new or changed), decided under the lock; the reads happen off it.
	type todo struct {
		id   session.SessionID
		seen time.Time
	}
	var stale []todo
	live := make(map[session.SessionID]bool, len(metas))
	for _, m := range metas {
		if strings.HasPrefix(m.Origin, "cron:") {
			continue
		}
		live[m.ID] = true
		if c, ok := wp.perSession[m.ID]; !ok || c.seen.Before(m.LastActivity) {
			stale = append(stale, todo{m.ID, m.LastActivity})
		}
	}
	for id := range wp.perSession { // evict deleted sessions so the cache cannot outgrow the store
		if !live[id] {
			delete(wp.perSession, id)
		}
	}
	a.promptMu.Unlock()

	fresh := make(map[session.SessionID]sessionPrompts, len(stale))
	for _, s := range stale {
		fresh[s.id] = sessionPrompts{seen: s.seen, prompts: a.readPrompts(ctx, s.id)}
	}

	a.promptMu.Lock()
	defer a.promptMu.Unlock()
	for id, sp := range fresh {
		wp.perSession[id] = sp
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range metas {
		sp, ok := wp.perSession[m.ID]
		if !ok {
			continue
		}
		for _, p := range sp.prompts {
			if !seen[p] { // the same instruction re-typed across sessions counts once
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// readPrompts pulls the user-typed prompts out of one session's log. Resurfaced (re-emitted queued)
// prompts are skipped so a drained interjection is not counted twice.
func (a *App) readPrompts(ctx context.Context, sid session.SessionID) []string {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return nil
	}
	var out []string
	for _, ev := range evs {
		if ev.Type != event.TypePromptSubmitted {
			continue
		}
		var d event.PromptSubmittedData
		if json.Unmarshal(ev.Data, &d) != nil || d.ResurfacedFrom != "" {
			continue
		}
		if t := strings.TrimSpace(partsText(d.Parts)); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// topHits returns the first n hits (ByIDF is already ranked best-first).
func topHits(hits []rank.Hit, n int) []rank.Hit {
	if len(hits) > n {
		return hits[:n]
	}
	return hits
}

// clipTail keeps the LAST max bytes — for a running snapshot the recent end is what matters.
func clipTail(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…\n" + s[len(s)-max:]
}

// exampleLine renders one past prompt as a single capped line for the few-shot list.
func exampleLine(s string) string {
	s = oneLine(s)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
