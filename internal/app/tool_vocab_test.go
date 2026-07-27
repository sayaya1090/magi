package app

import (
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// Policy all over this package decides what to do about a tool by writing its name as a literal:
// which tools count as acting, which are dangerous, which a curated worker always keeps, which a
// read-only child may reach. Every one of those literals is an unchecked assertion that a tool by
// that name exists.
//
// Nothing enforces it. A tool renamed or removed leaves the literal behind, and a name that was
// never right in the first place is indistinguishable from one that is — the set simply never
// matches, the policy silently does not apply, and no build or test says a word. This test is the
// missing check: it holds each set against the registry and names the literals nothing answers to.
//
// It found two on the day it was written. `specCanAct` had listed `notebook_edit` and `apply_patch`
// since before either was a tool anywhere in this binary.
func TestPolicyToolNamesAreRealTools(t *testing.T) {
	known := builtin.KnownNames()
	if len(known) < 20 {
		t.Fatalf("registry reported %d tools — the enumeration is broken, not the sets", len(known))
	}

	sets := map[string][]string{
		"actingTools":          toolNamesIn(actingTools),
		"curateBaseTools":      curateBaseTools,
		"specMineExploreTools": specMineExploreTools,
		"DangerTools (config default)": toolNamesIn(
			Config{}.withDefaults().DangerTools),
	}
	for name, tools := range sets {
		t.Run(name, func(t *testing.T) {
			if len(tools) == 0 {
				t.Fatal("set is empty — it is no longer reaching the value it means to check")
			}
			for _, tool := range tools {
				if !known[tool] {
					t.Errorf("%q names no tool: the policy that reads this set can never apply to it. "+
						"Either the tool was renamed or removed and the literal was left behind, or it was "+
						"never spelled right.", tool)
				}
			}
		})
	}
}

// knowledgeLookupTools is the one set that deliberately reaches past the built-ins: a plugin or MCP
// server can supply web search under its own name, and the set exists to catch a failed lookup
// whoever provides it. So the assertion is weaker and different — not "every name is a built-in",
// but "the built-in ones are spelled right", which is what the aliases would otherwise hide.
func TestKnowledgeLookupNamesTheBuiltinsCorrectly(t *testing.T) {
	known := builtin.KnownNames()
	var builtins int
	for name := range knowledgeLookupTools {
		if known[name] {
			builtins++
		}
	}
	// websearch and webfetch are built in; if a rename left the set holding only aliases, every
	// lookup this binary can actually perform would stop being recognized as one.
	if builtins < 2 {
		t.Errorf("knowledgeLookupTools matches %d built-in tool(s), want the built-in web tools among "+
			"them — the rest are plugin/MCP aliases and cannot carry the set on their own", builtins)
	}
}

// The tools that establish an absence are read by name in the event walk, and the one that does it
// has to be one the read-only explorer can actually call — otherwise the walk waits for a call that
// this child can never make.
func TestAbsenceEvidenceToolIsOneTheExplorerHas(t *testing.T) {
	if !builtin.KnownNames()[absenceSearchTool] {
		t.Fatalf("%q names no tool — no search would ever be paired", absenceSearchTool)
	}
	for _, tool := range specMineExploreTools {
		if tool == absenceSearchTool {
			return
		}
	}
	t.Errorf("the explorer's allowlist %v cannot call %q, so it can never establish an absence",
		specMineExploreTools, absenceSearchTool)
}

func toolNamesIn(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
