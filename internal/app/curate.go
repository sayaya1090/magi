package app

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

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

const curateSystem = "You prepare a work packet for a worker sub-agent that will carry out ONE sub-task. " +
	"You are given the surrounding context, the sub-task, and a list of SPECIALIZED tools available. " +
	"Reply with ONLY a JSON object: {\"brief\": string, \"tools\": [string]}.\n" +
	"brief: restate the sub-task as a self-contained instruction the worker can act on alone. Copy " +
	"EVERY literal identifier — a name, field, function/message, output format, threshold, or literal " +
	"string — VERBATIM from the input; never paraphrase, rename, shorten, or normalize it (if it says " +
	"`value`, write `value`, not `val`). Include only what THIS sub-task needs.\n" +
	"tools: the specialized tools even SLIGHTLY relevant to the sub-task (err toward including — a " +
	"missing tool blocks the worker). Use exact names from the provided list; omit any you are unsure " +
	"exist. Basic file/shell/report tools are always available and must NOT be listed."

type curatePacket struct {
	Brief string   `json:"brief"`
	Tools []string `json:"tools"`
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
	return strings.TrimSpace(pkt.Brief), a.resolveCuratedTools(pkt.Tools)
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
