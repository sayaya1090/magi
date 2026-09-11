package idebridge

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

func verdictEvent(seq int64, member, decision, why string) event.Event {
	d, _ := json.Marshal(event.CouncilVerdictData{
		Round: 1, Member: member, Decision: decision, Rationale: why,
	})
	return event.Event{Seq: seq, Type: event.TypeCouncilVerdict, Data: d}
}

func convenedEvent(seq int64) event.Event {
	d, _ := json.Marshal(event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar"}, Rule: "majority",
	})
	return event.Event{Seq: seq, Type: event.TypeCouncilConvened, Data: d}
}

func councilRows(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if r.Who == WhoCouncil && !r.Opened {
			out = append(out, r)
		}
	}
	return out
}

// A council draws once, even though every verdict arrives twice.
//
// The core shows a round as it lands — each verdict goes on the bus the moment it arrives, so
// nobody watches "3 of 3 answered" for ninety seconds — and then writes the same verdicts as facts.
// Transient events carry no seq. **Measured 2026-09-11 through this shaper: a council of three drew
// as six rows**, every member twice. The two editor clients had it too.
//
// This one matters for a third surface: the web console reads these rows, so the same defect was on
// a screen neither editor test could see.
func TestACouncilDrawsOnceThroughTheBridge(t *testing.T) {
	live := councilRows(Rows([]event.Event{
		convenedEvent(10),
		verdictEvent(0, "Melchior", "done", "r"), verdictEvent(0, "Balthasar", "continue", "r"),
		verdictEvent(11, "Melchior", "done", "r"), verdictEvent(12, "Balthasar", "continue", "r"),
	}))
	if len(live) != 2 {
		t.Fatalf("2인 카운슬이 %d 행으로 그려졌다", len(live))
	}
	if live[0].Seq != 11 || live[1].Seq != 12 {
		t.Errorf("프리뷰가 제가 된 사실보다 오래 살았다: %d, %d", live[0].Seq, live[1].Seq)
	}

	// The fact wins when it disagrees: a rebuttal round moves votes, and a preview left beside the
	// fact shows the council disagreeing with itself.
	moved := councilRows(Rows([]event.Event{
		convenedEvent(10),
		verdictEvent(0, "Melchior", "continue", "r"),
		verdictEvent(11, "Melchior", "done", "peers are right"),
	}))
	if len(moved) != 1 {
		t.Fatalf("행이 %d 개다", len(moved))
	}
	if moved[0].Decision != "done" || moved[0].Text != "peers are right" {
		t.Errorf("프리뷰가 사실을 덮었다: %s / %q", moved[0].Decision, moved[0].Text)
	}

	// ⚠ Two convenes in one conversation both open at round 1. Keyed on the round alone they
	// collapse into each other — one council swallowing the other's verdicts.
	twice := councilRows(Rows([]event.Event{
		convenedEvent(10), verdictEvent(11, "Melchior", "done", "r"),
		convenedEvent(20), verdictEvent(21, "Melchior", "continue", "r"),
	}))
	if len(twice) != 2 {
		t.Fatalf("둘째 소집이 첫째 위에 내려앉았다 — 1회차는 열쇠가 아니다 (%d 행)", len(twice))
	}
	if twice[0].Decision != "done" || twice[1].Decision != "continue" {
		t.Errorf("소집 둘이 섞였다: %s, %s", twice[0].Decision, twice[1].Decision)
	}

	// And a preview that arrives after its fact cannot un-decide it.
	late := councilRows(Rows([]event.Event{
		convenedEvent(10), verdictEvent(11, "Melchior", "done", "r"),
		verdictEvent(0, "Melchior", "continue", "r"),
	}))
	if len(late) != 1 {
		t.Fatalf("늦은 프리뷰가 행을 하나 더 만들었다 (%d 행)", len(late))
	}
	if late[0].Decision != "done" {
		t.Errorf("늦은 프리뷰가 사실을 덮었다: %s", late[0].Decision)
	}
}
