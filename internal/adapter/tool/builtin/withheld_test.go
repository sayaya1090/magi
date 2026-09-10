package builtin

import (
	"testing"
)

// The withheld list and the conditional registration are two halves of one decision.
//
// `port_owner` is registered only where /proc or lsof can answer, and named in `withheldHere`
// everywhere else. Two places, one fact — so they can drift, and the drift is silent in both
// directions:
//
//   - a tool in BOTH would be offered on this machine and reported as withheld from it, so a policy
//     check would pass for the wrong reason.
//   - a tool in NEITHER disappears from KnownNames the moment its platform stops registering it,
//     and every policy literal naming it starts reading as a stale name. That is the failure this
//     pair was written to close, measured 2026-09-11 as `"port_owner" names no tool` on Windows.
//
// So they are held against each other rather than against a list written here a third time.
func TestWithheldIsTheExactComplementOfWhatIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, tool := range Default().List() {
		registered[tool.Name()] = true
	}
	for _, tool := range withheldHere() {
		if registered[tool.Name()] {
			t.Errorf("%q 는 등록돼 있는데 물러난 것으로도 세어진다 — 두 자리가 갈렸다", tool.Name())
		}
	}
}

// And the vocabulary is the same everywhere, which is the property the policy checks rest on.
//
// A name that KnownNames drops on one platform makes every literal spelling it read as stale there
// — the policy is fine, the check is what breaks. So the union has to hold the conditional tools
// whichever way this build went.
func TestTheKnownVocabularyHoldsWhatThisPlatformWithholds(t *testing.T) {
	known := KnownNames()
	if len(known) < 20 {
		t.Fatalf("이름을 %d 개밖에 못 셌다 — 열거가 깨진 것이지 어휘가 아니다", len(known))
	}
	for _, tool := range withheldHere() {
		if !known[tool.Name()] {
			t.Errorf("%q 를 이 플랫폼이 안 내주는데 어휘에도 없다 — 그 이름을 쓴 정책이 전부 낡은 것으로 읽힌다",
				tool.Name())
		}
	}
	// port_owner is the one this exists for, and it must be nameable on every platform. Named
	// outright because a list that only checks itself would pass with the entry deleted.
	if !known["port_owner"] {
		t.Error(`"port_owner" 가 어휘에 없다 — 이 바이너리는 그 도구를 가지고 있다`)
	}
	// A name nothing answers to is still caught, which is the whole point of the check.
	if known["not_a_tool_anywhere"] {
		t.Error("아무것도 답하지 않는 이름이 어휘에 있다")
	}
}
