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
func TestRenderLedger(t *testing.T) {
	if s := renderLedger(nil); s != "" {
		t.Errorf("renderLedger(nil) = %q; want empty", s)
	}
	block := renderLedger([]ledgerEntry{
		{Step: "fetch data", Facts: "/app/data/in.csv"},
		{Step: "build proto", Facts: "/app/kv-store_pb2.py, /app/kv-store_pb2_grpc.py"},
	})
	for _, want := range []string{"SHARED DELIVERABLES LEDGER", "fetch data", "/app/data/in.csv", "/app/kv-store_pb2_grpc.py"} {
		if !strings.Contains(block, want) {
			t.Errorf("renderLedger missing %q\n---\n%s", want, block)
		}
	}
}

// appendLedger accumulates on the session; sharedLedger resolves a child to its PARENT's ledger so
// every worker in a plan sees the same shared deliverables.
func TestSharedLedgerParentResolution(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	parent := parentSession(t.TempDir())
	child := parentSession(t.TempDir())
	child.Parent = parent.ID
	a.mu.Lock()
	a.stateLocked(parent.ID).meta = parent
	a.stateLocked(child.ID).meta = child
	a.mu.Unlock()

	a.appendLedger(parent.ID, "step one", "/app/out.bin")
	a.appendLedger(parent.ID, "step two", "") // empty facts → dropped
	a.appendLedger(parent.ID, "step three", "/app/next.json")

	own := a.ledgerOf(parent.ID)
	if len(own) != 2 {
		t.Fatalf("ledgerOf(parent) = %d entries; want 2 (empty facts dropped)", len(own))
	}
	shared := a.sharedLedger(child.ID)
	if len(shared) != 2 || shared[0].Facts != "/app/out.bin" {
		t.Fatalf("sharedLedger(child) must resolve to the parent's ledger, got %+v", shared)
	}
	if rows := a.SharedLedger(child.ID); len(rows) != 2 || rows[1].Step != "step three" {
		t.Errorf("SharedLedger(child) = %+v; want the parent's 2 rows", rows)
	}
}

// The ledger's header promises produced paths; its fallback delivered narration. Observed live, the
// block a later worker received read:
//
//   - Read HACKING.adoc …: STATUS: DONE
//     I have read the HACKING.adoc file at `/app/ocaml/HACKING.adoc`. Now I will create the summary of the…
//
// The summary that step actually wrote never travelled. What magi observed — the write itself — is
// what the next worker needed, and magi had it all along.
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
