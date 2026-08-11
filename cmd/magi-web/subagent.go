package main

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
)

// The work a companion did not do itself.
//
// # Two kinds, one question
//
// A turn can be carried in part by something that is not the agent: a council member voting on
// whether it is finished, or a child spawned by a plugin's tool to go and do a piece. They are
// built differently — one is three events, the other is a session of its own — and the question a
// reader has about them is the same one. Who was this, what were they asked, what did they see,
// and what did they come back with.
//
// The terminal has answered that since it had a council: a verdict or a pane opens into a screen
// of its own. The console showed a folded row with the same one line in it that was already on the
// transcript, so "click for detail" led back to what you had just read.
//
// # Why a child is read from its own log and not from the daemon
//
// The daemon keeps a register of children that are running or just finished, because a session log
// cannot know it is over. That register is in the daemon's memory and this is a different process.
// What is durable is the child's own session — which is also the only thing that survives the
// daemon, and the week after a run is when somebody asks what a subagent actually did.
//
// So a child is found the way anything else here is found: by reading the store. A session records
// its parent when it is created, so the children of a turn are the sessions whose parent is this
// one — no registry, no second record to disagree with the first.

// subagentRow is one child of this companion's current session.
type subagentRow struct {
	ID string `json:"id"`
	// Role is what the child was spawned AS, when the spawning tool said. Empty is normal: a
	// plugin may spawn without naming a role, and inventing one here would be this process
	// deciding what somebody else's agent is for.
	Role string `json:"role,omitempty"`
	// Task is the request that opened it — the child's own title, which is the instruction it was
	// given rather than a summary of what it did.
	Task    string   `json:"task,omitempty"`
	Model   string   `json:"model,omitempty"`
	Labels  []string `json:"labels,omitempty"`
	Started string   `json:"started"`
	// Ago is seconds since it last did anything, so the page can say "3m ago" the way every other
	// list here does rather than inventing a second way to write a time.
	Ago int `json:"ago"`
	// Running is a claim this process can actually support: the child has moved recently and its
	// parent is the session that is open. A child that has been silent for a while is reported as
	// finished, which is what a log can say — the daemon's register knows better and is not here.
	Running bool `json:"running,omitempty"`
}

// childQuiet is how long a child may be silent before this process stops calling it running.
//
// Generous, because a child can spend minutes inside one tool call and a list that flickered
// between running and finished would be worse than one that lags. It is a reading of a log and
// says so; the daemon's own register is the authority and does not reach this process.
const childQuiet = 2 * time.Minute

func (s *server) subagents(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	kids, err := s.reader.ChildSessions(r.Context(), in.Workdir, in.Session)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	now := time.Now()
	out := []subagentRow{}
	for _, m := range kids {
		out = append(out, subagentRow{
			ID: string(m.ID), Role: m.Agent, Task: text.Clip(m.Title, 200), Model: m.Model,
			Labels: m.Labels, Started: m.Created.Format(time.RFC3339),
			Ago:     int(now.Sub(m.LastActivity).Seconds()),
			Running: now.Sub(m.LastActivity) < childQuiet,
		})
	}
	// Newest first: a turn spawns children as it goes, and the one you came to look at is the one
	// that just appeared.
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	writeJSON(w, "subagents", out)
}

// transcript renders ONE session of this companion's workspace as rows.
//
// The rows are built by the same code that builds the live stream, so a child's transcript reads
// like the parent's — the same folded calls, the same outcome glyphs — rather than like a second
// rendering of the same log that would drift from it.
//
// The session id arrives from a page, so it is checked against the sessions this workspace owns
// before anything is read. A path from a page must not become a path this process opens: without
// the check, "which session" would be an argument for reading any log on the machine.
func (s *server) transcript(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	want := r.URL.Query().Get("session")
	if want == "" {
		http.Error(w, "which session", http.StatusBadRequest)
		return
	}
	if !s.readable(r, in, want) {
		http.Error(w, "not this companion's to read", http.StatusNotFound)
		return
	}
	sid := session.SessionID(want)
	msgs, _, err := s.reader.SessionState(r.Context(), sid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := markPending(renderMessages(msgs))
	// A child holds councils of its own when it declares itself finished, so its transcript gets
	// them spliced back in exactly as the parent's does.
	if marks, cerr := s.reader.CouncilMarks(r.Context(), sid); cerr == nil {
		rows = spliceCouncil(rows, marks)
	}
	writeJSON(w, "transcript", rows)
}

// council answers what one round was shown, which is the half of a verdict that can be checked
// rather than weighed.
func (s *server) council(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	in, err := s.target(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	round, _ := strconv.Atoi(r.URL.Query().Get("round"))
	if round <= 0 {
		http.Error(w, "which round", http.StatusBadRequest)
		return
	}
	// A round of the session named by ?session=, when one is given — a council held by a child is
	// read from the child's log. Validated the same way, for the same reason.
	sid := session.SessionID(in.Session)
	if want := r.URL.Query().Get("session"); want != "" {
		if !s.readable(r, in, want) {
			http.Error(w, "not this companion's to read", http.StatusNotFound)
			return
		}
		sid = session.SessionID(want)
	}
	ev, found, err := s.reader.CouncilEvidenceOf(r.Context(), sid, round)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		// Not an error: a round whose convening was compacted away is a round whose evidence is
		// genuinely gone, and the page shows the verdict without it rather than a failure.
		writeJSON(w, "council", nil)
		return
	}
	writeJSON(w, "council", ev)
}

// readable reports whether a session id from a page is one this companion may be asked about.
//
// Its own conversation, or a child of it. Narrow deliberately: the id arrives in a URL, and
// without a check "which session" is an argument for reading any log on the machine — the same
// class of input the ?d= allowlist exists for one layer up.
//
// Its own conversation, a child of it, or a session this workspace ran before — the three things
// the page offers a way into. A child of an EARLIER turn is not among them: it is reachable from
// that turn's own screen, and nothing links to it from here.
func (s *server) readable(r *http.Request, in daemon.Info, want string) bool {
	if want == in.Session {
		return true
	}
	kids, err := s.reader.ChildSessions(r.Context(), in.Workdir, in.Session)
	if err != nil {
		return false
	}
	for _, k := range kids {
		if string(k.ID) == want {
			return true
		}
	}
	// And anything this WORKSPACE has run, which is what the list of past work offers to open. The
	// boundary is the workspace either way: this companion's own logs, never another's.
	past, err := s.reader.ListSessions(r.Context(), in.Workdir)
	if err != nil {
		return false
	}
	for _, m := range past {
		if string(m.ID) == want {
			return true
		}
	}
	return false
}
