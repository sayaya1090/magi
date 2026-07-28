package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/jsonx"
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
	"bash", "bash_output", "bash_input", "bash_kill", "port_owner", "todowrite", "report", "ask", "skill",
	"substitute_check", // every worker may hit a check it cannot satisfy as written and must be able to substitute it
}

const curateSystem = "You prepare a work packet for a worker sub-agent that carries out ONE sub-task of a " +
	"larger job. It starts with NO memory of the overall request — your packet is all it sees. Reply with " +
	"ONLY a JSON object with these fields; the STRUCTURE tells the worker what weighs most:\n" +
	"- goal: WHY this work exists — the overall objective and what the finished result should be, so the " +
	"worker understands where its part fits.\n" +
	"- progress: what earlier steps ALREADY produced (files created, decisions made, interfaces defined) so " +
	"the worker BUILDS ON it and does not redo or contradict it. Omit if this is the first step.\n" +
	"- missing: what earlier steps attempted but did NOT finish — outputs that do NOT exist. Never put " +
	"these under progress: a worker told to build on something that was never produced builds on nothing. " +
	"Say plainly that it is absent, and why if the input says so. Omit if nothing failed.\n" +
	"- task: the RESULT this worker must achieve, stated concretely — what must be TRUE when it is done. " +
	"Delegate the outcome, not the keystrokes: leave HOW to the worker unless one specific method is " +
	"required, and only then name it.\n" +
	"- literals: an array of the EXACT strings that must appear UNCHANGED in the worker's output — names, " +
	"fields, function/message names, output formats, thresholds, literal values — copied VERBATIM from the " +
	"input. Highest-weight field: the worker must never rename, shorten, or normalize any (if the input says " +
	"`value`, list `value`, never `val`). Include ONLY strings the request REQUIRES exactly; EXCLUDE anything " +
	"offered as an EXAMPLE or suggestion (introduced by \"e.g.\", \"for example\", \"such as\", \"like\", or a " +
	"name invented to illustrate) and anything the request leaves the worker free to choose — pinning an example " +
	"filename wrongly forces that exact name. When unsure whether a name is required or illustrative, leave it out. " +
	"Empty array if none.\n" +
	"- constraints: the boundaries the worker must respect — what NOT to change, behavior/interfaces that " +
	"must stay intact, non-goals, limits. An array; empty if none.\n" +
	"- deliverable: what must exist or pass for this sub-task to be counted done (the acceptance test).\n" +
	"- tools: the specialized tools even SLIGHTLY relevant (err toward including — a missing tool blocks the " +
	"worker). Exact names from the list; omit any you are unsure exist. Basic file/shell/report tools are " +
	"always available and must NOT be listed.\n" +
	"Include only what THIS sub-task needs; keep each field tight."

// curateRetryReminder names the defect the previous reply actually had. "Reply with only JSON" is
// useless advice to a model whose object WAS bare and merely malformed, and it is the shape observed
// live — prose written between two array elements, unquoted — so the syntax branch says which
// bracket rule was broken instead of restating the format. The stakes are stated too: the model
// cannot know that an unreadable packet is not a neutral outcome for the worker downstream.
func curateRetryReminder(text string) string {
	const head = "\n\n# Your previous reply could not be used\nThat is not a neutral outcome: without a " +
		"packet the worker is handed a mechanical brief that loses the verbatim identifiers this packet " +
		"exists to carry, and it renames them. "
	d := jsonx.Diagnose(text)
	switch {
	case strings.HasPrefix(d, "syntax error"):
		return head + "The reply DID contain a JSON object, so the problem is not prose around it — the JSON " +
			"itself is malformed:\n" + d + "\nSend the SAME packet again as one well-formed JSON object. Every " +
			"`[` must be closed by `]` BEFORE the next key begins, every `{` by `}`, and every string by its " +
			"closing quote. Explanatory text belongs INSIDE a quoted string; it can never sit between two array " +
			"elements or between two keys."
	case strings.HasPrefix(d, "the JSON parses"):
		return head + "The JSON is well-formed but carries none of the packet's content: " + d + "\nSend the " +
			"object again using the field names above (goal, progress, missing, task, literals, constraints, deliverable, " +
			"tools), with the context the worker needs actually filled in."
	default:
		return head + "Reply with ONLY the JSON object — no prose, explanation, or markdown fence before or " +
			"after it."
	}
}

