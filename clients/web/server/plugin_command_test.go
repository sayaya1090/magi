package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// commandEngine is a companion whose plugins contribute two slash commands.
type commandEngine struct{ *recordingEngine }

func (e commandEngine) PluginCommandList() []port.PluginCommandInfo {
	return []port.PluginCommandInfo{{Name: "routine-draft"}, {Name: "routine-adopt"}}
}

func (e commandEngine) RunPluginCommand(_ context.Context, _ string, name string, args []string) (string, bool, error) {
	_ = e.note("command:" + name + " " + strings.Join(args, ","))
	if name == "routine-draft" {
		return "", true, errors.New("call #8 has no verified sequential order")
	}
	return "[dummy-plug] [routine] adopted v2", true, nil
}

// A plugin's slash command typed into the console used to reach the model as a prompt. /submit now
// runs a first word the companion lists as a command — refused while a turn runs, as the TUI does —
// and answers with what the command logged (200) or its refusal (422). Any other line is said.
func TestTheConsoleRunsAPluginCommandInsteadOfSayingIt(t *testing.T) {
	f := newFleetFixture(t)
	eng := commandEngine{&recordingEngine{}}
	sock := f.liveDaemon(t, shortTempDir(t), "api", eng)
	list := get(t, f.srv.pluginCommands, "/plugin-commands?d="+url.QueryEscape(sock))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"routine-draft"`) || len(eng.seen()) != 0 {
		t.Fatalf("command list: %d %s, actions = %v", list.Code, list.Body, eng.seen())
	}

	submit := func(text string) (int, string) {
		w := post(t, f.srv, f.srv.submit, "/submit?d="+url.QueryEscape(sock), url.Values{"text": {text}})
		return w.Code, strings.TrimSpace(w.Body.String())
	}
	if code, body := submit("/routine-draft probe 8-9"); code != http.StatusUnprocessableEntity || !strings.Contains(body, "call #8 has no verified sequential order") {
		t.Fatalf("refused command: %d %q", code, body)
	}
	if code, body := submit("/routine-adopt rpe v2"); code != http.StatusOK || body != "[dummy-plug] [routine] adopted v2" {
		t.Fatalf("command: %d %q", code, body)
	}
	if code, body := submit("/nope x"); code != http.StatusNoContent {
		t.Fatalf("not a command: %d %q", code, body)
	}
	want := []string{"command:routine-draft probe,8-9", "command:routine-adopt rpe,v2", "steer:/nope x"}
	if got := eng.seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("the daemon was told %v, want %v", got, want)
	}
	eng.mu.Lock()
	eng.doing = "running bash"
	eng.mu.Unlock()
	if code, body := submit("/routine-adopt x"); code != http.StatusConflict || !strings.Contains(body, "cannot run while a turn is working") {
		t.Fatalf("during a turn: %d %q", code, body)
	}
	if got := eng.seen(); len(got) != len(want) {
		t.Fatalf("a command ran during a turn: %v", got)
	}
}
