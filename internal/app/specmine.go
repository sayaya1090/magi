package app

import (
	"context"
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
	"actually takes to satisfy it — on four fronts.\n" +
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
	"THIRD, when the request is a TRANSFORM or REPRODUCTION — it must produce output whose exact values are " +
	"DERIVED from an input that is PRESENT (a file to parse, an encoded/binary format to read, a dataset to " +
	"convert, or another program's output to match): the output is fully DETERMINED by that input plus a " +
	"precise mapping, so the failure is a subtly wrong RULE, not a wrong name — and a single self-consistent " +
	"implementation never catches its own rule error. Pin the mapping the output must obey and put it in the " +
	"requirement: WHICH elements are selected (which records/fields/sections, and the filter that includes " +
	"them), in what ORDER and at what offset/stride, the per-element COMPUTATION (byte width, endianness, " +
	"sign, base/offset arithmetic), and the exact ENCODING of every key and value (decimal vs hex, string vs " +
	"number, separators, padding, trailing newline). Where the prose underdetermines the rule, the " +
	"requirement is to READ the present input to fix it — name that input. This stays SEMANTIC (verify by " +
	"running against the REAL input, never by matching source text), but state it PRECISELY enough that any " +
	"two correct implementations would emit byte-identical output; a vague 'parse it and output the values' " +
	"is the failure, not the finding.\n" +
	"FOURTH, its explicit CONSTRAINTS — the MUST / MUST-NOT conditions the request STATES that are not the main " +
	"deliverable but bound HOW it is reached, and that an implementation tends to forget mid-work while a grader " +
	"still checks them: a SCOPE limit (only a named file/area may change, or one is off-limits — 'only modify X', " +
	"'do not touch Y', 'leave Z unchanged'), a STRUCTURAL requirement the output MUST satisfy (it must contain, " +
	"start with, or end with a specific element — a required marker, directive, or terminator), or a FORBIDDEN " +
	"action (a tool/approach/command that must NOT be used). For each, the requirement is the condition that must " +
	"hold (or never hold), and the construct is how to CONFIRM it read-only — the changed-file set stays within " +
	"the allowed set, the artifact contains the required element, the forbidden thing is absent. These are " +
	"CONTRACT terms: surface them so the executor keeps them and a checker verifies them against the real diff/" +
	"artifact, not the narration. Classify a constraint HARD when the task names the exact file/token it fixes, " +
	"else SEMANTIC; only capture a constraint the task itself states.\n" +
	"Derive ONLY what the given surfaces actually imply — do not invent requirements.\n" +
	"CLASSIFY each finding by HOW it must be honored, because the difference decides whether a checker " +
	"asserts it literally or by behavior: a FIXED identifier or value the request names (a message/service/" +
	"function NAME, a port, a filename, a pinned version) is HARD — match it verbatim; a SAMPLE it gives (an " +
	"input→output pair) is an EXAMPLE — reproduce that behavior, and capture the ACTUAL input and expected " +
	"output VERBATIM in the requirement (e.g. `input 208 → output 377`) so it can be reproduced and checked " +
	"exactly; a structure, type, or behavior it only DESCRIBES in prose (a field of some type, a return " +
	"value, a format) is SEMANTIC — satisfy the meaning and verify it by EFFECT (build/run/inspect), never by " +
	"demanding a particular source spelling of the prose. Also call out what the request LEAVES FREE — an " +
	"aspect it does NOT pin (the source layout, an internal helper's name, the field-declaration syntax, the " +
	"algorithm) is UNCONSTRAINED: note it so nothing downstream invents a constraint the task never stated. " +
	"Treating a SEMANTIC description or an UNCONSTRAINED aspect as if it were a HARD literal (asserting the " +
	"prose's exact wording in the source) forces correct code into a fabricated shape. HARD is NARROW and " +
	"the MINORITY: use it ONLY for a literal string a grader matches character-for-character — a filename, a " +
	"function/message/RPC NAME, a port number, a pinned version, or an exact output token the task quotes. A " +
	"described BEHAVIOR, ALGORITHM, FORMAT, or STRUCTURE is SEMANTIC even when it is required or important — " +
	"importance does NOT make it hard: 'parse the file header', 'output JSON with these fields', 'sort the " +
	"results', 'return an int' are all SEMANTIC (satisfied by the running artifact, not by matching source " +
	"text). Default to SEMANTIC; reach for HARD only when you can point to the exact literal the task wrote " +
	"and a grader will compare byte-for-byte, and mark a genuinely open aspect UNCONSTRAINED. And when a " +
	"fixed value sits INSIDE a larger string whose shape the task did NOT dictate — a port inside a bind " +
	"address (the interface part, `[::]` vs `0.0.0.0` vs a hostname, is the implementer's choice), a name " +
	"inside a URL, a value inside a connection string — only the value the task fixed is HARD; the enclosing " +
	"format is UNCONSTRAINED, so do NOT promote the whole string to a verbatim literal.\n" +
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
	`{"lines":[{"surface":"...","requirement":"...","construct":"...","kind":"hard|example|semantic|unconstrained"}],"final":"..."}` + "\n" +
	"Rules: at most 5 lines. Each construct names a concrete language/stdlib construct. Each line's `kind` " +
	"says HOW its requirement must be honored: `hard` = a FIXED identifier or value the grader checks " +
	"literally (a message/service/RPC/function NAME, a port, a filename, a pinned version) — match it " +
	"verbatim; `example` = a SAMPLE the task gives (an input→output pair, a reference row) — reproduce that " +
	"exact behavior, and put the ACTUAL input and expected output VERBATIM in `requirement` (e.g. `208 → " +
	"377`); `semantic` = a structure/type/behavior DESCRIBED in prose (a field of some type, a format, what " +
	"a call returns) — satisfy its MEANING and verify by EFFECT (build/run/inspect the produced artifact), " +
	"NOT by any particular source spelling; `unconstrained` = an aspect the task does NOT pin (source layout, " +
	"an internal name, the declaration syntax, the algorithm) — record it so nothing downstream asserts it. " +
	"`hard` is the MINORITY — reserve it for a literal a grader matches byte-for-byte (a name/port/filename/" +
	"pinned version/quoted output token); a described behavior, algorithm, format, or structure is `semantic` " +
	"even when required (importance does not make it hard). Default to `semantic` (the safe choice that never " +
	"forces a made-up surface form onto correct code). For a TRANSFORM/reproduction finding (output derived " +
	"from a present input), the `requirement` must carry the PRECISE mapping — element selection, order/" +
	"stride, per-element computation (width/endianness/arithmetic), and key/value encoding — so it is " +
	"reproducible byte-for-byte, not a vague 'parse it and output the values'. \"final\" is ONE sentence naming the winning " +
	"construct(s) — SINGLE and unconditional: where the analysis argued both ways, pick the winner and DROP " +
	"every caveat against it (a reader under pressure follows the escape hatch, not the advice). Do not " +
	"restate what the original request's prose already says. If the analysis concluded nothing beyond the " +
	"prose, output exactly {\"lines\":[],\"final\":\"\"}."

// specMineResult is the distilled pass-2 shape.
type specMineResult struct {
	Lines []struct {
		Surface     string `json:"surface"`
		Requirement string `json:"requirement"`
		Construct   string `json:"construct"`
		// Kind classifies HOW the requirement must be honored — hard (a fixed identifier/value:
		// match verbatim), example (a sample input→output: reproduce the behavior), or semantic
		// (a described structure/type/behavior: satisfy the meaning, verify by effect). Absent /
		// unknown normalizes to semantic (specKind), the safe default that never over-asserts source form.
		Kind string `json:"kind"`
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
	// Give the analysis the SAME repository map the planner sees (repoBlock), so it grounds the
	// request's identifiers/types in what the repo actually contains (e.g. an existing file the task
	// refers to) instead of reasoning from the prose alone. Only pass 1 (the analysis) needs it; pass 2
	// distills pass-1 output. repoMap is a cheap top-level listing, already bounded.
	// Mining is best-effort, but a SILENT empty note is indistinguishable in the log from a task that
	// genuinely had nothing to mine — and the note is the run's only record of the literals the grader
	// checks verbatim. Say which pass came up empty so a missing note is diagnosable.
	empty := func(why string) string {
		a.emitToolProgress(s.ID, plannerActor, "", "spec-mine", "spec-mine: no execution note — "+why)
		return ""
	}
	elicitSys := elicitSpecMineSystem + "\n\n# Repository (top level)\n" + repoMap(s.Workdir)
	analysis := a.specMineCall(ctx, spec, s.ID, "spec-mine", model, elicitSys, task)
	if analysis == "" {
		return empty("the analysis pass returned nothing (backend error or empty reply)")
	}
	if len(analysis) < 8 && strings.Contains(strings.ToUpper(analysis), "NONE") {
		return empty("the analysis pass found nothing to mine in this request")
	}
	distilled := a.specMineCall(ctx, spec, s.ID, "spec-mine", model, distillSpecMineSystem, analysis)
	res, ok := parseSpecMine(distilled)
	if !ok { // local models are flaky — one retry
		distilled = a.specMineCall(ctx, spec, s.ID, "spec-mine", model, distillSpecMineSystem, analysis)
		res, ok = parseSpecMine(distilled)
	}
	if !ok {
		return empty(fmt.Sprintf("the distill pass did not parse, twice (%d chars, analysis was %d)", len(distilled), len(analysis)))
	}
	if len(res.Lines) == 0 && strings.TrimSpace(res.Final) == "" {
		return empty("the distill pass parsed but carried no lines")
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
		// Tag each line by HOW to honor it so the executor and the check-author treat a fixed
		// identifier (verbatim) differently from a described behavior (verify by effect).
		b.WriteString("- ⟨" + specKind(ln.Kind) + "⟩ " + sfc + " → " + req + " → " + con + "\n")
	}
	if f := strings.TrimSpace(res.Final); f != "" {
		b.WriteString("USE: " + f + "\n")
	}
	return strings.TrimSpace(b.String())
}

// specKind normalizes a mined line's classification into one of the three ways its requirement must
// be honored, defaulting to the SAFE "semantic" when the model omits or garbles it — semantic means
// "verify the meaning by effect", the default that never over-asserts a particular source spelling
// (the kv-store `<key: string>` failure mode), so an unclassified line is treated conservatively.
func specKind(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hard", "literal", "identifier", "verbatim":
		return "hard"
	case "example", "sample":
		return "example"
	case "unconstrained", "free", "open", "unspecified":
		return "unconstrained"
	default:
		return "semantic"
	}
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
	return a.specMineCallMsgs(ctx, spec, sid, label, model, system,
		[]session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: user}}}})
}

