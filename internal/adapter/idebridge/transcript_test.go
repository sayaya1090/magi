package idebridge

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

// streamOf answers like a companion: the handshake, then one frame per event, then the frame that
// says the replay is over.
//
// caps is what this fake advertises, so a test can be an OLDER daemon — the case that made this
// door hang before the marker existed.
func streamOf(t *testing.T, events []event.Event, caps ...string) func(string) string {
	t.Helper()
	if caps == nil {
		caps = []string{"handshake", "transcript", "history"}
	}
	return func(raw string) string {
		if strings.Contains(raw, `"method":"about"`) {
			hi, err := json.Marshal(map[string]any{"ok": true, "version": "test", "proto": 1, "caps": caps})
			if err != nil {
				t.Fatal(err)
			}
			return string(hi)
		}
		if !strings.Contains(raw, `"method":"transcript"`) {
			return `{"ok":false,"error":"this fake only serves transcript"}`
		}
		var lines []string
		for i := range events {
			frame, err := json.Marshal(map[string]any{"ok": true, "event": events[i]})
			if err != nil {
				t.Fatal(err)
			}
			lines = append(lines, string(frame))
		}
		if slices.Contains(caps, "history") {
			lines = append(lines, `{"ok":true,"live":true}`)
		}
		return strings.Join(lines, "\n")
	}
}

// **An older companion gets a sentence, not a wait.**
//
// ⚠ This is the measurement that shaped the door. `transcript` is a live tail whose own note says
// the peer hanging up is the only thing that ends a quiet stream, so a reader taking the
// conversation once had no way to know when to stop: the first draft of this door waited 202s on a
// finished session and was killed. The marker fixed it for daemons from this build; a daemon already
// running an older one will never send it. So the door asks whether this companion can END a replay
// rather than whether it can stream one, and this test is an old daemon answering.
func TestTheRowsDoorRefusesACompanionThatCannotEndTheReplay(t *testing.T) {
	var events []event.Event
	read(t, "fold_events.json", &events)
	d := listen(t, streamOf(t, events, "handshake", "transcript"))

	done := make(chan []map[string]any, 1)
	go func() { done <- run(t, d.path, `{"id":3,"method":"rows","session":"s_1"}`) }()
	var got []map[string]any
	select {
	case got = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("구형 데몬에게 물었더니 답이 안 온다 — 이 문이 옛 결함 그대로 매달렸다")
	}
	if got[0]["ok"] != false {
		t.Fatalf("끝을 못 대는 데몬에게서 완전한 대화를 받았다고 답한다: %v", got[0])
	}
	why, _ := got[0]["error"].(string)
	if !strings.Contains(why, "history") || !strings.Contains(why, "older") {
		t.Errorf("거절이 무엇이 없어서인지·무엇을 하면 되는지 안 말한다: %q", why)
	}
}

// The door exists, is advertised, and gives back the SAME rows the fold does.
//
// ⚠ The comparison is against `Rows` rather than a second golden on purpose. A hand-written
// expectation here would be a third copy of the rule — the exact thing this package exists to end —
// and it would pass while the door quietly shaped something else.
func TestTheRowsDoorAnswersWithTheSharedFold(t *testing.T) {
	var events []event.Event
	read(t, "fold_events.json", &events)
	d := listen(t, streamOf(t, events))

	got := run(t, d.path, `{"id":7,"method":"rows","session":"s_1"}`)
	if len(got) != 1 {
		t.Fatalf("replies %d, want 1", len(got))
	}
	if got[0]["ok"] != true {
		t.Fatalf("the door refused: %v", got[0])
	}
	if n, _ := got[0]["events"].(float64); int(n) != len(events) {
		t.Errorf("이 문이 사건 %v 개를 봤다는데 %d 개를 보냈다 — 스트림을 다 안 읽었다", got[0]["events"], len(events))
	}

	want, err := json.Marshal(Rows(events))
	if err != nil {
		t.Fatal(err)
	}
	mine, err := json.Marshal(got[0]["rows"])
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, mine, want) {
		t.Errorf("문이 내놓은 행이 공용 접기와 다르다 — 그러면 이 문은 규칙의 네 번째 사본이다\n  문:   %s\n  접기: %s", mine, want)
	}

	// Advertised from the one list, so a client that reads `about` finds it.
	for _, m := range Methods() {
		if m == "rows" {
			return
		}
		_ = m
	}
	t.Error("문은 답하는데 about 이 이름을 안 댄다 — 클라이언트는 없는 문으로 읽는다")
}

// **The fold is defined over the WHOLE log, and that is why the door takes no cursor.**
//
// This measures the claim in the door's comment rather than restating it: the fold reaches backwards
// (a reply clears the waiting mark on the prompt above it), so folding a tail is not the tail of
// folding everything. A future `since` on this door would return rows that are wrong in a way no
// client could detect — each row looks fine; the marks are missing.
func TestTheFoldIsWholeLogAndTheDoorSaysSo(t *testing.T) {
	var events []event.Event
	read(t, "fold_events.json", &events)
	if len(events) < 6 {
		t.Fatalf("픽스처가 %d 개뿐이다 — 꼬리를 자를 수가 없다", len(events))
	}
	whole := Rows(events)
	tail := Rows(events[len(events)/2:])

	same := len(whole) == len(tail)
	if same {
		for i := range whole {
			if show(t, whole[i]) != show(t, tail[i]) {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("꼬리만 접은 것이 전체를 접은 것과 같다 — 그러면 이 문이 커서를 안 받는 근거가 사라진 것이고, " +
			"주석과 코드 중 하나가 틀렸다")
	}

	// And the door refuses to pretend: asked without a session it says which field it wants rather
	// than shaping an empty conversation, which would read as "this session has nothing in it".
	d := listen(t, streamOf(t, events))
	got := run(t, d.path, `{"id":1,"method":"rows"}`)
	if got[0]["ok"] != false {
		t.Errorf("세션을 안 대도 답한다 — 빈 대화와 「어느 대화인지 안 말했다」가 같은 그림이 된다: %v", got[0])
	}
	if why, _ := got[0]["error"].(string); !strings.Contains(why, "session") {
		t.Errorf("거절이 무엇이 빠졌는지 안 말한다: %q", why)
	}
}

// sameJSON compares two encodings by value: key order is not a fact about rows.
func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
