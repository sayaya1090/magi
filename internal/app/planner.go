package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// plannerAgent is the agent name the pre-flight planner is configured under (its
// system prompt, model, and provider come from cfg.Agents["planner"], so it is
// routable like any other agent, e.g. [routing] planner = "fast").
const plannerAgent = "planner"

// maxPlanGroups caps the planner's fan-out so a runaway decomposition can't
// spawn an unbounded number of explorers.
const maxPlanGroups = 5

// planGroup is one independent investigation the planner wants to parallelize.
type planGroup struct {
	Agent    string `json:"agent"`    // read-only explorer: explore|locator|analyst
	Focus    string `json:"focus"`    // short label of the area
	Question string `json:"question"` // what this explorer should find out
}

// planResult is the planner's verdict: investigate solo, or fan out to groups.
type planResult struct {
	Parallel bool        `json:"parallel"`
	Reason   string      `json:"reason"`
	Groups   []planGroup `json:"groups"`
}

// readOnlyExplorers are the only agents the planner may dispatch — investigation
// is read-only, so there are no file conflicts and nothing to fabricate-then-write.
var readOnlyExplorers = map[string]bool{"explore": true, "locator": true, "analyst": true}

// maybePlanPreflight runs the planner before a top-level turn (free-form or
// workflow). If the planner judges the task parallelizable, it dispatches the
// read-only explorers concurrently and injects their combined findings into the
// session so the main agent starts with that context. Best-effort: any failure
// degrades to solo (the normal path), never blocking the turn.
func (a *App) maybePlanPreflight(ctx context.Context, s session.Session) {
	if !a.cfg.Planner {
		return
	}
	spec, ok := a.cfg.Agents[plannerAgent]
	if !ok {
		return // planner not configured
	}
	prompt := a.lastUserPrompt(ctx, s.ID)
	if strings.TrimSpace(prompt) == "" {
		return
	}

	plan := a.runPlanner(ctx, spec, s, prompt)
	reason := strings.TrimSpace(plan.Reason)
	groups := sanitizePlan(plan)
	if len(groups) < 2 {
		a.emitPhase(s.ID, "plan", "solo", reason) // ran, judged single-area
		return                                    // solo — the default, cheap path
	}

	a.emitPhase(s.ID, "plan", "parallel", fmt.Sprintf("%d explorers · %s", len(groups), reason))
	findings := a.runExplorers(ctx, s, groups)
	if strings.TrimSpace(findings) == "" {
		return
	}
	a.injectPlannerFindings(ctx, s.ID, findings)
}

// runPlanner does a single, tool-free LLM call on the planner's own provider and
// parses the first JSON object from the reply. Returns a zero planResult on any
// error (→ solo).
func (a *App) runPlanner(ctx context.Context, spec AgentSpec, s session.Session, prompt string) planResult {
	sys := spec.System + "\n\n# Repository (top level)\n" + repoMap(s.Workdir) +
		"\n\nReply with ONLY a JSON object, no prose."
	// Write the human-facing 'reason' in the user's language (the JSON keys stay ASCII).
	if dir := langDirective(prompt); dir != "" {
		sys = dir + " Write the JSON \"reason\" value in that language.\n\n" + sys
	}
	model := s.Model.Model
	if spec.Model != (session.ModelRef{}) {
		model = spec.Model.Model
	}
	req := port.ChatRequest{
		Model:    model,
		System:   sys,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: prompt}}}},
	}
	stream, err := a.providerFor(spec).StreamChat(ctx, req)
	if err != nil {
		return planResult{}
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	return parsePlan(b.String())
}

// parsePlan extracts the first {...} JSON object from text (models often wrap it
// in prose or fences) and unmarshals it. Invalid → zero planResult.
func parsePlan(text string) planResult {
	i := strings.IndexByte(text, '{')
	j := strings.LastIndexByte(text, '}')
	if i < 0 || j <= i {
		return planResult{}
	}
	var p planResult
	if json.Unmarshal([]byte(text[i:j+1]), &p) != nil {
		return planResult{}
	}
	return p
}

// sanitizePlan enforces the guardrails: only when parallel, only read-only
// explorers (others coerced to explore), non-empty questions, capped fan-out.
func sanitizePlan(p planResult) []planGroup {
	if !p.Parallel {
		return nil
	}
	var out []planGroup
	for _, g := range p.Groups {
		if strings.TrimSpace(g.Question) == "" {
			continue
		}
		if !readOnlyExplorers[g.Agent] {
			g.Agent = "explore"
		}
		out = append(out, g)
		if len(out) == maxPlanGroups {
			break
		}
	}
	return out
}

// runExplorers dispatches the groups as read-only subagents concurrently and
// returns their findings concatenated in a stable order.
func (a *App) runExplorers(ctx context.Context, s session.Session, groups []planGroup) string {
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
			prompt := fmt.Sprintf("Investigate (read-only): %s\n\n%s", g.Focus, g.Question)
			r := a.spawn(ctx, s, 0, port.SpawnRequest{Agent: g.Agent, Prompt: prompt})
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

// injectPlannerFindings appends the explorers' combined findings as a system
// message so the main agent begins with them.
func (a *App) injectPlannerFindings(ctx context.Context, sid session.SessionID, findings string) {
	text := "# Investigation findings (from the explorer subagents you just dispatched)\n\n" + findings +
		"\n\n---\nThese are the results of YOUR OWN read-only explorers — trust them as your primary " +
		"source and SYNTHESIZE from them directly. Do NOT re-read or re-investigate what is already " +
		"covered above; open a file again only if you must quote or modify an exact line. Proceed with the task."
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})
	_ = a.appendFact(ctx, sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "planner"}, pd)
}

// lastUserPrompt returns the text of the most recent user-submitted prompt.
func (a *App) lastUserPrompt(ctx context.Context, sid session.SessionID) string {
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		return ""
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type == event.TypePromptSubmitted && evs[i].Actor.Kind == event.ActorUser {
			var d event.PromptSubmittedData
			if json.Unmarshal(evs[i].Data, &d) == nil {
				return joinPartText(d.Parts)
			}
		}
	}
	return ""
}

func joinPartText(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// repoMap lists the workdir's top-level entries (dirs marked) to ground the
// planner without an expensive scan. Bounded and best-effort.
func repoMap(workdir string) string {
	ents, err := os.ReadDir(workdir)
	if err != nil {
		return "(unavailable)"
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		if e.IsDir() {
			n += "/"
		}
		names = append(names, n)
		if len(names) == 40 {
			break
		}
	}
	return strings.Join(names, " ")
}
