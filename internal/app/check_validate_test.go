package app

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// parseChecksArray must pull a JSON array of checks out of a review reply and drop any check with no
// command (nothing to run), and reject non-array replies.
func TestParseChecksArray(t *testing.T) {
	raw := "here you go:\n[{\"step\":\"1\",\"deliverable\":\"deps\",\"command\":\"grep -q x\"}," +
		"{\"step\":\"2\",\"command\":\"\"}]\nthanks"
	out, ok := parseChecksArray(raw)
	if !ok {
		t.Fatal("a reply containing a JSON array must parse")
	}
	if len(out) != 1 || out[0].Command != "grep -q x" {
		t.Fatalf("must keep the one runnable check and drop the empty-command one, got %+v", out)
	}
	if _, ok := parseChecksArray("no array here"); ok {
		t.Error("a reply with no JSON array must not parse")
	}
	// Edge cases: an empty array parses to zero checks (valid), reversed brackets and a bracket span
	// that isn't valid JSON are rejected.
	if out, ok := parseChecksArray("done: []"); !ok || len(out) != 0 {
		t.Errorf("empty array must parse to zero checks, got ok=%v out=%+v", ok, out)
	}
	if _, ok := parseChecksArray("] backwards ["); ok {
		t.Error("reversed brackets (] before [) must not parse")
	}
	if _, ok := parseChecksArray("prose [1] more [2] text"); ok {
		t.Error("the outermost bracket span that isn't a valid checks array must not parse")
	}
}

// A reply whose real checks array is FOLLOWED by reasoning containing a stray ] — or that carries a ]
// inside a command string — used to be lost: the naive first-[/last-] span over-captured to the wrong
// bracket and failed to unmarshal, dropping the whole audit. balancedArrays takes the first balanced
// array (respecting strings), so the audit survives.
func TestParseChecksArrayStrayTrailingBracket(t *testing.T) {
	// A stray ] in trailing reasoning after the real array.
	trailing := `[{"step":"1","deliverable":"deps","command":"grep -q x"}] — see item [3] above`
	out, ok := parseChecksArray(trailing)
	if !ok || len(out) != 1 || out[0].Command != "grep -q x" {
		t.Fatalf("array before trailing stray bracket must still parse, got ok=%v out=%+v", ok, out)
	}
	// A ] inside a command string, with another bracket span trailing — the string bracket must not
	// close the array early, and the trailing span must not extend it.
	inString := `[{"step":"1","command":"test ${a[0]} -eq 1"}] and then [done]`
	out, ok = parseChecksArray(inString)
	if !ok || len(out) != 1 || out[0].Command != "test ${a[0]} -eq 1" {
		t.Fatalf("bracket inside a command string must be preserved, got ok=%v out=%+v", ok, out)
	}
}

// With the flag off, validateChecks is a pure passthrough — the authored checks are used as-is and no
// review call is made (the input is returned unchanged, including its exact contents).
func TestValidateChecksFlagOffPassthrough(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "0")
	a := newOrchApp(t, &gateLLM{text: "[]"}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Command: "pip show grpcio | grep -q Version"}}
	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 || out[0].Command != in[0].Command {
		t.Fatalf("flag off must return the authored checks unchanged, got %+v", out)
	}
}

// Both check-authoring prompts must state the shape the runner can actually evaluate: a check is DATA
// ({source, assert}), the gate reads the file and applies the assertion itself with no shell, and
// `assert` comes from a closed vocabulary. This is what replaced the old work≠check/idempotence rules —
// a check that cannot name a command cannot re-do the step's work or mutate anything — so a prompt that
// drifted back to describing commands would be asking for checks that gate nothing.
func TestCheckPromptsStateTheTypedShape(t *testing.T) {
	for name, prompt := range map[string]string{
		"check-audit":   validateChecksSystem,
		"coverage-fill": coverageFillSystem,
	} {
		for _, want := range []string{
			"A CHECK IS DATA, NOT A COMMAND", "no shell in the path",
			"nonempty", "matches <regexp>", "absent <regexp>", "equals <path>", "port_open <port>", "process_alive",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("%s must state the typed shape (missing %q)", name, want)
			}
		}
	}
}

