package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	for a.ProfileMaintenancePending() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.ProfileMaintenancePending() {
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
	deadline = time.Now().Add(time.Second)
	for a.ProfileMaintenancePending() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	again, err := maintainer.ProfileMaintenanceState(ctx, wd)
	if err != nil || len(again.History) != 1 {
		t.Fatal("host notice recursively produced another maintenance run", again, err)
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
	for a.ProfileMaintenancePending() && time.Now().Before(deadline) {
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
			for a.ProfileMaintenancePending() && time.Now().Before(deadline) {
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

type blockedMaintenanceState struct {
	port.ProfileMaintenance
	entered chan struct{}
	release chan struct{}
}

func (b *blockedMaintenanceState) ProfileMaintenanceState(ctx context.Context, wd string) (port.ProfileMaintenanceState, error) {
	close(b.entered)
	select {
	case <-ctx.Done():
		return port.ProfileMaintenanceState{}, ctx.Err()
	case <-b.release:
		return b.ProfileMaintenance.ProfileMaintenanceState(ctx, wd)
	}
}
func TestMaintenanceStateLockDoesNotBlockForegroundScheduling(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedMaintenanceState{ProfileMaintenance: &companionpolicy.Maintainer{ConfigDir: t.TempDir()}, entered: make(chan struct{}), release: make(chan struct{})}
	llm := &acCapLLM{text: "null"}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Interactive: true, ProfileMaintainer: blocked, CompanionProfileNeeded: func(string) bool { return true }}))
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	returned := make(chan struct{})
	go func() { a.scheduleCompanionProfile(sid); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("foreground waited for the metadata store")
	}
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("background worker did not reach store")
	}
	close(blocked.release)
	deadline := time.Now().Add(time.Second)
	for a.ProfileMaintenancePending() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if llm.called() {
		t.Fatal("empty evidence called the model")
	}
}

func TestMaintenanceRestoresQueuedSourceWithoutStartingAUserTurn(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	maintainer := &companionpolicy.Maintainer{ConfigDir: t.TempDir()}
	llm := &acCapLLM{text: `{"purpose":"Workspace","tasks":[{"id":"verify","description":"Run workspace tests and report results"}]}`}
	cfg := Config{Interactive: true, ProfileMaintainer: maintainer, CompanionProfileNeeded: func(string) bool { return true }}
	first := New(store, llm, builtin.Default(), bus.New(), nil, cfg)
	wd := t.TempDir()
	ctx := context.Background()
	sid, err := first.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.store.Append(ctx, sid, evToolCall("docs", "read"), evToolResult("docs", "Project test documentation", false)); err != nil {
		t.Fatal(err)
	}
	evs, err := store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := maintainer.QueueProfileMaintenance(ctx, wd, string(sid), evs[len(evs)-1].Seq); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	headlessConfig := cfg
	headlessConfig.Interactive = false
	headless := New(store, llm, builtin.Default(), bus.New(), nil, headlessConfig)
	headless.ResumeProfileMaintenance(wd)
	if err := headless.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if llm.called() {
		t.Fatal("headless resumption called model")
	}
	resumed := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, cfg))
	resumed.ResumeProfileMaintenance(wd)
	deadline := time.Now().Add(3 * time.Second)
	for resumed.ProfileMaintenancePending() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	state, err := maintainer.ProfileMaintenanceState(ctx, wd)
	if err != nil || state.Pending || len(state.PendingSources) != 0 || len(state.History) != 1 || state.History[0].Status != "published" {
		t.Fatal(state, err)
	}
	chats, err := resumed.ListSessions(ctx, wd)
	if err != nil || len(chats) != 1 || chats[0].ID != sid {
		t.Fatal("resumption created a chat", chats, err)
	}
	if len(llm.request().Tools) > 0 {
		t.Fatal("resumed worker has executable tools")
	}
}

func TestMaintenanceBootstrapsEmptyCodingDescriptionWithoutQuestionOrUserTurn(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "README.md"), []byte("Widget library. Run widget tests with npm test. Provides widget APIs."), 0600); err != nil {
		t.Fatal(err)
	}
	llm := &acCapLLM{text: `{"purpose":"Widget library","tasks":[{"id":"widget-tests","description":"Run widget tests and report results"}]}`}
	maintainer := &companionpolicy.Maintainer{ConfigDir: t.TempDir()}
	cfg := Config{Interactive: true, ProfileMaintainer: maintainer, CompanionProfileNeeded: func(string) bool { return true }, CompanionProfileUnpublished: func(string) bool { return true }}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, cfg))
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	a.InteractiveOpened(context.Background(), sid, "ko")
	deadline := time.Now().Add(3 * time.Second)
	for a.ProfileMaintenancePending() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	state, err := maintainer.ProfileMaintenanceState(context.Background(), wd)
	if err != nil || len(state.Current.Tasks) != 1 || state.LastError != "" {
		t.Fatal(state, err)
	}
	if len(llm.request().Tools) != 0 {
		t.Fatal("bootstrap model received tools")
	}
	evs, err := store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Type == event.TypeQuestionRequested || ev.Type == event.TypePermissionRequested || (ev.Type == event.TypePromptSubmitted && ev.Actor.Kind == event.ActorUser) {
			t.Fatalf("bootstrap leaked into user turn: %s", ev.Type)
		}
	}
	if got := a.initialProfileEvidence(context.Background(), wd); !strings.Contains(got, "Widget library") {
		t.Fatal("missing bounded project evidence", got)
	}
}

func TestMaintenanceBootstrapSkipsLinksOversizedAndDeniedReads(t *testing.T) {
	wd := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(outside, []byte("outside secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wd, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "README.ko.md"), []byte(strings.Repeat("large", 20000)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "AGENTS.md"), []byte("project description"), 0600); err != nil {
		t.Fatal(err)
	}
	a := closeAfter(t, New(nil, &acCapLLM{}, builtin.Default(), bus.New(), nil, Config{Interactive: true, CompanionProfileNeeded: func(string) bool { return true }}))
	got := a.initialProfileEvidence(context.Background(), wd)
	if strings.Contains(got, "outside secret") || strings.Contains(got, "large") || !strings.Contains(got, "project description") {
		t.Fatal("unsafe bootstrap evidence", got)
	}
	denied := closeAfter(t, New(nil, &acCapLLM{}, builtin.Default(), bus.New(), nil, Config{Interactive: true, Deny: []string{"read(**)"}, CompanionProfileNeeded: func(string) bool { return true }}))
	if got := denied.initialProfileEvidence(context.Background(), wd); got != "" {
		t.Fatal("bootstrap bypassed deny", got)
	}
}
