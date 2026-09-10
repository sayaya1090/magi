package idebridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The golden is not this package's opinion. It was produced by running the SAME events through the
// TypeScript original and through this port and comparing the two, row by row, on 2026-09-10 — 21
// events into 15 rows, every field equal.
//
// How to re-do that, because a golden nobody can re-derive is just a snapshot of whatever the code
// did on the day:
//
//	cp -r clients/vscode/src/core /tmp/core          # Node needs explicit .ts specifiers
//	perl -pi -e "s{from '(\./[\w./-]+)'}{from '\$1.ts'}g" /tmp/core/*.ts
//	perl -pi -e "s/^import \{/import type {/ if /^import \{ *(Event|Response)[ ,}]/" /tmp/core/*.ts
//	# a three-line driver that imports { rows } and prints JSON, then:
//	node --experimental-strip-types drive.ts testdata/fold_events.json
//
// Compare with keys sorted and empty values dropped: the TypeScript writes `pending:false` because
// a JS object has no omitempty, and this side omits it. Same fact, two spellings — a byte compare
// would fail on that and on key order while the rows agree completely.
func TestTheFoldAgreesWithTheGolden(t *testing.T) {
	var events []event.Event
	read(t, "fold_events.json", &events)
	if len(events) < 10 {
		t.Fatalf("픽스처에서 이벤트를 %d 개밖에 못 읽었다 — 골든이 맞는 게 아니라 읽기가 깨진 것이다", len(events))
	}
	var want []Row
	read(t, "fold_rows.json", &want)

	got := Rows(events)
	if len(got) != len(want) {
		t.Fatalf("행 갯수가 %d, 골든은 %d\n%s", len(got), len(want), show(t, got))
	}
	for i := range want {
		if show(t, got[i]) != show(t, want[i]) {
			t.Errorf("[%d]\n  got  %s\n  want %s", i, show(t, got[i]), show(t, want[i]))
		}
	}
}

// Every kind the vocabulary names is actually reachable through the fold.
//
// A constant nothing produces is a promise to a client that never comes true — and `image` was
// exactly that in one of the two existing copies, which is how a tool answering with a picture
// drew nothing at all.
func TestTheGoldenExercisesEveryRowKind(t *testing.T) {
	var events []event.Event
	read(t, "fold_events.json", &events)
	seen := map[string]bool{}
	for _, r := range Rows(events) {
		seen[r.Who] = true
	}
	for _, w := range Vocabulary() {
		if !seen[w] {
			t.Errorf("픽스처가 %q 행을 한 번도 안 낸다 — 그 갈래는 이 골든이 안 지킨다", w)
		}
	}
}

// The branches the first golden never walks.
//
// TestTheGoldenExercisesEveryRowKind covers the row KINDS; it says nothing about the rules that
// move rows around instead of making them. Measured after writing it: `resurfacedFrom`,
// `inReplyTo`, both ends of `interjection.deferred`, `interjection.answered` and
// `prompt.abandoned` were not walked once by that fixture — five of the subtlest rules in the fold,
// all of them about a bar that must come down, and none of them guarded.
//
// Verified the same way and on the same day: the same events through the TypeScript original and
// through this port, 12 rows, every field equal.
func TestTheFoldAgreesWithTheGoldenOnTheAwkwardOnes(t *testing.T) {
	var events []event.Event
	read(t, "fold_edges.json", &events)
	var want []Row
	read(t, "fold_edge_rows.json", &want)
	got := Rows(events)
	if len(got) != len(want) {
		t.Fatalf("행 갯수가 %d, 골든은 %d\n%s", len(got), len(want), show(t, got))
	}
	for i := range want {
		if show(t, got[i]) != show(t, want[i]) {
			t.Errorf("[%d]\n  got  %s\n  want %s", i, show(t, got[i]), show(t, want[i]))
		}
	}
}

// A question that was asked twice is one question, and it ends up next to its answer.
//
// Named apart from the golden because the golden would still pass if this came out in the original
// order with a second copy beside it — the ORDER is the fact here. A resurfaced interjection is a
// fresh prompt with a new id carrying `resurfacedFrom`, and reading neither leaves the question
// stranded far up the transcript wearing a queued mark that never clears, with a duplicate at the
// bottom and nothing saying they are one thing.
func TestAResurfacedQuestionMovesRatherThanRepeating(t *testing.T) {
	var events []event.Event
	read(t, "fold_edges.json", &events)
	got := Rows(events)

	var users []Row
	for _, r := range got {
		if r.Who == WhoUser {
			users = append(users, r)
		}
	}
	for _, r := range users {
		if r.MsgID == "q1" {
			t.Errorf("되살아난 질문의 원본이 그대로 남아 있다: %s", show(t, r))
		}
		if r.Queued {
			t.Errorf("큐에서 나온 줄이 아직 큐 표시를 달고 있다: %s", show(t, r))
		}
	}
	if len(users) != 3 {
		t.Fatalf("사람 줄이 %d 개다 — 되살아난 질문이 두 번 실렸을 수 있다: %s", len(users), show(t, users))
	}
}

