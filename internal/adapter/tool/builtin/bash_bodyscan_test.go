package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// maskedFailureNote fires only when exit==0 AND the body carries a high-precision
// crash/traceback signature — the fingerprint of a `|| echo`/`|| true`-masked failure.
func TestMaskedFailureNote(t *testing.T) {
	goPanic := "some output\npanic: runtime error\n\ngoroutine 1 [running]:\nmain.main()"
	goFatal := "fatal error: concurrent map writes\n\ngoroutine 7 [running]:"
	for _, tc := range []struct {
		name string
		exit int
		body string
		want bool
	}{
		{"python traceback, exit 0", 0, "exit 0\nTraceback (most recent call last):\n  File x", true},
		{"jvm exception, exit 0", 0, "Exception in thread \"main\" java.lang.NullPointerException", true},
		{"go panic + goroutine, exit 0", 0, goPanic, true},
		{"go fatal + goroutine, exit 0", 0, goFatal, true},
		// Non-zero already surfaces as an error — the note would be redundant noise.
		{"python traceback, exit 1", 1, "Traceback (most recent call last):", false},
		// "panic:" as incidental data with no goroutine dump must not be flagged.
		{"bare panic word, exit 0", 0, "echo 'panic: do not panic'", false},
		{"fatal error compiler diag, exit 0", 0, "main.c:3:10: fatal error: stdio.h: No such file", false},
		{"clean output, exit 0", 0, "exit 0\nall tests passed\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := maskedFailureNote(tc.exit, tc.body) != ""
			if got != tc.want {
				t.Errorf("maskedFailureNote(exit=%d) = %v, want %v", tc.exit, got, tc.want)
			}
		})
	}
}

// The default is ON; only an explicit off-value disables the scan.
func TestBodyscanEnabledDefault(t *testing.T) {
	if !bodyscanEnabled() {
		t.Fatal("default must be ON")
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_EXITCODE_BODYSCAN", v)
		if bodyscanEnabled() {
			t.Errorf("%q must disable the scan", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "yes", "whatever"} {
		t.Setenv("MAGI_EXITCODE_BODYSCAN", v)
		if !bodyscanEnabled() {
			t.Errorf("%q must leave the scan ON", v)
		}
	}
}

// The note carries the actionable hint (masked exit code) so the model and council both
// see why a clean-looking exit 0 is suspect.
func TestMaskedFailureNoteContent(t *testing.T) {
	note := maskedFailureNote(0, "Traceback (most recent call last):")
	if !strings.Contains(note, "exit 0") || !strings.Contains(note, "masked") {
		t.Errorf("note should explain the masked exit code, got %q", note)
	}
}

// End-to-end wiring: a `|| true`-masked crash gets the note directly after the status
// line (the head position both the model and the council's head-clip read); the off
// flag reproduces the un-annotated baseline; a genuine non-zero exit stays note-free.
func TestBashExecuteAnnotatesMaskedFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-specific masking idiom")
	}
	env := port.ToolEnv{Workdir: t.TempDir()}
	crash := `{"command":"printf 'Traceback (most recent call last):\\n  File x\\n'; false || true"}`

	r, _ := Bash{}.Execute(context.Background(), json.RawMessage(crash), env)
	out := resultText(t, r)
	if r.IsError {
		t.Fatalf("annotation must not reclassify: %s", out)
	}
	if !strings.HasPrefix(out, "exit 0\n[note: exit 0") {
		t.Errorf("note must sit right after the status line, got %q", out[:min(len(out), 120)])
	}

	t.Setenv("MAGI_EXITCODE_BODYSCAN", "off")
	r, _ = Bash{}.Execute(context.Background(), json.RawMessage(crash), env)
	if out := resultText(t, r); strings.Contains(out, "[note:") {
		t.Errorf("off flag must reproduce the un-annotated baseline, got %q", out)
	}
	t.Setenv("MAGI_EXITCODE_BODYSCAN", "")

	r, _ = Bash{}.Execute(context.Background(),
		json.RawMessage(`{"command":"printf 'Traceback (most recent call last):\\n'; exit 3"}`), env)
	out = resultText(t, r)
	if !r.IsError || strings.Contains(out, "[note:") {
		t.Errorf("non-zero exit already speaks for itself — no note, IsError=true; got IsError=%v %q", r.IsError, out)
	}
}