// The review's FIRST job is conversion: a check that arrives shaped as a shell command gates nothing,
// because nothing executes it. The prompt must say so and must say where the run goes instead — to the
// step, which records its real output — or the reviewer will keep returning commands it believes run.
func TestValidateChecksPromptConvertsCommandChecks(t *testing.T) {
	for _, want := range []string{"CONVERT (do this FIRST)", "gates NOTHING", "commands are no longer executed",
		"belongs to the STEP", "Keep WHAT was proven"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must convert command-shaped checks (missing %q)", want)
		}
	}
}

// The validation prompt must keep a cleanup/absence check on its own step rather than merging it
// with an existence check for the same artifact — the review's half of the step-scoping guard
// that prevents a jointly-unsatisfiable checklist.
func TestValidateChecksPromptKeepsStepScoping(t *testing.T) {
	for _, want := range []string{"scopes the check to its step", "jointly-unsatisfiable"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must keep checks step-scoped (missing %q)", want)
		}
	}
}

// The validation prompt must require a check that greps a generated file to use the name the tool
// actually emits: a generator whose target language forbids a character in the source name
// substitutes a legal one, so a check demanding the input's spelling fights the toolchain in an
// unwinnable rename loop. The clause is stated in that general form on purpose — the concrete
// generator it was derived from is an eval-set toolchain, and naming it here would teach the model
// one benchmark's filename convention instead of the rule ([[prompt-necessity-and-deoverfit]]).
func TestValidateChecksPromptRespectsToolDerivedNames(t *testing.T) {
	for _, want := range []string{"TOOL-DERIVED NAMES", "substitutes a legal one", "unwinnable loop"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must respect tool-derived filenames (missing %q)", want)
		}
	}
}

// A check must verify a prose-described structure/behavior by its EFFECT (compile/run and inspect the
// produced type), not by grepping the SOURCE for the task's wording or an invented pseudo-notation of
// it. Observed (kv-store-grpc): the plan-audit encoded "a message with a key (string) field" as a
// literal source grep `GetValRequest <key: string>` plus `^service X$` — patterns no valid proto3
// contains — so the agent contorted the source toward a fabricated shape for ~30 actions. Guards the
// SEMANTICS clause with task-agnostic tokens (no eval-set identifiers).
func TestValidateChecksPromptForbidsSourceNotationAssertion(t *testing.T) {
	for _, want := range []string{"SEMANTICS, not source spelling", "<field: type>", "^service X$", "source layout"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must forbid asserting source spelling over semantics (missing %q)", want)
		}
	}
	// The clause states the general principle; it must not smuggle the eval-set's own identifiers.
	for _, banned := range []string{"KVStore", "GetVal", "GetValRequest", "kv-store"} {
		if strings.Contains(validateChecksSystem, banned) {
			t.Errorf("validateChecksSystem leaks an eval-set token %q — keep the SEMANTICS example task-agnostic", banned)
		}
	}
}

// The whole PORTABLE family of rules — absolute tool paths, absent shell utilities, non-stdlib python
// modules, the pgrep/os.kill liveness workaround — is gone from both prompts, and must stay gone. Those
// clauses existed because a check was a command that could name a tool the image does not have and exit
// 127. A typed check names no program: the only executables in the path are magi's own `cat` and the two
// probes it owns. Re-adding that guidance would ask the model for commands the runner cannot run.
func TestCheckPromptsDropTheMissingToolGuidance(t *testing.T) {
	for name, prompt := range map[string]string{
		"check-audit":   validateChecksSystem,
		"coverage-fill": coverageFillSystem,
	} {
		for _, gone := range []string{"pkg_resources", "importlib.metadata", "pgrep", "os.kill(pid, 0)",
			"/usr/bin/pip3", "exhaustive list"} {
			if strings.Contains(prompt, gone) {
				t.Errorf("%s still carries missing-tool guidance %q, which a typed check cannot act on", name, gone)
			}
		}
	}
}

// The validation prompt must forbid over-demand: a check may assert only what the task itself states,
// never a version/build-id/incidental the task did not pin — over-specification false-fails a correct
// deliverable and can never converge on an environment that differs in that incidental.
func TestValidateChecksPromptForbidsOverDemand(t *testing.T) {
	for _, want := range []string{"NECESSITY", "over-demand", "minimal condition"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must forbid over-demand (missing %q)", want)
		}
	}
}

