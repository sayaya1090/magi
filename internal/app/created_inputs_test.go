package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The council is told, as a fact, when a file the task names was made by the turn itself. Measured
// live (2026-09-27): told to count the rows of invoices.csv in a workspace without one, the agent
// wrote a file of invented rows, counted them, and three members accepted the count as "a real run
// matched". A file the task names that was already there must not be reported: that is the input
// the task meant, and calling it new would send a member hunting for a phantom.
func TestTheCouncilIsToldWhichNamedFilesTheTurnMade(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	llm := workingLLM(
		toolStep("write", `{"path":"invoices.csv","content":"id,amount\n1,100\n"}`),
		toolStep("council", `{"complete":true}`),
		textStep("2 rows"),
	)
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	if err := os.WriteFile(filepath.Join(wd, "customers.csv"), []byte("id\n1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "read invoices.csv and customers.csv and tell me how many rows each has"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	waitForTerminal(t, a, sid)

	got := fc.lastReq.Actions
	i := strings.Index(got, "NAMED BY THE TASK, CREATED BY THIS TURN")
	if i < 0 {
		t.Fatalf("the council was not told the turn made a file the task names:\n%s", got)
	}
	line := got[i:]
	if j := strings.Index(line, "\n\n"); j >= 0 {
		line = line[:j]
	}
	if !strings.Contains(line, "invoices.csv") {
		t.Errorf("the created file is not named:\n%s", line)
	}
	if strings.Contains(line, "customers.csv") {
		t.Errorf("a file that was there before the task is reported as made by the turn:\n%s", line)
	}
}

// No baseline, no claim: a session whose turn never recorded one must not report every file as new.
func TestNoBaselineNamesNothingAsCreated(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, "invoices.csv", "id\n")
	if got := namedFilesCreatedThisTurn("count invoices.csv", root, nil); len(got) != 0 {
		t.Errorf("with no baseline, %v was reported as created by the turn", got)
	}
	// And a named file in a subfolder is matched by its name, the way a task names it.
	base := indexWorkspace(root)
	wsWrite(t, root, "src/report.md", "x")
	if got := namedFilesCreatedThisTurn("write report.md", root, base); len(got) != 1 || got[0] != "src/report.md" {
		t.Errorf("a created file in a subfolder was not matched by the name the task used: %v", got)
	}
}
