package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// An `at`/`to` edit names a location without naming its content, so the only thing standing between
// it and the wrong line is whether magi remembers what that line held when the agent read it.
//
// magi used to carry that check in the read gutter as `N#hh|content`. The gutter is gone — the
// `#hh|` prefix read as a data column and models parsed a phantom field out of CSV/TSV — and the
// guard went with it: `checkAnchor` has been a bounds check ever since, so an anchored edit onto a
// file that shifted under the agent lands silently on whatever is at that line now.
//
// These lock the record that brings the guard back without the gutter.

func TestAnAnchorOntoAShiftedLineIsRefused(t *testing.T) {
	g := newRunGuard()
	const p = "internal/app/loop.go"

	// What the agent was shown: the read tool's own gutter, `N\tcontent`.
	g.noteReadLines(p, "10\tfunc run() {\n11\t\tstart()\n12\t}")

	// An earlier edit inserted a line above, so everything below shifted by one. Line 11 now holds
	// what line 10 held, which is exactly the case that used to write to the wrong place.
	lines := "1\n2\n3\n4\n5\n6\n7\n8\n9\nfunc run() {\n\tsetup()\n\tstart()\n}"
	if !g.anchorDrifted(p, 11, lines) {
		t.Fatal("line 11 now holds setup(), not the start() the agent read — the anchor has drifted")
	}
}

func TestAnUnchangedAnchorIsAllowed(t *testing.T) {
	g := newRunGuard()
	const p = "internal/app/loop.go"
	g.noteReadLines(p, "10\tfunc run() {\n11\t\tstart()\n12\t}")

	same := "1\n2\n3\n4\n5\n6\n7\n8\n9\nfunc run() {\n\tstart()\n}"
	if g.anchorDrifted(p, 11, same) {
		t.Fatal("line 11 still holds what the agent read; refusing it would block a correct edit")
	}
}

// The guard answers only where it has a record. A file the agent never read is not evidence of
// drift, and treating it as such would refuse edits that were fine before this existed.
func TestAnUnreadFileIsNotRefused(t *testing.T) {
	g := newRunGuard()
	if g.anchorDrifted("never/read.go", 3, "a\nb\nc\n") {
		t.Fatal("no record is not evidence of drift")
	}
	g.noteReadLines("partly/read.go", "1\tone\n2\ttwo")
	if g.anchorDrifted("partly/read.go", 40, "one\ntwo\nthree") {
		t.Fatal("line 40 was never delivered, so there is nothing to compare it against")
	}
}

// An anchor past the end is checkAnchor's to report — it says how many lines the file has, which is
// what the agent needs. Answering "drifted" here would replace that with a worse message.
func TestPastEndIsLeftToTheBoundsCheck(t *testing.T) {
	g := newRunGuard()
	g.noteReadLines("x.go", "1\tone\n2\ttwo\n3\tthree")
	if g.anchorDrifted("x.go", 3, "one\ntwo") {
		t.Fatal("past-end is reported by checkAnchor, not by the drift guard")
	}
}

// Half a file's record is worse than none: the tracked half would refuse and the untracked half
// would allow, so the guard would be firing on where the line sits rather than on whether it moved.
func TestAPathPastTheLineCapIsDroppedEntirely(t *testing.T) {
	g := newRunGuard()
	const p = "huge.txt"
	big := ""
	for i := 1; i <= maxShownLinesPerPath+50; i++ {
		big += strconv.Itoa(i) + "\tline " + strconv.Itoa(i) + "\n"
	}
	g.noteReadLines(p, big)
	if _, ok := g.shownLines[p]; ok {
		t.Fatalf("a path past the %d-line cap keeps no partial record", maxShownLinesPerPath)
	}
	if g.anchorDrifted(p, 1, "something else\n") {
		t.Fatal("an untracked path is unguarded, never refused")
	}
}

// The record and the check are only worth having if the gate in front of `edit` actually consults
// them. This drives the real path: a read that shows the agent three lines, a file that shifts
// under it, and an anchored edit onto the line that moved.
func TestTheGateRefusesAnEditOntoAShiftedAnchor(t *testing.T) {
	a, sid, wd := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	s := session.Session{ID: sid, Workdir: wd}
	actor := event.Actor{Kind: event.ActorAgent, ID: "main"}
	g := newRunGuard()

	if err := os.WriteFile(filepath.Join(wd, "loop.go"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What the read handed over.
	g.noteReadLines("loop.go", "1\tone\n2\ttwo\n3\tthree")

	// Something inserted a line above: "two" is now on line 3, and the agent's anchor at 2 points
	// at "inserted".
	if err := os.WriteFile(filepath.Join(wd, "loop.go"), []byte("one\ninserted\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call := &session.ToolCall{
		Name: "edit", CallID: "c1",
		Args: json.RawMessage(`{"path":"loop.go","at":2,"new":"TWO"}`),
	}
	if !a.gateStaleAnchor(context.Background(), s, actor, call, g, "m1") {
		t.Fatal("the gate must stop an edit anchored to a line that moved")
	}
	_ = call
	if got := lastToolResultText(t, a, sid); !strings.Contains(got, "not the line you read") ||
		!strings.Contains(got, "Re-read") {
		t.Fatalf("the refusal must say what happened and what to do next; got %q", got)
	}

	// And it must not stand in the way of an edit whose anchor still holds. Line 1 is untouched.
	ok := &session.ToolCall{
		Name: "edit", CallID: "c2",
		Args: json.RawMessage(`{"path":"loop.go","at":1,"new":"ONE"}`),
	}
	if a.gateStaleAnchor(context.Background(), s, actor, ok, g, "m2") {
		t.Fatal("line 1 still holds what the agent read; the gate must let it through")
	}
}

// Driving executeTool, so the gate is tested where it is WIRED and not only where it is defined.
// Disabling the call in the gate chain has to break this; a test of the function alone would not
// notice, which is how a guard ends up correct and unreachable at the same time.
func TestAStaleAnchoredEditNeverReachesTheFile(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(),
		Config{Permission: "allow"}))
	wd := t.TempDir()
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	path := filepath.Join(wd, "loop.go")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	guard := newRunGuard()
	guard.noteReadLines("loop.go", "1\tone\n2\ttwo\n3\tthree")

	// The file shifts under the agent between its read and its edit.
	if err := os.WriteFile(path, []byte("one\ninserted\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"path": "loop.go", "at": 2, "new": "REPLACED"})
	a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor,
		&session.ToolCall{CallID: "c1", Name: "edit", Args: args}, guard, "")

	after, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(after), "REPLACED") {
		t.Fatalf("the edit landed on the shifted line; file is now %q", string(after))
	}
	if got := lastToolResultText(t, a, sid); !strings.Contains(got, "not the line you read") {
		t.Fatalf("the agent must be told why, and what to do; got %q", got)
	}
}
