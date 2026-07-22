package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The context curator (MAGI_CURATE): before a delegate worker is spawned, it prepares the worker's
// context packet — a focused, literal-preserving brief plus a task-scoped tool allowlist — so the
// worker runs lean instead of inheriting the full tool corpus and a mechanical brief.
//
// Safety is structural: the worker ALWAYS keeps curateBaseTools (basic file/shell/report ops), so
// the curator's selection can only ADD specialized tools (lsp, web, aggregation, …), never starve
// the worker. And the brief must carry every literal identifier VERBATIM — the make-or-break rule,
// since a paraphrased spec is exactly how a weak worker renames `value`→`val` and fails a grader.

// curateBaseTools are always granted to a curated worker, whatever the curator selects: without
// them it cannot read, edit, run, or report. The curator only ADDS specialized tools on top.
var curateBaseTools = []string{
	"read", "write", "edit", "multiedit", "grep", "glob", "list",
	"bash", "bash_output", "bash_input", "bash_kill", "todowrite", "report", "ask", "skill",
}

const curateSystem = "You prepare a work packet for a worker sub-agent that carries out ONE sub-task of a " +
	"larger job. It starts with NO memory of the overall request — your packet is all it sees. Reply with " +
	"ONLY a JSON object with these fields; the STRUCTURE tells the worker what weighs most:\n" +
	"- goal: WHY this work exists — the overall objective and what the finished result should be, so the " +
	"worker understands where its part fits.\n" +
	"- progress: what earlier steps ALREADY produced (files created, decisions made, interfaces defined) so " +
	"the worker BUILDS ON it and does not redo or contradict it. Omit if this is the first step.\n" +
	"- task: the RESULT this worker must achieve, stated concretely — what must be TRUE when it is done. " +
	"Delegate the outcome, not the keystrokes: leave HOW to the worker unless one specific method is " +
	"required, and only then name it.\n" +
	"- literals: an array of the EXACT strings that must appear UNCHANGED in the worker's output — names, " +
	"fields, function/message names, output formats, thresholds, literal values — copied VERBATIM from the " +
	"input. Highest-weight field: the worker must never rename, shorten, or normalize any (if the input says " +
	"`value`, list `value`, never `val`). Empty array if none.\n" +
	"- constraints: the boundaries the worker must respect — what NOT to change, behavior/interfaces that " +
	"must stay intact, non-goals, limits. An array; empty if none.\n" +
	"- deliverable: what must exist or pass for this sub-task to be counted done (the acceptance test).\n" +
	"- tools: the specialized tools even SLIGHTLY relevant (err toward including — a missing tool blocks the " +
	"worker). Exact names from the list; omit any you are unsure exist. Basic file/shell/report tools are " +
	"always available and must NOT be listed.\n" +
	"Include only what THIS sub-task needs; keep each field tight."

type curatePacket struct {
	Goal        string   `json:"goal"`        // why the work exists / the final objective
	Progress    string   `json:"progress"`    // what earlier steps already produced
	Task        string   `json:"task"`        // the RESULT wanted (outcome, not method)
	Literals    []string `json:"literals"`    // verbatim strings that must not change
	Constraints []string `json:"constraints"` // boundaries: what not to change / non-goals
	Deliverable string   `json:"deliverable"` // acceptance test for done-ness
	Tools       []string `json:"tools"`
}

// renderCurateBrief formats a packet into the weighted, sectioned CONTEXT a worker reads around its
// task: WHY the work exists, what is already done (build on it), the verbatim literals it must not
// change (highest weight), the boundaries it must not cross, and the done-when acceptance test. The
// task itself is NOT rendered here — delegatePrompt states it under its own "YOUR PART" header, so
// this brief stays pure context and never duplicates the instruction. Empty when unusable.
func renderCurateBrief(p curatePacket) string {
	var b strings.Builder
	section := func(title, body string) {
		if s := strings.TrimSpace(body); s != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("# " + title + "\n" + s + "\n")
		}
	}
	bullets := func(items []string) string {
		var out []string
		for _, it := range items {
			if s := strings.TrimSpace(it); s != "" {
				out = append(out, "- "+s)
			}
		}
		return strings.Join(out, "\n")
	}
	section("Goal (why this exists)", p.Goal)
	section("Progress so far (build on this — do NOT redo it)", p.Progress)
	section("Preserve these EXACTLY (verbatim — never rename, shorten, or normalize)", bullets(p.Literals))
	section("Boundaries (do NOT cross)", bullets(p.Constraints))
	section("Done when", p.Deliverable)
	return strings.TrimSpace(b.String())
}

