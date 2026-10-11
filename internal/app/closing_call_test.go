package app

import (
	"strings"
	"testing"
)

func TestClosingCallUsesExplicitReportContract(t *testing.T) {
	if !strings.Contains(closingCall(true), "do not write it again") {
		t.Fatal("ready report must stand")
	}
	if !strings.Contains(closingCall(false), "Write your final answer") {
		t.Fatal("missing report must be requested")
	}
	for _, report := range []string{"완료했습니다.", strings.Repeat("x", 1000)} {
		if !strings.Contains(notRunTail(report), "do not write it again") {
			t.Fatal("report length must not decide repetition")
		}
	}
}
