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
// # Why what each one has LEARNED is in the list
//
// A companion that keeps getting the design work accumulates design skills in its own experience
// store, and that record is the honest answer to "who has done this before". Showing it makes
// specialisation happen without a router: whoever is choosing sees that one of them has twelve
// lessons about tokens and another has none, picks accordingly, and the picked one learns more.
//
// magi does not rank them. Ranking would be magi choosing the worker, which is the exact line the
// old delegation machinery crossed; showing the evidence and letting the caller choose is not. The
// difference matters in the failure case: a ranker that is wrong sends the work silently, and a
// list that is unhelpful is visibly unhelpful.
//
// # What it deliberately does NOT do
//
// It does not act. Listing companions and sending one work are different powers with different
// dangers, and this package holds only the first: nothing here starts a turn, cancels one, or
// writes to another workspace. A companion reading this list learns who is there and what they are
// busy with, the same way a person reading the dashboard does.
//
// # Why it takes a query
//
// The expensive part of a roster is not reading it off the disk — fifty companions cost 57µs — it
// is that the whole thing lands in a model's context every time somebody wants to hand out one
// piece of work. Measured on a realistic row: ~75 tokens each, so fifty is ~3,800 tokens per
// dispatch and two hundred is ~15,000.
//
// A query cuts that to the few that could plausibly do the work, which is the same win a tier of
// hub companions would buy — and buys it without the second hop, the hub that can be down, or the
// group membership that goes stale. Filtering is not choosing: everything that matched comes back,
// unranked, and the count of what did not is stated so nobody mistakes a narrow answer for the
// whole team.
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
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/core/text"

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
	return "List the other magi running on this machine — each one's workspace, what it is for, " +
		"what it is doing right now, whether it is blocked waiting for a person, and what it has " +
		"learned. Pass `matching` with a few words from the work you want done and only the " +
		"companions who could plausibly do it come back, which on a large team is the difference " +
		"between a page and a paragraph; leave it out to see everyone. It only reads: it cannot " +
		"send work to them or interrupt them."
}

