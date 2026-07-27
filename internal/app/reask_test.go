package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// diagnostics returns every diagnostic fact persisted in a session, in order.
func diagnostics(t *testing.T, a *App, sid session.SessionID) []event.DiagnosticData {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []event.DiagnosticData
	for _, e := range evs {
		if e.Type != event.TypeDiagnostic {
			continue
		}
		var d event.DiagnosticData
		if json.Unmarshal(e.Data, &d) == nil {
			out = append(out, d)
		}
	}
	return out
}

// The re-ask is BOUNDED at two calls. It is the property every one of these passes depends on to stay
// cheap on the failure path, and the one a loop would destroy quietly — a third call costs a model
// round-trip and looks, in a log, exactly like the second.
func TestReaskAsksAtMostOnceMore(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	calls := 0
	_, ok := reask[string]{
		pass:   "probe-pass",
		actor:  plannerActor,
		ask:    func(string) (string, string, bool) { calls++; return "", "still bad", false },
		defect: func(v string, _ bool, _ string) string { return "never usable" },
		reminder: func(string, bool) string {
			return "fix it"
		},
		fallback: "giving up",
	}.run(context.Background(), a, sid, "", "bad", false)

	if ok {
		t.Error("a reply the defect never accepts must not be reported as usable")
	}
	if calls != 1 {
		t.Errorf("the re-ask made %d further call(s), want exactly 1 — the first ask is the caller's", calls)
	}
}

// Both unusable replies are PERSISTED, and this is the drift the shared exchange exists to end: the
// planner recorded them and the other four did not, though the reason it gave is about the log, not
// about plans. The progress line is bus-only and whitespace-collapsed, so the raw newline inside a
// string value — the most common way a weak model's JSON fails — survives only here.
func TestReaskPersistsBothUnusableRepliesAtFullFidelity(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	first := "{\"a\":\"one\ntwo\"}"  // a raw control char inside a string value
	second := "{\"b\":\"three\nfour" // truncated before it closed
	_, _ = reask[string]{
		pass:     "probe-pass",
		label:    "round-2",
		actor:    plannerActor,
		ask:      func(string) (string, string, bool) { return "", second, false },
		defect:   func(string, bool, string) string { return "unusable" },
		reminder: func(string, bool) string { return "fix it" },
		fallback: "giving up",
	}.run(context.Background(), a, sid, "", first, false)

	ds := diagnostics(t, a, sid)
	if len(ds) != 2 {
		t.Fatalf("both unusable replies must be recorded, got %d", len(ds))
	}
	// The label is what tells one occasion of a repeating pass from the next; without it a re-plan
	// loop's rounds are indistinguishable in the record.
	if ds[0].Source != "probe-pass:round-2" || ds[1].Source != "probe-pass:round-2-retry" {
		t.Errorf("sources must name the pass, the occasion, and which ask: %q / %q", ds[0].Source, ds[1].Source)
	}
	if ds[0].Kind != "control-char-in-string" || ds[1].Kind != "unbalanced-or-truncated" {
		t.Errorf("each reply must be classified on its own defect: %q / %q", ds[0].Kind, ds[1].Kind)
	}
	if !strings.Contains(ds[0].Detail, "one\ntwo") {
		t.Errorf("the detail must keep the reply verbatim, real newline and all, got %q", ds[0].Detail)
	}
}

// A call that returned NOTHING is a transport failure, not a reply that could not be read. Filing it
// as a parse failure would seed the record a later reader groups by cause with entries that have no
// cause in them, and every one of those is a wrong lead.
func TestReaskDoesNotFileAnEmptyReplyAsAParseFailure(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	_, _ = reask[string]{
		pass:     "probe-pass",
		actor:    plannerActor,
		ask:      func(string) (string, string, bool) { return "", "", false }, // backend error
		defect:   func(string, bool, string) string { return "unusable" },
		reminder: func(string, bool) string { return "fix it" },
		fallback: "giving up",
	}.run(context.Background(), a, sid, "", "", false)

	if ds := diagnostics(t, a, sid); len(ds) != 0 {
		t.Errorf("a reply that never arrived must not be recorded as a parse failure, got %+v", ds)
	}
}

