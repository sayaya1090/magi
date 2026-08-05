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
// It found two on the day it was written: a since-removed read-only-role set had listed
// `notebook_edit` and `apply_patch` from before either was a tool anywhere in this binary. That
// set and its one caller are both gone now, so what is left here is the policy set that is still
// consulted — which is the point, a set nothing reads is not a policy to check.
// The policy sets name tools by string literal. A rename or a typo leaves the literal behind and
// the policy it belongs to silently stops applying to that tool — no error, no warning, just a
// guardrail that covers one less thing than it says it does.
func TestPolicyToolNamesAreRealTools(t *testing.T) {
	known := builtin.KnownNames()
	if len(known) < 20 {
		t.Fatalf("registry reported %d tools — the enumeration is broken, not the sets", len(known))
	}

	sets := map[string][]string{
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

func toolNamesIn(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
