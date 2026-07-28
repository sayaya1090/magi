package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// handoffFacts must pull the HANDOFF section (the last weighted section of a rendered worker report)
// verbatim, and return "" when the finding carries none.
func TestHandoffFacts(t *testing.T) {
	finding := "### make dump (delegated to worker)\n" + reportStatusPrefix + "DONE\nDownloaded the archive.\n\n" +
		"EVIDENCE: sha256 ok\n\nHANDOFF: archive at /app/data/dump_2026.tar.gz (untar into /app/work)"
	got := handoffFacts(finding)
	want := "archive at /app/data/dump_2026.tar.gz (untar into /app/work)"
	if got != want {
		t.Errorf("handoffFacts = %q; want %q", got, want)
	}
	if h := handoffFacts("### x (delegated to worker)\n" + reportStatusPrefix + "DONE\njust prose, no handoff"); h != "" {
		t.Errorf("handoffFacts with no HANDOFF section = %q; want empty", h)
	}
}

// renderLedger must emit a verbatim block naming each step and its produced paths, and nothing for
// an empty ledger.
func TestLedgerCarriesWhatTheWorkerWroteWhenItFiledNoHandoff(t *testing.T) {
	app := newShellApp(t, &shellPlatform{})
	ctx := context.Background()
	wd := t.TempDir()
	if _, err := app.CreateSession(ctx, command.CreateSession{Workdir: wd}); err != nil {
		t.Fatal(err)
	}
	spawn := func(id, parent session.SessionID) {
		t.Helper()
		app.mu.Lock()
		app.stateLocked(id).meta = session.Session{ID: id, Workdir: wd, Agent: "worker", Parent: parent}
		app.mu.Unlock()
		cd, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "worker", Parent: string(parent)})
		if aerr := app.appendFact(ctx, id, event.TypeSessionCreated, event.Actor{Kind: event.ActorAgent, ID: "worker"}, cd); aerr != nil {
			t.Fatal(aerr)
		}
	}
	const kid, grandkid, empty = session.SessionID("s_kid"), session.SessionID("s_grand"), session.SessionID("s_empty")
	spawn(kid, "")
	spawn(grandkid, kid)
	spawn(empty, "")

	call := func(sid session.SessionID, name, args string) {
		t.Helper()
		if _, aerr := app.store.Append(ctx, sid, toolCallEvent(name, args)); aerr != nil {
			t.Fatal(aerr)
		}
	}
	call(kid, "write", `{"path":"/app/docs/hacking_summary.txt","content":"Build Process Summary\n"}`)
	call(grandkid, "bash", `{"command":"echo 'ok' > /app/docs/notes.md"}`)
	// A read is not a production: the ledger must not advertise a file the step only looked at.
	call(kid, "read", `{"path":"/app/ocaml/HACKING.adoc"}`)

	const narration = "STATUS: DONE\nI have read the HACKING.adoc file. Now I will create the summary of the"
	facts := handoffFacts(withObservedHandoff(ctx, app, kid, narration))
	for _, want := range []string{"/app/docs/hacking_summary.txt", "/app/docs/notes.md"} {
		if !strings.Contains(facts, want) {
			t.Errorf("a path the worker's subtree wrote must reach the ledger: %q missing from %q", want, facts)
		}
	}
	if strings.Contains(facts, "HACKING.adoc") {
		t.Errorf("a file only read is not a deliverable, got %q", facts)
	}
	if got := withObservedHandoff(ctx, app, kid, narration); !strings.HasPrefix(got, narration) {
		t.Errorf("the worker's own report must survive intact, got %q", got)
	}

	// A worker that filed its own HANDOFF said what its paths MEAN. That wins: a list cannot.
	own := narration + "\nHANDOFF: /app/docs/hacking_summary.txt — section map, keyed by build target"
	if out := withObservedHandoff(ctx, app, kid, own); out != own {
		t.Errorf("an authored handoff must be left alone, got %q", out)
	}
	// A step that produced no file has nothing observed to carry, and the report is all there is.
	if out := withObservedHandoff(ctx, app, empty, narration); out != narration {
		t.Errorf("with no writes there is nothing to append, got %q", out)
	}
	// No child session at all (the solo path) must not reach into the store for one.
	if out := withObservedHandoff(ctx, app, "", narration); out != narration {
		t.Errorf("an empty session id must be a no-op, got %q", out)
	}
}
