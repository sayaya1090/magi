package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Two children that can only look have nothing to race over, so a step that asks for both runs
// both at once.
//
// A subagent used to run alone whatever it was, for one reason: a child writes files, and the
// parent's guard reads each file's before and after around an edit, which is only race-free when
// the writes are serialised. That reason does not apply to a child with no writing tool — and the
// cost of pretending it does is a whole extra child turn of wall clock, every time.
func TestReadOnlyChildrenRunTogether(t *testing.T) {
	reg := builtin.NewRegistry()
	reg.Register(metaTool{name: "planner", meta: port.ToolMetadata{Subagent: true, ReadOnlyChildren: true}})
	reg.Register(metaTool{name: "builder", meta: port.ToolMetadata{Subagent: true}})
	a := &App{cfg: Config{}, tools: reg}
	a.cfg = a.cfg.withDefaults()

	calls := func(names ...string) []*session.ToolCall {
		out := make([]*session.ToolCall, 0, len(names))
		for _, n := range names {
			out = append(out, &session.ToolCall{Name: n})
		}
		return out
	}
	if !a.allParallelSafe(calls("planner", "planner")) {
		t.Error("two children that can only look are still being run one after the other")
	}
	if !a.allParallelSafe(calls("planner", "read")) {
		t.Error("a looking child beside a read is not parallel-safe")
	}
	// And a subagent that says nothing about its children is what it always was: alone.
	if a.allParallelSafe(calls("builder", "builder")) {
		t.Error("a subagent that may write is being run beside another")
	}
	if a.allParallelSafe(calls("planner", "builder")) {
		t.Error("a batch with one writing child in it must serialise")
	}
}

// …and the claim is checked rather than believed.
//
// The loop runs two of these at once on the strength of a flag in a plugin's table. What it is
// promising about is two children writing the same file at the same time, so the promise is
// enforced where a child's tools are actually decided: at the spawn.
func TestAToolThatSaysItsChildrenOnlyLookIsHeldToIt(t *testing.T) {
	reg := builtin.NewRegistry()
	reg.Register(metaTool{name: "planner", meta: port.ToolMetadata{Subagent: true, ReadOnlyChildren: true}})
	reg.Register(metaTool{name: "builder", meta: port.ToolMetadata{Subagent: true}})

	if err := onlyLooks(reg, "planner", []string{"read", "grep"}); err != nil {
		t.Errorf("a child asking for two reads was refused: %v", err)
	}
	err := onlyLooks(reg, "planner", []string{"read", "write"})
	if err == nil {
		t.Fatal("a child that can write was allowed under a tool that says its children only look")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("the refusal does not name the tool that broke it: %v", err)
	}
	// The whole set is not a set of read-only tools either — it is everything this companion has.
	if onlyLooks(reg, "planner", nil) == nil {
		t.Error("a child with no allowlist at all was allowed under a read-only claim")
	}
	// A tool that claims nothing is not held to anything.
	if err := onlyLooks(reg, "builder", []string{"write"}); err != nil {
		t.Errorf("a tool that made no claim was checked against it anyway: %v", err)
	}
}

// And the check is on the path a spawn actually takes, not only in the function that spells it out.
//
// A rule that is written and not wired is the shape this tree keeps finding: the mechanism is
// there, the caller is not, and the promise it makes is false in exactly the case it was written
// for. Here the promise is that two children cannot write at the same time.
func TestTheSpawnItselfRefusesAWritingChildUnderAReadOnlyClaim(t *testing.T) {
	a, parent, _ := spawnApp(t, &usageLLM{text: "done"})
	a.tools.Register(metaTool{name: "planner", meta: port.ToolMetadata{Subagent: true, ReadOnlyChildren: true}})

	spawn, _, _, _, _ := a.spawnFnFor(0, parent, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "c1", "planner")
	_, err := spawn(context.Background(), port.SpawnSpec{Prompt: "plan it", Tools: []string{"read", "write"}})
	if err == nil {
		t.Fatal("a child that can write was started by a tool that says its children only look")
	}
	if !strings.Contains(err.Error(), "write") {
		t.Errorf("the refusal does not name what broke it: %v", err)
	}
	// The same tool, asking for what it said it would: allowed.
	if _, err := spawn(context.Background(), port.SpawnSpec{Prompt: "plan it",
		Tools: []string{"read", "glob"}}); err != nil {
		t.Errorf("a child asking only to look was refused: %v", err)
	}
}
