package idebridge

import (
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// backwards is every path where a later event changes a row that is already drawn.
//
// ⚠ **The fixture alone does not reach them.** It is a batch log: no streaming chunks, no resurfaced
// interjection, and its one inline answer happens to be about the LAST row — so "moved to the bottom"
// and "was already at the bottom" produce the same list. The first version of this measurement
// reported three words for that reason. Each case below is named by the word it forces.
func backwards() map[string][]event.Event {
	return map[string][]event.Event{
		"초안이 사실이 된다": {
			mk(1, "prompt.submitted", map[string]any{"messageId": "u1", "parts": []any{map[string]any{"kind": "text", "text": "hi"}}}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "reasoning", "text": "thi"}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "reasoning", "text": "nking"}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "ans"}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "wer"}, nil),
			mk(6, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "part": map[string]any{"kind": "reasoning", "text": "thinking"}}, nil),
			mk(7, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "part": map[string]any{"kind": "text", "text": "answer"}}, nil),
			mk(8, "turn.finished", map[string]any{}, nil),
		},
		"되살아난 인터젝션": {
			mk(1, "prompt.submitted", map[string]any{"messageId": "u1", "parts": []any{map[string]any{"kind": "text", "text": "first"}}}, nil),
			mk(2, "prompt.submitted", map[string]any{"messageId": "q1", "parts": []any{map[string]any{"kind": "text", "text": "wait, also this"}}}, nil),
			mk(3, "interjection.deferred", map[string]any{"messageId": "q1"}, nil),
			mk(4, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "part": map[string]any{"kind": "text", "text": "answering the first"}}, nil),
			mk(5, "prompt.submitted", map[string]any{"messageId": "q2", "resurfacedFrom": "q1", "parts": []any{map[string]any{"kind": "text", "text": "wait, also this"}}}, nil),
		},
		// ⚠ **`grow` 는 첫 줄이 끝난 뒤부터 나온다.** 요약은 첫 줄이고, 그 줄이 아직 자라는 중이면
		// 본문과 요약이 **둘 다** 바뀌므로 그 조각은 행 전체(patch)로 간다. 첫 줄이 끝나면 이후
		// 조각은 요약을 안 건드리니 글자만 보낸다 — 아껴지는 자리가 바로 긴 답이다.
		"조각이 첫 줄을 넘어 흐른다": {
			mk(1, "prompt.submitted", map[string]any{"messageId": "u1", "parts": []any{map[string]any{"kind": "text", "text": "explain"}}}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "one line done\n"}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "second line"}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": " keeps going\n"}, nil),
			mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "third\n"}, nil),
			mk(6, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "part": map[string]any{"kind": "text", "text": "one line done\nsecond line keeps going\nthird\n"}}, nil),
		},
		// ⚠ 이 갈래가 `move` 를 만드는 유일한 자리이고, **큐잉된 질문 뒤에 다른 행이 있어야** 만든다.
		// 질문이 이미 마지막 행이면 「끝으로 옮김」이 아무것도 안 바꾼다 — 첫 측정이 놓친 그 자리다.
		"큐가 인라인으로 답해진다": {
			mk(1, "prompt.submitted", map[string]any{"messageId": "u1", "parts": []any{map[string]any{"kind": "text", "text": "first"}}}, nil),
			mk(2, "prompt.submitted", map[string]any{"messageId": "q1", "parts": []any{map[string]any{"kind": "text", "text": "and this"}}}, nil),
			mk(3, "interjection.deferred", map[string]any{"messageId": "q1"}, nil),
			mk(4, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "part": map[string]any{"kind": "text", "text": "working on the first"}}, nil),
			mk(5, "part.appended", map[string]any{"messageId": "m1", "role": "assistant", "inReplyTo": "q1", "part": map[string]any{"kind": "text", "text": "and both at once"}}, nil),
		},
	}
}

func listJSON(t *testing.T, rows []Row) string {
	t.Helper()
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("행 목록을 JSON 으로 못 만든다: %v", err)
	}
	return string(b)
}

