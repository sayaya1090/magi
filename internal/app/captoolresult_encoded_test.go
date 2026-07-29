package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// The cap is on the ENCODED bytes but the cut is made in the DECODED string, and encoding expands:
// every \n, \t and " becomes two bytes. A payload of numbered source lines is full of them, so its
// decoded length can sit under the cap while its JSON form is over.
//
// Observed live (fix-ocaml-gc, 2026-07-30): a bare `read` of a 62 KiB C file, whose JSON form was
// past 64 KiB, hit exactly that band. The old code clamped cut to len(text) and then evaluated
// text[cut] — one past the end — and the panic killed the process:
//
//	panic: runtime error: index out of range [62289] with length 62289
//	  internal/app.capToolResult  guard.go:720
//
// magi exited 2 eight calls into a three-hour task, harbor recorded NonZeroAgentExitCodeError, and
// the task scored 0 with nothing attempted. A crash in the truncation path costs the whole run.
func TestCapToolResultSurvivesAnEncodingThatOutgrowsItsText(t *testing.T) {
	// The live shape: gutter-numbered lines, one \n and one \t each, decoded under the cap and
	// encoded over it.
	var sb strings.Builder
	for i := 1; sb.Len() < toolResultCap-600; i++ {
		fmt.Fprintf(&sb, "%d\tstatic void caml_do_something(struct caml_heap_state* s, value v);\n", i)
	}
	text := sb.String()
	b, err := json.Marshal(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > toolResultCap {
		t.Fatalf("the point is a decoded length UNDER the cap, got %d", len(text))
	}
	if len(b) <= toolResultCap {
		t.Fatalf("and an encoded length OVER it, got %d — the band this test exists for", len(b))
	}

	out := capToolResult(b) // must not panic

	var got string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the capped result must still be a JSON string: %v", err)
	}
	if len(out) > toolResultCap {
		t.Errorf("the cap is on the encoded bytes: got %d, cap %d", len(out), toolResultCap)
	}
	if !strings.Contains(got, "output truncated") {
		t.Errorf("a truncated result must say so:\n%s", got[max(0, len(got)-200):])
	}
	if !strings.HasPrefix(got, "1\tstatic void") {
		t.Errorf("the head of the payload must survive: %q", got[:min(40, len(got))])
	}
	if !utf8.ValidString(got) {
		t.Error("the cut must land on a rune boundary")
	}

	// The ordinary case still works: decoded and encoded both far over the cap.
	big, _ := json.Marshal(strings.Repeat("x", toolResultCap*2))
	o2 := capToolResult(big)
	if len(o2) > toolResultCap {
		t.Errorf("plain oversized text must be capped too, got %d", len(o2))
	}
	// And something already small is returned untouched.
	small, _ := json.Marshal("hello")
	if o3 := capToolResult(small); string(o3) != string(small) {
		t.Errorf("an under-cap result must pass through unchanged: %s", o3)
	}
	// A multibyte payload in the same band must not be split mid-rune.
	var mb strings.Builder
	for mb.Len() < toolResultCap-600 {
		mb.WriteString("한\t글\n")
	}
	mbj, _ := json.Marshal(mb.String())
	if len(mbj) > toolResultCap {
		var s string
		if err := json.Unmarshal(capToolResult(mbj), &s); err != nil || !utf8.ValidString(s) {
			t.Errorf("multibyte cut must stay valid: err=%v", err)
		}
	}
}
