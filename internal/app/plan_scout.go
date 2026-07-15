package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// capGroups trims a parallel step's groups to the remaining per-turn budget.
func capGroups(groups []planGroup, budget *int) []planGroup {
	if len(groups) > *budget {
		groups = groups[:*budget]
	}
	*budget -= len(groups)
	return groups
}

// scoutGroups runs the scout: one read-only explorer produces a work-list, then
// each item becomes a parallel investigation. This is the adaptive scout→fanout —
// the fan-out targets are discovered at runtime, not pre-planned.
func (a *App) scoutGroups(ctx context.Context, s session.Session, st planStep, budget *int, depth int) []planGroup {
	agent := st.Agent
	if !readOnlyExplorers[agent] {
		agent = "explore"
	}
	if *budget <= 0 {
		return nil
	}
	listPrompt := fmt.Sprintf("List %s. Output ONLY the items, one per line — no prose, no numbering, no bullets, no markdown. The FIRST line must already be an item: no title, header, preamble (\"Here are…\", \"List of…:\"), or closing remark.", st.Discover)
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agent, Prompt: listPrompt})
	*budget-- // the scout itself counts
	if r.Err != "" {
		return nil
	}
	items := parseList(stripReportStatus(r.Text))
	kept := items[:0]
	for _, it := range items {
		if keepScoutItem(s.Workdir, it) {
			kept = append(kept, it)
		}
	}
	items = kept
	each := strings.TrimSpace(st.Each)
	if each == "" {
		each = "Investigate this item (read-only)"
	}
	var groups []planGroup
	for _, it := range items {
		if *budget <= 0 || len(groups) == maxPlanGroups {
			break
		}
		groups = append(groups, planGroup{Agent: agent, Focus: it, Question: each + "\n\nItem: " + it})
		*budget--
	}
	return groups
}

// stripReportStatus drops the leading "STATUS: <WORD>" line that subReport.result
// (app.go) prepends to a subagent's report. A scout's discovery agent files its
// work-list via the report tool, so r.Text arrives report-framed; without this the
// frame line itself ("STATUS: DONE") would be parsed as a bogus work-item. Only the
// exact two-field frame is removed (via reportStatusWord, which matches the "STATUS:"
// keyword case-insensitively — the sole producer emits it upper-case), so a legitimate
// first item that merely starts with "STATUS:" (multi-word) or a path is never touched.
func stripReportStatus(text string) string {
	text = strings.TrimLeft(text, "\n")
	if line, rest, ok := strings.Cut(text, "\n"); ok && reportStatusWord(line) != "" {
		return rest
	}
	return text
}

// endsWithSentencePunct reports whether s ends in heading/sentence punctuation, which
// marks a header or prose line rather than a work-item (paths/names never do).
func endsWithSentencePunct(s string) bool {
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case ':', '.', '!', '?':
		return true
	}
	return false
}

// keepScoutItem decides whether a parsed discovery line is a real work-item. A real
// file/path/symbol target is a SINGLE TOKEN; a multi-word line is almost always prose —
// a header or sentence the model emitted around its list ("List of files in project
// root and docs directory") that punctuation/field heuristics miss. Keep a multi-word
// line ONLY when it resolves to a real path inside the workdir (a genuine path that
// happens to contain a space). Dropping a multi-word non-path just skips one explorer
// (a benign per-step degrade); keeping one sends an explorer chasing a target that does
// not exist, which flails until its timeout and stalls the whole scout step.
func keepScoutItem(workdir, item string) bool {
	item = strings.Trim(item, "\"'`")
	if item == "" {
		return false
	}
	if !strings.ContainsAny(item, " \t") {
		return true // single token: a path/symbol we can't cheaply validate — keep
	}
	p := filepath.Join(workdir, item)
	root := filepath.Clean(workdir)
	if p != root && !strings.HasPrefix(p, root+string(os.PathSeparator)) {
		return false // escapes the workdir → not a real in-tree item
	}
	_, err := os.Stat(p)
	return err == nil
}

// parseList turns a scout's free-text reply into a clean work-list: one item per
// line, stripping numbering/bullets/fences and blank or prose-like lines.
func parseList(text string) []string {
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "```") {
			continue
		}
		ln = strings.TrimLeft(ln, "-*•0123456789.) \t")
		ln = strings.TrimSpace(ln)
		if ln == "" || len(ln) > 200 {
			continue
		}
		// A work-item is a short token/path/name. A MULTI-WORD line is prose, not an
		// item, when it is long OR ends in sentence/heading punctuation — i.e. a header
		// or preamble the model printed before its list ("List of documentation files:",
		// "Here are the docs.") or a trailing remark. Dispatching one sends an explorer
		// chasing a target that doesn't exist, flailing until the subagent timeout.
		// Single-token items are always kept ("path:line", a "server:" config key).
		if strings.Contains(ln, " ") && (len(strings.Fields(ln)) > 12 || endsWithSentencePunct(ln)) {
			continue
		}
		out = append(out, ln)
		if len(out) == maxPlanGroups {
			break
		}
	}
	return out
}

func (a *App) runExplorers(ctx context.Context, s session.Session, groups []planGroup, goal string, depth int) string {
	type res struct {
		i    int
		text string
	}
	results := make([]res, 0, len(groups))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i, g := range groups {
		wg.Add(1)
		go func(i int, g planGroup) {
			defer wg.Done()
			prompt := explorerPrompt(goal, g)
			// Bound each read-only explorer well under the 5m subagent cap: a focused
			// investigation is quick, and one explorer chasing a bad target must not stall
			// the whole step (runExplorers waits for all) for the full SubagentTimeout.
			ectx, ecancel := context.WithTimeout(ctx, explorerTimeout)
			defer ecancel()
			r := a.spawn(ectx, s, depth, port.SpawnRequest{Agent: g.Agent, Prompt: prompt})
			text := r.Text
			if r.Err != "" {
				text = "(failed: " + r.Err + ")"
			}
			mu.Lock()
			results = append(results, res{i, fmt.Sprintf("## %s\n%s", g.Focus, strings.TrimSpace(text))})
			mu.Unlock()
		}(i, g)
	}
	wg.Wait()
	sort.Slice(results, func(a, b int) bool { return results[a].i < results[b].i })
	parts := make([]string, len(results))
	for i, r := range results {
		parts[i] = r.text
	}
	return strings.Join(parts, "\n\n")
}
