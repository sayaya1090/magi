package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// magi already captured a command's output to a file and deleted it when the call returned, so the
// model composed `> log 2>&1` for a copy it could read later and `| tail -100` to bound what came
// back — and both cost it the exit code, because a redirect and a pipe move the status to something
// that is not the command. Keeping the file for the turn removes the reason to write either one,
// and the result has to NAME it or the file might as well not exist.
func TestBashKeepsItsOutputInTheTurnScratch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics")
	}
	logs := t.TempDir()
	wd := t.TempDir()
	args, _ := json.Marshal(map[string]any{"command": "printf 'line1\\nline2\\n'; printf 'err\\n' >&2"})
	res, err := Bash{}.Execute(context.Background(), args, port.ToolEnv{Workdir: wd, ScratchLogs: logs})
	if err != nil {
		t.Fatal(err)
	}
	out := resultText(t, res)
	if !strings.Contains(out, "output: "+logs) {
		t.Fatalf("the result must name the captured file:\n%s", out)
	}
	// …and the file must hold the real thing, both streams.
	ents, err := os.ReadDir(logs)
	if err != nil || len(ents) != 1 {
		t.Fatalf("expected exactly one capture file, got %v (%v)", ents, err)
	}
	body, err := os.ReadFile(filepath.Join(logs, ents[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"line1", "line2", "err"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("the capture must hold %q, got %q", want, body)
		}
	}

	// Without a scratch it behaves exactly as before: captured to a temp file, deleted, unnamed.
	res2, _ := Bash{}.Execute(context.Background(), args, port.ToolEnv{Workdir: wd})
	if strings.Contains(resultText(t, res2), "output: ") {
		t.Errorf("with no scratch the result must not name a file:\n%s", resultText(t, res2))
	}
}

// The other half of the scratch: TMPDIR. Everything that ASKS for a temp path — mktemp, python's
// tempfile, a compiler's intermediates — then writes outside the deliverable tree, with no model
// awareness at all. Observed without it: an agent copying its working files into the very workspace
// it was being graded on, left behind after the run.
func TestBashPointsTMPDIRAtTheTurnScratch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics")
	}
	tmp := t.TempDir()
	wd := t.TempDir()
	// $TMPDIR is what a well-behaved program reads; assert the wiring, not one tool's quirks (BSD
	// mktemp on macOS resolves its own temp dir and ignores the variable entirely).
	args, _ := json.Marshal(map[string]any{"command": `echo "seen=$TMPDIR"; printf 'x' > "$TMPDIR/probe.txt"`})
	res, err := Bash{}.Execute(context.Background(), args, port.ToolEnv{Workdir: wd, ScratchTmp: tmp})
	if err != nil {
		t.Fatal(err)
	}
	if out := resultText(t, res); !strings.Contains(out, "seen="+tmp) {
		t.Fatalf("the command must see TMPDIR pointing at the turn scratch, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(tmp, "probe.txt")); err != nil {
		t.Errorf("a temp write must land in the scratch: %v", err)
	}
	// Nothing may land in the workspace: that tree is the deliverable.
	if ents, err := os.ReadDir(wd); err != nil || len(ents) != 0 {
		t.Errorf("the workspace must stay clean, got %v", ents)
	}

	// With no scratch the command keeps the process's own TMPDIR — this can never be the reason a
	// command fails to start.
	if got := scratchEnv(""); got != nil {
		t.Errorf("no scratch must inherit the environment unchanged, got %d entries", len(got))
	}
	env := scratchEnv(tmp)
	var n int
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMPDIR=") {
			n++
			if kv != "TMPDIR="+tmp {
				t.Errorf("TMPDIR must be the scratch, got %q", kv)
			}
		}
	}
	if n != 1 {
		t.Errorf("TMPDIR must appear exactly once (the inherited one is replaced), got %d", n)
	}
}
