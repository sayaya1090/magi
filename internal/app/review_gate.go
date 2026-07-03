package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// The review gate dispatches two kinds of read-only specialist, and they scale
// differently. VERIFY (tester) is holistic — one build/test run covers the whole
// change, and splitting it would only lose cross-cutting/integration signal — so
// there is always exactly one. REVIEW (reviewer) reads the changed source, which
// parallelizes by area: a single reviewer over dozens of files goes shallow, so
// the changed surface is grouped (by directory) and one reviewer is dispatched
// per group, exactly as the planner fans explorers out over independent areas.
const (
	testerFocus = "Independently VERIFY that the work satisfies the task. Actually run the build and the tests " +
		"(or the program/command the task is about) and check the real behavior against what the task " +
		"specifies — do not trust the transcript's claims. Report concrete evidence (the command you ran " +
		"and its real output); never invent or hand-write output to make it look verified. Your reasoning " +
		"may be in any language, but you MUST end your report with a single line containing exactly one of: " +
		"`VERDICT: PASS` (you ran the verification and it really works), `VERDICT: FAIL` (you ran it and " +
		"found a real problem), or `VERDICT: BLOCKED` (you could NOT actually run the verification). If you " +
		"did not or could not run the real thing, the verdict is BLOCKED — never PASS on an assumption."
	reviewerFocus = "Independently REVIEW the changes for correctness against the task. Read the changed files " +
		"assigned to you and report concrete problems: missing requirements, wrong content/format/location, " +
		"off-by-one or edge cases, or a placeholder left where real work was asked for. Be specific (file and " +
		"line). If your slice is correct and complete, say so plainly."

	// reviewGateTimeout bounds each delegated reviewer well under the 5m subagent
	// hard cap: verification/review of one turn's work is focused, and one reviewer
	// chasing a bad target must not stall the gate (which waits for all of them).
	reviewGateTimeout = 4 * time.Minute
	// reviewSpawnBudget caps the review subagents dispatched across a whole run
	// (summed over re-arm firings), mirroring the planner's per-turn explorer cap.
	// Each firing spends 1 (tester) + one per changed-area group out of this budget;
	// when it is gone the council alone gates termination.
	reviewSpawnBudget = 8
	// reviewGroupMaxFiles bounds how many changed files one reviewer inspects before
	// the area is split into a further group, keeping each reviewer's slice shallow
	// enough to review deeply.
	reviewGroupMaxFiles = 5

	// The tester ends its report with one of these verdict tokens on a `VERDICT: <token>`
	// line. This is a small fixed PROTOCOL, deliberately NOT natural-language phrase
	// matching: the tester may reason in any language, but the machine-checked outcome is
	// one of these three tokens — so completion is gated on real behavior, not on scanning
	// the model's prose for English confession phrases (which miss other languages and
	// paraphrases). See parseTesterVerdict.
	verdictPass    = "PASS"    // the tester ran the real verification and it works
	verdictFail    = "FAIL"    // the tester ran it and found a real problem
	verdictBlocked = "BLOCKED" // the tester could NOT actually run it → treat as unverified
)

// parseTesterVerdict extracts the tester's PASS/FAIL/BLOCKED verdict from its report by
// scanning for the mandated `VERDICT: <token>` line, case-insensitively, and returns the
// LAST recognized token (the tester's final conclusion, after any intermediate mention).
// A missing or unrecognized verdict is treated as BLOCKED — an unverified result must
// never read as a pass, so the fresh-evidence gate keeps completion closed. This parses a
// protocol token we mandated, not a free-form confession, so it is language-agnostic: the
// tester's evidence and reasoning can be in any language.
func parseTesterVerdict(text string) string {
	verdict := verdictBlocked
	for _, ln := range strings.Split(text, "\n") {
		// Tolerate markdown/emphasis/quote decoration around the token line.
		t := strings.ToUpper(strings.Trim(strings.TrimSpace(ln), "*_`>#- \t"))
		if !strings.HasPrefix(t, "VERDICT:") {
			continue
		}
		switch strings.TrimSpace(strings.TrimPrefix(t, "VERDICT:")) {
		case verdictPass:
			verdict = verdictPass
		case verdictFail:
			verdict = verdictFail
		case verdictBlocked:
			verdict = verdictBlocked
		}
	}
	return verdict
}

// hasReviewGateAgents reports whether both delegated roles are configured, so the
// gate can degrade to the self-verify prompt when they are not.
func (a *App) hasReviewGateAgents() bool {
	_, hasTester := a.cfg.Agents["tester"]
	_, hasReviewer := a.cfg.Agents["reviewer"]
	return hasTester && hasReviewer
}

// reviewGroup is a coherent slice of the turn's changed files assigned to one
// reviewer (empty files = "inspect the working tree", used for bash-only changes).
type reviewGroup struct {
	label string
	files []string
}