// TestApplyingTheWordsRebuildsTheFold is the contract itself.
//
// A live screen holds rows and is sent differences; a screen that just opened is sent the fold. If
// those two ever disagree, the same conversation looks different depending on WHEN somebody opened
// the panel — the drift this package exists to end. So: fold every prefix, send the difference
// against the prefix before it, and require the result to equal the fold field for field.
func TestApplyingTheWordsRebuildsTheFold(t *testing.T) {
	streams := backwards()
	var fixture []event.Event
	read(t, "fold_events.json", &fixture)
	streams["정본 픽스처"] = fixture

	for what, events := range streams {
		if len(events) < 5 {
			t.Fatalf("%s: 사건이 %d 개뿐이라 잴 것이 없다", what, len(events))
		}
		held := Rows(events[:0])
		for n := 1; n <= len(events); n++ {
			want := Rows(events[:n])
			got := Apply(held, Diff(Rows(events[:n-1]), want))
			if listJSON(t, got) != listJSON(t, want) {
				t.Fatalf("%s: 사건 %d(%s) 을 차이로 전한 결과가 접기와 다르다\n낱말: %s\n받은 쪽: %s\n접기: %s",
					what, n, events[n-1].Type, opsJSON(t, Diff(Rows(events[:n-1]), want)),
					listJSON(t, got), listJSON(t, want))
			}
			held = got
		}
		// 그리고 그 끝이 창을 지금 연 사람이 받는 것과 같아야 한다.
		if listJSON(t, held) != listJSON(t, Rows(events)) {
			t.Errorf("%s: 차이만 받아 온 화면과 지금 창을 연 화면이 다른 대화를 그린다", what)
		}
	}
}

func opsJSON(t *testing.T, ops []Op) string {
	t.Helper()
	b, _ := json.Marshal(ops)
	return string(b)
}

// TestTheLiveWordsAreMeasured keeps the vocabulary honest in both directions.
//
// ⚠ **Both directions matter.** A word that nothing produces is a word six clients implement for
// nothing; a change that no word covers is a live screen quietly drawing the wrong conversation. The
// counts are logged rather than asserted exactly — a fixture edit would then be a test failure about
// arithmetic — but every word must be exercised by the case named for it.
func TestTheLiveWordsAreMeasured(t *testing.T) {
	streams := backwards()
	var fixture []event.Event
	read(t, "fold_events.json", &fixture)
	streams["정본 픽스처"] = fixture

	seen := map[string]int{}
	for what, events := range streams {
		per := map[string]int{}
		for n := 1; n <= len(events); n++ {
			for _, op := range Diff(Rows(events[:n-1]), Rows(events[:n])) {
				per[op.Op]++
				seen[op.Op]++
			}
		}
		t.Logf("%s: %v", what, per)
	}
	for _, word := range []string{OpAdd, OpGrow, OpPatch, OpDrop, OpMove} {
		if seen[word] == 0 {
			t.Errorf("`%s` 를 아무 갈래도 만들지 않는다 — 낱말이 필요 없어졌으면 계약에서 지우고, "+
				"그것을 만들던 갈래가 시험에서 사라졌으면 그 갈래를 되살릴 것", word)
		}
	}
	for word := range seen {
		switch word {
		case OpAdd, OpGrow, OpPatch, OpDrop, OpMove, OpReset:
		default:
			t.Errorf("계약에 없는 낱말 %q 가 나왔다", word)
		}
	}
	t.Logf("합계: %v", seen)
}