// A log with nothing in it is no rows, not one empty row.
func TestAnEmptyLogFoldsToNothing(t *testing.T) {
	if got := Rows(nil); len(got) != 0 {
		t.Errorf("빈 로그가 %d 행을 냈다: %s", len(got), show(t, got))
	}
}

// An event this build has never heard of is skipped, not drawn.
//
// `context.usage` and `todos.changed` are facts for other screens; putting them in the transcript
// would make the conversation a log file. A payload that will not parse is the same: no row.
func TestFactsForOtherScreensAreNotRows(t *testing.T) {
	events := []event.Event{
		{Seq: 1, Type: event.TypeContextUsage, Data: json.RawMessage(`{"tokens":10}`)},
		{Seq: 2, Type: event.TypeTodosChanged, Data: json.RawMessage(`{"todos":[]}`)},
		{Seq: 3, Type: "something.this.build.never.heard.of", Data: json.RawMessage(`{"x":1}`)},
		{Seq: 4, Type: event.TypePartAppended, Data: json.RawMessage(`not json at all`)},
	}
	if got := Rows(events); len(got) != 0 {
		t.Errorf("다른 화면의 사실이 전사에 실렸다: %s", show(t, got))
	}
}

// Absent and false are different facts on a tool row, and the fold must keep them apart.
//
// A call with no result yet has no Ok at all; one that failed has Ok=false. A screen that cannot
// tell them apart draws a running command as a broken one.
func TestAToolCallWithNoResultYetHasNoVerdict(t *testing.T) {
	events := []event.Event{{
		Seq: 1, Type: event.TypePartAppended,
		Data: json.RawMessage(`{"role":"assistant","part":{"kind":"tool-call","toolCall":{"callId":"c1","name":"bash","args":{"command":"sleep 60"}}}}`),
	}}
	got := Rows(events)
	if len(got) != 1 {
		t.Fatalf("행이 %d 개다: %s", len(got), show(t, got))
	}
	if got[0].Ok != nil {
		t.Errorf("아직 안 끝난 도구 호출에 판정이 붙었다: %v", *got[0].Ok)
	}
	if got[0].Args != "sleep 60" {
		t.Errorf("무엇을 시켰는지가 %q 로 실렸다", got[0].Args)
	}
}

// SizeNote states the case that reads backwards rather than clamping it.
//
// A summary that came out bigger than what it replaced is the one outcome a person should see, and
// rendering it as "−0, −0%" would hide it.
func TestSizeNoteSaysWhenTheFoldMadeThingsBigger(t *testing.T) {
	for _, c := range []struct {
		before, after float64
		want          string
	}{
		{1000, 250, "−750, −75%"},
		{1000, 0, "−1000, −100%"},
		{0, 0, "−0, −0%"},
		{100, 150, "+50, the summary is LARGER than what it replaced"},
	} {
		if got := SizeNote(c.before, c.after); got != c.want {
			t.Errorf("SizeNote(%v, %v) = %q, 원하는 것은 %q", c.before, c.after, got, c.want)
		}
	}
}

// AskedFor invents nothing: a call given no arguments summarises to nothing and the row is the
// bare name again, which is the truth about it.
func TestAskedForSaysOnlyWhatWasAsked(t *testing.T) {
	for _, c := range []struct {
		name string
		args any
		want string
	}{
		{"nothing at all", nil, ""},
		{"an empty object", map[string]any{}, ""},
		{"the path it names", map[string]any{"path": "a.go", "mode": "w"}, "a.go"},
		{"a JSON string is parsed, not printed", `{"command":"go test"}`, "go test"},
		{"a string that is not JSON is itself", "just words", "just words"},
		{"anything else is its JSON", map[string]any{"depth": 2.0}, `{"depth":2}`},
	} {
		if got := AskedFor(c.args); got != c.want {
			t.Errorf("%s: AskedFor(%v) = %q, 원하는 것은 %q", c.name, c.args, got, c.want)
		}
	}
}

func read(t *testing.T, name string, into any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func show(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
