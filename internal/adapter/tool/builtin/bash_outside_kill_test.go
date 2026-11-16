//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func runBashText(t *testing.T, command string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"command": command, "timeout": 30})
	res, err := Bash{}.Execute(context.Background(), args,
		port.ToolEnv{Workdir: t.TempDir(), SessionID: session.SessionID("s-outside-kill")})
	if err != nil {
		t.Fatal(err)
	}
	var s string
	if json.Unmarshal(res.Content, &s) != nil {
		s = string(res.Content)
	}
	return s
}

// A process killed from outside magi must say so on every result, whatever it printed first.
// Observed 2026-11-16: a child killed by SIGKILL that had written output came back as a bare
// "exit 137" (the signal sentence was reached only by an EMPTY capture), and a shell killed itself
// came back "exit -1" with nothing at all — Go reports a signalled process as ExitCode -1.
func TestAnOutsideKillIsNamedWhateverTheCommandPrinted(t *testing.T) {
	for _, c := range []struct{ what, cmd string }{
		{"child killed, output before", "echo partial-A; sh -c 'kill -9 $$'"},
		{"child killed, no output of its own", "sh -c 'kill -9 $$'"},
		{"the shell itself killed", "echo partial-C; kill -9 $$"},
	} {
		got := runBashText(t, c.cmd)
		if !strings.Contains(got, "killed by signal 9") {
			t.Errorf("%s: the kill is not named:\n%s", c.what, got)
		}
		if !strings.Contains(got, "not by magi") {
			t.Errorf("%s: must say the kill came from outside magi:\n%s", c.what, got)
		}
	}
	if got := runBashText(t, "echo fine; exit 3"); strings.Contains(got, "killed by") {
		t.Errorf("an ordinary failing exit is not a kill:\n%s", got)
	}
}