// The list fields are read tolerantly: a model that answers "literals" with a single bare string
// instead of a one-element list would otherwise fail the WHOLE packet, and the worker then falls
// back to the mechanical brief that loses exactly the verbatim identifiers this field carries.
type curatePacket struct {
	Goal        jsonx.Text  `json:"goal"`        // why the work exists / the final objective
	Progress    jsonx.Text  `json:"progress"`    // what earlier steps already produced
	Missing     jsonx.Text  `json:"missing"`     // what earlier steps did NOT produce (kept out of Progress on purpose)
	Task        jsonx.Text  `json:"task"`        // the RESULT wanted (outcome, not method)
	Literals    jsonx.Texts `json:"literals"`    // verbatim strings that must not change
	Constraints jsonx.Texts `json:"constraints"` // boundaries: what not to change / non-goals
	Deliverable jsonx.Text  `json:"deliverable"` // acceptance test for done-ness
	Tools       jsonx.Texts `json:"tools"`
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
	section("Goal (why this exists)", string(p.Goal))
	section("Progress so far (build on this — do NOT redo it)", string(p.Progress))
	// Its own section, never folded into progress: an earlier step's failure is information the
	// worker needs, but under the progress label it reads as a finished input to build on.
	section("NOT done — these outputs do NOT exist (do not build on them; produce what you need yourself, "+
		"or report that you are blocked)", string(p.Missing))
	section("Preserve these EXACTLY (verbatim — never rename, shorten, or normalize)", bullets(p.Literals))
	section("Boundaries (do NOT cross)", bullets(p.Constraints))
	section("Done when", string(p.Deliverable))
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
	raw := a.specMineCall(ctx, agent, s.ID, "curator", model, curateSystem, b.String()) // reuse the tool-free elicitation
	p0, ok0, salv0 := parseCuratePacketSalvage(raw)
	first := curateAttempt{pkt: p0, raw: raw, parsed: ok0, salvaged: salv0}
	// Falling back to the mechanical brief loses exactly what the packet exists to carry — the
	// verbatim identifiers the acceptance depends on — so an unreadable reply must not end the pass
	// silently-in-effect. Observed live: a 1490-char packet lost to one unquoted stretch of prose
	// inside an array, which the model can fix in a second attempt and cannot fix without being told.
	//
	// An empty BRIEF is the same loss by a different route — the object parsed but carries no context
	// worth handing over — so it takes the same re-ask rather than returning quietly. So is a packet
	// RECOVERED from a damaged reply: it renders a brief that reads complete while the fields the
	// truncation ate — the literals, the boundaries, the done-when — are simply absent.
	att, ok := reask[curateAttempt]{
		pass:  "curator",
		actor: event.Actor{Kind: event.ActorAgent, ID: "curator"},
		ask: func(system string) (curateAttempt, string, bool) {
			raw := a.specMineCall(ctx, agent, s.ID, "curator", model, system, b.String())
			p, ok, salvaged := parseCuratePacketSalvage(raw)
			at := curateAttempt{pkt: p, raw: raw, parsed: ok, salvaged: salvaged}
			return at, raw, at.parsed && !at.salvaged
		},
		defect: func(at curateAttempt, _ bool, _ string) string {
			switch {
			case !at.parsed:
				return fmt.Sprintf("packet unusable (%d chars, unparsed)", len(at.raw))
			case at.salvaged:
				return fmt.Sprintf("packet recovered from a DAMAGED reply (%d chars) — it carries %s, and "+
					"whatever the damage ate is missing from it", len(at.raw), briefShape(at.pkt))
			case renderCurateBrief(at.pkt) == "":
				return fmt.Sprintf("packet parsed (%d chars) but carries no brief", len(at.raw))
			}
			return ""
		},
		reminder: func(raw string, _ bool) string { return curateSystem + curateRetryReminder(raw) },
		probe:    func(b []byte) error { var p curatePacket; return json.Unmarshal(b, &p) },
		fallback: "falling back to the mechanical brief",
	}.run(ctx, a, s.ID, first, raw, first.parsed && !first.salvaged)
	pkt := att.pkt
	if !ok {
		// A partial packet still carries more of the task's own words than the mechanical brief does,
		// so a damaged reply that the re-ask could not repair is landed rather than discarded — but it
		// is landed as what it is, with the sections it actually has named.
		if !first.salvaged || renderCurateBrief(first.pkt) == "" {
			return "", nil
		}
		pkt = first.pkt
		a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorAgent, ID: "curator"}, "", "curator",
			"curator: using the packet recovered from the damaged reply — it is more of the task's own words than the "+
				"mechanical brief, but it is PARTIAL: "+briefShape(pkt))
	}
	// The floor goes on before the brief is rendered: what the REQUEST itself pins reaches the
	// "Preserve these EXACTLY" section whether or not the curator listed it. The step's own task is a
	// source too — a planner writes the literal it was given into the task text.
	if requestLiteralsEnabled() {
		pkt.Literals = withRequestLiterals(pkt.Literals, a.lastUserPrompt(ctx, s.ID), task, contextBrief)
	}
	brief := renderCurateBrief(pkt)
	tools := a.resolveCuratedTools(pkt.Tools)
	// Transparency: surface what the curator produced so a run is interpretable (which specialized
	// tools it added over the base, and the brief size) — the delegate hand-off is otherwise opaque.
	added := selectedSpecialized(tools)
	a.emitToolProgress(s.ID, event.Actor{Kind: event.ActorAgent, ID: "curator"}, "", "curator",
		fmt.Sprintf("curated worker context — brief %d chars %s, +%d specialized tool(s) [%s]",
			len(brief), briefShape(pkt), len(added), strings.Join(added, ", ")))
	return brief, tools
}

