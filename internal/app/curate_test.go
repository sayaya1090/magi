package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
)

func curateApp(t *testing.T) *App {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	reg.Register(builtin.Task{})
	reg.Register(builtin.Ask{})    // base tool, registered in main.go in production
	reg.Register(builtin.Report{}) // base tool, registered in main.go in production
	reg.Register(builtin.AskUser{})
	reg.Register(builtin.Replan{})
	return New(store, &fakeLLM{}, reg, bus.New(), nil, Config{Permission: "allow"})
}

func toSet(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func TestCurateEnabledDefaultOff(t *testing.T) {
	if !curateEnabled() {
		t.Fatal("default must be ON")
	}
	t.Setenv("MAGI_CURATE", "0")
	if curateEnabled() {
		t.Error("=0 must disable")
	}
}

func TestParseCuratePacket(t *testing.T) {
	p, ok := parseCuratePacket(`prefix {"task":"do X","literals":["value"],"tools":["lsp","astgrep"]} trailing`)
	if !ok || p.Task != "do X" || len(p.Literals) != 1 || p.Literals[0] != "value" || len(p.Tools) != 2 {
		t.Fatalf("parse = %v %+v", ok, p)
	}
	if _, ok := parseCuratePacket("no json here"); ok {
		t.Error("prose-only must not parse")
	}
}

// renderCurateBrief lays the packet out as weighted sections so the context-free worker can tell the
// verbatim must-not-change literals and boundaries from background — and delegates the outcome, not a
// keystroke script. An empty packet renders empty so the caller falls back to the mechanical brief.
func TestRenderCurateBrief(t *testing.T) {
	if got := renderCurateBrief(curatePacket{}); got != "" {
		t.Errorf("empty packet must render empty, got %q", got)
	}
	brief := renderCurateBrief(curatePacket{
		Goal:        "ship a KV store",
		Progress:    "server skeleton exists",
		Task:        "implement Get",
		Literals:    []string{"value", "  ", "GetResponse"}, // blank entry must be dropped
		Constraints: []string{"do not change the proto"},
		Deliverable: "grpcurl Get returns value",
	})
	for _, want := range []string{
		"# Goal (why this exists)\nship a KV store",
		"# Progress so far", "- value", "- GetResponse",
		"# Boundaries (do NOT cross)\n- do not change the proto",
		"# Done when\ngrpcurl Get returns value",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing %q\n---\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "-  \n") || strings.Contains(brief, "- \n") {
		t.Errorf("blank literal was not dropped:\n%s", brief)
	}
}

// The worker always keeps the base toolset; the curator can only ADD registered specialized tools,
// and an invented name is dropped.
func TestResolveCuratedTools(t *testing.T) {
	a := curateApp(t)
	m := toSet(a.resolveCuratedTools([]string{"lsp", "bogus_tool", "tabulate"}))
	for _, n := range curateBaseTools {
		if !m[n] {
			t.Errorf("base tool %q must always be granted", n)
		}
	}
	if !m["lsp"] || !m["tabulate"] {
		t.Error("selected specialized tools must be granted")
	}
	if m["bogus_tool"] {
		t.Error("an unregistered tool must be dropped")
	}
}

// selectedSpecialized reports only the non-base tools (what the curator ADDED for the sub-task).
func TestSelectedSpecialized(t *testing.T) {
	got := toSet(selectedSpecialized([]string{"read", "bash", "lsp", "webfetch"}))
	if got["read"] || got["bash"] {
		t.Error("base tools must be excluded from the added set")
	}
	if !got["lsp"] || !got["webfetch"] {
		t.Error("specialized tools must be reported as added")
	}
}

// The selectable menu excludes both base tools and orchestration-only tools.
func TestSpecializedToolNames(t *testing.T) {
	a := curateApp(t)
	m := toSet(a.specializedToolNames())
	if m["read"] || m["bash"] {
		t.Error("base tools must not be in the selectable menu")
	}
	if m["task"] || m["ask_user"] || m["replan"] {
		t.Error("orchestration-only tools must not be selectable by a worker")
	}
	if !m["lsp"] {
		t.Error("a specialized tool (lsp) should be selectable")
	}
}
