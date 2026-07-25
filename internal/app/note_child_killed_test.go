package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

// noteChildKilled posts the kill reason onto the CHILD's own session bus (State prefixed "killed —"),
// so the child's detail-view pane can render it at the end — the "양쪽 모두" half that complements the
// parent-log notice.
func TestNoteChildKilledPublishesOnChildSession(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &usageLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	child, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, child, 0)
	defer cancelSub()

	a.noteChildKilled(child, "s_parent", "worker", "cancelled by orchestrator: no longer needed")

	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type != event.TypeAgentStatus {
				continue
			}
			var d event.AgentStatusData
			if json.Unmarshal(e.Data, &d) != nil {
				t.Fatal("bad AgentStatus payload")
			}
			if d.State != "killed — cancelled by orchestrator: no longer needed" {
				t.Fatalf("kill State = %q, want the killed-prefixed reason", d.State)
			}
			if d.Parent != "s_parent" || d.Role != "worker" {
				t.Errorf("provenance lost: parent=%q role=%q", d.Parent, d.Role)
			}
			return
		case <-deadline:
			t.Fatal("no AgentStatus kill event arrived on the child session")
		}
	}
}
