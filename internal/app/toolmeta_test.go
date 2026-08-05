package app

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// metaTool is a plugin-shaped tool carrying declared metadata.
type metaTool struct {
	name string
	meta port.ToolMetadata
}

func (t metaTool) Name() string            { return t.name }
func (t metaTool) Description() string     { return t.name + " does a thing" }
func (t metaTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t metaTool) Meta() port.ToolMetadata { return t.meta }
func (t metaTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

// An INTERNAL tool is off the main agent's list and on a child's — when that child's allowlist
// names it. A plugin ships a narrow helper (say, one that only runs git) for its own specialist
// without adding weight to every request the main agent makes; the tool list a weak model carries
// is a real cost, which is why the built-ins already prune themselves this way.
func TestAnInternalToolReachesOnlyTheAgentThatNamesIt(t *testing.T) {
	reg := builtin.Default()
	reg.Register(metaTool{name: "git_log", meta: port.ToolMetadata{Internal: true}})
	reg.Register(metaTool{name: "plain_helper"})
	a := &App{tools: reg}

	names := func(spec AgentSpec) []string {
		var out []string
		for _, s := range a.toolSpecs(spec) {
			out = append(out, s.Name)
		}
		return out
	}
	// The main agent: an empty allowlist, which allows() reads as "everything".
	main := names(AgentSpec{})
	if slices.Contains(main, "git_log") {
		t.Error("an internal tool was advertised to the main agent")
	}
	if !slices.Contains(main, "plain_helper") {
		t.Error("an ordinary plugin tool must still reach the main agent")
	}
	// A child spawned with it named.
	child := names(AgentSpec{Tools: []string{"git_log"}})
	if !slices.Contains(child, "git_log") {
		t.Errorf("the child that named it did not get it: %v", child)
	}
	// And naming something else does not smuggle it in.
	other := names(AgentSpec{Tools: []string{"plain_helper"}})
	if slices.Contains(other, "git_log") {
		t.Error("an internal tool reached an agent that did not name it")
	}
}

// A subagent tool never runs beside another tool. It runs a whole child turn, which writes files
// under the PARENT's guard — and the guard's before/after capture assumes writes to a file are
// serialised. It also blocks for as long as the child takes, and the parallel path does not
// re-check the context between launches.
func TestASubagentToolIsNeverRunInParallel(t *testing.T) {
	reg := builtin.Default()
	reg.Register(metaTool{name: "reviewer", meta: port.ToolMetadata{Subagent: true}})
	reg.Register(metaTool{name: "plain_helper"})
	a := &App{tools: reg, cfg: Config{}}

	calls := func(names ...string) []*session.ToolCall {
		var out []*session.ToolCall
		for _, n := range names {
			out = append(out, &session.ToolCall{Name: n})
		}
		return out
	}
	if !a.allParallelSafe(calls("read", "grep")) {
		t.Error("two read-only builtins should still run together")
	}
	if a.allParallelSafe(calls("read", "reviewer")) {
		t.Error("a subagent must not run beside another tool")
	}
	if a.allParallelSafe(calls("reviewer", "reviewer")) {
		t.Error("two subagents must not run together either")
	}
	if !a.allParallelSafe(calls("read", "plain_helper")) {
		t.Error("an ordinary plugin tool is not affected by this")
	}
}

// Metadata is optional. A tool that declares none — every built-in — behaves exactly as before.
func TestAToolWithoutMetadataIsUnchanged(t *testing.T) {
	reg := builtin.Default()
	rd, ok := reg.Get("read")
	if !ok {
		t.Fatal("read is not registered")
	}
	if m := port.ToolMetaOf(rd); m != (port.ToolMetadata{}) {
		t.Errorf("a built-in should declare nothing, got %+v", m)
	}
}
