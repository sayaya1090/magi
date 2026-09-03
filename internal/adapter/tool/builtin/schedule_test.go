package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// **The agent may not schedule a command.**
//
// A command job runs unattended without going through the tool permission gate — writing it into
// the config file IS the approval, the same way an `allow` rule is. That reasoning only holds
// while a person is the one who wrote it: if this tool could write one, a model could grant itself
// an unattended shell, every night, on a schedule nobody read.
//
// Refused with the reason and the other road, and — this is the half that matters — the change
// never reaches the engine. A refusal that still wrote the job would be worse than no check.
func TestTheAgentCannotScheduleACommand(t *testing.T) {
	var seen []port.ScheduleChange
	env := port.ToolEnv{Schedule: func(c port.ScheduleChange) (string, error) {
		seen = append(seen, c)
		return "written", nil
	}}
	res, err := Schedule{}.Execute(context.Background(),
		json.RawMessage(`{"action":"set","name":"nightly","schedule":"@daily","command":"make test"}`), env)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("scheduling a command was allowed: %+v", res)
	}
	if len(seen) != 0 {
		t.Fatalf("refused and written anyway: %+v", seen)
	}
	var text string
	_ = json.Unmarshal(res.Content, &text)
	if !strings.Contains(text, "person") || !strings.Contains(text, "config.toml") {
		t.Errorf("the refusal must say why and where to put it instead, got %q", text)
	}
	// And a prompt job still goes through — the check is about the field, not about the tool.
	if _, err := (Schedule{}).Execute(context.Background(),
		json.RawMessage(`{"action":"set","name":"nightly","schedule":"@daily","prompt":"read the commits"}`),
		env); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Prompt != "read the commits" {
		t.Fatalf("a prompt job must still reach the engine, got %+v", seen)
	}
}