// The sufficiency floor (opposite of over-demand): a check that only confirms the deliverable can be
// reached — file exists, port accepts a connection, module imports, build succeeds — is a precondition,
// not proof, and a non-functional stub passes it. The prompt must demand the check invoke the stated
// behavior and assert the result, using the weakest input that forces the real code path.
func TestValidateChecksPromptDemandsContractExercise(t *testing.T) {
	for _, want := range []string{"precondition", "non-functional stub", "weakest input", "real code path"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must forbid proxy-only (too-weak) checks (missing %q)", want)
		}
	}
}

// Two proxy classes the reachability list did not name, each observed slipping a wrong deliverable
// past the gate: (1) a check that a configuration is SET rather than that it took EFFECT — "gcov flag
// configured" instead of "the instrumented build actually emits coverage files" (a set flag that never
// compiled passes); (2) a behavioral check that validates a single SAMPLE rather than the task's whole
// reference/threshold — one address value instead of the required fraction of the reference (a solution
// wrong on the rest passes). The prompt must name both generally, with NO task-specific token.
func TestValidateChecksPromptForbidsConfigProxyAndSpotCheck(t *testing.T) {
	for _, want := range []string{"never took effect", "EFFECT, not its cause", "WHOLE standard", "hand-picked sample"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must forbid config-proxy / single-sample checks (missing %q)", want)
		}
	}
	// The no-invent clause must NOT read as forbidding the proxy→behavior strengthening above:
	// strengthening a check's OWN deliverable is explicitly permitted; only inventing a check for a
	// DIFFERENT deliverable / retargeting to another step is forbidden. (A weak model reading "do not
	// change what a check verifies" literally would skip the strengthening — the reconciliation matters.)
	for _, want := range []string{"NOT a forbidden change", "DIFFERENT deliverable"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must reconcile no-invent with strengthening (missing %q)", want)
		}
	}
}

// Guard against benchmark overfitting: prompt examples must be task-agnostic. No eval-set task's exact
// identifiers (a pinned dependency version, a specific task filename) may be baked into a prompt the
// model sees — an example lifted verbatim from the test set tunes the prompt to the benchmark.
// The list covers EVERY model-facing prompt in this package, not a sample: an audit found the
// leaks in the three prompts nobody had wired into a guard (a sample I/O pair copied off one task,
// a grader's literal output string, and one task's code-generator named by product), while the two
// that were guarded stayed clean. A prompt that no test reads is a prompt that drifts.
func TestPromptsCarryNoEvalSetSpecifics(t *testing.T) {
	banned := []string{
		// dependency pins and identifiers lifted off a task
		"grpcio", "1.73", "kv-store", "kv_store", "flashpaper", "rave.red", "extract.js",
		// eval-set task names
		"pmars", "extract-elf", "ocaml", "cobol", "compcert", "corewars", "caffe", "cifar",
		"qemu", "fasttext", "sparql", "mteb", "pystan", "metacircular", "codegolf", "zork", "pdp11",
		// one task's toolchain or artifacts, named by product
		"grpc", "_pb2", ".proto", "gcov", "gcda", "opam", "valgrind", "ccomp", "sqlite",
		// a sample I/O pair and an expected output string, copied verbatim
		"208", "377", "Cleaned up",
		// harness framing: magi is not told there is a scorer, so a prompt must not assert one —
		// off-bench there is none, and inventing one licenses guessing at what it wants
		"grader", "benchmark", "leaderboard", "reward",
	}
	for _, p := range []struct {
		name, text string
	}{
		{"validateChecksSystem", validateChecksSystem},
		{"coverageFillSystem", coverageFillSystem},
		{"coverageJSONOnlyReminder", coverageJSONOnlyReminder},
		{"checkAuditJSONOnlyReminder", checkAuditJSONOnlyReminder},
		{"checkAuditKeepSomeReminder", checkAuditKeepSomeReminder},
		{"elicitCriteriaSystem", elicitCriteriaSystem},
		{"elicitSpecMineSystem", elicitSpecMineSystem},
		{"distillSpecMineSystem", distillSpecMineSystem},
		{"specMineExploreSystem", specMineExploreSystem},
		{"contractDraftSystem", contractDraftSystem},
		{"consolidateContractSystem", consolidateContractSystem},
		{"curateSystem", curateSystem},
		{"plannerContract", plannerContract},
		{"literalRule", literalRule},
		{"checkpointFirstRule", checkpointFirstRule},
		{"implicitAcceptRule", implicitAcceptRule},
		{"verifyContract", verifyContract},
		{"divergeClause", divergeClause},
		{"planJSONOnlyReminder", planJSONOnlyReminder},
		{"councilKeepWork", councilKeepWork},
		{"councilCompletionAudit", councilCompletionAudit},
		{"councilContestAffordance", councilContestAffordance},
		{"asideHandlerSystem", asideHandlerSystem},
		{"queuedTriageSystem", queuedTriageSystem},
		{"reasoningSpinNudge", reasoningSpinNudge},
		{"subagentGuide", subagentGuide},
		{"outputFormatGuide", outputFormatGuide},
		{"securityGuide", securityGuide},
	} {
		for _, b := range banned {
			if strings.Contains(p.text, b) {
				t.Errorf("%s leaks eval-set-specific token %q — use a task-agnostic example", p.name, b)
			}
		}
	}
}

