package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// The self-edit check reads a bash command's destination back the moment executeTool returns. For a
// FOREGROUND command that is after it ran; for a BACKGROUND one it is the moment it was launched,
// so the read returns what was there before and the two sides match by construction.
//
// Observed live: `rm /app/run_test_interrupt.py && python3 run.py &` came back carrying "this write
// left the file byte-for-byte as it already was — nothing changed" about a command whose first act
// was to delete that file. It did delete it, a moment later.
func TestABackgroundCommandGetsNoSelfEditVerdict(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	dir := t.TempDir()
	path := filepath.Join(dir, "scratch_test.py")
	if err := os.WriteFile(path, []byte("print('probe')\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The file is still on disk when the after-read happens — exactly the live race.
	run := func(background bool, cmd string) string {
		args := `{"command":` + strconv.Quote(cmd) + `}`
		if background {
			args = `{"command":` + strconv.Quote(cmd) + `,"background":"true"}`
		}
		tc := &session.ToolCall{CallID: "c", Name: "bash", Args: json.RawMessage(args)}
		res := session.ToolResult{CallID: "c", Content: json.RawMessage(`"started"`)}
		a.noteToolOutcome(sid, newRunGuard(nil), toolOutcome{
			tc: tc, res: &res, workdir: dir, fp: "fp", novel: true, toolOK: true,
			bashChanges: []bashChange{{path: path, before: "print('probe')\n", readable: true}},
		})
		return string(res.Content)
	}

	if got := run(true, "rm scratch_test.py && python3 run.py"); strings.Contains(got, "self-edit check") {
		t.Errorf("a launched-but-not-yet-run command proves nothing about the file:\n%s", got)
	}
	// The control: run in the foreground, the same reading is earned — the command HAS run, so a
	// file that still holds its old bytes really was left alone.
	if got := run(false, "rm scratch_test.py; echo done"); !strings.Contains(got, "self-edit check") {
		t.Errorf("a finished command's destination is comparable and must still be reported:\n%s", got)
	}
}
