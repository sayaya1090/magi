package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
const elicitSpecMineSystem = "You read a coding request and work out, BEFORE any code is written, what it " +
	"actually takes to satisfy it — on two fronts.\n" +
	"FIRST, its NAMES and TYPE SIGNATURES: find where an implementation written from the prose alone would " +
	"go WRONG. For each identifier, parameter/return type, or stated format that guards against such a " +
	"failure, note: the surface, the unsaid requirement it implies, and the STANDARD construct (name it) the " +
	"language/stdlib provides that satisfies it. Reason from what the surfaces state: a type constrains what " +
	"its values — and their lifecycles — can do; a name like max_*/timeout_*/n_* states an exact bound; a " +
	"format fixes shape. Prefer the named standard construct over hand-assembling the mechanism from " +
	"lower-level parts — the idiom already carries the edge semantics (ordering, cancellation, partial " +
	"failure) a hand-rolled version drops.\n" +
	"SECOND, its PREREQUISITES: the things that must already exist or be provisioned for the work to succeed, " +
	"which the request names but an implementation tends to ASSUME are present — an exact dependency and " +
	"version to install, a service/process that must be running, a file/directory/port/credential to create " +
	"or open, a tool that must be on PATH. For each, the requirement is what to CHECK, and the construct is " +
	"the concrete action that PROVISIONS it when absent (the install/create/start command). Understanding " +
	"what the task needs comes first; provisioning follows from it.\n" +
	"Derive ONLY what the given surfaces actually imply — do not invent requirements.\n" +
	"CRITICAL — do NOT treat a name that a compiler, code generator, or language convention DERIVES from " +
	"the request as a fixed literal to preserve. A generated module/file name, or an identifier a tool " +
	"sanitizes (a hyphenated `.proto` filename yields an UNDERSCORED Python module; `protoc`/`grpc_tools` " +
	"emit `foo_bar_pb2.py`, never `foo-bar_pb2.py`), takes whatever form the tool ACTUALLY emits — forcing " +
	"the request's raw spelling onto it breaks the build. For such a name, the requirement is 'use the " +
	"generator's real output', and the construct is the tool that produces it; never 'match the raw " +
	"filename/spelling'.\n" +
	"If no surface implies anything beyond the prose, say NONE."

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
	analysis := a.specMineCall(ctx, spec, s.ID, "spec-mine", model, elicitSpecMineSystem, task)
	if analysis == "" || (len(analysis) < 8 && strings.Contains(strings.ToUpper(analysis), "NONE")) {
		return ""
	}
	distilled := a.specMineCall(ctx, spec, s.ID, "spec-mine", model, distillSpecMineSystem, analysis)
	res, ok := parseSpecMine(distilled)
	if !ok { // local models are flaky — one retry
		distilled = a.specMineCall(ctx, spec, s.ID, "spec-mine", model, distillSpecMineSystem, analysis)
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

// specMineCallTimeout bounds ONE tool-free side call. Signature mining and the curator both make
// these calls on the critical pre-execution path with no other guard, so an unbounded generation
// that hangs (a stuck backend, a runaway reasoning spin) would freeze the whole turn until the
// harbor/task wall clock — the observed multi-minute stalls right around the mining seam. The call
// is strictly best-effort (empty result → no note injected, the turn proceeds), so cutting it off
// is safe; the bound is generous enough for a slow local model's legitimate 2–3 minute generation.
const specMineCallTimeout = 180 * time.Second

// specMineBeatInterval throttles the "thinking…" heartbeat a side call emits while the model streams
// reasoning, so a slow-but-working generation is visibly ALIVE without flooding the UI with the side
// call's internal reasoning.
const specMineBeatInterval = 5 * time.Second

// specMineCall is one tool-free side call (signature mining, curation); empty string on transport
// failure or timeout. Note generation used to show NOTHING until the final text arrived — it captured
// only ProviderText and silently dropped the model's reasoning — so a reasoning model that thinks for
// a while looked identical to a wedged backend. It now streams a throttled, labelled "thinking…"
// heartbeat while the model reasons: a heartbeat means it is alive and thinking; continued silence
// means the backend is genuinely stuck. sid=="" disables the heartbeat (no session to emit under).
func (a *App) specMineCall(ctx context.Context, spec AgentSpec, sid session.SessionID, label, model, system, user string) string {
	cctx, cancel := context.WithTimeout(ctx, specMineCallTimeout)
	defer cancel()
	stream, err := a.providerFor(spec).StreamChat(cctx, port.ChatRequest{
		Model:    model,
		System:   system,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}},
	})
	if err != nil {
		return ""
	}
	var b strings.Builder
	start := time.Now()
	lastBeat := start
	for ev := range stream {
		switch ev.Type {
		case port.ProviderText:
			b.WriteString(ev.Text)
		case port.ProviderReasoning:
			if sid != "" && time.Since(lastBeat) >= specMineBeatInterval {
				a.emitToolProgress(sid, plannerActor, "", label, fmt.Sprintf("%s: thinking… (%ds)", label, int(time.Since(start).Seconds())))
				lastBeat = time.Now()
			}
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

// storeSpecMine caches this turn's mined note so the termination council can see the
// soft contract the executor received (cleared by resetForNewTopLevel).
func (a *App) storeSpecMine(sid session.SessionID, mined string) {
	a.mu.Lock()
	a.stateLocked(sid).minedNote = mined
	a.mu.Unlock()
}

// cachedSpecMine returns this turn's mined note ("" when mining didn't run).
func (a *App) cachedSpecMine(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.minedNote
	}
	return ""
}

// specMineNote wraps a mined result for injection into the main session. The header
// mirrors the other execution notes so the executor reads it as system guidance.
func specMineNote(mined string) string {
	return "# Execution note — what this task needs (its identifiers, types, and prerequisites)\n" +
		"Worked out from the request's own names, type signatures, and stated dependencies (not its prose). " +
		"Honor the identifiers/formats exactly and prefer the named standard construct over hand-rolling; and " +
		"CHECK each prerequisite below is actually present, provisioning what is missing BEFORE you rely on it. " +
		"But a name a tool or language DERIVES (a generated module/file, a sanitized identifier — e.g. a " +
		"hyphenated `.proto` filename becomes an UNDERSCORED Python module) follows the tool's ACTUAL " +
		"output; never force the request's raw spelling onto a generated name, and don't fault one for not " +
		"matching it:\n" +
		mined
}
