package app

import (
	"strings"
	"testing"
)

// The reminder escalates. The first ask assumes the agent merely forgot the form; from the second
// on that is disproved, and repeating the first ask verbatim is what let the field case loop —
// "if it is not [finished], keep working" is not violated by announcing the same file a fourth time.
func TestDeclareAskNudgeNeverSaysTheSameThingTwice(t *testing.T) {
	const said = "Now I'll write `eval.scm`. Let me write it."
	seen := map[string]bool{}
	for n := 1; n <= declareAskCap; n++ {
		msg := declareAskNudge(n, said, true)
		if seen[msg] {
			t.Fatalf("ask %d repeated an earlier reminder verbatim", n)
		}
		seen[msg] = true
	}

	first := declareAskNudge(1, said, true)
	if !strings.Contains(first, "call the `council` tool with `complete: true`") {
		t.Errorf("the first ask must still name the declaration form, got %q", first)
	}
	if strings.Contains(first, said) {
		t.Error("the first ask quotes nothing back: one quiet response is not yet a pattern")
	}

	// The second ask is the lever: the model cannot see that it repeated itself, because each
	// response is locally reasonable. Only its own words, handed back, show the loop.
	second := declareAskNudge(2, said, true)
	if !strings.Contains(second, said) {
		t.Errorf("the second ask must quote what the agent actually said, got %q", second)
	}
	if !strings.Contains(second, "must contain a tool call") {
		t.Errorf("the second ask must demand an action, not a declaration, got %q", second)
	}

	// And the cap says it is the cap. Cutting the turn off without warning spends the last ask on
	// nothing: the agent never learns that this response is the one that decides.
	last := declareAskNudge(declareAskCap, said, true)
	if !strings.Contains(last, "last time") || !strings.Contains(last, "lands exactly as it stands") {
		t.Errorf("the final ask must say the turn ends here, got %q", last)
	}

	// A quiet response with no text at all still gets a reminder that reads.
	if q := declareAskNudge(2, "   ", true); strings.Contains(q, "said:") {
		t.Errorf("an empty last response must not be quoted as if it spoke, got %q", q)
	}
}

// **넛지는 있는 문을 가리켜야 한다.**
//
// 이 문장은 카운슬을 조건 없이 이름으로 불렀다. 카운슬 없이 도는 컴패니언에서는 없는 도구를
// 부르라고 가르치는 셈이고, 그 호출은 `unknown tool: council` 로 돌아온다 — 턴은 왕복 하나를
// 버리고, 보고 있는 사람에게는 **판이 안 끝나는 것처럼 보인다.** 실물에서 셋을 봤다
// (2026-09-04, PowerPoint 컴패니언: config 에 `[council] enabled = false`).
func TestTheDeclareNudgeNamesADoorThatExists(t *testing.T) {
	with := declareAskNudge(1, "", true)
	if !strings.Contains(with, "council") {
		t.Errorf("카운슬이 있는 판에서 그 길을 안 알려 준다: %s", with)
	}
	without := declareAskNudge(1, "", false)
	if strings.Contains(without, "council") {
		t.Errorf("카운슬이 없는 판에서 없는 도구를 가리킨다: %s", without)
	}
	// 그리고 **끝내는 법은 여전히 말해 줘야 한다.** 「카운슬 얘기만 빼기」로 고치면 모델은
	// 어떻게 끝내는지를 못 듣고, 그건 조용히 멈추는 것과 같아진다.
	for _, must := range []string{"say", "stopping"} {
		if !strings.Contains(without, must) {
			t.Errorf("끝내는 법을 안 적었다(%q 없음): %s", must, without)
		}
	}
}
