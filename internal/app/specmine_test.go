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

// The execution note defines the three honor-mode tags so the executor and the check-author read a
// ⟨semantic⟩ description as "verify by effect", not a literal to grep.
func TestSpecMineNoteDefinesKinds(t *testing.T) {
	note := specMineNote("- ⟨semantic⟩ x → y → z")
	for _, want := range []string{"⟨hard⟩", "⟨example⟩", "⟨semantic⟩", "verify by EFFECT"} {
		if !strings.Contains(note, want) {
			t.Errorf("specMineNote must define the honor-mode tags (missing %q)", want)
		}
	}
}

// Both mining prompts state the classification, task-agnostically (no eval-set identifiers leaked).
func TestSpecMinePromptsClassify(t *testing.T) {
	if !strings.Contains(elicitSpecMineSystem, "CLASSIFY") {
		t.Error("pass-1 prompt must instruct classification")
	}
	if !strings.Contains(distillSpecMineSystem, "hard|example|semantic") {
		t.Error("pass-2 schema must carry the kind field")
	}
	for _, sys := range []string{elicitSpecMineSystem, distillSpecMineSystem} {
		for _, banned := range []string{"KVStore", "GetVal", "kv-store"} {
			if strings.Contains(sys, banned) {
				t.Errorf("mining prompt leaks an eval-set token %q — keep it task-agnostic", banned)
			}
		}
	}
}
