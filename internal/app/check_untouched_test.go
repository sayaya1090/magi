package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// `absent` is satisfied by a file being left alone, so a pass on a file nothing in the run touched
// is about the file's history rather than about the step — it would have read the same before the
// run started. Observed live: `absent /next\s*=\s*NULL/` on `ocaml/runtime/major_gc.c`, passing
// while the work went into `runtime/shared_heap.c` and that file was never opened.
func TestAbsentOnAnUntouchedFileYieldsNoVerdict(t *testing.T) {
	skipOnWindows(t)
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	for _, n := range []string{"untouched.c", "fixed.c"} {
		if err := os.WriteFile(filepath.Join(wd, n), []byte("int main(void) { return 0; }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	// The worker runs in its own session — that is where the edit is recorded.
	const kid = session.SessionID("s_worker")
	app.mu.Lock()
	app.stateLocked(kid).meta = session.Session{ID: kid, Workdir: wd, Agent: "worker", Parent: main}
	app.mu.Unlock()
	cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "worker", Parent: string(main)})
	if aerr := app.appendFact(ctx, kid, event.TypeSessionCreated, event.Actor{Kind: event.ActorAgent, ID: "worker"}, cd); aerr != nil {
		t.Fatal(aerr)
	}
	if _, aerr := app.store.Append(ctx, kid, toolCallEvent("edit",
		`{"path":"`+filepath.ToSlash(filepath.Join(wd, "fixed.c"))+`","old":"return 0;","new":"return 1;"}`)); aerr != nil {
		t.Fatal(aerr)
	}

	absent := func(src string) council.DeliverableCheck {
		return council.DeliverableCheck{Step: "1", Deliverable: "bug removed", Source: src,
			Assert: `absent /next\s*=\s*NULL/`}
	}

	// The file the step actually changed: an ordinary pass.
	if out, code := app.runCheck(ctx, main, wd, absent("fixed.c")); code != 0 {
		t.Fatalf("absent on a file the step changed must pass: code=%d out=%s", code, out)
	}
	// A file nothing in the run wrote to: no verdict.
	out, code := app.runCheck(ctx, main, wd, absent("untouched.c"))
	if code != 126 {
		t.Fatalf("absent on an untouched file proves nothing: code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "nothing in this run touched it") {
		t.Errorf("the verdict must say why: %s", out)
	}
	// The flag restores the previous behavior exactly.
	t.Setenv("MAGI_CHECK_UNTOUCHED", "0")
	if _, code := app.runCheck(ctx, main, wd, absent("untouched.c")); code != 0 {
		t.Errorf("MAGI_CHECK_UNTOUCHED=0 must restore the plain pass, got %d", code)
	}
}

// pathTouched asks a broader question than the provenance audit's: not who composed the bytes, but
// whether anything changed the file at all. A bash redirect counts, and it is read out of the
// command by the same helper the self-revert check uses so the two agree on what a bash write is.
func TestPathTouchedSeesEveryWritingShape(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	main, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []event.Event{
		toolCallEvent("write", `{"path":"/app/a.txt","content":"x"}`),
		toolCallEvent("bash", `{"command":"make world > /app/build.log 2>&1"}`),
		toolCallEvent("read", `{"path":"/app/readonly.txt"}`),
		toolCallEvent("bash", `{"command":"grep -n foo /app/scanned.c"}`),
	} {
		if _, aerr := app.store.Append(ctx, main, ev); aerr != nil {
			t.Fatal(aerr)
		}
	}
	for _, hit := range []string{"a.txt", "build.log"} {
		if !app.pathTouched(ctx, main, hit) {
			t.Errorf("%s was written and must read as touched", hit)
		}
	}
	for _, miss := range []string{"readonly.txt", "scanned.c", "never.txt"} {
		if app.pathTouched(ctx, main, miss) {
			t.Errorf("%s was only read, not written", miss)
		}
	}
}
