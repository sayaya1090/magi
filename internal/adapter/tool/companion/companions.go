// Package companion lets an agent see the other magi running beside it.
//
// # Why an agent could not see them, and why that is a gap
//
// A companion is one magi bound to one workspace. Which ones exist on a machine has been derivable
// since the daemon landed — each publishes a record, and `magi --agents` and the console both read
// them — but nothing put that in front of the AGENT. The console knew; the thing being watched did
// not. So a companion asked to coordinate with another could not name it, and a person had to be
// the wire between two processes on the same laptop.
//
// # Why it is not in builtin
//
// It cannot be. `internal/app` imports builtin, and the daemon package imports app, so a builtin
// tool that reads daemon records closes an import cycle. Registered by cmd/magi at wiring time
// instead, which is the seam adapters are supposed to use anyway.
//
// # What it deliberately does NOT do
//
// It does not act. Listing companions and sending one work are different powers with different
// dangers, and this package holds only the first: nothing here starts a turn, cancels one, or
// writes to another workspace. A companion reading this list learns who is there and what they are
// busy with, the same way a person reading the dashboard does.
//
// # Scope
//
// This machine, under this config directory — the same set `magi --agents` prints. Companions on
// other machines are reached by a console federating another console, and that peer list belongs to
// the operator who started the console; a daemon has no copy of it and should not invent one.
package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// List is the `companions` tool.
type List struct {
	// Reader is the log-reading half of the engine. Late-bound because the App does not exist when
	// the registry is built — the same bind-after-construction the plugin observer uses.
	Reader func() fleet.Reader
	// ConfigDir is where daemons publish, and Self is this process's own socket so the list can say
	// which row is the caller. Empty Self simply marks nobody.
	ConfigDir string
	Self      string
	// Cache is optional and shared with nothing: a tool call is not a poll, so the expensive
	// derivations are paid each time unless a caller hands one in.
	Cache *fleet.Cache
}

func (List) Name() string { return "companions" }

func (List) Description() string {
	return "List the other magi running on this machine — each one's workspace, what it is doing " +
		"right now, and whether it is blocked waiting for a person. Use it when a task involves " +
		"work in another workspace, to find out whether anybody is already on it and whether they " +
		"are free. It only reads: it cannot send work to them or interrupt them."
}

func (List) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}

// Execute answers with the same derivation the dashboard draws, rendered as lines.
//
// Text rather than JSON: this is read by a model deciding whether to bother somebody, and the
// decision is made on the state and the task, not on a schema.
func (l List) Execute(ctx context.Context, _ json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if l.Reader == nil || l.Reader() == nil {
		return errText("this magi has no reader for the session store, so it cannot see the others"), nil
	}
	list, err := fleet.ListCached(ctx, l.Reader(), l.ConfigDir, l.Self, l.Cache)
	if err != nil {
		return errText("cannot read the published companions: " + err.Error()), nil
	}
	var b strings.Builder
	others := 0
	for _, a := range list {
		mine := a.Here
		if !mine {
			others++
		}
		fmt.Fprintf(&b, "%s  %s", a.Name, a.State)
		if mine {
			b.WriteString("  (this is you)")
		}
		fmt.Fprintf(&b, "\n  workspace: %s\n", a.Workdir)
		switch {
		case a.Asking != "":
			// The one state worth calling out: it is not working and will not start again until a
			// person answers, so waiting for it to finish is waiting forever.
			fmt.Fprintf(&b, "  blocked on a person: %s\n", a.Asking)
		case a.Task != "":
			fmt.Fprintf(&b, "  %s\n", a.Task)
		}
		if a.Idle >= 0 {
			fmt.Fprintf(&b, "  last moved %ds ago\n", a.Idle)
		}
	}
	if others == 0 {
		b.WriteString("No other magi is running on this machine.\n")
	}
	b.WriteString("\nThis is the whole list: reading it is all this tool does. To have one of them " +
		"do something, ask the person supervising them — nothing here can start or stop another " +
		"companion's work.")
	return session.ToolResult{Content: json.RawMessage(mustJSON(b.String()))}, nil
}

func errText(msg string) session.ToolResult {
	return session.ToolResult{IsError: true, Content: json.RawMessage(mustJSON(msg))}
}

func mustJSON(s string) []byte {
	b, err := json.Marshal(s)
	if err != nil { // a Go string always marshals; this is here so the failure is loud if it ever does not
		return []byte(`"unrenderable"`)
	}
	return b
}
