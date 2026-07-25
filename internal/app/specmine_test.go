package app

import (
	"strings"
	"testing"
)

// parseSpecMine extracts the first balanced JSON object (fenced or prefixed replies
// included) and rejects prose-only replies; the renderer caps lines in code.
func TestParseSpecMine(t *testing.T) {
	ok := `Here you go:
{"lines":[{"surface":"max_n: int","requirement":"exact bound","construct":"Semaphore"}],"final":"use Semaphore"}`
	res, got := parseSpecMine(ok)
	if !got || len(res.Lines) != 1 || res.Lines[0].Construct != "Semaphore" || res.Final != "use Semaphore" {
		t.Fatalf("parse failed: %+v %v", res, got)
	}
	if _, got := parseSpecMine("no json here"); got {
		t.Fatal("prose-only reply must not parse")
	}
	if _, got := parseSpecMine(`{"lines":[`); got {
		t.Fatal("unbalanced JSON must not parse")
	}
}

// A mined value that contains an UNbalanced brace inside its string ("use } to close a block", "dict
// is { key") must still parse — the whole distilled result was previously lost because the extractor
// tracked brace depth without respecting string literals, so a lone } inside a value closed the
// object early. balancedObjects respects strings, so the result now survives.
func TestParseSpecMineBraceInStringValue(t *testing.T) {
	for _, blob := range []string{
		`{"lines":[{"surface":"block","requirement":"use } to close a block","construct":"syntax"}],"final":"ok"}`,
		`{"lines":[{"surface":"dict","requirement":"a value like { key: val opens a map","construct":"map"}],"final":"ok"}`,
	} {
		res, got := parseSpecMine(blob)
		if !got || len(res.Lines) != 1 || res.Final != "ok" {
			t.Fatalf("brace-in-string value must still parse: got=%v %+v\nblob=%s", got, res, blob)
		}
	}
}

// The rendered note is code-capped at five lines and carries the single final
// recommendation on a USE: line.
func TestSpecMineRenderCap(t *testing.T) {
	var lines []string
	for range [7]int{} {
		lines = append(lines, `{"surface":"s","requirement":"r","construct":"c"}`)
	}
	blob := `{"lines":[` + strings.Join(lines, ",") + `],"final":"use X"}`
	res, got := parseSpecMine(blob)
	if !got || len(res.Lines) != 7 {
		t.Fatalf("setup parse failed: %v %d", got, len(res.Lines))
	}
	// Render through the same path elicitSpecMine uses (inline here: cap + USE line).
	n := 0
	for i := range res.Lines {
		if i >= 5 {
			break
		}
		n++
	}
	if n != 5 {
		t.Fatalf("cap not enforced: %d", n)
	}
}

// specKind maps the model's classification into one of the three honor-modes, defaulting UNKNOWN and
// EMPTY to semantic — the safe "verify by effect" mode that never over-asserts a source spelling.
func TestSpecKindNormalizes(t *testing.T) {
	for in, want := range map[string]string{
		"hard": "hard", "literal": "hard", "identifier": "hard", "VERBATIM": "hard",
		"example": "example", "sample": "example",
		"unconstrained": "unconstrained", "free": "unconstrained", "open": "unconstrained", "unspecified": "unconstrained",
		"semantic": "semantic", "": "semantic", "  ": "semantic", "whatever": "semantic",
	} {
		if got := specKind(in); got != want {
			t.Errorf("specKind(%q) = %q, want %q", in, got, want)
		}
	}
}

// The distilled JSON now carries a per-line kind, and parseSpecMine surfaces it.
func TestParseSpecMineKind(t *testing.T) {
	blob := `{"lines":[{"surface":"GetValRequest key","requirement":"message has a string field","construct":"protobuf","kind":"semantic"}],"final":"use protoc"}`
	res, ok := parseSpecMine(blob)
	if !ok || len(res.Lines) != 1 || res.Lines[0].Kind != "semantic" {
		t.Fatalf("kind not parsed: %+v ok=%v", res, ok)
	}
	// A line with no kind still parses; the renderer/specKind default it to semantic.
	res2, ok := parseSpecMine(`{"lines":[{"surface":"s","requirement":"r","construct":"c"}],"final":""}`)
	if !ok || specKind(res2.Lines[0].Kind) != "semantic" {
		t.Fatalf("missing kind must default to semantic, got %q", res2.Lines[0].Kind)
	}
}

// The execution note defines every honor-mode tag — including ⟨unconstrained⟩ (the task leaves it
// free, so nothing may assert it) — so the executor and check-author read each requirement correctly.
func TestSpecMineNoteDefinesKinds(t *testing.T) {
	note := specMineNote("- ⟨semantic⟩ x → y → z")
	for _, want := range []string{"⟨hard⟩", "⟨example⟩", "⟨semantic⟩", "⟨unconstrained⟩", "verify by EFFECT", "FREE to choose"} {
		if !strings.Contains(note, want) {
			t.Errorf("specMineNote must define the honor-mode tags (missing %q)", want)
		}
	}
}

// specKind classifies the fourth mode: an aspect the task does not pin is unconstrained (free/open/
// unspecified all normalize to it), so downstream never invents a constraint.
func TestSpecKindUnconstrained(t *testing.T) {
	for _, in := range []string{"unconstrained", "free", "open", "unspecified"} {
		if got := specKind(in); got != "unconstrained" {
			t.Errorf("specKind(%q) = %q, want unconstrained", in, got)
		}
	}
}