// curateAttempt is one curator reply and what could be read out of it. salvaged separates a packet
// the model wrote whole from one the recovery reconstructed out of a damaged reply — the two are
// indistinguishable once the brief is rendered, and only one of them is complete.
type curateAttempt struct {
	pkt      curatePacket
	raw      string
	parsed   bool
	salvaged bool
}

// briefShape names WHAT the curated brief carried: which sections it filled, and the verbatim
// literals it preserved. The size alone cannot answer the only question worth asking of a hand-off
// — was the worker told enough, and told it clearly — and the brief body itself lives in the CHILD
// session's event log, which a run that ships only its stdout does not carry. Two failures show up
// here and nowhere else: a brief with no `done-when`, which is a worker that was never told what
// finishing means, and a literals list that lost an identifier the acceptance depends on, which is
// the paraphrase spec-loss this packet exists to prevent — so the literals go in verbatim.
func briefShape(p curatePacket) string {
	var have []string
	for _, sec := range []struct {
		name string
		body string
	}{
		{"goal", string(p.Goal)}, {"progress", string(p.Progress)}, {"missing", string(p.Missing)},
		{"boundaries", strings.Join(p.Constraints, " ")}, {"done-when", string(p.Deliverable)},
	} {
		if strings.TrimSpace(sec.body) != "" {
			have = append(have, sec.name)
		}
	}
	if len(have) == 0 {
		have = append(have, "none")
	}
	out := "[" + strings.Join(have, " ") + "]"
	// Clipped, because a wide literals list is a legitimate answer and must not push the rest of the
	// line out of a terminal — but clipped LAST, so the section list is never the part that is lost.
	if lits := strings.Join(p.Literals, ", "); strings.TrimSpace(lits) != "" {
		out += " verbatim: " + clipLine(lits, 300)
	}
	return out
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

// hasContent reports whether a parsed packet carries anything usable — used to skip a stray/
// non-packet object (e.g. a code fragment `{...}` in the curator's reasoning) in favor of the
// real packet that follows.
func (p curatePacket) hasContent() bool {
	return strings.TrimSpace(string(p.Goal)) != "" || strings.TrimSpace(string(p.Progress)) != "" ||
		strings.TrimSpace(string(p.Missing)) != "" ||
		strings.TrimSpace(string(p.Task)) != "" || strings.TrimSpace(string(p.Deliverable)) != "" ||
		len(p.Literals) > 0 || len(p.Constraints) > 0 || len(p.Tools) > 0
}

// parseCuratePacket extracts the curator's JSON packet from a reply that may wrap it in prose or
// reasoning. A naive first-`{`-to-last-`}` span over-captures when the reasoning contains a stray
// brace (a code fragment, a set literal) and then fails to parse — losing the whole curation and
// dropping the worker back to the mechanical, literal-losing brief. So scan EVERY top-level
// balanced object (like parsePlan) and take the first that unmarshals into a packet with content;
// else the first that unmarshals at all; else none.
func parseCuratePacket(raw string) (curatePacket, bool) {
	p, ok, _ := parseCuratePacketSalvage(raw)
	return p, ok
}

// parseCuratePacketSalvage is parseCuratePacket plus whether the packet came from a span that had to
// be REPAIRED to be read at all — a reply cut off by the output budget, or one a stray quote
// swallowed — rather than one whose braces closed on their own.
//
// The distinction is not cosmetic here. What a truncated packet loses is its TAIL, and the tail is
// where the fields that matter most sit: the literals, the boundaries, the done-when. A packet that
// lost half its literals renders a brief that looks complete and hands the worker an acceptance it
// cannot meet, because the identifier it had to preserve verbatim is simply not in it — the exact
// spec-loss this packet exists to prevent. Measured: `{"task":"…","literals":["GetResponse","kv.proto"`
// recovers as a valid packet carrying `GetResponse` alone, reported as a clean parse.
func parseCuratePacketSalvage(raw string) (curatePacket, bool, bool) {
	// The spans the reply yielded WITHOUT repair, so an accepted span can be told apart from one the
	// recovery reconstructed (same idiom as readPlan).
	intact := map[string]bool{}
	for _, js := range jsonx.BalancedObjects(raw) {
		intact[js] = true
	}
	var firstValid *curatePacket
	var firstValidIntact bool
	for _, js := range balancedObjects(raw) {
		var p curatePacket
		if !unmarshalLenient(js, &p) {
			continue // not JSON, or not the packet shape — try the next object
		}
		if p.hasContent() {
			return p, true, !intact[js]
		}
		if firstValid == nil {
			pp := p
			firstValid = &pp
			firstValidIntact = intact[js]
		}
	}
	if firstValid != nil {
		return *firstValid, true, !firstValidIntact
	}
	return curatePacket{}, false, false
}
