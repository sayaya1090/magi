package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The schema the model is shown must declare intent and mark it required — that is what gets it
// filled in — and it must leave alone the schemas it does not understand.
func TestSchemaWithIntentDeclaresIt(t *testing.T) {
	for _, tc := range []struct {
		what, in string
		want     bool // intent added?
	}{
		{"an ordinary tool", `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`, true},
		{"no required list yet", `{"type":"object","properties":{"q":{"type":"string"}}}`, true},
		{"no properties (takes nothing)", `{"type":"object"}`, false},
		{"not an object schema", `{"type":"string"}`, false},
		{"malformed", `{ not json`, false},
		{"already has its own intent", `{"type":"object","properties":{"intent":{"type":"integer"}}}`, false},
	} {
		t.Run(tc.what, func(t *testing.T) {
			out := schemaWithIntent(json.RawMessage(tc.in))
			var sch struct {
				Properties map[string]json.RawMessage `json:"properties"`
				Required   []string                   `json:"required"`
			}
			_ = json.Unmarshal(out, &sch)
			_, declared := sch.Properties[toolIntentKey]
			required := false
			for _, r := range sch.Required {
				if r == toolIntentKey {
					required = true
				}
			}
			if !tc.want {
				if string(out) != tc.in {
					t.Fatalf("this schema should have been returned untouched:\ngot  %s\nwant %s", out, tc.in)
				}
				return
			}
			if !declared || !required {
				t.Fatalf("intent declared=%v required=%v in %s", declared, required, out)
			}
			// The tool's own arguments survive.
			if len(sch.Properties) < 2 {
				t.Errorf("the tool's own properties were lost: %s", out)
			}
		})
	}
	// An unrecognised `required` shape must not be rewritten into one the tool's parser rejects.
	odd := `{"type":"object","properties":{"a":{"type":"string"}},"required":"a"}`
	if got := string(schemaWithIntent(json.RawMessage(odd))); got != odd {
		t.Errorf("a schema with an unparseable required list should be left alone, got %s", got)
	}
}

// The guard compares calls, and the intent is prose the model rewrites every time. If it reached
// the fingerprint, two identical calls would look like two different ones and the guard would
// notice LESS than it did before the field existed.
func TestTheIntentIsNotPartOfWhatMakesACallTheSame(t *testing.T) {
	a := json.RawMessage(`{"command":"go build ./...","intent":"build after the edit"}`)
	b := json.RawMessage(`{"command":"go build ./...","intent":"confirm the fix compiles"}`)
	if guardArgs("bash", a) != guardArgs("bash", b) {
		t.Errorf("the same command under two labels fingerprints differently:\n%s\n%s",
			guardArgs("bash", a), guardArgs("bash", b))
	}
	// And a genuinely different command still does.
	c := json.RawMessage(`{"command":"go test ./...","intent":"build after the edit"}`)
	if guardArgs("bash", a) == guardArgs("bash", c) {
		t.Error("two different commands must not collapse onto one fingerprint")
	}
	// Same for the per-tool paths that strip a volatile parameter of their own.
	r1 := json.RawMessage(`{"path":"/app/main.go","limit":60,"intent":"read the top"}`)
	r2 := json.RawMessage(`{"path":"/app/main.go","limit":65,"intent":"look again"}`)
	if guardArgs("read", r1) != guardArgs("read", r2) {
		t.Errorf("read: limit+intent should both drop out:\n%s\n%s", guardArgs("read", r1), guardArgs("read", r2))
	}
	// The mutation signature is the other comparison: an identical write under new wording must
	// not read as a real change, which would reset the no-progress counters.
	w1 := json.RawMessage(`{"path":"/app/x","content":"same","intent":"first try"}`)
	w2 := json.RawMessage(`{"path":"/app/x","content":"same","intent":"second try"}`)
	if canonicalArgs(stripToolIntent(w1)) != canonicalArgs(stripToolIntent(w2)) {
		t.Error("an identical write under a new label must not look like a change")
	}
}

// The schema magi validates against has to be the schema the model was shown, or the intent magi
// asked for comes back and is reported as an argument the tool does not have.
func TestTheIntentIsNotReportedAsAnUnknownArgument(t *testing.T) {
	schema := schemaWithIntent(json.RawMessage(
		`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`))
	args := json.RawMessage(`{"path":"/app/x","intent":"read the file"}`)
	misspelled, ignored, _ := unknownToolArgs(schema, args)
	if len(misspelled) > 0 {
		t.Errorf("intent read as a misspelling: %v", misspelled)
	}
	if len(ignored) > 0 {
		t.Errorf("intent reported as ignored: %v", ignored)
	}
	// A real unknown key is still caught alongside it.
	_, ignored, _ = unknownToolArgs(schema, json.RawMessage(`{"path":"/app/x","intent":"x","banana":1}`))
	if len(ignored) != 1 || ignored[0] != "banana" {
		t.Errorf("a genuinely unknown key should still be named, got %v", ignored)
	}
	// And a missing intent must not turn some other key into a rename of it: intent is magi's
	// field, not the tool's contract, so a call is not dead for lacking one.
	misspelled, _, _ = unknownToolArgs(schema, json.RawMessage(`{"path":"/app/x","user_intention":"x"}`))
	if got, ok := misspelled["user_intention"]; ok {
		t.Errorf("a missing intent must not make %q a rename of %q", "user_intention", got)
	}
}

// Off means off: the schema is untouched, so a wave can be run either way.
func TestTheIntentCanBeTurnedOff(t *testing.T) {
	t.Setenv("MAGI_TOOL_INTENT", "off")
	in := `{"type":"object","properties":{"path":{"type":"string"}}}`
	if got := string(schemaWithIntent(json.RawMessage(in))); got != in {
		t.Errorf("with the field off the schema must be untouched, got %s", got)
	}
	// Stripping stays on: a call that arrived with an intent before the flag flipped must still
	// compare equal to one without.
	if strings.Contains(string(stripToolIntent(json.RawMessage(`{"a":1,"intent":"x"}`))), toolIntentKey) {
		t.Error("stripping must not depend on the flag")
	}
}

// The two seams above are functions, and a test that calls them proves only that they work when
// called. What matters is that the DISPATCH path uses them: prompt.go advertises the injected
// schema, so execute.go must validate against the same one. Run a real call with an intent through
// executeTool and read what came back.
func TestARealCallCarryingAnIntentIsNotRefused(t *testing.T) {
	a, sid, wd := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	if err := os.WriteFile(filepath.Join(wd, "x.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := a.sessionInfo(context.Background(), sid)
	call := &session.ToolCall{
		CallID: "c1", Name: "read",
		Args: json.RawMessage(`{"path":"x.txt","intent":"see what the file holds"}`),
	}
	a.executeTool(context.Background(), s, AgentSpec{Name: "coder"}, 0,
		event.Actor{Kind: event.ActorAgent, ID: "coder"}, call, newRunGuard())

	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolResult {
			continue
		}
		var text string
		if json.Unmarshal(d.Part.ToolResult.Content, &text) == nil {
			got = text
		}
	}
	if got == "" {
		t.Fatal("the call produced no tool result, so this asserts nothing")
	}
	if strings.Contains(got, "has no argument") || strings.Contains(got, toolIntentKey) {
		t.Errorf("the intent magi asked for came back as a complaint:\n%s", got)
	}
	if !strings.Contains(got, "hello") {
		t.Errorf("the read did not run:\n%s", got)
	}
}
