package app

import "testing"

// A high-water mark that could not be taken is not a mark of zero.
//
// The mark is how many user prompts the log held when the turn finished; anything past it arrived
// during triage and is re-surfaced as its own fresh turn. It comes from a store read whose error
// used to be discarded, and Read answers (nil, err) when the log cannot be read — so an unreadable
// log arrived as "this session has said nothing", the mark became 0, and every prompt the
// conversation ever held counted as new. Each was re-appended as a top-level prompt and the run
// restarted: the person's whole history replayed as if they had just typed it, out of a fact about
// the disk.
//
// Both directions are checked. A guard that only proves the unknown case stays quiet is satisfied
// just as well by one that never reports a steer at all, which strands real ones.
func TestAnUntakenMarkIsNotAMarkOfZero(t *testing.T) {
	log := []userPrompt{{MsgID: "m1", Text: "first"}, {MsgID: "m2", Text: "second"}, {MsgID: "m3", Text: "third"}}

	if got := steersSince(promptMark{}, log); got != nil {
		t.Errorf("an unknown mark must claim no steer, not the whole conversation: %d prompts, "+
			"starting %q", len(got), got[0].Text)
	}
	// Known and unmoved: nothing arrived.
	if got := steersSince(promptMark{n: 3, taken: true}, log); got != nil {
		t.Errorf("nothing arrived after the mark, got %d", len(got))
	}
	// Known and moved: exactly what came after it, which is the whole point of the mechanism.
	got := steersSince(promptMark{n: 2, taken: true}, log)
	if len(got) != 1 || got[0].MsgID != "m3" {
		t.Fatalf("a steer that landed after the mark must be reported: %+v", got)
	}
	// A genuinely empty session with a known mark of zero still reports what came in — this is
	// what separates "unknown" from "zero", and it is the assertion that fails if the fix is
	// written as `if mark.n == 0 { return nil }`. The zero VALUE of promptMark is the untaken
	// one, so the accident — a mark nobody set — lands on the harmless answer above, not here.
	if got := steersSince(promptMark{n: 0, taken: true}, log); len(got) != 3 {
		t.Errorf("a known mark of zero means the session had said nothing, so all three are new: %d", len(got))
	}
}
