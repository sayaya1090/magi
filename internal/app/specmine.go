package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// specMineAgent is the optional dedicated agent name for the signature-mining
// elicitation. When cfg.Agents defines it (e.g. routed to a stronger model via
// [routing]), mining uses that spec; otherwise it falls back to the caller's spec
// and the session model. The fallback keeps a bench run single-model; the knob
// exists because the mined note's TRUTH is bounded by the eliciting model's
// knowledge (a wrong belief about a stdlib construct survives any prompt), and
// routing to different weights is the only structural fix for that.
const specMineAgent = "specmine"

// elicitSpecMineSystem instructs pass 1 (analysis): free-form is fine — a weak model
// cannot both derive and self-restrain in one generation (observed: the "resolve
// conflicts yourself" rule produced a page of out-loud arguing that talked itself out
// of the right construct). Pass 2 distills. The one goal survives: find where a
// prose-only implementation goes wrong.
const elicitSpecMineSystem = "You review a coding request's NAMES and TYPE SIGNATURES with ONE goal: find " +
	"where an implementation written from the prose alone would go WRONG. For each identifier, " +
	"parameter/return type, or stated format that guards against such a failure, note: the surface, the " +
	"unsaid requirement it implies, and the STANDARD construct (name it) the language/stdlib provides " +
	"that satisfies it. Reason from what the surfaces state: a type constrains what its values — and " +
	"their lifecycles — can do; a name like max_*/timeout_*/n_* states an exact bound; a format fixes " +
	"shape. Prefer the named standard construct over hand-assembling the mechanism from lower-level " +
	"parts — the idiom already carries the edge semantics (ordering, cancellation, partial failure) a " +
	"hand-rolled version drops. Derive ONLY what the given surfaces actually imply — do not invent " +
	"requirements. If no surface implies anything beyond the prose, say NONE."

// distillSpecMineSystem instructs pass 2: compress the pass-1 analysis into a strict
// JSON shape. Compression of GIVEN text is a task weak models perform far more
// reliably than self-restraint during generation; the line cap and the single-winner
// rule are enforced here (and again in code).
const distillSpecMineSystem = "You distill a working analysis into its final conclusions. From the analysis " +
	"given, keep ONLY the highest-stakes findings and output ONLY a JSON object, no prose, no code fence:\n" +
	`{"lines":[{"surface":"...","requirement":"...","construct":"..."}],"final":"..."}` + "\n" +
	"Rules: at most 5 lines. Each construct names a concrete language/stdlib construct. \"final\" is ONE " +
	"sentence naming the winning construct(s) — SINGLE and unconditional: where the analysis argued both " +
	"ways, pick the winner and DROP every caveat against it (a reader under pressure follows the escape " +
	"hatch, not the advice). Do not restate what the original request's prose already says. If the " +
	"analysis concluded nothing beyond the prose, output exactly {\"lines\":[],\"final\":\"\"}."

// specMineResult is the distilled pass-2 shape.
type specMineResult struct {
	Lines []struct {
		Surface     string `json:"surface"`
		Requirement string `json:"requirement"`
		Construct   string `json:"construct"`
	} `json:"lines"`
	Final string `json:"final"`
}

// specMineSpec picks the elicitation's agent spec: the dedicated "specmine" agent
// when configured (different weights = different priors), else the caller's spec.
func (a *App) specMineSpec(fallback AgentSpec) AgentSpec {
	if sp, ok := a.cfg.Agents[specMineAgent]; ok {
		return sp
	}
	return fallback
}

// elicitSpecMine mines the request's identifiers/types in two passes: a free-form
// analysis, then a strict JSON distillation (parse retried once; the cap and shape
// are re-enforced in code). Empty string on any failure — strictly best-effort.
func (a *App) elicitSpecMine(ctx context.Context, agent AgentSpec, s session.Session, task string) string {
	spec := a.specMineSpec(agent)
	model := s.Model.Model
	if spec.Model != (session.ModelRef{}) {
		model = spec.Model.Model
	}
	analysis := a.specMineCall(ctx, spec, model, elicitSpecMineSystem, task)
	if analysis == "" || (len(analysis) < 8 && strings.Contains(strings.ToUpper(analysis), "NONE")) {
		return ""
	}
	distilled := a.specMineCall(ctx, spec, model, distillSpecMineSystem, analysis)
	res, ok := parseSpecMine(distilled)
	if !ok { // local models are flaky — one retry
		distilled = a.specMineCall(ctx, spec, model, distillSpecMineSystem, analysis)
		res, ok = parseSpecMine(distilled)
	}
	if !ok || (len(res.Lines) == 0 && strings.TrimSpace(res.Final) == "") {
		return ""
	}
	var b strings.Builder
	for i, ln := range res.Lines {
		if i >= 5 { // the cap is code-enforced, not trusted to the model
			break
		}
		sfc, req, con := strings.TrimSpace(ln.Surface), strings.TrimSpace(ln.Requirement), strings.TrimSpace(ln.Construct)
		if sfc == "" && req == "" && con == "" {
			continue
		}
		b.WriteString("- " + sfc + " → " + req + " → " + con + "\n")
	}
	if f := strings.TrimSpace(res.Final); f != "" {
		b.WriteString("USE: " + f + "\n")
	}
	return strings.TrimSpace(b.String())
}

// specMineCall is one tool-free side call; empty string on transport failure.
func (a *App) specMineCall(ctx context.Context, spec AgentSpec, model, system, user string) string {
	stream, err := a.providerFor(spec).StreamChat(ctx, port.ChatRequest{
		Model:    model,
		System:   system,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
	})
	if err != nil {
		return ""
	}
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// parseSpecMine extracts the first balanced {...} JSON object and unmarshals it.
func parseSpecMine(text string) (specMineResult, bool) {
	var res specMineResult
	start := strings.Index(text, "{")
	if start < 0 {
		return res, false
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if json.Unmarshal([]byte(text[start:i+1]), &res) == nil {
					return res, true
				}
				return res, false
			}
		}
	}
	return res, false
}

// specMineNote wraps a mined result for injection into the main session. The header
// mirrors the other execution notes so the executor reads it as system guidance.
func specMineNote(mined string) string {
	return "# Execution note — requirements mined from the request's identifiers/types\n" +
		"Derived from the request's own names and type signatures (not from its prose). Honor these " +
		"alongside the stated requirements, and prefer the named standard construct over hand-rolling:\n" +
		mined
}
