package jsonx

import "testing"

// A backslash that begins no legal JSON escape kills the document at that byte, and the two shapes
// that produce it are content a model has no reason to think twice about: a Windows-style path and
// a regex. Observed live in a council reply, where `\e` in a cited filename ended the parse at
// offset 1106 of 1332 — the member's decision survived on the prefix and its feedback and grounds
// did not.
func TestAnInvalidEscapeDoesNotCostTheDocument(t *testing.T) {
	cases := []struct{ name, js, field, want string }{
		{"windows path in a citation",
			`{"decision":"done","cite":"heap-crash__VwdqwmF\exception.txt — 80.6 KB"}`,
			"cite", `heap-crash__VwdqwmF\exception.txt — 80.6 KB`},
		{"a regex in feedback",
			`{"decision":"continue","feedback":"the pattern \d+\.\d+ never matches"}`,
			"feedback", `the pattern \d+\.\d+ never matches`},
		{"a \\u that is not four hex digits",
			`{"a":"bad \u12 escape"}`, "a", `bad \u12 escape`},
	}
	for _, c := range cases {
		var v map[string]string
		if !Unmarshal(c.js, &v) {
			t.Errorf("%s: still unreadable: %s", c.name, c.js)
			continue
		}
		if got := v[c.field]; got != c.want {
			t.Errorf("%s: %s = %q, want %q", c.name, c.field, got, c.want)
		}
	}
}

// The repair only touches what was already broken: a legal escape is never doubled, and a
// backslash outside a string is not string content.
func TestValidEscapesSurviveUntouched(t *testing.T) {
	for _, js := range []string{
		`{"a":"line\nbreak\ttab \"quoted\" back\\slash"}`,
		`{"a":"unicode \u00e9 and \u0041"}`,
		`{"a":"slash \/ escaped"}`,
	} {
		if got := EscapeInvalidEscapes(js); got != js {
			t.Errorf("a valid document was rewritten:\n in: %s\nout: %s", js, got)
		}
		var v map[string]string
		if !Unmarshal(js, &v) {
			t.Errorf("a valid document stopped parsing: %s", js)
		}
	}
	// Round-trip: whatever the repair produces must be parseable and mean the literal backslash.
	var v map[string]string
	if !Unmarshal(`{"a":"x\qy"}`, &v) || v["a"] != `x\qy` {
		t.Errorf("the backslash must survive as a literal, got %q", v["a"])
	}
}

// The limit, stated so nobody reads more into the repair than it does. A Windows path whose next
// character happens to spell a LEGAL escape is ambiguous in the format itself: `\f` is form feed,
// and `C:\x\file.txt` cannot be told apart from a string that wanted one. Prose with a real \n or
// \t is far commoner than a path, so the legal reading wins and that one separator is lost — while
// every other field in the document, which used to go with it, survives.
func TestALegalEscapeInsideAPathIsStillAnEscape(t *testing.T) {
	var v map[string]string
	if !Unmarshal(`{"a":"C:\Users\x\file.txt","b":"kept"}`, &v) {
		t.Fatal("the document must still parse")
	}
	if v["b"] != "kept" {
		t.Errorf("the rest of the document was lost anyway: %+v", v)
	}
	if want := `C:\Users\x` + "\f" + "ile.txt"; v["a"] != want {
		t.Errorf("a = %q, want %q — the two invalid separators are literal, the \\f is form feed", v["a"], want)
	}
}
