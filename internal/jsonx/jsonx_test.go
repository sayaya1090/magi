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
		C Number `json:"c"`
	}
	// The shapes a model actually emits where the schema says string / list / float.
	raw := `{"a":["one","two"],"c":"0.9"}`
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("tolerant types must not fail: %v", err)
	}
	if string(v.A) != "one; two" {
		t.Errorf("a list must join: %q", v.A)
	}
	if float64(v.C) != 0.9 {
		t.Errorf("a quoted number must parse: %v", v.C)
	}
	// The declared shapes still work unchanged.
	raw2 := `{"a":"plain","c":0.5}`
	if err := json.Unmarshal([]byte(raw2), &v); err != nil {
		t.Fatal(err)
	}
	if string(v.A) != "plain" || float64(v.C) != 0.5 {
		t.Errorf("declared shapes must be unchanged: %q %v", v.A, v.C)
	}
	// Unusable shapes degrade to empty rather than failing the document.
	raw3 := `{"a":{"k":1},"c":{"k":1}}`
	if err := json.Unmarshal([]byte(raw3), &v); err != nil {
		t.Fatalf("an unusable shape must degrade, not fail: %v", err)
	}
	if v.A != "" || v.C != 0 {
		t.Errorf("want zero values, got %q %v", v.A, v.C)
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

// The four shapes below all cost a whole council reply in one run of one task, and none of them was
// a bad FIELD — each is a stray token that destroys the SPAN, so the extractor returned no candidate
// and the tolerant parse behind it never ran on anything. Each is also already-invalid JSON, so
// repairing it cannot corrupt a document that was going to parse.
func TestRecoversDamagedSpans(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantDec  string
		wantCrit int
	}{
		{
			// Cut off by an output budget: the closing brace never arrives. Costs the vote, which
			// was the reply's FIRST field.
			name:    "truncated mid-document",
			in:      `{"decision":"continue","confidence":0.85,"criteria":["the compiler bootstraps","the suite passes"]`,
			wantDec: "continue", wantCrit: 2,
		},
		{
			// A stray quote after the closed array swallows the rest, so the final } is read as
			// string content and the span never closes.
			name:    "stray quote after a closed array",
			in:      `{"decision":"done","criteria":["make test completes with no unexpected failures."]" }`,
			wantDec: "done", wantCrit: 1,
		},
		{
			name:    "the element's own brace written twice",
			in:      `{"decision":"done","criteria":["a","b"]}}`,
			wantDec: "done", wantCrit: 2,
		},
		{
			name:    "a trailing fragment that is not a pair",
			in:      `{"decision":"done","criteria":["a"], ""}`,
			wantDec: "done", wantCrit: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cands := Objects(tc.in)
			if len(cands) == 0 {
				t.Fatalf("no candidate recovered — the reply is lost: %s", Diagnose(tc.in))
			}
			var got struct {
				Decision string   `json:"decision"`
				Criteria []string `json:"criteria"`
			}
			ok := false
			for _, c := range cands {
				if Unmarshal(c, &got) && got.Decision != "" {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("candidates parsed to nothing: %q", cands)
			}
			if got.Decision != tc.wantDec || len(got.Criteria) != tc.wantCrit {
				t.Errorf("got decision=%q criteria=%d, want %q / %d", got.Decision, len(got.Criteria), tc.wantDec, tc.wantCrit)
			}
		})
	}
}

