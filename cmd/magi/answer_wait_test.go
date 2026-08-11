package main

import "testing"

// This layer answers only whether an answerer is somewhere ELSE.
//
// A terminal has the person in front of it and waits as long as they need; a daemon has whoever
// attaches. Which MODES actually wait is app.answerBound's question, because the mode changes while
// the process runs and a bound frozen here would outlive it.
func TestTheWaitSaysWhetherTheAnswererIsElsewhere(t *testing.T) {
	if got := answerWait(true); got != daemonAnswerWait {
		t.Errorf("a daemon somebody attaches to: answerWait = %v, want %v", got, daemonAnswerWait)
	}
	if got := answerWait(false); got != 0 {
		t.Errorf("a terminal waits for the person in front of it: answerWait = %v, want 0", got)
	}
}
