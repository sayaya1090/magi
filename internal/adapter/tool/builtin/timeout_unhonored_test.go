package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A `timeout` magi could not use as given is a part of the call it did not honor, and it used to be
// reported only when the deadline happened to fire — timedOutNote is reached from the
// DeadlineExceeded branch alone. A command that finished in time left its caller believing the
// limit it asked for was in force.
//
// Measured: `timeout:-5` on a command that ran 0.1s came back with no mention of the -5 at all.
func TestBashReportsATimeoutItCouldNotUse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	run := func(args map[string]any) string {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := Bash{}.Execute(context.Background(), b, port.ToolEnv{Workdir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			s = string(res.Content)
		}
		return s
	}

	// Negative: not a usable duration. The command SUCCEEDS, so nothing else would ever mention it.
	out := run(map[string]any{"command": "echo done", "timeout": -5})
	if !strings.Contains(out, "done") {
		t.Fatalf("the command still runs: %s", out)
	}
	for _, want := range []string{"`timeout` of -5s is not a usable duration",
		fmt.Sprintf("default %ds applied", defaultBashTimeout), "not by what you asked for"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}

	// Over the cap: clamped, and the caller is told which number actually bounded the command.
	out = run(map[string]any{"command": "echo done", "timeout": 9999})
	for _, want := range []string{"capped at the 600s maximum", "bounded by 600s"} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in:\n%s", want, out)
		}
	}

	// A usable timeout is used as given — nothing to report.
	if out := run(map[string]any{"command": "echo done", "timeout": 30}); strings.Contains(out, "not by what you asked for") {
		t.Errorf("30s was honored, so nothing may say otherwise:\n%s", out)
	}
	// Absent decodes to 0 through a plain int field and cannot be told apart from an explicit 0,
	// so neither is reported — inventing a complaint about a value the caller may never have typed
	// is the same fault in the other direction.
	if out := run(map[string]any{"command": "echo done"}); strings.Contains(out, "`timeout`") {
		t.Errorf("no timeout was given, so none may be discussed:\n%s", out)
	}
	if out := run(map[string]any{"command": "echo done", "timeout": 0}); strings.Contains(out, "not a usable duration") {
		t.Errorf("an explicit 0 is indistinguishable from absent:\n%s", out)
	}
}

// When the deadline DOES fire, timedOutNote already names the limit's origin — two sentences about
// one number is one too many.
func TestAnUnusableTimeoutIsExplainedOnceWhenItAlsoTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	// timedOutNote is the one that speaks on the deadline path; check its wording covers the
	// negative case, so suppressing the other note loses nothing.
	got := timedOutNote(defaultBashTimeout, -5)
	for _, want := range []string{"-5s is not a usable duration", "KILLED at that mark"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in the timeout note:\n%s", want, got)
		}
	}
	got = timedOutNote(maxBashTimeout, 9999)
	if !strings.Contains(got, "capped at the 600s maximum") {
		t.Errorf("the cap is named on the deadline path too:\n%s", got)
	}
}
