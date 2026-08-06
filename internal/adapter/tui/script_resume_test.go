package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

// Sweep eleven: the resume round trip.
//
// A live transcript is built from two sources — what the view echoed locally when the user pressed
// enter, and what the app published afterwards. A RESUMED one has only the second. So the question
// this asks is whether the record alone can rebuild what the user saw, and it is the one path where
// "the view renders what it is told" and "the view is told everything" come apart: anything the
// live view knew only because it put it there itself is, on resume, simply gone.

// replay builds a fresh Model on the same session and folds every persisted event into it, the way
// a resumed session does.
func (r *realTurn) replay(t *testing.T) Model {
	t.Helper()
	evs, cancel, err := r.app.Subscribe(context.Background(), r.m.sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	m := New(context.Background(), r.app, nil, r.m.sid, "m", r.m.workdir, true, "")
	fresh := &script{t: t, m: m}
	fresh.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	// The past is pushed by a goroutine, so the channel is empty for an instant after Subscribe
	// returns — a non-blocking read here would rebuild an empty transcript and every comparison
	// against it would pass for the wrong reason. Read until the log has been quiet for a beat.
	deadline := time.After(5 * time.Second * deadlineScale)
	quiet := time.NewTimer(300 * time.Millisecond)
	for {
		select {
		case ev, ok := <-evs:
			if !ok {
				return fresh.m
			}
			fresh.send(eventMsg{ev: ev, sid: fresh.m.sid, sub: fresh.m.mainSub})
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(300 * time.Millisecond)
		case <-quiet.C:
			fresh.send(renderTickMsg{}) // the frame is painted on the tick, not on the fold
			return fresh.m
		case <-deadline:
			fresh.send(renderTickMsg{})
			return fresh.m
		}
	}
}

// blockSummary is the shape of a transcript: what kind each block is and what it says. Styling,
// timestamps and the turn's ELAPSED time are excluded on purpose — a resumed session renders at its
// own width, and it never timed a turn it did not run, so a summary without a duration is the
// honest thing for it to show rather than a number it would have to invent.
func blockSummary(bs []block) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		text := strings.TrimSpace(elapsedInSummary.ReplaceAllString(b.text, ""))
		if len(text) > 60 {
			text = text[:60]
		}
		out = append(out, fmt.Sprintf("%d:%s", b.kind, text))
	}
	return out
}