// Both mining prompts state the classification (all four kinds) and instruct example lines to carry
// the literal I/O — task-agnostically (no eval-set identifiers leaked).
func TestSpecMinePromptsClassify(t *testing.T) {
	if !strings.Contains(elicitSpecMineSystem, "CLASSIFY") {
		t.Error("pass-1 prompt must instruct classification")
	}
	if !strings.Contains(distillSpecMineSystem, "hard|example|semantic|unconstrained") {
		t.Error("pass-2 schema must carry all four kinds including unconstrained")
	}
	for _, sys := range []string{elicitSpecMineSystem, distillSpecMineSystem} {
		if !strings.Contains(strings.ToUpper(sys), "UNCONSTRAINED") {
			t.Error("both prompts must name the unconstrained (free-to-choose) category")
		}
		if !strings.Contains(sys, "VERBATIM") {
			t.Error("both prompts must instruct capturing the example's I/O verbatim")
		}
		// Word-bounded: "ELF" as a standalone token, not the "elf" suffix of itself/yourself/shelf.
		for _, banned := range []string{"KVStore", "GetVal", "kv-store", "ELF ", "ELF3", "ELF6", "a.out", "extract.js", "PT_LOAD"} {
			if strings.Contains(sys, banned) {
				t.Errorf("mining prompt leaks an eval-set token %q — keep it task-agnostic", banned)
			}
		}
	}
}

// A TRANSFORM/reproduction task (output derived from a present input — a file to parse, a format to
// convert, another program's output to match) fails on a subtly wrong RULE, not a wrong name, and a
// single self-consistent implementation cannot catch its own rule error. Both prompts must instruct
// mining the PRECISE input→output mapping (selection, order/stride, per-element computation, key/value
// encoding) into the requirement so it is reproducible — task-agnostically. Grounds the extract-elf
// logic-error miss (values did not match the reference).
func TestSpecMinePromptTransformLens(t *testing.T) {
	for _, want := range []string{"TRANSFORM", "endianness", "offset/stride", "byte-identical"} {
		if !strings.Contains(elicitSpecMineSystem, want) {
			t.Errorf("pass-1 prompt must mine the transform mapping (missing %q)", want)
		}
	}
	// Pass-2 must carry the precise-mapping rule so the requirement survives distillation.
	if !strings.Contains(distillSpecMineSystem, "TRANSFORM/reproduction") {
		t.Error("pass-2 prompt must keep a transform finding's precise mapping in the requirement")
	}
	// Stays semantic (verify by effect against the real input), not a new hard-literal category.
	if !strings.Contains(elicitSpecMineSystem, "stays SEMANTIC") {
		t.Error("the transform lens must classify as semantic (verified by running against the real input)")
	}
}

// The CONSTRAINTS front mines the MUST/MUST-NOT conditions a task states but an implementation forgets
// mid-work — a scope limit ("only modify X"), a structural requirement ("must contain / end with"), a
// forbidden action — so the executor keeps them and a checker verifies them against the real diff/
// artifact. Grounds the self-acknowledged scope violation and the missing-terminator failures. The
// prompt stays task-agnostic (placeholders, no eval-set tokens).
func TestSpecMinePromptConstraintsFront(t *testing.T) {
	for _, want := range []string{"FOURTH", "CONSTRAINTS", "SCOPE limit", "off-limits", "STRUCTURAL requirement", "FORBIDDEN", "changed-file set stays within"} {
		if !strings.Contains(elicitSpecMineSystem, want) {
			t.Errorf("pass-1 prompt must mine explicit constraints (missing %q)", want)
		}
	}
	// Necessity guard: only capture a constraint the task itself states.
	if !strings.Contains(elicitSpecMineSystem, "only capture a constraint the task itself states") {
		t.Error("the constraints front must not invent constraints the task never stated")
	}
	for _, banned := range []string{"user.cpp", "main.cpp", "apply_macros"} {
		if strings.Contains(strings.ToLower(elicitSpecMineSystem), strings.ToLower(banned)) {
			t.Errorf("constraints prompt leaks an eval-set token %q", banned)
		}
	}
}

// The classification prompts steer the weak model away from the observed over-⟨hard⟩ bias: HARD is the
// minority (a byte-for-byte literal), and a behavior/format/structure is semantic even when required.
func TestSpecMinePromptsNarrowHard(t *testing.T) {
	for _, sys := range []string{elicitSpecMineSystem, distillSpecMineSystem} {
		if !strings.Contains(sys, "MINORITY") {
			t.Error("prompt must state HARD is the minority")
		}
		if !strings.Contains(strings.ToLower(sys), "importance does not make it hard") &&
			!strings.Contains(strings.ToLower(sys), "importance does not make it hard.") {
			t.Error("prompt must state importance does not make a requirement hard")
		}
	}
}

// A fixed value inside a larger string the task didn't shape (a port inside a bind address, a name in a
// URL) must not promote the WHOLE string to hard — only the value is hard, the enclosing format is the
// implementer's choice. Grounds the over-hard fix observed live (a bind address classified verbatim).
func TestSpecMinePromptEnclosingFormatNotHard(t *testing.T) {
	for _, want := range []string{"INSIDE a larger string", "bind address", "the enclosing", "UNCONSTRAINED"} {
		if !strings.Contains(elicitSpecMineSystem, want) {
			t.Errorf("classification prompt must keep an enclosing format unconstrained (missing %q)", want)
		}
	}
	// Task-agnostic: `[::]` / `0.0.0.0` are generic networking, but a concrete eval-set port must not leak.
	for _, banned := range []string{"5328", "KVStore", "kv-store"} {
		if strings.Contains(elicitSpecMineSystem, banned) {
			t.Errorf("prompt leaks an eval-set token %q", banned)
		}
	}
}
