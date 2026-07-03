package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// reviewGateAgents are the read-only specialists the pre-finish review gate
// dispatches. Both are inspection-only (tester runs builds/tests but has no
// write/edit; reviewer has no bash at all), so dispatching them after the main
// agent's work is safe — there is no file conflict to race and nothing to
// fabricate-then-write. They mirror the roles a human runs before committing.
var reviewGateAgents = []struct {
	agent, role, focus string
}{
	{
		agent: "tester",
		role:  "VERIFY",
		focus: "Independently VERIFY that the work satisfies the task. Actually run the build and the tests " +
			"(or the program/command the task is about) and check the real behavior against what the task " +
			"specifies — do not trust the transcript's claims. Report PASS or FAIL with concrete evidence " +
			"(the command you ran and its real output). If you could not run something, say so; never invent " +
			"or hand-write output to make it look verified.",
	},
	{
		agent: "reviewer",
		role:  "REVIEW",
		focus: "Independently REVIEW the changes for correctness against the task. Read the changed files and " +
			"report concrete problems: missing requirements, wrong content/format/location, off-by-one or edge " +
			"cases, or a placeholder left where real work was asked for. Be specific (file and line). If it is " +
			"correct and complete, say so plainly.",
	},
}

// hasReviewGateAgents reports whether both delegated reviewers are configured, so
// the gate can degrade to the self-verify prompt when they are not.
func (a *App) hasReviewGateAgents() bool {
	for _, r := range reviewGateAgents {
		if _, ok := a.cfg.Agents[r.agent]; !ok {
			return false
		}
	}
	return true
}

// reviewGateTimeout bounds each delegated reviewer well under the 5m subagent
// hard cap: verification/review of a single turn's work is focused, and one
// reviewer chasing a bad target must not stall the gate (which waits for both).
const reviewGateTimeout = 4 * time.Minute

// runReviewGate dispatches the tester and reviewer subagents in parallel to
// independently check the turn's work, then injects their combined findings as a
// system prompt so the main agent addresses any real issue before finishing. It
// is the delegated counterpart to the self-verify self-prompt: an independent,
// fresh-context check catches what the implementer's own confirmation bias
// misses. A reviewer that errors or returns nothing still contributes a note
// (e.g. "could not complete: ..."), which the injected prompt's tail treats as
// UNFINISHED so a failed check can't read as a pass. Best-effort: only the
// degenerate case of no configured reviewers injects nothing (the caller has
// consumed the once-per-run budget and the council still gates termination).
func (a *App) runReviewGate(ctx context.Context, s session.Session, task string, changes []fileChange) {
	files := changedFilesList(changes)

	// Each goroutine writes its own index, so texts stays ordered without a sort
	// and the writes are race-free (no append, no reallocation).
	texts := make([]string, len(reviewGateAgents))
	var wg sync.WaitGroup
	for i, r := range reviewGateAgents {
		wg.Add(1)
		go func(i int, agent, role, focus string) {
			defer wg.Done()
			prompt := fmt.Sprintf("%s\n\nTask being checked:\n%s\n\nFiles changed this turn:\n%s\n\n"+
				"Report your findings concisely. Do NOT modify any file.", focus, strings.TrimSpace(task), files)
			rctx, cancel := context.WithTimeout(ctx, reviewGateTimeout)
			defer cancel()
			out := a.spawn(rctx, s, 0, port.SpawnRequest{Agent: agent, Prompt: prompt})
			text := strings.TrimSpace(out.Text)
			if out.Err != "" {
				text = "(could not complete: " + out.Err + ")"
			}
			if text == "" {
				text = "(no findings returned)"
			}
			texts[i] = fmt.Sprintf("## %s (%s)\n%s", role, agent, text)
		}(i, r.agent, r.role, r.focus)
	}
	wg.Wait()

	parts := make([]string, 0, len(texts))
	for _, t := range texts {
		if t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return
	}

	msg := "# Independent verification & review\n\n" +
		"Before finishing, two independent read-only subagents checked your work with fresh eyes. " +
		"Their findings:\n\n" + strings.Join(parts, "\n\n") + "\n\n---\n" +
		"Treat these as an outside check, not as your own optimistic self-assessment. If they found a REAL " +
		"problem — a failing build/test, an unmet requirement, wrong or placeholder content — FIX it now and " +
		"re-check, then finish. If they confirm the work is correct and complete, finish. Do not paper over a " +
		"gap they surfaced or restate that it works without addressing it; if verification could not actually " +
		"run, treat that as UNFINISHED rather than assuming success."
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
	})
	_ = a.appendFact(ctx, s.ID, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "review"}, pd)
}

// changedFilesList renders the turn's touched files as a short bullet list for the
// reviewers' prompt, marking newly-created files. Falls back to a hint when the
// turn mutated via bash (which never populates the changeSet).
func changedFilesList(changes []fileChange) string {
	if len(changes) == 0 {
		return "(none tracked via edit/write — the turn may have changed files via bash; " +
			"inspect the working tree, e.g. `git status`/`git diff`, to find what changed)"
	}
	var b strings.Builder
	for _, c := range changes {
		if c.before == "" {
			fmt.Fprintf(&b, "- %s (new)\n", c.path)
		} else {
			fmt.Fprintf(&b, "- %s\n", c.path)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