// specMineCallMsgs is specMineCall with an explicit MESSAGE conversation instead of a single user
// string — so a caller that wants the side-LLM to see the prior back-and-forth (the session window,
// like the planner does) can pass it, not just one prompt. Same timeout + thinking-heartbeat.
func (a *App) specMineCallMsgs(ctx context.Context, spec AgentSpec, sid session.SessionID, label, model, system string, msgs []session.Message) string {
	cctx, cancel := context.WithTimeout(ctx, specMineCallTimeout)
	defer cancel()
	stream, err := a.providerFor(spec).StreamChat(cctx, port.ChatRequest{
		Model:    model,
		System:   system,
		Messages: msgs,
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
// parseSpecMine extracts the distilled JSON object from a reply that may wrap it in prose or
// reasoning. It scans EVERY top-level balanced object (balancedObjects, which respects strings and
// escapes) and takes the first that unmarshals into a result with mined lines; else the first that
// unmarshals at all; else none. A hand-rolled first-`{`-to-matching-`}` depth scan that ignored
// string literals lost the whole result whenever a mined value contained an UNbalanced brace (e.g.
// "use } to close a block", "dict is { key: val") — exactly the code/shape strings spec-mining emits.
func parseSpecMine(text string) (specMineResult, bool) {
	var firstValid *specMineResult
	for _, js := range balancedObjects(text) {
		var res specMineResult
		if !unmarshalLenient(js, &res) {
			continue // not JSON, or not the result shape — try the next object
		}
		if len(res.Lines) > 0 {
			return res, true // a mined result wins immediately
		}
		if firstValid == nil {
			rr := res
			firstValid = &rr
		}
	}
	if firstValid != nil {
		return *firstValid, true
	}
	return specMineResult{}, false
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
		"Each line is tagged by HOW to honor it: ⟨hard⟩ = a fixed identifier/value — match it verbatim; " +
		"⟨example⟩ = a sample input→output — reproduce that behavior exactly (the literal I/O is in the line); " +
		"⟨semantic⟩ = a described structure/type/behavior — satisfy its MEANING and verify by EFFECT (build/" +
		"run/inspect), NEVER by forcing a particular source spelling (a ⟨semantic⟩ 'field key of type string' " +
		"is met by the built artifact having that field, not by the source literally containing the words); " +
		"⟨unconstrained⟩ = an aspect the task does NOT pin (source layout, an internal name, the algorithm) — " +
		"you are FREE to choose it, so do not treat it as a requirement and do not let any check assert it. " +
		"Prefer the named " +
		"standard construct over hand-rolling; and CHECK each prerequisite below is actually present, " +
		"provisioning what is missing BEFORE you rely on it. A name a tool or language DERIVES (a generated " +
		"module/file, a sanitized identifier — e.g. a hyphenated `.proto` filename becomes an UNDERSCORED " +
		"Python module) follows the tool's ACTUAL output; never force the request's raw spelling onto a " +
		"generated name, and don't fault one for not matching it:\n" +
		mined
}