// The reminder is chosen from WHICH failure happened, and conflating the two has already made the log
// lie once: an `[]` reply reported as "unusable (2 chars)" when it parsed perfectly and said something
// specific. A reminder that names the wrong defect asks the model to fix something it did not do.
func TestReaskRemindersSeeWhichFailureItWas(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	for _, c := range []struct {
		name, want string
		parsed     bool
	}{
		{"unparsed", "json-only", false},
		{"parsed but empty", "attach-to-steps", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			var got string
			_, _ = reask[string]{
				pass:   "probe-pass",
				actor:  plannerActor,
				ask:    func(system string) (string, string, bool) { got = system; return "", "bad", false },
				defect: func(string, bool, string) string { return "unusable" },
				reminder: func(_ string, parsed bool) string {
					if parsed {
						return "attach-to-steps"
					}
					return "json-only"
				},
				fallback: "giving up",
			}.run(context.Background(), a, sid, "", "first", c.parsed)
			if got != c.want {
				t.Errorf("re-ask was sent %q, want %q — the reminder did not see which failure it was", got, c.want)
			}
		})
	}
}

// keep exists for the one caller whose FIRST reply is partly usable: a plan salvaged from a damaged
// reply. A second answer that is not strictly better must not displace it, and the pass still reports
// the first as usable — nothing was lost, the re-ask simply did not help.
func TestReaskKeepsTheFirstWhenTheReAskIsNotBetter(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	got, ok := reask[int]{
		pass:  "probe-pass",
		actor: plannerActor,
		ask:   func(string) (int, string, bool) { return 1, "{\"steps\":1}", true },
		defect: func(v int, _ bool, _ string) string {
			if v >= 2 {
				return ""
			}
			return "only 1 step"
		},
		reminder: func(string, bool) string { return "whole plan please" },
		keep:     func(retry, first int) bool { return retry > first },
		fallback: "giving up",
	}.run(context.Background(), a, sid, 2, "{\"steps\":2}", true)

	if !ok || got != 2 {
		t.Errorf("the salvaged first reply must stand, got %d (usable=%t)", got, ok)
	}
}

// parseFailureKind names the defect a record is grouped by, so a wrong token sends every later
// investigation after the wrong thing. The scan looks for objects AND arrays because two of these
// passes ask for a bare array — which means each pass is offered spans of the other kind, and a
// classifier that read those as defects would blame the reply for the scan's breadth.
func TestParseFailureKindNamesTheDefectAndNotTheScan(t *testing.T) {
	type wrapped struct {
		Steps []struct {
			Title string `json:"title"`
		} `json:"steps"`
	}
	objectProbe := func(b []byte) error { var w wrapped; return json.Unmarshal(b, &w) }
	arrayProbe := func(b []byte) error { var a []struct{ Step string }; return json.Unmarshal(b, &a) }

	cases := []struct {
		name, text, want string
		probe            func([]byte) error
	}{
		{"prose", "I will write it now.", "no-json", objectProbe},
		{"truncated object", `{"steps":[{"title":"a"`, "unbalanced-or-truncated", objectProbe},
		{"control char", "{\"steps\":[],\"why\":\"one\ntwo\"}", "control-char-in-string", objectProbe},
		// The nested empty array is not a defect: an object-reading pass is simply not asked about it.
		{"object with no content", `{"steps":[]}`, "parsed-but-empty", objectProbe},
		// The mirror case — an array-reading pass is offered each element object.
		{"array with no content", `[]`, "parsed-but-empty", arrayProbe},
		// A mismatch INSIDE a field is a real schema defect and must survive the skip above.
		{"wrong type in a field", `{"steps":"all of them"}`, "unmarshal-error", objectProbe},
	}
	for _, c := range cases {
		if got := parseFailureKind(c.text, c.probe); got != c.want {
			t.Errorf("%s: parseFailureKind=%q, want %q", c.name, got, c.want)
		}
	}
}
