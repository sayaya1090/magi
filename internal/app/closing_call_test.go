package app

import (
	"strings"
	"testing"
)

// The closing call and the not-run notice ask for a final answer only when one is still owed.
// A report-sized message just before the declaration IS the answer (live 2026-09-05: the old
// wording made the agent paste 2–3K characters again); a one-liner is not.
func TestClosingCallAsksForTheAnswerOnlyWhenOneIsOwed(t *testing.T) {
	report := strings.Repeat("슬라이드 3: 시장 규모 차트와 출처. ", 30) // well over the floor
	for _, tc := range []struct {
		name, last string
		wantRepeat bool
	}{
		{"report already written", report, false},
		{"one-liner", "done.", true},
		{"nothing", "", true},
	} {
		got := closingCall(tc.last)
		tail := notRunTail(tc.last)
		asks := strings.Contains(got, "write your final answer") && strings.Contains(tail, "write your final answer")
		stands := strings.Contains(got, "do not write it again") && strings.Contains(tail, "do not write it again")
		if tc.wantRepeat && !asks || !tc.wantRepeat && !stands || asks == stands {
			t.Errorf("%s: closing=%q tail=%q", tc.name, got, tail)
		}
	}
	if strings.Contains(closingCall(strings.Repeat("x", finalAnswerFloor-1)), "do not write it again") {
		t.Error("just under the floor still owes the answer")
	}
}
