package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A routine is worth the model responses it replaces. A shell chain or pipeline the model already
// writes as one bash call is one response, and a routine for it replaces one response with one
// run_routine call: it saves nothing, so the plug-in does not save it however often it repeats.
func TestARepeatedOneCallShellCommandSavesNoRoutine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell routines")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	observe := func(id, cmd string) {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		r.fire("tool_finished", "tool", "bash", "args", string(raw), "ok", "true", "order_known", "true", "call_id", id)
	}
	const inc = " | grep -E '^[-+]' | grep -v '^+++\\|^---'"
	for i := range 3 {
		r.fire("user_message", "text", fmt.Sprintf("prepare and review change %d", i))
		observe(fmt.Sprintf("chain-%d", i), fmt.Sprintf("mkdir -p dir%d && touch dir%d/f%d.txt && test -f dir%d/f%d.txt", i, i, i, i, i))
		files := "one"
		for k := 0; k < i; k++ {
			files += fmt.Sprintf(" more%d", k)
		}
		observe(fmt.Sprintf("pipe-%d", i), "git show --format= HEAD~"+fmt.Sprint(i)+" -- "+files+inc)
		r.fire("turn_finished")
	}
	if files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/write-*/routine.md")); len(files) != 0 {
		t.Fatalf("one-call shell commands were saved as routines: %v", files)
	}
}

// A flow that does span two model responses (a listing, then a command over what it listed) is saved, and
// consumers that differ only in output shape (the cut -c width, a head count) are one routine, not one
// routine per width.
func TestOutputShapeVariantsOfAnInventoryConsumerAreOneRoutine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pipeline")
	}
	r := newRoutineRig(t, t.TempDir(), routineFile)
	r.a.plat = platform.OS{}
	rawArgs, _ := json.Marshal(map[string]string{"command": "git show --format='--- %h' --stat HEAD"})
	var last string
	for _, pair := range []struct {
		rev, cut string
		paths    []string
	}{{"1111111", "-c1-180", []string{"a", "b"}}, {"2222222", "-c1-240", []string{"c", "d", "e"}}} {
		var text strings.Builder
		fmt.Fprintf(&text, "--- %s\n", pair.rev)
		for _, path := range pair.paths {
			fmt.Fprintf(&text, " %s | 1 +\n", path)
		}
		fmt.Fprintf(&text, " %d files changed\n", len(pair.paths))
		raw, _ := json.Marshal(text.String())
		inventory := resultInventory("bash", rawArgs, session.ToolResult{Content: raw})
		consumer, _ := json.Marshal(map[string]string{"command": "git show --format= " + pair.rev + " -- " + strings.Join(pair.paths, " ") + " | grep -E '^[-+]' | cut " + pair.cut})
		r.host.InventoryRoutine(r.s.Workdir, inventory, string(consumer))
		last = inventory
	}
	files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/write-*/routine.md"))
	if len(files) != 1 {
		t.Fatalf("consumers differing only in cut width should be one routine: %v", files)
	}
	// The next run uses the width seen last.
	var plan struct{ Args map[string]any }
	if raw := r.host.InventoryRoutine(r.s.Workdir, last, ""); json.Unmarshal([]byte(raw), &plan) != nil || plan.Args["s3_arg1"] != "-c1-240" {
		t.Fatalf("plan should carry the latest width: %+v", plan)
	}
}

// A head count is output shape too, written either way: head -N or head -n N. Consumers that differ only
// in the count are one routine, and the next plan uses the count seen last.
func TestHeadCountVariantsOfAnInventoryConsumerAreOneRoutine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX pipeline")
	}
	for _, tc := range []struct {
		name, first, second, key, want string
	}{
		{"dash-count", "head -40", "head -80", "s3_arg1", "-80"},
		{"n-option", "head -n 40", "head -n 80", "s3_arg2", "80"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRoutineRig(t, t.TempDir(), routineFile)
			r.a.plat = platform.OS{}
			rawArgs, _ := json.Marshal(map[string]string{"command": "git show --format='--- %h' --stat HEAD"})
			var last string
			for _, pair := range []struct {
				rev, head string
				paths     []string
			}{{"1111111", tc.first, []string{"a", "b"}}, {"2222222", tc.second, []string{"c", "d", "e"}}} {
				var text strings.Builder
				fmt.Fprintf(&text, "--- %s\n", pair.rev)
				for _, path := range pair.paths {
					fmt.Fprintf(&text, " %s | 1 +\n", path)
				}
				fmt.Fprintf(&text, " %d files changed\n", len(pair.paths))
				raw, _ := json.Marshal(text.String())
				inventory := resultInventory("bash", rawArgs, session.ToolResult{Content: raw})
				consumer, _ := json.Marshal(map[string]string{"command": "git show --format= " + pair.rev + " -- " + strings.Join(pair.paths, " ") + " | grep -E '^[-+]' | " + pair.head})
				r.host.InventoryRoutine(r.s.Workdir, inventory, string(consumer))
				last = inventory
			}
			files, _ := filepath.Glob(filepath.Join(r.s.Workdir, ".magi/routines/write-*/routine.md"))
			if len(files) != 1 {
				t.Fatalf("consumers differing only in the head count should be one routine: %v", files)
			}
			var plan struct{ Args map[string]any }
			if raw := r.host.InventoryRoutine(r.s.Workdir, last, ""); json.Unmarshal([]byte(raw), &plan) != nil || plan.Args[tc.key] != tc.want {
				t.Fatalf("plan should carry the latest count %s=%s: %+v", tc.key, tc.want, plan)
			}
			// The count stays a count: an option in its place is refused before bash is asked.
			var name struct{ Name string }
			_ = json.Unmarshal([]byte(r.host.InventoryRoutine(r.s.Workdir, last, "")), &name)
			plan.Args[tc.key] = "-rf"
			if _, asks, failed := r.call(t, "bad-count", "run_routine", map[string]any{"name": name.Name, "args": plan.Args}, "allow"); !failed || len(asks) != 0 {
				t.Fatalf("an option in the count slot reached bash: asks=%+v failed=%v", asks, failed)
			}
		})
	}
}
