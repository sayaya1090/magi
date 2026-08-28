package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// pathlessWriter is the shape port.FileTool explicitly allows and the builtins never take: a tool
// that changes a file and names none in its arguments. FileArg is documented to answer "" for a
// call that names no file — a slide add-in addressing slide 3 of the deck it is attached to, an
// editor tool acting on the buffer that is already open — and "" is a perfectly good map key.
type pathlessWriter struct{ name string }

func (p pathlessWriter) Name() string                   { return p.name }
func (p pathlessWriter) Description() string            { return "edits the document it is attached to" }
func (p pathlessWriter) Schema() json.RawMessage        { return json.RawMessage(`{"type":"object"}`) }
func (p pathlessWriter) WritesFile() bool               { return true }
func (p pathlessWriter) FileArg(json.RawMessage) string { return "" }
func (p pathlessWriter) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

// toolSet is a registry holding more than one tool, which is what this needs and oneTool is not.
type toolSet []port.Tool

func (s toolSet) Register(port.Tool) {}
func (s toolSet) List() []port.Tool  { return s }
func (s toolSet) Get(n string) (port.Tool, bool) {
	for _, t := range s {
		if t != nil && t.Name() == n {
			return t, true
		}
	}
	return nil, false
}

func pathlessGuard(t *testing.T) (*runGuard, func(name string, args json.RawMessage) (n int, reset bool)) {
	t.Helper()
	reg := toolSet{pathlessWriter{"mcp__deck__apply"}, pathlessWriter{"mcp__sheet__apply"}}
	touches := func(name string, args json.RawMessage) (fileTouch, bool) {
		return touchesFileIn(reg, name, args)
	}
	g := newRunGuard(touches)
	// Exactly what the tool-outcome path does around a landed write: fingerprint the call, then
	// record the mutation under the call's guard slot.
	return g, func(name string, args json.RawMessage) (int, bool) {
		t.Helper()
		touch, ok := touches(name, args)
		if !ok || !touch.writes {
			t.Fatalf("%s no longer reads as a write", name)
		}
		if touch.path != "" {
			t.Fatalf("%s reported path %q — the fallback belongs in the guard slot, not in the "+
				"field the deny rules, the snapshot, the diagnostics and the change record read as a path",
				name, touch.path)
		}
		_, n, _ := g.check(name, args)
		return n, g.mutated(touch.guard, canonicalArgs(args))
	}
}

// Two path-less writers must not share one slot in lastMut.
//
// The write path's "the world may have moved" term is the last mutation recorded FOR THAT FILE, so
// an identical rewrite accumulates as a repeat while the file stands still. Keyed by a path that is
// "", every path-less writer lands in the same slot: two of them alternating each overwrite the
// other's signature, so neither one's replay is ever recognised as the idempotent no-op it is —
// every swing is credited as a real change, and the repeat counting restarts each time.
func TestPathlessWritersDoNotShareOneGuardSlot(t *testing.T) {
	_, write := pathlessGuard(t)
	deck := json.RawMessage(`{"slide":3,"text":"hello"}`)
	sheet := json.RawMessage(`{"cell":"A1","text":"hello"}`)

	if _, reset := write("mcp__deck__apply", deck); !reset {
		t.Fatal("the first path-less write was not recorded as a change at all")
	}
	if _, reset := write("mcp__sheet__apply", sheet); !reset {
		t.Fatal("the first write through the second path-less tool was not recorded as a change")
	}

	var deckN int
	for i := 0; i < repeatLimit+1; i++ {
		n, reset := write("mcp__deck__apply", deck)
		if reset {
			t.Fatalf("replay %d: the identical path-less write was credited as a real change — "+
				"the other tool's mutation is sitting in the same slot", i+1)
		}
		deckN = n
		if _, reset := write("mcp__sheet__apply", sheet); reset {
			t.Fatalf("replay %d: the same, from the other side", i+1)
		}
	}
	if deckN <= repeatLimit {
		t.Errorf("the identical path-less write counted %d repeats over %d replays — the repeat "+
			"never comes within reach of being remarked on", deckN, repeatLimit+1)
	}
}

// …and the fix must not be "skip a write that names no file".
//
// check() climbs sinceProgress on every call and only mutated() clears it (noteInspectProgress
// merely pulls the window forward on a novel read). Skipping would mean a turn doing its work
// through a path-less editor never registers progress at all, while the stalled nudge — which
// re-arms every noProgressNudge calls, with no cap and no force-stop behind it — tells that turn
// it is going nowhere, forever.
func TestAPathlessMutationStillCountsAsProgress(t *testing.T) {
	g, write := pathlessGuard(t)
	for i := 0; i < noProgressNudge*3; i++ {
		args, _ := json.Marshal(map[string]any{"slide": i, "text": "v"})
		if _, reset := write("mcp__deck__apply", args); !reset {
			t.Fatalf("call %d: a genuinely new path-less write was not counted as progress", i)
		}
		if kind := g.shouldNudge(); kind == "stalled" {
			t.Fatalf("call %d: a turn that is editing on every call was told it has stalled", i)
		}
	}
	if g.sinceProgress != 0 {
		t.Errorf("sinceProgress = %d after a run of successful path-less writes", g.sinceProgress)
	}
}
