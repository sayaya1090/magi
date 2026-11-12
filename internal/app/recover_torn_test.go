package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A crash can land mid-write and leave half a line at the end of the log. Recovery over such a log
// must keep every whole event already there, pair each open call exactly once, and — run again —
// write nothing more: the torn fragment is not an event, and it must not hide the turn or split the
// recovery's own lines.
func TestRecoveryOverATornLogTailKeepsTheLogAndPairsOnce(t *testing.T) {
	ctx := context.Background()
	dir, wd := t.TempDir(), t.TempDir()
	store, _ := jsonl.New(dir)
	old := New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := old.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_user", Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}})
	if err := old.appendFact(ctx, sid, event.TypePromptSubmitted, crashActor, b); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"c_write", "c_read"} {
		pb, _ := json.Marshal(event.PartAppendedData{MessageID: "m_asst", Role: session.RoleAssistant, Part: session.Part{ID: "p_" + c,
			Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: c, Name: c[2:], Args: json.RawMessage(`{}`)}}})
		if err := old.appendFact(ctx, sid, event.TypePartAppended, crashActor, pb); err != nil {
			t.Fatal(err)
		}
	}
	sb, _ := json.Marshal(event.ToolStartedData{CallID: "c_write", Tool: "write"})
	if err := old.appendFact(ctx, sid, event.TypeToolStarted, crashActor, sb); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Read(ctx, sid, 0)

	paths, _ := filepath.Glob(filepath.Join(dir, "projects", "*", string(sid)+".jsonl"))
	if len(paths) != 1 {
		t.Fatalf("want the one session file, got %v", paths)
	}
	f, err := os.OpenFile(paths[0], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"seq":99,"type":"part.appended","data":{"messageId":"m_x","part":{"kind":"tool-res`); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	// The next start: a fresh store over the same files.
	restarted, _ := jsonl.New(dir)
	a := closeAfter(t, New(restarted, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].Err != nil || got[0].Resumable || len(got[0].Uncertain) != 1 || got[0].Uncertain[0] != "c_write" ||
		len(got[0].NotRun) != 1 || got[0].NotRun[0] != "c_read" {
		t.Fatalf("recovery over a torn tail: %+v", got)
	}
	if again := a.RecoverInterrupted(ctx, wd); len(again) != 0 {
		t.Fatalf("a second pass should find nothing to do; got %+v", again)
	}

	reread, _ := jsonl.New(dir)
	evs, err := reread.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < len(before) {
		t.Fatalf("events were lost: %d before, %d after", len(before), len(evs))
	}
	for i, e := range before {
		if evs[i].Type != e.Type || evs[i].Seq != e.Seq || !bytes.Equal(evs[i].Data, e.Data) {
			t.Fatalf("event %d changed: was %s seq %d, now %s seq %d", i, e.Type, e.Seq, evs[i].Type, evs[i].Seq)
		}
	}
	for _, c := range []string{"c_write", "c_read"} {
		n := 0
		for _, e := range evs {
			var d event.PartAppendedData
			if e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == c {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s has %d results, want exactly 1", c, n)
		}
	}
	if e, f := countByRecovery(evs, event.TypeError), countByRecovery(evs, event.TypeTurnFinished); e != 1 || f != 1 {
		t.Fatalf("want one interruption and one turn end, got %d and %d: %v", e, f, typesOf(evs))
	}
	// Every whole line after recovery is an event; the torn fragment did not swallow the next write.
	raw, _ := os.ReadFile(paths[0])
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	last := lines[len(lines)-1]
	var probe map[string]any
	if json.Unmarshal(last, &probe) != nil || probe["type"] != string(event.TypeTurnFinished) {
		t.Fatalf("the log's last line should be the recovery's turn end, got %q", last)
	}
}
