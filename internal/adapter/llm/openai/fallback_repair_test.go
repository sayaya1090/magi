package openai

import (
	"encoding/json"
	"testing"
)

// usableArgs reports whether a tool dispatch could read these arguments at all.
func usableArgs(t *testing.T, raw json.RawMessage) bool {
	t.Helper()
	var probe map[string]any
	return json.Unmarshal(raw, &probe) == nil
}

// The same defect had two fates on the same path. A model that wrapped its arguments in a string
// got them repaired on the way out of the wrapper; a model that wrote the object directly got them
// handed on untouched — and an unparseable argument payload does not degrade a call here, it
// erases it, because on this path the reply IS the action.
//
// This is the path taken when the model was too weak to emit a native tool call at all, and the
// native path repairs every call's arguments at finish(). That is not the population to give the
// weaker treatment to.
func TestADirectlyWrittenObjectIsRepairedLikeAWrappedOne(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"unescaped quote inside a command", `{"command":"echo "hi""}`},
		{"raw newline inside content", "{\"content\":\"line one\nline two\"}"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if usableArgs(t, json.RawMessage(c.in)) {
				t.Fatalf("the fixture is already valid JSON, so it tests nothing: %s", c.in)
			}
			got := normalizeArgs(json.RawMessage(c.in))
			if !usableArgs(t, got) {
				t.Errorf("a directly written object was handed on unparseable: %s", got)
			}
		})
	}
	// The wrapped form of the first one, which already worked, must keep working — and the two
	// must agree, since they are the same arguments written two ways.
	direct := normalizeArgs(json.RawMessage(`{"command":"echo "hi""}`))
	wrapped := normalizeArgs(json.RawMessage(`"{\"command\":\"echo \"hi\"\"}"`))
	if string(direct) != string(wrapped) {
		t.Errorf("the same arguments came out differently:\n direct  %s\n wrapped %s", direct, wrapped)
	}
}

// Valid arguments are not touched. The repair runs on every object now, so a payload that was
// already fine must come through as itself rather than reformatted or reordered.
func TestValidArgumentsSurviveTheRepairUnchanged(t *testing.T) {
	for _, in := range []string{
		`{"command":"ls -la"}`,
		`{"path":"/app/run.py","content":"print(1)\n"}`,
		`{"a":1,"b":[1,2,3],"c":{"d":true}}`,
	} {
		if got := string(normalizeArgs(json.RawMessage(in))); got != in {
			t.Errorf("valid arguments were rewritten:\n in  %s\n out %s", in, got)
		}
	}
}

// What is NOT an object is left alone. An array, a bare word or a quoted non-object cannot be
// turned into arguments without inventing them; they are handed on and rejected by whoever asked
// for a shape, which is the honest outcome.
func TestNonObjectsAreNotInvented(t *testing.T) {
	for _, in := range []string{`[1,2]`, `"ls -la"`, `ls`, `42`} {
		if got := string(normalizeArgs(json.RawMessage(in))); got != in {
			t.Errorf("a non-object was rewritten into %q from %q", got, in)
		}
	}
	// …and the two shapes that mean "no arguments" still become an empty object.
	for _, in := range []string{``, `   `, `null`} {
		if got := string(normalizeArgs(json.RawMessage(in))); got != "{}" {
			t.Errorf("normalizeArgs(%q) = %q, want {}", in, got)
		}
	}
}
