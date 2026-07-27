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
	var capture string
	ents, err := os.ReadDir(logs)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.Name() != "index.tsv" {
			capture = filepath.Join(logs, e.Name())
		}
	}
	if capture == "" {
		t.Fatalf("no capture file among %v", ents)
	}
	body, err := os.ReadFile(capture)
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
	// Nothing may land in the workspace by ACCIDENT: that tree is the deliverable.
	if ents, err := os.ReadDir(wd); err != nil || len(ents) != 0 {
		t.Errorf("the workspace must stay clean, got %v", ents)
	}
	// …and everything the work is FOR still lands there and stays there. Only TMPDIR moved: the
	// working directory is untouched, so a relative path is the workspace exactly as before, and a
	// file the deliverable needs is not swept away with the scratch.
	args, _ = json.Marshal(map[string]any{"command": `printf 'keep me' > deliverable.txt`})
	if _, err := (Bash{}).Execute(context.Background(), args, port.ToolEnv{Workdir: wd, ScratchTmp: tmp}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(wd, "deliverable.txt"))
	if err != nil || string(body) != "keep me" {
		t.Fatalf("a relative write must land in the workspace and stay: %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "deliverable.txt")); err == nil {
		t.Error("the deliverable must NOT have been diverted into the scratch")
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

// The index is what makes the scratch legible: `ls` shows a pile of logs, and this says which is
// which without opening any of them. It is also the turn's execution history in a form that
// survives context compaction, which the conversation does not.
func TestRunIndexRecordsWhatRanAndWhereItWent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics")
	}
	logs, wd := t.TempDir(), t.TempDir()
	for _, cmd := range []string{"printf 'a\\n'", "sh -c 'exit 3'"} {
		args, _ := json.Marshal(map[string]any{"command": cmd})
		if _, err := (Bash{}).Execute(context.Background(), args, port.ToolEnv{Workdir: wd, ScratchLogs: logs}); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := os.ReadFile(filepath.Join(logs, "index.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(idx)), "\n")
	if len(rows) != 2 {
		t.Fatalf("one row per command, got %d:\n%s", len(rows), idx)
	}
	for i, want := range []struct{ exit, cmd string }{{"0", "printf"}, {"3", "exit 3"}} {
		f := strings.Split(rows[i], "\t")
		if len(f) != 4 {
			t.Fatalf("row %d has %d fields, want 4: %q", i, len(f), rows[i])
		}
		if f[1] != want.exit {
			t.Errorf("row %d exit = %s, want %s", i, f[1], want.exit)
		}
		if !strings.Contains(f[3], want.cmd) {
			t.Errorf("row %d must name what ran, got %q", i, f[3])
		}
		if _, err := os.Stat(filepath.Join(logs, f[2])); err != nil {
			t.Errorf("row %d must point at a real capture file (%s): %v", i, f[2], err)
		}
	}
	// A command whose text spans lines must not produce a row that spans lines.
	args, _ := json.Marshal(map[string]any{"command": "printf 'x'\necho done"})
	(Bash{}).Execute(context.Background(), args, port.ToolEnv{Workdir: wd, ScratchLogs: logs})
	idx, _ = os.ReadFile(filepath.Join(logs, "index.tsv"))
	if n := len(strings.Split(strings.TrimSpace(string(idx)), "\n")); n != 3 {
		t.Errorf("a multi-line command must still be one row, got %d rows:\n%s", n, idx)
	}
	// No scratch, no index — and no error either.
	appendRunIndex("", 0, "x", "")
}