// reviewGroups partitions the changed files by directory into review-sized slices.
// With no tracked changes (a bash-only turn) it returns a single working-tree group
// so one reviewer still inspects `git status`/`git diff`.
func reviewGroups(changes []fileChange, maxPerGroup int) []reviewGroup {
	if len(changes) == 0 {
		return []reviewGroup{{label: "(working tree)"}}
	}
	byDir := map[string][]string{}
	var order []string
	for _, c := range changes {
		d := filepath.Dir(c.path)
		if d == "." || d == "" {
			d = "(root)"
		}
		if _, ok := byDir[d]; !ok {
			order = append(order, d)
		}
		marker := c.path
		if c.before == "" {
			marker += " (new)"
		}
		byDir[d] = append(byDir[d], marker)
	}
	var groups []reviewGroup
	for _, d := range order {
		files := byDir[d]
		for i := 0; i < len(files); i += maxPerGroup {
			end := i + maxPerGroup
			if end > len(files) {
				end = len(files)
			}
			label := d
			if i > 0 {
				label = fmt.Sprintf("%s [%d]", d, i/maxPerGroup+1)
			}
			groups = append(groups, reviewGroup{label: label, files: files[i:end]})
		}
	}
	return groups
}

// runReviewGate dispatches one holistic tester plus one reviewer per changed-area
// group in parallel — up to `budget` subagents total — to independently check the
// turn's work, then injects their combined findings as a system prompt so the main
// agent addresses any real issue before finishing. It is the delegated counterpart
// to the self-verify self-prompt: an independent, fresh-context check catches what
// the implementer's own confirmation bias misses. A reviewer that errors or returns
// nothing still contributes a note (e.g. "could not complete: ..."), which the
// injected prompt's tail treats as UNFINISHED so a failed check can't read as a
// pass. Returns the number of subagents actually spawned (so the caller can debit
// the per-run budget) and the tester's PASS/FAIL/BLOCKED verdict (the behavioral,
// language-agnostic completion signal — see parseTesterVerdict). A spawn count of 0
// yields a BLOCKED verdict (nothing was verified). budget is assumed > 0 by the caller.
func (a *App) runReviewGate(ctx context.Context, s session.Session, task string, changes []fileChange, budget int) (spawned int, verdict string) {
	if budget <= 0 { // enforce the budget contract even if a caller forgets to
		return 0, verdictBlocked
	}
	// The tester always runs first (holistic build/test is the anti-fabrication
	// core); remaining budget fans out reviewers over the changed-area groups.
	type job struct{ role, agent, focus, files string }
	jobs := []job{{role: "VERIFY", agent: "tester", focus: testerFocus, files: changedFilesList(changes)}}
	for _, g := range reviewGroups(changes, reviewGroupMaxFiles) {
		if len(jobs) >= budget {
			break
		}
		jobs = append(jobs, job{
			role:  "REVIEW · " + g.label,
			agent: "reviewer",
			focus: reviewerFocus,
			files: groupFilesList(g),
		})
	}

	// Each goroutine writes its own index, so texts stays ordered without a sort
	// and the writes are race-free (no append, no reallocation). raw[i] keeps the
	// unwrapped agent text so the tester's verdict line can be parsed (raw[0] — the
	// tester always runs first).
	texts := make([]string, len(jobs))
	raw := make([]string, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, j job) {
			defer wg.Done()
			prompt := fmt.Sprintf("%s\n\nTask being checked:\n%s\n\nFiles to check:\n%s\n\n"+
				"Report your findings concisely. Do NOT modify any file.", j.focus, strings.TrimSpace(task), j.files)
			rctx, cancel := context.WithTimeout(ctx, reviewGateTimeout)
			defer cancel()
			out := a.spawn(rctx, s, 0, port.SpawnRequest{Agent: j.agent, Prompt: prompt})
			text := strings.TrimSpace(out.Text)
			raw[i] = text
			if out.Err != "" {
				text = "(could not complete: " + out.Err + ")"
			}
			if text == "" {
				text = "(no findings returned)"
			}
			texts[i] = fmt.Sprintf("## %s (%s)\n%s", j.role, j.agent, text)
		}(i, j)
	}
	wg.Wait()
	// The tester (jobs[0]) carries the behavioral verdict; a spawn that errored/returned
	// nothing parses as BLOCKED (unverified), never PASS.
	verdict = parseTesterVerdict(raw[0])

	parts := make([]string, 0, len(texts))
	for _, t := range texts {
		if t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return len(jobs), verdict
	}

	msg := "# Independent verification & review\n\n" +
		"Before finishing, independent read-only subagents checked your work with fresh eyes " +
		"(one verifier running the build/tests, plus a reviewer per changed area). Their findings:\n\n" +
		strings.Join(parts, "\n\n") + "\n\n---\n" +
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
	return len(jobs), verdict
}

// changedFilesList renders the turn's touched files as a short bullet list for the
// tester's prompt, marking newly-created files. Falls back to a hint when the turn
// mutated via bash (which never populates the changeSet).
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

// groupFilesList renders one reviewer's assigned slice; an empty group (bash-only
// changes) directs the reviewer to the working tree.
func groupFilesList(g reviewGroup) string {
	if len(g.files) == 0 {
		return "(no edit/write-tracked files — inspect the working tree with `git status`/`git diff` " +
			"and review whatever this turn changed)"
	}
	return "- " + strings.Join(g.files, "\n- ")
}