// What arrived complete is kept; the fragment the model was in the middle of writing is dropped
// rather than closed over, so no field ever carries a value that was only half transmitted.
func TestCloseTruncatedKeepsWhatArrivedComplete(t *testing.T) {
	in := `{"steps":[{"title":"build","strategy":"solo"},{"title":"verify","strat`
	got, ok := CloseTruncated(in)
	if !ok {
		t.Fatal("a truncated document must be recognised")
	}
	var plan struct {
		Steps []struct {
			Title    string `json:"title"`
			Strategy string `json:"strategy"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(got), &plan); err != nil {
		t.Fatalf("recovered document must parse: %v (%s)", err, got)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Title != "build" || plan.Steps[0].Strategy != "solo" {
		t.Fatalf("the completed step must survive intact, got %+v (%s)", plan.Steps, got)
	}
	// The second step's title arrived; the key it was cut in the middle of did not, and no half of
	// it is carried forward.
	if plan.Steps[1].Title != "verify" || plan.Steps[1].Strategy != "" {
		t.Fatalf("want the completed fields only, got %+v (%s)", plan.Steps[1], got)
	}
}

// Every one of these repairs must be identity on documents that were going to parse — they run on
// replies that already failed, but they must never be the reason one starts failing.
func TestDamageRepairsAreIdentityOnLegalJSON(t *testing.T) {
	legal := []string{
		`{"a":"b"}`,
		`{"a":["x","y"],"b":{"c":1}}`,
		`[{"step":1,"command":"make && ./run"},{"step":2,"command":"echo }] done"}]`,
		`{"note":"a closing brace } and bracket ] inside a string"}`,
		`{"url":"http://x/y","n":-1.5,"ok":true,"z":null}`,
		`{"nested":{"deep":{"deeper":[1,2,3]}}}`,
		`"top-level string"`,
		`[]`,
		`{}`,
	}
	for _, s := range legal {
		for name, f := range map[string]func(string) string{
			"DropStrayQuoteAfterContainer": DropStrayQuoteAfterContainer,
			"DropUnmatchedClosers":         DropUnmatchedClosers,
			"DropDanglingPair":             DropDanglingPair,
		} {
			if got := f(s); got != s {
				t.Errorf("%s changed a legal document:\n in: %s\nout: %s", name, s, got)
			}
		}
		if got, ok := CloseTruncated(s); ok {
			t.Errorf("CloseTruncated reported a whole document as truncated: %s → %s", s, got)
		}
		// And the recovered-candidate list still leads with the document's own span.
		if strings.HasPrefix(s, "{") {
			if c := Objects(s); len(c) == 0 || c[0] != s {
				t.Errorf("Objects must offer the original span first: %s → %q", s, c)
			}
		}
	}
}

// The defect that motivated SalvagePrefix: a model closes an array with the NEXT key instead of a
// `]`, so the document is invalid from that byte on while everything ahead of it — including the
// first field, which is the one the caller usually needs — arrived intact.
func TestSalvagePrefixKeepsTheFieldsBeforeAMisNestedArray(t *testing.T) {
	const bad = `{"decision":"continue","confidence":0.9,"rationale":"the plan skips verification",` +
		`"severity":"critical","criteria":["the fix is confirmed by running it","checks":[]]}`
	if json.Unmarshal([]byte(bad), new(map[string]any)) == nil {
		t.Fatal("the fixture is supposed to be invalid JSON")
	}
	cut, ok := SalvagePrefix(bad)
	if !ok {
		t.Fatalf("nothing salvaged from a reply whose first four fields were whole: %s", bad)
	}
	var got struct {
		Decision string   `json:"decision"`
		Severity string   `json:"severity"`
		Criteria []string `json:"criteria"`
		Checks   []any    `json:"checks"`
	}
	if err := json.Unmarshal([]byte(cut), &got); err != nil {
		t.Fatalf("the salvaged prefix does not parse: %v\n%s", err, cut)
	}
	if got.Decision != "continue" || got.Severity != "critical" {
		t.Errorf("the fields before the defect were lost: %+v", got)
	}
	// The criterion that had fully arrived is kept; the key that broke the array is not smuggled in.
	if len(got.Criteria) != 1 || got.Criteria[0] != "the fix is confirmed by running it" {
		t.Errorf("criteria = %q, want the one complete element", got.Criteria)
	}
	if got.Checks != nil {
		t.Errorf("the salvage must not invent content after the defect: checks = %v", got.Checks)
	}
}

// A raw newline inside a string is REPAIRABLE, so it must not be mistaken for the cut point — that
// would throw away every field after the first multi-line prose value, which is most of them.
func TestSalvagePrefixRepairsBeforeChoosingTheCutPoint(t *testing.T) {
	bad := "{\"decision\":\"done\",\"rationale\":\"line one\nline two\",\"criteria\":[\"kept\",\"checks\":[]]}"
	cut, ok := SalvagePrefix(bad)
	if !ok {
		t.Fatalf("nothing salvaged: %s", bad)
	}
	var got struct {
		Decision  string   `json:"decision"`
		Rationale string   `json:"rationale"`
		Criteria  []string `json:"criteria"`
	}
	if err := json.Unmarshal([]byte(cut), &got); err != nil {
		t.Fatalf("the salvaged prefix does not parse: %v\n%s", err, cut)
	}
	if got.Decision != "done" || !strings.Contains(got.Rationale, "line two") {
		t.Errorf("the raw newline was treated as the cut point instead of being repaired: %+v", got)
	}
	if len(got.Criteria) != 1 {
		t.Errorf("criteria = %q, want the element that arrived before the defect", got.Criteria)
	}
}

func TestSalvagePrefixDeclinesWhenThereIsNothingToSalvage(t *testing.T) {
	for _, s := range []string{
		`{"decision":"done","criteria":["a","b"]}`, // whole: a prefix cannot improve on it
		`{"criteria":["a","b"]}`,                   // whole but missing a field — a SCHEMA gap, not syntax
		`not json at all`,
		``,
		`{`, // only the opener arrived: no field survived
	} {
		if cut, ok := SalvagePrefix(s); ok {
			t.Errorf("SalvagePrefix(%q) salvaged %q, want nothing", s, cut)
		}
	}
}

// SalvagePrefix is lossy, so it must stay OUT of the shared repair path: a caller that did not ask
// for it must still see a malformed document fail, or a truncated plan silently becomes a short one.
func TestSalvagePrefixIsNotWiredIntoTheSharedRepairs(t *testing.T) {
	const bad = `{"decision":"continue","criteria":["a","checks":[]]}`
	var v map[string]any
	if Unmarshal(bad, &v) {
		t.Errorf("Unmarshal accepted a malformed document: %v", v)
	}
	for _, c := range RepairCandidates(bad) {
		if json.Unmarshal([]byte(c), new(map[string]any)) == nil {
			t.Errorf("a repair candidate parsed the malformed document: %s", c)
		}
	}
}
