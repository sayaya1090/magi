package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The panel shows magi's own record, so the split it renders has to match what the record says:
// a masked exit is "unknown" rather than a pass, an inspection command is not a run at all, and a
// failing command carries its status so its line cannot be read as a success.
func TestObservationSplitsTheRecordTheWayThePanelShowsIt(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	add := func(a, b event.Event) {
		t.Helper()
		for _, e := range []event.Event{a, b} {
			if _, err := app.store.Append(ctx, sid, e); err != nil {
				t.Fatal(err)
			}
		}
	}
	wd, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: "w1", Name: "write", Args: jsonOf(map[string]string{"path": "/app/run.py"})}},
	})
	if _, err := app.store.Append(ctx, sid, event.Event{Type: event.TypePartAppended, Data: wd}); err != nil {
		t.Fatal(err)
	}
	add(bashPair("c1", "python3 /app/run.py", "exit 0\n"))  // ran clean
	add(bashPair("c2", "pytest -q", "exit 1\n"))            // ran and failed
	add(bashPair("c3", "make build | tail -1", "exit 0\n")) // the exit belongs to the tail
	add(bashPair("c4", "cat /app/run.py", "exit 0\n"))      // inspection only

	obs := app.Observation(ctx, sid)
	if len(obs.Changed) != 1 || obs.Changed[0] != "/app/run.py" {
		t.Errorf("changed = %v, want the written path", obs.Changed)
	}
	if len(obs.Clean) != 2 {
		t.Errorf("every clean command belongs on the panel, got %v", obs.Clean)
	}
	if len(obs.Failed) != 1 || !strings.Contains(obs.Failed[0], "exit 1") {
		t.Errorf("failed = %v, want the failing command carrying its status", obs.Failed)
	}
	if len(obs.Unknown) != 1 || !strings.Contains(obs.Unknown[0], "make build") {
		t.Errorf("unknown = %v, want the command whose exit belonged to a tail", obs.Unknown)
	}
	// `cat` printed state rather than exercising anything — and it is still a command magi granted,
	// so the panel shows it with its exit. Sorting it out was a reading, and a wrong one often
	// enough: `sed -n` and `grep` read as program runs until the verb list was fixed.
	var found bool
	for _, line := range obs.Clean {
		if strings.HasPrefix(line, "cat ") {
			found = true
		}
	}
	if !found {
		t.Errorf("an inspection is a command magi granted and belongs on the panel: %v", obs.Clean)
	}
}

// A session that has done nothing observes nothing, so the panel can hide the block instead of
// showing an empty heading.
func TestObservationIsEmptyBeforeAnythingHappens(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if obs := app.Observation(ctx, sid); !obs.Empty() {
		t.Errorf("a session that has done nothing must observe nothing, got %+v", obs)
	}
}
