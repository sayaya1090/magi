package jsonx

import (
	"encoding/json"
	"testing"
)

// Go aborts the whole document on the first type mismatch, so one field answered in the wrong shape
// costs every sibling beside it. For a council verdict that means the member's VOTE is lost and
// recorded as an abstain the tally cannot tell from "no opinion"; for a plan it means every step.
func TestTolerantTypes(t *testing.T) {
	var v struct {
		A Text   `json:"a"`
		B Texts  `json:"b"`
		C Number `json:"c"`
	}
	// The shapes a model actually emits where the schema says string / list / float.
	raw := `{"a":["one","two"],"b":"single","c":"0.9"}`
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("tolerant types must not fail: %v", err)
	}
	if string(v.A) != "one; two" {
		t.Errorf("a list must join: %q", v.A)
	}
	if len(v.B) != 1 || v.B[0] != "single" {
		t.Errorf("a bare string must become a one-element list: %+v", v.B)
	}
	if float64(v.C) != 0.9 {
		t.Errorf("a quoted number must parse: %v", v.C)
	}
	// The declared shapes still work unchanged.
	raw2 := `{"a":"plain","b":["x","y"],"c":0.5}`
	if err := json.Unmarshal([]byte(raw2), &v); err != nil {
		t.Fatal(err)
	}
	if string(v.A) != "plain" || len(v.B) != 2 || float64(v.C) != 0.5 {
		t.Errorf("declared shapes must be unchanged: %q %+v %v", v.A, v.B, v.C)
	}
	// Unusable shapes degrade to empty rather than failing the document.
	raw3 := `{"a":{"k":1},"b":{"k":1},"c":{"k":1}}`
	if err := json.Unmarshal([]byte(raw3), &v); err != nil {
		t.Fatalf("an unusable shape must degrade, not fail: %v", err)
	}
	if v.A != "" || v.B != nil || v.C != 0 {
		t.Errorf("want zero values, got %q %+v %v", v.A, v.B, v.C)
	}
}

// Both defects below are ALREADY invalid JSON, so repairing them cannot corrupt a well-formed
// document — and both were observed in one run, where they cost the plan twice: the first reply and
// its JSON-only retry each failed, leaving the turn with no plan at all.
func TestStructuralRepairs(t *testing.T) {
	// A bare identifier value — observed as {"agent":explore,…}.
	bare := `{"steps":[{"agent":explore,"focus":"caml/","question":"read the sweep fn"}]}`
	if !parses(t, bare) {
		t.Errorf("a bare value must be repaired: %s", bare)
	}
	// Python-style single quotes, mixed in AFTER a double-quoted prefix (what the model actually did).
	mixed := `{"reason":"ok","steps":[{"title":"a"},{'title': 'Verify crash is fixed', 'strategy': 'solo'}]}`
	if !parses(t, mixed) {
		t.Errorf("single-quoted objects must be repaired: %s", mixed)
	}
	// A legal bare value must NOT be quoted.
	for _, legal := range []string{`{"a":true}`, `{"a":false}`, `{"a":null}`, `{"a":12}`, `{"a":-1.5}`} {
		got := QuoteBareValues(legal)
		if got != legal {
			t.Errorf("a legal document must be untouched: %s → %s", legal, got)
		}
	}
	// An apostrophe INSIDE a double-quoted string is not a delimiter and must survive.
	apos := `{"note":"don't touch this"}`
	if SingleToDoubleQuotes(apos) != apos {
		t.Errorf("an apostrophe inside a string must be left alone: %s", SingleToDoubleQuotes(apos))
	}
	// Repairs are additive, never destructive: a clean document yields itself first.
	clean := `{"a":"b"}`
	if c := RepairCandidates(clean); c[0] != clean {
		t.Errorf("the original must be tried first, got %q", c[0])
	}
}

func parses(t *testing.T, s string) bool {
	t.Helper()
	for _, c := range RepairCandidates(s) {
		var v any
		if json.Unmarshal([]byte(c), &v) == nil {
			return true
		}
	}
	return false
}
