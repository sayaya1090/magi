package app

import (
	"strings"
	"testing"
)

func has(t *testing.T, got []string, want string) {
	t.Helper()
	for _, g := range got {
		if g == want {
			return
		}
	}
	t.Errorf("missing literal %q from %q", want, got)
}

// The request's exact words are lexical facts, not a judgement — magi can read them without asking.
func TestRequestLiteralsReadsWhatTheRequestPins(t *testing.T) {
	req := "Fix the GC crash. Read HACKING.adoc first, then run:\n\n" +
		"```bash\n./configure --prefix=/usr/local\nmake world opt\n```\n\n" +
		"Verify with `make -C testsuite one DIR=tests/basic` and make sure the output says \"All tests passed\". " +
		"The bug is in ocaml/runtime/shared_heap.c near line 620."
	got := requestLiterals(req)
	for _, want := range []string{
		"./configure --prefix=/usr/local",       // fenced line
		"make world opt",                        // fenced line
		"make -C testsuite one DIR=tests/basic", // backticks
		"All tests passed",                      // quoted
		"ocaml/runtime/shared_heap.c",           // path
	} {
		has(t, got, want)
	}
	// A bare filename with no separator is left alone: this pass reads structure, it does not guess
	// which words in a sentence are names.
	for _, g := range got {
		if g == "HACKING.adoc" {
			t.Error("a bare word must not be collected as a literal")
		}
	}
	// The leading dot is part of the literal, not punctuation around it — trimming it once produced
	// `/configure`, a path that exists nowhere.
	for _, g := range got {
		if g == "/configure --prefix=/usr/local" {
			t.Error("`./configure` must not be trimmed to `/configure`")
		}
	}
	// Every entry must be a substring of the input — this pass reads, it never invents.
	for _, g := range got {
		if !strings.Contains(req, g) {
			t.Errorf("literal %q is not in the request", g)
		}
	}
	if len(requestLiterals(strings.Repeat("`a/b.c` ", 100))) > requestLiteralCap {
		t.Errorf("the floor must stay bounded, got %d", len(requestLiterals(strings.Repeat("`a/b.c` ", 100))))
	}
	if n := len(requestLiterals("just some prose with no pinned spans at all")); n != 0 {
		t.Errorf("prose pins nothing, got %d literals", n)
	}
}

// The model's list is kept and comes first: it knows which literal MATTERS, which the floor cannot.
// Overlap is dropped in both directions so the section stays short enough to read.
func TestFloorGoesUnderTheModelsList(t *testing.T) {
	model := []string{"make world", "run_tasks"}
	out := withRequestLiterals(model, "run `make world opt` in /app/ocaml/runtime and see `port 2222`")
	if out[0] != "make world" || out[1] != "run_tasks" {
		t.Fatalf("the model's literals must come first, got %q", out)
	}
	for _, dup := range out[2:] {
		if strings.Contains(dup, "make world") {
			t.Errorf("a span already covered by the model's list must not repeat: %q", dup)
		}
	}
	has(t, out, "port 2222")
	has(t, out, "/app/ocaml/runtime")

	// The whole point: a curator that returned NOTHING still yields a preserved list.
	if out := withRequestLiterals(nil, "run `make -C testsuite one DIR=tests/basic`"); len(out) == 0 {
		t.Fatal("an empty curator answer must not mean zero literals")
	}
	// Nothing to read is still nothing — the floor never invents.
	if out := withRequestLiterals(nil, "please have a look at the code"); len(out) != 0 {
		t.Errorf("with nothing pinned the list stays empty, got %q", out)
	}
	// Rendering carries it into the section that says not to reword it.
	brief := renderCurateBrief(curatePacket{Goal: "g", Literals: withRequestLiterals(nil, "run `sim 208`")})
	if !strings.Contains(brief, "Preserve these EXACTLY") || !strings.Contains(brief, "sim 208") {
		t.Errorf("the floor must reach the preserve section:\n%s", brief)
	}
}
