package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
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

// The validation prompt must carry the work≠check principle so the review can turn a mutating,
// non-idempotent "check" (which re-does the step's work and traps the run in a redo loop) into a
// read-only probe. Guards the criteria.go prompt against a silent regression.
func TestValidateChecksPromptForbidsMutatingChecks(t *testing.T) {
	for _, want := range []string{"IDEMPOTENT", "work≠check", "tar -czf"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must state the work≠check rule (missing %q)", want)
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

// The validation prompt must forbid hardcoded absolute tool paths: a check that pins `/usr/bin/pip3`
// false-fails on an image where the tool lives at `/usr/local/bin/pip3` or a venv shim. Guards the
// PORTABLE clause against the kv-store-grpc regression (an absolute pip path the container did not have).
func TestValidateChecksPromptForbidsAbsoluteToolPaths(t *testing.T) {
	for _, want := range []string{"/usr/bin/pip3", "BARE name", "PATH resolves"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must forbid hardcoded absolute tool paths (missing %q)", want)
		}
	}
}

// The validation prompt must require a check that greps a generated file to use the name the tool
// actually emits: `grpc_tools` sanitizes a hyphenated `.proto` to an UNDERSCORED module, so a check
// demanding the hyphenated form fights the toolchain in an unwinnable rename loop. Guards the
// TOOL-DERIVED NAMES clause with a task-agnostic example (no eval-set filename).
func TestValidateChecksPromptRespectsToolDerivedNames(t *testing.T) {
	for _, want := range []string{"TOOL-DERIVED NAMES", "data_feed_pb2.py", "UNDERSCORED"} {
		if !strings.Contains(validateChecksSystem, want) {
			t.Errorf("validateChecksSystem must respect tool-derived filenames (missing %q)", want)
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

// Guard against benchmark overfitting: prompt examples must be task-agnostic. No eval-set task's exact
// identifiers (a pinned dependency version, a specific task filename) may be baked into a prompt the
// model sees — an example lifted verbatim from the test set tunes the prompt to the benchmark.
func TestPromptsCarryNoEvalSetSpecifics(t *testing.T) {
	banned := []string{"grpcio", "1.73", "kv-store", "kv_store", "pmars", "flashpaper", "rave.red", "extract-elf", "extract.js"}
	for _, p := range []struct {
		name, text string
	}{
		{"validateChecksSystem", validateChecksSystem},
	} {
		for _, b := range banned {
			if strings.Contains(p.text, b) {
				t.Errorf("%s leaks eval-set-specific token %q — use a task-agnostic example", p.name, b)
			}
		}
	}
}

// With the flag on, validateChecks replaces a mutating check with the review's read-only repair:
// the authored `ssh host 'tar -czf ...'` (which re-compresses the remote tree every gate cycle) must
// not survive verbatim — the reviewed idempotent probe is used instead.
func TestValidateChecksRepairsMutatingCheck(t *testing.T) {
	t.Setenv("MAGI_CHECK_VALIDATE", "1")
	repaired := `[{"step":"2","deliverable":"downloaded archive","command":"tar -tzf ./bench.tgz >/dev/null"}]`
	a := newOrchApp(t, &gateLLM{text: repaired}, Config{Permission: "allow", MaxAgents: 10})
	s := parentSession(t.TempDir())
	in := []council.DeliverableCheck{{Step: "2", Deliverable: "downloaded archive",
		Command: "ssh host 'tar -czf /tmp/bench.tgz /remote/dir' && echo OK"}}
	out := a.validateChecks(context.Background(), a.agentFor(s), s, in)
	if len(out) != 1 {
		t.Fatalf("want 1 reviewed check, got %d", len(out))
	}
	if strings.Contains(out[0].Command, "tar -czf") || strings.Contains(out[0].Command, "ssh ") {
		t.Errorf("the mutating authored check must be replaced by the read-only probe, got %q", out[0].Command)
	}
}
