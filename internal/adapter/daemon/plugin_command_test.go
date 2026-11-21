package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A plugin's slash commands (/routine-draft, …) ran only in the TUI: the IDE and web clients sent
// them to the model as an ordinary prompt. These doors give every client the TUI's path — the list
// of commands a composer may route, and running one, with what the command logged as its answer.
type commandEngine struct {
	*fakeEngine
	ran []string
}

func (e *commandEngine) PluginCommandList() []port.PluginCommandInfo {
	return []port.PluginCommandInfo{{Name: "routine-draft", Description: "draft a routine"}, {Name: "routine-adopt", Description: "adopt"}}
}

func (e *commandEngine) RunPluginCommand(_ context.Context, session, name string, args []string) (string, bool, error) {
	e.ran = append(e.ran, session+" "+name+" "+strings.Join(args, ","))
	switch name {
	case "routine-draft":
		return "", true, errors.New("call #8 has no verified sequential order")
	case "routine-adopt":
		return "[dummy-plug] [routine] adopted v2", true, nil
	}
	return "", false, nil
}

func TestPluginCommandDoorsListAndRunWithTheCommandsOwnAnswer(t *testing.T) {
	e := &commandEngine{fakeEngine: &fakeEngine{}}
	list := answers["commands"].run(context.Background(), e, Request{})
	if !list.OK || len(list.Commands) != 2 || list.Commands[0].Name != "routine-draft" {
		t.Fatalf("list: %+v", list)
	}
	ok := answers["command-run"].run(context.Background(), e, Request{Session: "s1", Name: "/routine-adopt", Text: "rpe v2"})
	if !ok.OK || ok.Out != "[dummy-plug] [routine] adopted v2" {
		t.Fatalf("a command's log is its answer: %+v", ok)
	}
	if e.ran[0] != "s1 routine-adopt rpe,v2" {
		t.Fatalf("leading slash dropped and the rest split into arguments: %v", e.ran)
	}
	failed := answers["command-run"].run(context.Background(), e, Request{Session: "s1", Name: "routine-draft", Text: "probe 8-9"})
	if failed.OK || failed.Err != "call #8 has no verified sequential order" {
		t.Fatalf("the command's own refusal is the answer: %+v", failed)
	}
	unknown := answers["command-run"].run(context.Background(), e, Request{Session: "s1", Name: "nope", Text: "x"})
	if unknown.OK || unknown.Err != "unknown command /nope" {
		t.Fatalf("unknown: %+v", unknown)
	}
	if r := answers["command-run"].run(context.Background(), &fakeEngine{}, Request{Name: "routine-draft"}); r.OK {
		t.Fatal("an engine without plugin commands accepted one")
	}
	// The command reads and writes the conversation it was typed in, so that conversation is named
	// on the request; and whether its turn runs is decided here, where the turn lives, not only by a
	// client that looked a moment earlier.
	if r := answers["command-run"].run(context.Background(), e, Request{Name: "routine-adopt", Text: "x"}); r.OK || !strings.Contains(r.Err, "session") {
		t.Fatalf("a command with no target conversation ran: %+v", r)
	}
	before := len(e.ran)
	e.fakeEngine.mu.Lock()
	e.fakeEngine.doing = "running bash"
	e.fakeEngine.mu.Unlock()
	if r := answers["command-run"].run(context.Background(), e, Request{Session: "s1", Name: "routine-adopt", Text: "x"}); r.OK || !strings.Contains(r.Err, "while a turn is working") {
		t.Fatalf("a command ran during the conversation's turn: %+v", r)
	}
	if len(e.ran) != before {
		t.Fatalf("the engine was asked to run it anyway: %v", e.ran)
	}
	if !slices.Contains(capsOf(e), "plugin-commands") {
		t.Fatalf("not advertised: %v", capsOf(e))
	}
}
