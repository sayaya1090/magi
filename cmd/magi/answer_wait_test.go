package main

import (
	"testing"
	"time"
)

// How long a prompt waits for a person follows the permission MODE, not the process.
//
// Choosing "ask" is choosing to be asked. Resolving it by default after a few minutes answers the
// question on the person's behalf, which is the one thing the mode exists to prevent — and with an
// unanswered prompt resolving to "deny", an unattended companion on ask used to stall for three
// minutes per dangerous call and then refuse it. Waiting instead puts it in the fleet's `waiting`
// state, which is badged on the console and pushed to a phone, until somebody answers.
func TestHowLongAPromptWaitsFollowsTheMode(t *testing.T) {
	for _, c := range []struct {
		what       string
		answerable bool
		perm       string
		want       time.Duration
	}{
		{"a daemon told to ask", true, "ask", 0},
		{"a daemon on auto", true, "auto", daemonAnswerWait},
		{"a terminal", false, "ask", 0},
		// auto in a terminal is not bounded either: the person is in front of it, and a prompt that
		// expires while they are reading it is a decision taken out of their hands.
		{"a terminal on auto", false, "auto", 0},
		{"allow never prompts, so the number never applies", true, "allow", 0},
	} {
		if got := answerWait(c.answerable, c.perm); got != c.want {
			t.Errorf("%s: answerWait = %v, want %v", c.what, got, c.want)
		}
	}
}
