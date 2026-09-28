package app

import (
	"context"
	"github.com/sayaya1090/magi/internal/core/command"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/port"
)

func TestContextRetrievalBaselineOrderAndWhitespace(t *testing.T) {
	a := &App{contextProviders: []port.ContextProvider{
		&fakeProvider{chunks: []port.ContextChunk{{Source: "same", Text: "  첫째 \n"}, {Source: "same", Text: "둘째"}, {Text: "  "}}},
		&fakeProvider{chunks: []port.ContextChunk{{Text: "셋째"}}},
	}}
	got := a.gatherContext(context.Background(), port.ContextQuery{})
	want := "## same\n첫째\n\n## same\n둘째\n\n셋째"
	if got != want {
		t.Fatalf("context changed: got %q want %q", got, want)
	}
}

func TestContextAssemblyExactBytesAndMissingTool(t *testing.T) {
	parts := []contextFragment{
		{id: "a", text: " first\n", lane: turnSystem},
		{id: "b", text: "load memory", lane: volatileStable, requires: []string{"recall_memory"}},
		{id: "c", text: "\nlast ", lane: volatileClock},
	}
	got, decisions := assembleContext(parts, map[string]bool{})
	if got != " first\n\nlast " || decisions[1].Reason != "missing_tool" {
		t.Fatalf("%q %#v", got, decisions)
	}
	got, _ = assembleContext(parts, map[string]bool{"recall_memory": true})
	if got != " first\nload memory\nlast " {
		t.Fatal(got)
	}
}

func TestContextRetrievalUTF8Boundary(t *testing.T) {
	for _, char := range []string{"한", "😀"} {
		a := &App{contextProviders: []port.ContextProvider{&fakeProvider{chunks: []port.ContextChunk{{Text: strings.Repeat(char, contextBudget)}}}}}
		got := a.gatherContext(context.Background(), port.ContextQuery{})
		if !utf8.ValidString(got) || len(got) > contextBudget || len(got) < contextBudget-3 {
			t.Fatalf("invalid boundary: %d", len(got))
		}
	}
}

func TestRequestHintsUseItsFrozenToolCatalog(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(wd, ".magi", "skills"), "test-skill", "test description")
	s := a.sessionInfo(ctx, sid)
	tc := turnCtx{s: s, agent: AgentSpec{Name: "restricted", Tools: []string{"read"}}, runStart: time.Now(), guard: newRunGuard(nil)}
	req, evs := a.buildStepRequest(ctx, tc, nil, 1, 0)
	if contextToolNames(req.Tools)["skill"] || strings.Contains(req.System, "Available skills") {
		t.Fatal("unavailable skill advertised")
	}
	tc.agent = AgentSpec{Name: "main"}
	a.resetTurnPrompt(sid)
	req, evs = a.buildStepRequest(ctx, tc, evs, 1, 0)
	if !contextToolNames(req.Tools)["skill"] || !strings.Contains(req.System, "test-skill") {
		t.Fatal("available skill missing")
	}
	writeSkill(t, filepath.Join(wd, ".magi", "skills"), "late-skill", "late description")
	next, evs := a.buildStepRequest(ctx, tc, evs, 2, 0)
	again, _ := a.buildStepRequest(ctx, tc, evs, 3, 0)
	if next.System != req.System || !reflect.DeepEqual(next.Tools, req.Tools) {
		t.Fatal("prefix moved")
	}
	count := 0
	for _, m := range again.Messages {
		for _, p := range m.Parts {
			if strings.Contains(p.Text, "A skill became available") && strings.Contains(p.Text, "late-skill") {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("arrival count %d", count)
	}
}