// curateDelegate builds a delegate worker's context packet from the surrounding context and the
// step. Returns (brief, tools). Best-effort: on any failure it returns ("", nil) so the caller
// falls back to the mechanical brief and the worker's default toolset — curation never blocks a
// delegate.
func (a *App) curateDelegate(ctx context.Context, agent AgentSpec, s session.Session, st planStep, contextBrief string) (string, []string) {
	task := strings.TrimSpace(st.Task)
	if task == "" {
		task = strings.TrimSpace(st.Title)
	}
	if task == "" {
		return "", nil
	}
	specialized := a.specializedToolNames()
	if len(specialized) == 0 {
		return "", nil
	}
	model := s.Model.Model
	if agent.Model != (session.ModelRef{}) {
		model = agent.Model.Model
	}
	var b strings.Builder
	if c := strings.TrimSpace(contextBrief); c != "" {
		b.WriteString("Context:\n" + clipSpec(c, 1500) + "\n\n")
	}
	b.WriteString("Sub-task:\n" + clipSpec(task, 1500) + "\n\nSpecialized tools available: " + strings.Join(specialized, ", "))
	raw := a.specMineCall(ctx, agent, model, curateSystem, b.String()) // reuse the tool-free elicitation
	pkt, ok := parseCuratePacket(raw)
	if !ok {
		return "", nil
	}
	brief := renderCurateBrief(pkt)
	if brief == "" { // nothing usable produced → fall back to the mechanical brief
		return "", nil
	}
	tools := a.resolveCuratedTools(pkt.Tools)
	// Transparency: surface what the curator produced so a run is interpretable (which specialized
	// tools it added over the base, and the brief size) — the delegate hand-off is otherwise opaque.
	added := selectedSpecialized(tools)
	a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorAgent, ID: "curator"}, "", "curator",
		fmt.Sprintf("curated worker context — brief %d chars, +%d specialized tool(s) [%s]",
			len(brief), len(added), strings.Join(added, ", ")))
	return brief, tools
}

// selectedSpecialized returns the non-base tools in a curated allowlist — the ones the curator
// actually chose to ADD for the sub-task (the base set is always present).
func selectedSpecialized(tools []string) []string {
	base := map[string]bool{}
	for _, n := range curateBaseTools {
		base[n] = true
	}
	var out []string
	for _, n := range tools {
		if !base[n] {
			out = append(out, n)
		}
	}
	return out
}

// specializedToolNames lists the non-base, worker-callable registered tools the curator may select
// from (sorted, stable). Base tools are always granted; orchestration-only tools are never a
// worker's to call, so both are excluded from the menu.
func (a *App) specializedToolNames() []string {
	base := map[string]bool{}
	for _, n := range curateBaseTools {
		base[n] = true
	}
	var out []string
	for _, t := range a.tools.List() {
		n := t.Name()
		if base[n] {
			continue
		}
		switch n {
		case "task", "resolveconcern", "cancel_dispatch", "route_interjection", "ask_user", "replan":
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// resolveCuratedTools returns the worker's allowlist: the always-on base UNION the curator's
// selection, keeping only names that are actually registered (an invented name is dropped).
func (a *App) resolveCuratedTools(selected []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			return
		}
		if _, ok := a.tools.Get(n); !ok {
			return
		}
		seen[n] = true
		out = append(out, n)
	}
	for _, n := range curateBaseTools {
		add(n)
	}
	for _, n := range selected {
		add(n)
	}
	return out
}

func parseCuratePacket(raw string) (curatePacket, bool) {
	s := strings.TrimSpace(raw)
	i, j := strings.IndexByte(s, '{'), strings.LastIndexByte(s, '}')
	if i < 0 || j <= i {
		return curatePacket{}, false
	}
	var p curatePacket
	if json.Unmarshal([]byte(s[i:j+1]), &p) != nil {
		return curatePacket{}, false
	}
	return p, true
}