// What the user asked must survive a resume. It is the half of the transcript the live view put
// there itself, so if the record cannot rebuild it, coming back to a session shows magi talking to
// nobody.
func TestAResumedSessionStillShowsWhatTheUserAsked(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Writing the note."), call("c1", "write", `{"path":"n.txt","content":"x\n"}`), finish},
		{say("The note is written."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("write a note for me")

	resumed := r.replay(t)
	plain := ansiSeq.ReplaceAllString(resumed.View().Content, "")
	if !strings.Contains(plain, "write a note for me") {
		t.Errorf("the question is gone from the resumed transcript:\n%s", plain)
	}
	if !strings.Contains(plain, "The note is written.") {
		t.Errorf("the answer is gone from the resumed transcript:\n%s", plain)
	}
}

// And the two transcripts must agree. A resumed session that shows the same conversation in a
// different shape — a lost tool call, a doubled bubble, a reordered pair — is a different record of
// the same work, and the user has no way to tell which one is right.
func TestALiveAndAResumedTranscriptAgree(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Step one."), call("c1", "bash", `{"command":"echo one"}`), finish},
		{say("Step two."), call("c2", "write", `{"path":"a.txt","content":"a\n"}`), finish},
		{say("All done."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("do two things")

	live := blockSummary(r.m.blocks)
	resumed := blockSummary(r.replay(t).blocks)
	if len(live) != len(resumed) {
		t.Fatalf("live has %d blocks, resumed has %d\nlive:    %v\nresumed: %v", len(live), len(resumed), live, resumed)
	}
	for i := range live {
		if live[i] != resumed[i] {
			t.Errorf("block %d differs:\n  live:    %q\n  resumed: %q", i, live[i], resumed[i])
		}
	}
}

// A turn that ended in an error must resume as one. Coming back to a session that looks like it
// finished cleanly, when it did not, is the display telling the user something the record denies.
func TestAResumedFailureStillLooksLikeAFailure(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Trying."), call("c1", "bash", `{"command":"exit 7"}`), finish},
		{say("It exited 7."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("run it")

	plain := ansiSeq.ReplaceAllString(r.replay(t).View().Content, "")
	if !strings.Contains(plain, "exit 7") {
		t.Errorf("the failure is not in the resumed transcript:\n%s", plain)
	}
}

// The event log is the whole record, so replaying it twice must produce the same transcript. A
// fold that accumulates — appending where it should replace, or keying on something that changes —
// shows up here and nowhere else.
func TestReplayingTwiceGivesTheSameTranscript(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Working."), call("c1", "bash", `{"command":"echo hi"}`), finish},
		{say("Done."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("go")

	first := blockSummary(r.replay(t).blocks)
	second := blockSummary(r.replay(t).blocks)
	if len(first) != len(second) {
		t.Fatalf("two replays of one log gave %d and %d blocks", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Errorf("replay %d differs at block %d:\n  %q\n  %q", i, i, first[i], second[i])
		}
	}
	_ = event.TypeTurnFinished
}

// elapsedInSummary matches the "· 12s" tail a live turn summary carries and a resumed one cannot.
var elapsedInSummary = regexp.MustCompile(` · [0-9]+(\.[0-9]+)?(ms|s|m|h)[0-9a-z]*$`)

// A session whose log ends MID-TURN. magi was killed, the machine rebooted, the container went
// away — the record simply stops, with no turn.finished and no error. Reopening it must not leave
// the user watching a spinner for work that ended hours ago, and must not claim the last thing the
// agent said was its answer.
func TestResumingASessionThatWasKilledMidTurn(t *testing.T) {
	llm := &scriptedLLM{steps: [][]port.ProviderEvent{
		{say("Starting the build."), call("c1", "bash", `{"command":"echo building"}`), finish},
		{say("Still going."), finish},
	}}
	r := newRealTurn(t, llm)
	r.run("build it")

	// Replay what a real resume would see, minus the turn's ending — the shape a killed process
	// leaves behind. Transient events (seq 0: the streaming deltas, live progress) are published
	// to the bus but never stored, so a resumed session never sees them; keeping them here would
	// hand the fresh view a stream that a real reopen cannot produce.
	var truncated []event.Event
	for _, e := range r.seen {
		if e.Seq == 0 || e.Type == event.TypeTurnFinished || e.Type == event.TypeError {
			continue
		}
		truncated = append(truncated, e)
	}
	m := New(context.Background(), r.app, nil, r.m.sid, "m", r.m.workdir, true, "")
	fresh := &script{t: t, m: m}
	fresh.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	for _, e := range truncated {
		fresh.send(eventMsg{ev: e, sid: fresh.m.sid, sub: fresh.m.mainSub})
	}
	_ = fresh.view()

	// The work that DID happen is still there — a truncated log is not an empty one.
	plain := fresh.view()
	if !strings.Contains(plain, "build it") || !strings.Contains(plain, "Starting the build.") {
		t.Errorf("the work before the kill is missing from the resumed transcript:\n%s", plain)
	}
	// Nothing is running. The turn ended when the process died; a reopened session that animates a
	// spinner is telling the user to wait for work that stopped hours ago.
	if fresh.m.running {
		t.Error("a resumed session is still claiming the turn is running")
	}
	if strings.Contains(plain, "working…") {
		t.Errorf("the resumed frame shows a live turn:\n%s", plain)
	}
	// And the frame is coherent: the block map must still resolve.
	if len(fresh.m.blockLineStart) != len(fresh.m.blocks) {
		t.Errorf("%d start lines for %d blocks after a truncated replay",
			len(fresh.m.blockLineStart), len(fresh.m.blocks))
	}
}

// The log ends between a tool call and its result — the most common place a kill lands, because a
// command is where the time goes. The call must not render as though it completed.
func TestACallWithNoResultDoesNotLookCompleted(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "run the build")
	s.toolCall("bash", "c1")
	_ = s.view()

	for _, b := range s.m.blocks {
		if b.kind == blockToolCall && b.done {
			t.Errorf("a call with no result is marked done: %+v", b)
		}
	}
	s.renders("a call still in flight", "bash")
}