// validateChecks takes the review's REPLACEMENT for a check the runner cannot evaluate: the authored
// command-shaped check gates nothing (no `assert`), and the reviewed typed check that reads what the
// step recorded is what gets stored for the rest of the run.
func TestValidateChecksRepairsMutatingCheck(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	repaired := `[{"step":"2","deliverable":"downloaded archive (step saves listing.txt)","source":"listing.txt","assert":"matches payload/"}]`
	a := newOrchApp(t, &gateLLM{text: repaired}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "2", Deliverable: "downloaded archive",
		Command: "ssh host 'tar -czf /tmp/archive.tgz /remote/dir' && echo OK"}}
	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 {
		t.Fatalf("want 1 reviewed check, got %d", len(out))
	}
	if out[0].Source != "listing.txt" || out[0].Assert != "matches payload/" {
		t.Errorf("the reviewed typed check must replace the unevaluable authored one, got %+v", out[0])
	}
}

// auditLLM answers the check-audit side call with a scripted sequence and records the System prompt
// of every call, so a test can assert BOTH what came back and what was asked the second time.
type auditLLM struct {
	mu      sync.Mutex
	replies []string
	systems []string
}

func (f *auditLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	f.mu.Lock()
	n := len(f.systems)
	f.systems = append(f.systems, r.System)
	text := "done"
	if n < len(f.replies) {
		text = f.replies[n]
	}
	f.mu.Unlock()
	ch := make(chan port.ProviderEvent, 4)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: text}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func (f *auditLLM) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.systems...)
}