func (List) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{
		"matching":{"type":"string","description":"a few words from the work, to narrow the list; omit for everyone"}
	},"additionalProperties":false}`)
}

// Execute answers with the same derivation the dashboard draws, rendered as lines.
//
// Text rather than JSON: this is read by a model deciding whether to bother somebody, and the
// decision is made on the state and the task, not on a schema.
func (l List) Execute(ctx context.Context, args json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		Matching string `json:"matching"`
	}
	if len(args) > 0 {
		// Reported rather than shrugged off. A key the tool does not declare is already caught by
		// the engine against this schema, so what is left here is a `matching` of the wrong TYPE —
		// and quietly treating that as "no filter" hands back the whole team as if it had been
		// asked for, which is the same silent-wrong-answer shape as a dropped argument.
		if err := json.Unmarshal(args, &in); err != nil {
			return errText("could not read the arguments: " + err.Error() +
				" — `matching` is a string, or leave it out to see everyone"), nil
		}
	}
	if l.Reader == nil || l.Reader() == nil {
		return errText("this magi has no reader for the session store, so it cannot see the others"), nil
	}
	list, err := fleet.ListCached(ctx, l.Reader(), l.ConfigDir, l.Self, l.Cache)
	if err != nil {
		return errText("cannot read the published companions: " + err.Error()), nil
	}
	learned := make(map[string]string, len(list))
	for _, a := range list {
		learned[a.Socket] = learnedIn(ctx, a.Workdir)
	}
	list, hidden := narrow(list, in.Matching, learned)

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
		if a.Team != "" {
			fmt.Fprintf(&b, "  [%s%s]", a.Team, map[bool]string{true: ", speaks for it"}[a.Hub])
		}
		b.WriteString("\n")
		// What it is for, first: it is the basis of the choice this list is read to make. It was
		// missing entirely until a test asked for the roster and found only names — the filter
		// matched on a role the reader could not see.
		if a.Role != "" {
			fmt.Fprintf(&b, "  %s\n", a.Role)
		}
		fmt.Fprintf(&b, "  workspace: %s\n", a.Workdir)
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
		if l := learned[a.Socket]; l != "" {
			fmt.Fprintf(&b, "  has learned: %s\n", l)
		}
	}
	if others == 0 && hidden == 0 {
		b.WriteString("No other magi is running on this machine.\n")
	}
	// Said, always. A narrowed list that does not admit it is narrowed reads as the whole team, and
	// the reader concludes nobody else could have done the work.
	if hidden > 0 {
		fmt.Fprintf(&b, "\n%d other companion(s) did not match those words. Call this again without "+
			"`matching` to see everyone.\n", hidden)
	}
	// Counted on OTHERS, not on the list: the caller is always in its own answer, so a list of one
	// is the empty result and reads as a full one unless it says so.
	if others == 0 && hidden > 0 {
		b.WriteString("Nobody else matched those words. That is not the same as nobody being there.\n")
	}
	b.WriteString("\nThis is the whole list: reading it is all this tool does. To have one of them " +
		"do something, ask the person supervising them — nothing here can start or stop another " +
		"companion's work.")
	return session.ToolResult{Content: json.RawMessage(mustJSON(b.String()))}, nil
}

// narrow keeps the companions whose declaration or record shares a word with the query.
//
// Word overlap, deliberately: the same lexical, deterministic matching the experience store uses,
// for the same reason — nothing here should depend on an embedding service being up, and a filter
// whose behaviour a person cannot predict is one they stop trusting the first time it surprises
// them. Unranked: everything that matched comes back in the listing's own order, because ordering
// by score is a hair's breadth from choosing, and choosing is the caller's.
func narrow(list []fleet.Agent, query string, learned map[string]string) ([]fleet.Agent, int) {
	terms := words(query)
	if len(terms) == 0 {
		return list, 0
	}
	kept := make([]fleet.Agent, 0, len(list))
	for _, a := range list {
		// Its own row is always kept: a caller that asked "who does design" and cannot see itself
		// in the answer may hand its own work away.
		if a.Here || matches(terms, a.Name+" "+a.Role+" "+a.Task+" "+learned[a.Socket]) {
			kept = append(kept, a)
		}
	}
	return kept, len(list) - len(kept)
}

func matches(terms []string, hay string) bool {
	low := strings.ToLower(hay)
	for _, t := range terms {
		if strings.Contains(low, t) {
			return true
		}
	}
	return false
}

// words splits a query into the terms worth matching on: short ones ("a", "of", "the") match
// everything and would turn a filter into a no-op that looks like a filter.
func words(q string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(w)) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

// learnedIn summarises a companion's own experience tier: what that workspace has accumulated,
// which is the record of what it has actually been doing.
//
// The project tier only. The global tier is this person's craft and is shared by every companion
// they run, so it says nothing about which of them to ask.
//
// Bounded hard at three, and by description rather than by name: a roster is read to make one
// choice, and a companion with forty lessons would otherwise push the rest of the team off the
// screen — which would make the list worse at the only thing it is for.
func learnedIn(ctx context.Context, workdir string) string {
	if workdir == "" {
		return ""
	}
	inv, err := expgit.New(filepath.Join(workdir, ".magi", "experience")).Inventory(ctx)
	if err != nil || len(inv) == 0 {
		return ""
	}
	// Most-observed first: a lesson re-learned four times says more about what this companion does
	// than one written down once and never seen again.
	sort.SliceStable(inv, func(i, j int) bool { return inv[i].Observed > inv[j].Observed })
	shown := make([]string, 0, 3)
	for _, e := range inv {
		if len(shown) == 3 {
			break
		}
		if d := strings.TrimSpace(e.Description); d != "" {
			shown = append(shown, text.Clip(d, 70))
		}
	}
	if len(shown) == 0 {
		return ""
	}
	out := strings.Join(shown, "; ")
	if more := len(inv) - len(shown); more > 0 {
		out += fmt.Sprintf(" (+%d more)", more)
	}
	return out
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
