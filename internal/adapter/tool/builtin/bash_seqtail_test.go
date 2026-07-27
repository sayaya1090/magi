package builtin

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A `;` list reports only its LAST command's status, so a trailing reporter/truncator masks the
// build exactly as `| tail` does. `&&` must NOT be flagged: there the tail runs only when the
// primary succeeded, so a failure still surfaces its own exit code.
func TestSequencedTailNote(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit int
		cmd  string
		want bool
	}{
		// The live form: log capture + exit capture, both swallowed by the final segment.
		{"build ; echo exit", 0, `make world > /tmp/build.log 2>&1; echo "exit=$?" >> /tmp/build.log`, true},
		{"build ; echo ; tail", 0, `make world > /tmp/b.log 2>&1; echo "exit=$?" >> /tmp/b.log; tail -30 /tmp/b.log`, true},
		{"test ; cat log", 0, "pytest > /tmp/t.log 2>&1; cat /tmp/t.log", true},
		{"build ; true", 0, "make world; true", true},
		{"build ; :", 0, "make world; :", true},
		{"build ; head", 0, "cargo build 2> /tmp/e.log; head -20 /tmp/e.log", true},
		// Ungated, so the shape is what decides — including on a command nobody called a check.
		{"cd ; echo", 0, "cd /app; echo hi", true},
		// && is control flow, not masking — a failed primary short-circuits and keeps its exit.
		{"build && echo ok", 0, "make world && echo ok", false},
		// A real command after the reporter means the exit is that command's, not the reporter's.
		{"echo then real cmd", 0, "make world; echo done; ./run-tests", false},
		// Nothing to mask, or the exit already speaks.
		{"plain build", 0, "make world", false},
		{"non-zero exit", 2, `make world > /tmp/b.log 2>&1; echo "exit=$?" >> /tmp/b.log`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			note := sequencedTailNote(tc.exit, tc.cmd)
			if (note != "") != tc.want {
				t.Errorf("sequencedTailNote(%d, %q) fired=%v, want %v", tc.exit, tc.cmd, note != "", tc.want)
			}
			if tc.want && !strings.Contains(note, "the last `;` segment") {
				t.Errorf("the note must say whose exit code this is: %s", note)
			}
		})
	}
}

// ExitCodeMasked is the guard-facing form of the same judgement, reading the COMMAND ONLY. It and
// the notes now agree on everything, which is the point: whose exit code this is does not depend on
// what the caller said the command was for. Gating it on verify once meant one optional field could
// switch magi's churn accounting off — observed as a build sent with verify=false whose trailing
// echo's exit 0 was booked as the build converging.
func TestExitCodeMasked(t *testing.T) {
	for _, tc := range []struct {
		cmd  string
		want bool
	}{
		{"make world || true", true},
		{"make world || echo failed", true},
		{"make world 2>&1 | tail -50", true},
		{`make world > /tmp/b.log 2>&1; echo "exit=$?" >> /tmp/b.log`, true},
		// The live specimen: the same masked build, submitted as NOT a verification.
		{`make -j 4 > /tmp/build1.log 2>&1; echo "build exit=$?" >> /tmp/build1.log`, true},
		// A plain build's exit is its own — this must never be called masked.
		{"make world", false},
		{"cd /app && make world", false},
		{"pytest -q", false},
		{"go test ./... > /tmp/t.log 2>&1", false},
		// `&&` is not a mask: the tail runs only if the primary succeeded, so a failure still
		// surfaces its own non-zero exit.
		{"make world && echo done", false},
	} {
		if got := ExitCodeMasked(tc.cmd); got != tc.want {
			t.Errorf("ExitCodeMasked(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

// End-to-end: the `;`-masked verification the bench produced must come back annotated, so the
// exit 0 is not the only thing the model (or the council reading the [ok] result) has to go on.
func TestBashExecuteAnnotatesSequencedTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics")
	}
	dir := t.TempDir()
	args, _ := json.Marshal(map[string]any{
		"command": `false > /dev/null 2>&1; echo "exit=$?" > /dev/null`,
		"verify":  true,
	})
	res, err := Bash{}.Execute(context.Background(), args, port.ToolEnv{Workdir: dir, SessionID: session.SessionID("s-seqtail")})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("expected the trailing echo's exit 0, got an error result: %s", res.Content)
	}
	if !strings.Contains(string(res.Content), "the last `;` segment") {
		t.Errorf("masked `;` verification was not annotated: %s", res.Content)
	}
}

// The timeout line must name whose limit it was and that the command was killed rather than
// judged — a model that set the limit itself otherwise reads the kill as a verdict on its work.
func TestTimedOutNote(t *testing.T) {
	caller := timedOutNote(10, 10)
	if !strings.Contains(caller, "your own `timeout` argument") {
		t.Errorf("caller-set limit not attributed: %s", caller)
	}
	for _, want := range []string{"timed out after 10s", "KILLED", "NOT evidence it failed", "up to 600s", "background:true"} {
		if !strings.Contains(caller, want) {
			t.Errorf("timedOutNote missing %q: %s", want, caller)
		}
	}
	if def := timedOutNote(120, 0); !strings.Contains(def, "the default limit (no `timeout` given)") {
		t.Errorf("default limit not attributed: %s", def)
	}
	if capped := timedOutNote(600, 3600); !strings.Contains(capped, "your `timeout` of 3600s capped at the 600s maximum") {
		t.Errorf("clamped limit not attributed: %s", capped)
	}
}