// An `[]` reply is well-formed JSON that asks to drop every check — and honoring it would leave the
// plan with NO executable gate (storePlanChecks stores nothing for an empty set). A single such reply
// used to end the pass silently-in-effect: the authored checks were kept unreviewed, which is how four
// checks that each rebuilt an entire compiler reached the gate. Re-ask once, telling the model what an
// empty answer actually costs.
func TestCheckAuditEmptyReplyIsRetriedWithTheConsequence(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	repaired := `[{"step":"1","deliverable":"the compiler builds (step saves build.log)","source":"build.log","assert":"matches ^PASS"}]`
	llm := &auditLLM{replies: []string{"[]", repaired}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	in := []council.DeliverableCheck{{Step: "1", Deliverable: "the compiler builds", Command: "make world opt"}}

	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 || out[0].Assert != "matches ^PASS" {
		t.Fatalf("the retry's repair must be used, got %+v", out)
	}
	sys := llm.calls()
	if len(sys) != 2 {
		t.Fatalf("want exactly one retry (2 calls), got %d", len(sys))
	}
	if strings.Contains(sys[0], "DROPPED EVERY CHECK") {
		t.Error("the first call must carry the plain review prompt")
	}
	if !strings.Contains(sys[1], "DROPPED EVERY CHECK") || !strings.Contains(sys[1], "it removes the gate") {
		t.Error("the retry must tell the model that an empty answer removes the gate rather than a bad gate")
	}
	// The log has to distinguish this from an unparseable reply: `[]` parsed fine and said something.
	if n := sub.notes("check-audit"); !strings.Contains(n, "drop all 1 check") {
		t.Errorf("the note must say the review asked to drop every check, got:\n%s", n)
	}
}

// The other failure shape: no checks array in the reply at all. Same retry, different ask — strip the
// prose, exactly as the planner does after an unparseable plan.
func TestCheckAuditUnparseableReplyIsRetriedJSONOnly(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	repaired := `[{"step":"1","deliverable":"deps (step saves deps.txt)","source":"deps.txt","assert":"matches Version"}]`
	llm := &auditLLM{replies: []string{"I reviewed the checks and they look fine to me.", repaired}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	in := []council.DeliverableCheck{{Step: "1", Deliverable: "deps", Command: "test -f /usr/bin/pip3"}}

	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 || out[0].Assert != "matches Version" {
		t.Fatalf("the retry's repair must be used, got %+v", out)
	}
	sys := llm.calls()
	if len(sys) != 2 {
		t.Fatalf("want exactly one retry (2 calls), got %d", len(sys))
	}
	if !strings.Contains(sys[1], "COULD NOT BE PARSED") {
		t.Error("the retry must ask for the bare JSON array")
	}
	if n := sub.notes("check-audit"); !strings.Contains(n, "not a checks array") || !strings.Contains(n, "look fine to me") {
		t.Errorf("the note must name the shape and quote the reply, got:\n%s", n)
	}
}

// The retry is ONE retry. When it fails too, the authored checks are kept — dropping them would
// remove the contract on the word of a call that just failed twice — and the terminal note carries
// the second reply, since nothing is printed after it.
func TestCheckAuditRetryFailureKeepsAuthoredChecksAndReports(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	llm := &auditLLM{replies: []string{"[]", "still nothing worth keeping honestly"}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	in := []council.DeliverableCheck{{Step: "1", Deliverable: "the compiler builds", Command: "make world opt"}}

	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 || out[0].Command != "make world opt" {
		t.Fatalf("a twice-failed review must keep the authored checks verbatim, got %+v", out)
	}
	if n := len(llm.calls()); n != 2 {
		t.Fatalf("the retry must not loop: want 2 calls, got %d", n)
	}
	n := sub.notes("check-audit")
	if !strings.Contains(n, "unreviewed") || !strings.Contains(n, "worth keeping honestly") {
		t.Errorf("the terminal note must say the checks went unreviewed and quote the retry's reply, got:\n%s", n)
	}
}

// The effect-vs-cause floor belongs on the AUTHORING prompts too, not only on the audit that reviews
// them. A check is repaired far more reliably by never being written that way: the observed shape was
// a build configured with a coverage flag, checked by "configure exits 0" and "make exits 0" — both
// passed, while the artifact the flag was supposed to produce never appeared where the task looked
// for it, so the gate approved a deliverable the grader rejected.
func TestCoverageFillPromptRejectsACommandThatMerelySucceeded(t *testing.T) {
	for _, want := range []string{"command that SUCCEEDED", "ACCEPTED, not that it took EFFECT", "AT THE LOCATION the task names"} {
		if !strings.Contains(coverageFillSystem, want) {
			t.Errorf("coverageFillSystem must reject a command-exit-code proxy for an effect (missing %q)", want)
		}
	}
}

// A check that comes back with no `assert` is the WORST outcome available: the runner evaluates only
// `source`/`assert`, so it returns 126, which every gate reads as "no verdict" — the step lands neither
// proven nor failed, and nothing in the log looks wrong. The review prompt already carries the
// conversion rule and one live review is still enough to leave a command behind, so the miss is
// detected deterministically and re-asked ONCE, naming the offender.
func TestCheckAuditReasksWhenAReviewedCheckCarriesNoAssertion(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	reviewed := `[{"step":"1","deliverable":"the suite passes","command":"make -C testsuite one DIR=t"},
	              {"step":"2","deliverable":"binary runs","source":"version.txt","assert":"matches 1\\.2"}]`
	repaired := `[{"step":"1","deliverable":"the suite passes (step saves suite.log)","source":"suite.log","assert":"matches ^All tests passed"},
	              {"step":"2","deliverable":"binary runs","source":"version.txt","assert":"matches 1\\.2"}]`
	llm := &auditLLM{replies: []string{reviewed, repaired}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	in := []council.DeliverableCheck{{Step: "1", Deliverable: "the suite passes", Command: "make world"}}

	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)

	for _, c := range out {
		if strings.TrimSpace(c.Assert) == "" {
			t.Errorf("the repaired set must carry an assertion on every check, got %+v", c)
		}
	}
	if len(out) != 2 {
		t.Fatalf("both checks must survive the repair, got %d: %+v", len(out), out)
	}
	calls := llm.calls()
	if len(calls) != 2 {
		t.Fatalf("want exactly one re-ask (bounded), got %d call(s)", len(calls))
	}
	// The re-ask must name the offending check and the consequence — an abstract rule is what the
	// first pass already ignored.
	if !strings.Contains(calls[1], "make -C testsuite") || !strings.Contains(calls[1], "NO `assert`") {
		t.Errorf("the re-ask must name the unasserted check and what it costs, got %q", clipLine(calls[1], 400))
	}
	if note := sub.notes("check-audit"); !strings.Contains(note, "no `assert`") {
		t.Errorf("the missing assertion must be reported, got %q", note)
	}
}

// The re-ask is told to return everything; a model that answers "repair these" with only the repaired
// check must not thereby delete the checks that were fine.
func TestCheckAuditReaskCannotShrinkTheContract(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	reviewed := `[{"step":"1","deliverable":"built","command":"make all"},
	              {"step":"2","deliverable":"binary runs","source":"version.txt","assert":"matches 1\\.2"}]`
	onlyRepaired := `[{"step":"1","deliverable":"built","source":"build.log","assert":"matches ^BUILD OK"}]`
	llm := &auditLLM{replies: []string{reviewed, onlyRepaired}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "1", Deliverable: "built", Command: "make all"}}

	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 2 {
		t.Fatalf("the already-typed check must be unioned back, got %d: %+v", len(out), out)
	}
	var asserts []string
	for _, c := range out {
		asserts = append(asserts, c.Assert)
	}
	if !strings.Contains(strings.Join(asserts, "|"), "1\\.2") {
		t.Errorf("the check that was already typed must survive, got %v", asserts)
	}
}

// A re-ask that repairs nothing must leave the reviewed set alone rather than adopt a degraded reply,
// and must say the step lands ungated — the failure mode here is silence, not a wrong verdict.
func TestCheckAuditKeepsReviewedSetWhenTheReaskDoesNotHelp(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	reviewed := `[{"step":"1","deliverable":"built","command":"make all"}]`
	llm := &auditLLM{replies: []string{reviewed, `[{"step":"1","deliverable":"built","command":"cmake --build ."}]`}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	sub := watchProgress(t, a, s.ID)
	in := []council.DeliverableCheck{{Step: "1", Deliverable: "built", Command: "make all"}}

	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 || out[0].Command != "make all" {
		t.Fatalf("the reviewed set must be kept when the re-ask does not convert anything, got %+v", out)
	}
	if note := sub.notes("check-audit"); !strings.Contains(note, "ungated") {
		t.Errorf("an unconverted check must be reported as an ungated step, got %q", note)
	}
}

// A reviewed set where every check already carries an assertion has nothing to repair, so predicting
// one would spend a side call on a non-problem.
func TestCheckAuditDoesNotReaskWhenEveryCheckIsTyped(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	llm := &auditLLM{replies: []string{`[{"step":"1","deliverable":"built","source":"build.log","assert":"matches ^BUILD OK"}]`}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	a.validateChecks(context.Background(), a.agentFor(s), s,
		[]council.DeliverableCheck{{Step: "1", Deliverable: "built", Command: "make all"}})
	if calls := llm.calls(); len(calls) != 1 {
		t.Errorf("want the single review call when the reply is fully typed, got %d", len(calls))
	}
}

// The three prompts that AUTHOR, FILL and REVIEW checks must all carry the same shape — the STEP
// performs the run and redirects its REAL output to a fixed path, and the check reads that path. It is
// now the ONLY shape the runner can evaluate, so a prompt that dropped it would be asking for checks
// that gate nothing; and "the REAL output" is the clause that separates a recorded run from a
// hand-written file that says the run went well.
func TestCheckPromptsMakeRecordAndReadTheDefault(t *testing.T) {
	for name, prompt := range map[string]string{
		"coverage-fill": coverageFillSystem,
		"check-audit":   validateChecksSystem,
		"typed-reask": typedRepairReminder([]string{
			"step 2 the suite passes (`make test`)",
		}),
	} {
		if !strings.Contains(prompt, "the STEP") {
			t.Errorf("%s prompt must put the run on the step:\n%s", name, prompt)
		}
		if !strings.Contains(prompt, "REAL output") {
			t.Errorf("%s prompt must require the recorded file to be the run's real output", name)
		}
		if !strings.Contains(prompt, "fixed path") {
			t.Errorf("%s prompt must point at a fixed recorded path", name)
		}
	}
	// The reviewer must not be told the rule applies only to expensive runs — that framing is what
	// left ordinary executing checks in place.
	if strings.Contains(validateChecksSystem, "expensive run, assert on the result file") {
		t.Error("check-audit prompt still conditions record-and-read on the run being expensive")
	}
}
