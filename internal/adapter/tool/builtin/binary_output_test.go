package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The read tool refuses a binary file outright. bash cannot refuse — a command may emit bytes on
// purpose, and the capture file has to keep them — but it can stop handing the model a wall of them
// unlabelled, which is the same judgement applied where it can be applied.
//
// Observed live (sqlite-with-gcov, 2026-07-30): `cat /app/sqlite/bin/sqlite3 | head -5` put 9225
// bytes of ELF header into the context with nothing to say what it was.
func TestBashLabelsBinaryOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	run := func(cmd string) (string, bool) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"command": cmd})
		res, err := Bash{}.Execute(context.Background(), b, port.ToolEnv{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			s = string(res.Content)
		}
		return s, res.IsError
	}

	// A stream carrying NUL is not text.
	out, _ := run(`printf 'ELF\0\0\0binary here\0'`)
	if !strings.Contains(out, "this output is BINARY") {
		t.Fatalf("binary output must be labelled:\n%s", out)
	}
	for _, want := range []string{"contains NUL", "not\nreadable text", "xxd", "strings"} {
		if !strings.Contains(out, strings.ReplaceAll(want, "\n", " ")) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}
	// The bytes are still there — the note labels, it does not suppress.
	if !strings.Contains(out, "binary here") {
		t.Errorf("captured output is not dropped:\n%s", out)
	}

	// Ordinary text output says nothing about binary.
	out, _ = run(`echo hello; echo world`)
	if strings.Contains(out, "BINARY") {
		t.Errorf("text output carries no binary label:\n%s", out)
	}
	// Nor does an empty one.
	out, _ = run(`true`)
	if strings.Contains(out, "BINARY") {
		t.Errorf("an empty capture is not binary:\n%s", out)
	}
}

// The note states only what was measured, and names where the whole of it is.
func TestBinaryOutputNoteStatesWhatWasMeasured(t *testing.T) {
	got := binaryOutputNote(9225, "/tmp/logs/magi-bash-1.log")
	for _, want := range []string{"9225 bytes", "/tmp/logs/magi-bash-1.log", "file", "xxd", "od -c"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	// No capture file to point at → say so rather than name an empty path.
	if got := binaryOutputNote(12, ""); strings.Contains(got, "in .") || !strings.Contains(got, "the capture file") {
		t.Errorf("a missing capture path is described, not printed empty:\n%s", got)
	}
}
