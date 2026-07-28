package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The live specimen: the agent verified its own fix with a pipeline whose exit belongs to `tail`,
// magi said so, and the fresh `tee` replaced the previous build's log with one that had not reached
// a crash yet. `absent (Segmentation fault|…)` then passed — on how far the log got, not on the
// build.
func TestAbsentOnAnUnfinishedRunYieldsNoVerdict(t *testing.T) {
	skipOnWindows(t)
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	log := filepath.Join(wd, "build.log")
	if err := os.WriteFile(log, []byte("  CC runtime/memory.npic.o\n  CC runtime/meta.npic.o\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sid, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	record := func(cmd, result string) {
		t.Helper()
		call, res := bashCallWithResult(cmd, result)
		if _, aerr := app.store.Append(ctx, sid, call); aerr != nil {
			t.Fatal(aerr)
		}
		if _, aerr := app.store.Append(ctx, sid, res); aerr != nil {
			t.Fatal(aerr)
		}
	}
	check := council.DeliverableCheck{Step: "6", Deliverable: "bootstrap completes without crash",
		Source: log, Assert: `absent (Segmentation fault|Fatal error)`}

	// The exit belongs to `tail`, so magi never learned whether make finished.
	record("cd /app/ocaml && make -j 4 world opt 2>&1 | tee "+filepath.ToSlash(log)+" | tail -50", "exit 0\nCC runtime/memory.npic.o\n")
	out, code := app.runCheck(ctx, sid, wd, check)
	if code != 126 {
		t.Fatalf("absent on a log whose command was never confirmed to finish: code=%d out=%s", code, out)
	}
	for _, want := range []string{"belongs to its", "never learned whether that command finished"} {
		if !strings.Contains(out, want) {
			t.Errorf("the verdict must name why: %q missing from %q", want, out)
		}
	}

	// A LATER clean run of the same build replaces it: newest wins, and the check stands again.
	record("cd /app/ocaml && make -j 4 world opt > "+filepath.ToSlash(log)+" 2>&1", "exit 0\n")
	if out, code := app.runCheck(ctx, sid, wd, check); code != 0 {
		t.Fatalf("a clean exit 0 must leave the check standing: code=%d out=%s", code, out)
	}

	// A non-zero exit is a log of a run that did not succeed.
	record("cd /app/ocaml && make world opt > "+filepath.ToSlash(log)+" 2>&1", "exit 2\n")
	if out, code := app.runCheck(ctx, sid, wd, check); code != 126 || !strings.Contains(out, "exited 2") {
		t.Errorf("a failed producing command must yield no verdict: code=%d out=%s", code, out)
	}

	// A timeout: the log stops where the deadline cut it.
	record("cd /app/ocaml && make world opt > "+filepath.ToSlash(log)+" 2>&1", "[timed out after 300s]\n")
	if out, code := app.runCheck(ctx, sid, wd, check); code != 126 || !strings.Contains(out, "TIMED OUT") {
		t.Errorf("a timed-out producing command must yield no verdict: code=%d out=%s", code, out)
	}

	// The flag restores the previous behavior exactly.
	t.Setenv("MAGI_CHECK_UNTOUCHED", "0")
	if _, code := app.runCheck(ctx, sid, wd, check); code != 0 {
		t.Errorf("MAGI_CHECK_UNTOUCHED=0 must restore the plain pass, got %d", code)
	}
}

func TestExitOfBashResult(t *testing.T) {
	for _, tc := range []struct {
		res  string
		want int
		ok   bool
	}{
		{"exit 0\noutput: /tmp/x.log", 0, true},
		{"exit 137\n", 137, true},
		{`"exit 2\nboom"`, 2, true},
		{"started background command bg_1", 0, false}, // a start is not an exit
		{"", 0, false},
	} {
		got, ok := exitOfBashResult(tc.res)
		if got != tc.want || ok != tc.ok {
			t.Errorf("exitOfBashResult(%q) = (%d,%v); want (%d,%v)", tc.res, got, ok, tc.want, tc.ok)
		}
	}
}

// bashCallWithResult builds a paired bash tool-call and its result, with a distinct call id per
// pair so lastBashProducing matches the right one — the pairing IS what this gate reads.
func bashCallWithResult(cmd, result string) (event.Event, event.Event) {
	id := "c_" + strconv.Itoa(len(cmd)+len(result))
	cd, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: id, Name: "bash", Args: jsonOf(map[string]string{"command": cmd}),
		}},
	})
	rd, _ := json.Marshal(event.PartAppendedData{
		Role: session.RoleUser,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: id, Content: jsonOf(result),
		}},
	})
	now := time.Now()
	return event.Event{Type: event.TypePartAppended, TS: now, Data: cd},
		event.Event{Type: event.TypePartAppended, TS: now, Data: rd}
}

func jsonOf(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