// TestAStreamedAnswerCostsItsOwnLength is why OpGrow exists.
//
// A draft grows by one chunk at a time. If each of those were a whole row, delivering a long answer
// would cost the answer's length TIMES the number of chunks — and that is not a rounding error: the
// ratio below is what a person on a slow model pays to watch it type.
func TestAStreamedAnswerCostsItsOwnLength(t *testing.T) {
	const chunks = 200
	events := []event.Event{
		mk(1, "prompt.submitted", map[string]any{"messageId": "u1", "parts": []any{map[string]any{"kind": "text", "text": "write something long"}}}, nil),
	}
	for i := 0; i < chunks; i++ {
		events = append(events, mk(0, "part.delta", map[string]any{"messageId": "m1", "kind": "text", "text": "0123456789 abcdefghij 0123456789 abcdefghij\n"}, nil))
	}
	answer := len(Rows(events)[1].Text)

	grew, whole := 0, 0
	for n := 2; n <= len(events); n++ {
		before, after := Rows(events[:n-1]), Rows(events[:n])
		for _, op := range Diff(before, after) {
			if op.Op != OpGrow {
				continue
			}
			grew += len(op.Text)
			whole += len(after[len(after)-1].Text)
		}
	}
	if grew == 0 {
		t.Fatalf("조각 %d 개가 `grow` 를 하나도 안 만들었다 — 이 낱말이 그 자리에 안 쓰이고 있다", chunks)
	}
	t.Logf("답 %d 바이트: grow 가 보낸 글자 %d, 같은 변화를 행 전체로 보내면 %d (%.0f배)",
		answer, grew, whole, float64(whole)/float64(grew))
	if grew > answer+chunks {
		t.Errorf("grow 가 보낸 글자(%d)가 답 자체(%d)보다 많다 — 조각마다 앞부분을 다시 보내고 있다", grew, answer)
	}
	if whole < grew*10 {
		t.Errorf("행 전체로 보내는 쪽이 %d 바이트뿐이라, 이 낱말이 무엇을 아끼는지 재고 있지 않다", whole)
	}
}

// TestNamesMustBeUniqueOrTheWholeListIsSent is the safety valve.
//
// A patch is aimed by name. Two rows with one name means an edit meant for one of them silently lands
// on the other, and nothing downstream can notice — so Diff must answer the whole list instead. The
// fold does not produce such a list today (TestWhetherARowCanBeNamed measures that); this is about
// what happens if it ever does.
func TestNamesMustBeUniqueOrTheWholeListIsSent(t *testing.T) {
	before := []Row{{ID: "1", Who: WhoUser, Text: "a"}, {ID: "2", Who: WhoAgent, Text: "b"}}
	clash := []Row{{ID: "1", Who: WhoUser, Text: "a"}, {ID: "1", Who: WhoAgent, Text: "b"}}

	ops := Diff(before, clash)
	if len(ops) != 1 || ops[0].Op != OpReset {
		t.Errorf("이름이 겹치는 목록을 차이로 전하려 한다: %s", opsJSON(t, ops))
	}
	if got := listJSON(t, Apply(before, ops)); got != listJSON(t, clash) {
		t.Errorf("리셋을 적용한 결과가 그 목록이 아니다: %s", got)
	}
	// 이름이 없는 행도 같다 — 지목할 수 없는 것은 고칠 수 없다.
	if ops := Diff(before, []Row{{Who: WhoUser, Text: "unnamed"}}); len(ops) != 1 || ops[0].Op != OpReset {
		t.Errorf("이름 없는 행을 차이로 전하려 한다: %s", opsJSON(t, ops))
	}
	// 그리고 리셋은 빈 대화도 말할 수 있어야 한다.
	if got := Apply(before, []Op{{Op: OpReset, Rows: []Row{}}}); len(got) != 0 {
		t.Errorf("빈 목록으로 리셋했는데 %d 행이 남았다", len(got))
	}
}

// TestAnUnknownPlaceGoesToTheEnd is the one forgiving rule, stated as a measurement.
//
// A client that missed a frame does not know every name. Putting the row at the end is a row in the
// wrong PLACE — which the next reset fixes — where guessing a position could overwrite a line.
func TestAnUnknownPlaceGoesToTheEnd(t *testing.T) {
	held := []Row{{ID: "1", Who: WhoUser, Text: "a"}}
	row := Row{ID: "9", Who: WhoAgent, Text: "late"}
	got := Apply(held, []Op{{Op: OpAdd, ID: "9", Row: &row, After: "7"}})
	if len(got) != 2 || got[1].ID != "9" {
		t.Errorf("모르는 자리를 지목한 행이 끝에 붙지 않았다: %s", listJSON(t, got))
	}
	// 그리고 모르는 행을 고치라는 말은 아무것도 지우지 않는다.
	same := Apply(held, []Op{{Op: OpDrop, ID: "404"}, {Op: OpGrow, ID: "404", Text: "x"}})
	if listJSON(t, same) != listJSON(t, held) {
		t.Errorf("모르는 행에 대한 말이 목록을 바꿨다: %s", listJSON(t, same))
	}
}
