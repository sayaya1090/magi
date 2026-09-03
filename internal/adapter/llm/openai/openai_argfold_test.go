package openai

import (
	"encoding/json"
	"testing"
)

// TestEmptyArgumentsFoldToAnObject pins the three spellings of "this call takes no arguments".
//
// Measured on a live run (2026-09-04): a model called `websearch` with `[]` and got back
// `invalid arguments: json: cannot unmarshal array into Go value of type builtin.webSearchArgs`.
// The search never happened, and the model had no way to read that line as "send an object".
//
// The accumulator's doc comment already claimed this job. It knew one spelling.
func TestEmptyArgumentsFoldToAnObject(t *testing.T) {
	for _, raw := range []string{"[]", " [] ", "null", ""} {
		if got := string(emptyArgsToObject(json.RawMessage(raw))); got != "{}" {
			t.Errorf("%q should be an empty call, got %q", raw, got)
		}
	}
	// A non-empty array is a real mistake: the tool must still get to name what it wanted.
	for _, raw := range []string{`[1,2]`, `{"query":"x"}`, `["query"]`} {
		if got := string(emptyArgsToObject(json.RawMessage(raw))); got != raw {
			t.Errorf("%q must pass through unchanged, got %q", raw, got)
		}
	}
}
