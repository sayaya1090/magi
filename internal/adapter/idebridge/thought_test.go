package idebridge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A verdict's thought has to reach the row, or the two editor clients and the console draw a
// silent member as a considered shrug.
//
// This shaper is the ONLY thing between the fact and three surfaces. The field being on the wire
// proves nothing about what they can draw: the row is hand-assembled here field by field, so
// anything not named in `verdictRow` is carried to this line and dropped.
func TestTheRowCarriesWhatTheMemberWasThinking(t *testing.T) {
	const musing = "The report claims the tests pass. I cannot find the run."
	d, _ := json.Marshal(event.CouncilVerdictData{
		Round: 1, Member: "verification", Lens: "verification",
		Decision: "abstain", Silent: true, Thought: musing,
	})
	rows := Rows([]event.Event{{Seq: 7, Type: event.TypeCouncilVerdict, Data: d}})
	if len(rows) != 1 {
		t.Fatalf("행이 %d 개다 — 평결은 언제나 한 행이다", len(rows))
	}
	if rows[0].Thought != musing {
		t.Errorf("생각이 셰이퍼에서 사라졌다: %q", rows[0].Thought)
	}
	// Silent still says what it said: this row draws "no answer came back", and the thought is
	// the only thing on it explaining why.
	if rows[0].Text != "no answer came back" {
		t.Errorf("답 없음 문구가 바뀌었다: %q", rows[0].Text)
	}

	// And it survives the JSON the clients actually read — an unexported or untagged field would
	// pass the check above and still reach nobody.
	b, err := json.Marshal(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"thought":`) {
		t.Errorf("행 JSON 에 thought 가 없다 — 클라이언트가 읽을 것이 없다: %s", b)
	}

	// A verdict with no thought must not grow an empty key on every row of every council.
	d2, _ := json.Marshal(event.CouncilVerdictData{Round: 1, Member: "m", Decision: "done"})
	q, _ := json.Marshal(Rows([]event.Event{{Seq: 8, Type: event.TypeCouncilVerdict, Data: d2}})[0])
	if strings.Contains(string(q), `"thought"`) {
		t.Errorf("생각이 없는데 칸이 실려 나간다: %s", q)
	}
}
