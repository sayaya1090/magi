package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/companion"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/port"
)

func TestMaintenanceUsesToolFreeWorkerAndPersistsOutsideChatList(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := &acCapLLM{text: `{"purpose":"Widget workspace","responsibilities":["Widget maintenance"],"tasks":[{"id":"test-widget","description":"Run widget tests and report output"}]}`}
	maintainer := &companionpolicy.Maintainer{ConfigDir: t.TempDir()}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Interactive: true, ProfileMaintainer: maintainer, CompanionProfileNeeded: func(string) bool { return true }}))
	wd := t.TempDir()
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.Append(ctx, sid, evToolCall("read-doc", "read"), evToolResult("read-doc", "Widget source and test documentation", false)); err != nil {
		t.Fatal(err)
	}
	a.scheduleCompanionProfile(sid)
	deadline := time.Now().Add(5 * time.Second)
	for a.profileBusy.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.profileBusy.Load() {
		t.Fatal("worker did not finish")
	}
	state, err := maintainer.ProfileMaintenanceState(ctx, wd)
	if err != nil || state.LastError != "" || len(state.Current.Tasks) != 1 || len(state.History) != 1 || state.History[0].Status != "published" || state.Checkpoints[string(sid)] == 0 {
		t.Fatal(state, err)
	}
	req := llm.request()
	if len(req.Tools) > 0 || !strings.Contains(req.System, "internal metadata session") {
		t.Fatal("worker received tool authority or wrong prompt", req)
	}
	sessions, err := a.ListSessions(ctx, wd)
	if err != nil || len(sessions) != 1 || sessions[0].ID != sid {
		t.Fatal("internal session leaked into chat list", sessions, err)
	}
	evs, err := store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	notices := 0
	for _, e := range evs {
		if e.Actor.ID == "companion-profile" && e.Type == event.TypePromptSubmitted {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("expected one host publication summary, got %d", notices)
	}
	a.scheduleCompanionProfile(sid)
	if a.profileBusy.Load() {
		t.Fatal("host publication notice recursively started maintenance")
	}
}
func TestMaintenanceEvidenceHashIgnoresCallIdentityAndRejectsFailure(t *testing.T) {
	a := []event.Event{evToolCall("a", "read"), evToolResult("a", "same source", false)}
	b := []event.Event{evToolCall("b", "read"), evToolResult("b", "same source", false)}
	if maintenanceEvidenceHash(a) != maintenanceEvidenceHash(b) {
		t.Fatal("same read generated new evidence identity")
	}
	c := []event.Event{evToolCall("c", "read"), evToolResult("c", "changed source", false)}
	if maintenanceEvidenceHash(a) == maintenanceEvidenceHash(c) {
		t.Fatal("changed evidence lost")
	}
	failed := []event.Event{evToolCall("f", "read"), evToolResult("f", "failure", true)}
	if companionProfileEvidence(maintenanceEvents(failed)) != "" {
		t.Fatal("failed read qualified")
	}
}

type blockingMaintenanceLLM struct{ started chan struct{} }

func (p blockingMaintenanceLLM) StreamChat(ctx context.Context, req port.ChatRequest) (<-chan port.ProviderEvent, error) {
	close(p.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestMaintenanceCancellationPersistsFailureWithoutPublication(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := blockingMaintenanceLLM{started: make(chan struct{})}
	maintainer := &companionpolicy.Maintainer{ConfigDir: t.TempDir()}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Interactive: true, ProfileMaintainer: maintainer, CompanionProfileNeeded: func(string) bool { return true }}))
	wd := t.TempDir()
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.Append(ctx, sid, evToolCall("read", "read"), evToolResult("read", "source", false)); err != nil {
		t.Fatal(err)
	}
	a.scheduleCompanionProfile(sid)
	select {
	case <-llm.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	a.CancelCompanionProfile()
	deadline := time.Now().Add(3 * time.Second)
	for a.profileBusy.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	state, err := maintainer.ProfileMaintenanceState(ctx, wd)
	if err != nil || state.Running || state.LastError == "" || len(state.Current.Tasks) > 0 || state.Checkpoints[string(sid)] != 0 {
		t.Fatal(state, err)
	}
}

func TestMaintenancePromptExcludesAuthorityAndBoundsDescription(t *testing.T) {
	c := companion.Contract{Purpose: "Workspace", Access: []companion.Access{{Resource: "private-root", Mode: "read"}}, Instructions: []string{"private instruction"}, Operations: []companion.Operation{{ID: "private-operation"}}}
	for i := 0; i < 64; i++ {
		c.Tasks = append(c.Tasks, companion.Task{ID: fmt.Sprintf("task-%d", i), Description: strings.Repeat("x", 2000)})
	}
	raw, err := maintenanceDescriptionJSON(c)
	if err != nil || len(raw) > 24<<10 || strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "private instruction") || !strings.Contains(string(raw), `"truncated":true`) {
		t.Fatal(string(raw), err)
	}
}

func TestNativeMaintenanceUsesOnlyOwnReadOnlyTools(t *testing.T) {
	for _, kind := range []string{"powerpoint", "figma"} {
		t.Run(kind, func(t *testing.T) {
			registry := builtin.NewRegistry()
			prefix := "mcp__figma__"
			if kind == "powerpoint" {
				prefix = "mcp__ppt__"
			}
			allowed := prefix + "read_document"
			registry.Register(declaresReadOnly{name: allowed, ro: true})
			registry.Register(declaresReadOnly{name: prefix + "write_document", ro: false})
			registry.Register(declaresReadOnly{name: "mcp__foreign__read", ro: true})
			store, err := jsonl.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			llm := &acCapLLM{text: `{"purpose":"Native document assistant","tasks":[{"id":"review-document","description":"Review the selected document and report issues"}]}`}
			maintainer := &companionpolicy.Maintainer{ConfigDir: t.TempDir()}
			a := closeAfter(t, New(store, llm, registry, bus.New(), nil, Config{Interactive: true, ClientRole: kind, ProfileMaintainer: maintainer, CompanionProfileNeeded: func(string) bool { return true }}))
			if !a.maintenanceReadTool(allowed) || a.maintenanceReadTool(prefix+"write_document") || a.maintenanceReadTool("mcp__foreign__read") || a.maintenanceReadTool("read") {
				t.Fatal("native evidence scope incorrect")
			}
			wd := t.TempDir()
			sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.store.Append(context.Background(), sid, evToolCall("native", allowed), evToolResult("native", "Selected document contents", false)); err != nil {
				t.Fatal(err)
			}
			a.scheduleCompanionProfile(sid)
			deadline := time.Now().Add(3 * time.Second)
			for a.profileBusy.Load() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			state, err := maintainer.ProfileMaintenanceState(context.Background(), wd)
			if err != nil || state.LastError != "" || len(state.Current.Tasks) != 1 {
				t.Fatal(state, err)
			}
			req := llm.request()
			if len(req.Tools) > 0 || !strings.Contains(req.Messages[0].Parts[0].Text, "Client kind: "+kind) || !strings.Contains(req.Messages[0].Parts[0].Text, allowed) {
				t.Fatal("native identity or observed operation lost", req)
			}
		})
	}
}
