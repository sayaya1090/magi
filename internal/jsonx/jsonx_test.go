package jsonx

import (
	"encoding/json"
	"strings"
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

// An unescaped quote inside a string value: the model quotes a command or an identifier in a prose
// field and never escapes it. Observed live in three council verdicts of one round, each of which
// was recorded as an abstain — the decision AND the rationale were lost, not just the field.
func TestEscapeStrayQuotes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string // the value the repair must recover, "" to only require that it parses
	}{
		{
			name: "quoted command inside a prose value",
			in:   `{"note":""run the suite" passes without failure."}`,
			want: `"run the suite" passes without failure.`,
		},
		{
			name: "quote opening a value, closed only at the end",
			in:   `{"note":""The suite passes cleanly under the documented target."}`,
			want: `"The suite passes cleanly under the documented target.`,
		},
		{
			name: "stray quote mid-sentence",
			in:   `{"note":"the flag is named "strict" here"}`,
			want: `the flag is named "strict" here`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v struct {
				Note string `json:"note"`
			}
			if !Unmarshal(tc.in, &v) {
				t.Fatalf("must be repaired: %s", tc.in)
			}
			if v.Note != tc.want {
				t.Errorf("value not recovered:\n got %q\nwant %q", v.Note, tc.want)
			}
		})
	}

	// A stray quote must not cost the SIBLING fields either — the decision is what the tally reads.
	verdict := `{"decision":"continue","confidence":0.9,"criteria":["it builds",""make check" runs cleanly."],"checks":null}`
	var v struct {
		Decision   string   `json:"decision"`
		Confidence float64  `json:"confidence"`
		Criteria   []string `json:"criteria"`
	}
	if !Unmarshal(verdict, &v) {
		t.Fatalf("must be repaired: %s", verdict)
	}
	if v.Decision != "continue" || v.Confidence != 0.9 || len(v.Criteria) != 2 {
		t.Errorf("sibling fields lost: %+v", v)
	}
	if v.Criteria[1] != `"make check" runs cleanly.` {
		t.Errorf("criterion not recovered: %q", v.Criteria[1])
	}

	// Identity on well-formed documents: every legal closing quote passes the lookahead test,
	// including an empty string, a nested object and whitespace before the structural character.
	for _, legal := range []string{
		`{"a":"b"}`,
		`{"a":""}`,
		`["", "a", ""]`,
		`{"a": {"b": ["c"]} , "d": "e"}`,
		"{\n  \"a\"  :  \"b\"\n}",
		`{"a":"he said \"hi\""}`,
		`"top-level string"`,
	} {
		if got := EscapeStrayQuotes(legal); got != legal {
			t.Errorf("a legal document must be untouched:\n%s\n%s", legal, got)
		}
	}

	// The original is still tried first, so a clean document never reaches this repair.
	if c := RepairCandidates(`{"a":"b"}`); c[0] != `{"a":"b"}` {
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

// A parse failure is only actionable if the log says WHY. Excerpt keeps the head and the tail, so a
// defect in the middle — where the long prose fields live — is precisely what it hides; Diagnose
// names the defect class, the offset, and a window around it.
func TestDiagnose(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string // substrings that must appear
	}{
		{
			name: "prose with no JSON at all",
			in:   "I think the task is done because the build passed.",
			want: []string{"no JSON object or array"},
		},
		{
			name: "unescaped inner quote in the middle",
			in:   `{"decision":"continue","rationale":"the check ""make test" passes" is wrong","confidence":0.9}`,
			want: []string{"syntax error at offset", "⟪HERE⟫"},
		},
		{
			name: "valid JSON that simply is not the expected shape",
			in:   `{"verdict":"done","why":"it builds"}`,
			want: []string{"the mismatch is the SCHEMA", "keys: [verdict why]"},
		},
		{
			name: "JSON embedded in prose is diagnosed, not dismissed",
			in:   "Here is my verdict:\n```json\n{\"decision\":\"done\",\"confidence\":1}\n```\nThanks!",
			want: []string{"the mismatch is the SCHEMA", "decision"},
		},
		{
			name: "a trailing comma names its own offset",
			in:   `{"a":1,"b":2,}`,
			want: []string{"syntax error at offset"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Diagnose(tc.in)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("Diagnose(%q) = %q, missing %q", tc.in, got, w)
				}
			}
		})
	}
}

// The window must actually contain the defect — that is the whole point of the offset.
func TestDiagnoseWindowShowsTheDefect(t *testing.T) {
	// The bad quote sits in the MIDDLE — past where the excerpt's head ends and before its tail.
	filler := strings.Repeat("a description of the work that was carried out. ", 12)
	js := `{"decision":"continue","rationale":"` + filler + `the SENTINEL is wrong: "` + filler +
		`","confidence":0.9}`
	got := Diagnose(js)
	if !strings.Contains(got, "⟪HERE⟫") {
		t.Fatalf("no window: %s", got)
	}
	if !strings.Contains(got, "SENTINEL") {
		t.Errorf("window does not show the defect: %s", got)
	}
	// And the excerpt alone would NOT have shown it — which is why Diagnose exists.
	if strings.Contains(Excerpt(js), "SENTINEL") {
		t.Errorf("excerpt already showed the defect; the test no longer covers the blind spot")
	}
}

// Report is the one rendering every failing call site uses: content AND reason.
func TestReportCarriesBothHalves(t *testing.T) {
	got := Report(`{"a":1,}`)
	if !strings.Contains(got, `{"a":1,}`) {
		t.Errorf("Report lost the content: %s", got)
	}
	if !strings.Contains(got, "syntax error") {
		t.Errorf("Report lost the reason: %s", got)
	}
}
