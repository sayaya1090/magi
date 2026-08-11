// Package fleet answers one question for every magi running on this machine: what is it doing?
//
// It exists because two surfaces ask it — the browser dashboard and `magi --agents` — and a state
// derived twice is a state that will eventually be derived differently. The rule this repository
// keeps relearning is that the second copy does not disagree on the day it is written; it disagrees
// six weeks later, when only one of them is updated.
package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"

	"github.com/sayaya1090/magi/internal/core/text"
)

// Reader is the part of the engine a fleet view needs: the log, and nothing that runs a turn.
// Declared here rather than taking *app.App so it is visible that this package only READS.
type Reader interface {
	Interventions(ctx context.Context, sid session.SessionID) ([]app.Intervention, error)
	UnfinishedTurnOf(ctx context.Context, sid session.SessionID) (app.UnfinishedTurn, bool)
	PlanOf(ctx context.Context, sid session.SessionID) ([]session.Todo, error)
	SessionState(ctx context.Context, sid session.SessionID) ([]session.Message, int64, error)
	ListSessions(ctx context.Context, workdir string) ([]session.SessionMeta, error)
	// CouncilMarks is what the council said, anchored to the messages it said it after. A read of
	// the same log, for a surface that assembles its transcript from messages and would otherwise
	// have no way to learn that a vote happened at all.
	CouncilMarks(ctx context.Context, sid session.SessionID) ([]app.CouncilMark, error)
	// RankSessions searches this workspace's past turns. The same call the terminal's picker makes,
	// so the two surfaces cannot come to disagree about what matched.
	RankSessions(ctx context.Context, workdir, query string) ([]app.SessionHit, error)
	NewSince(ctx context.Context, sid session.SessionID, seq int64) (int64, bool, error)
}

// Cache remembers what a listing derives from a log, between polls.
//
// Both halves of a row come out of the whole log: the last thing an idle agent said needs the
// transcript rebuilt (12ms on a long session), and whether a turn is still open needs every event
// read and decoded. A dashboard refreshing every three seconds paid both, for every agent, forever,
// to re-derive answers that by definition are not changing. Asking the log whether anything arrived
// costs 2µs.
//
// Only what the LOG decides is kept, and only while the session's sequence number is unchanged. The
// rest of a row — liveness, whether the daemon is blocked on a person, how long ago it last moved —
// comes from the daemon probe and the session index, and is re-derived every time, because those
// are the answers a dashboard exists to refresh.
//
// Held together rather than in two caches: they are invalidated by the same event and read in the
// same loop, so two entries would be two chances for one of them to go stale on its own.
//
// A zero Cache is usable and does nothing, so a caller that has nowhere to keep one (the CLI, which
// runs once and exits) passes nil and pays the full price once.
type Cache struct {
	mu    sync.Mutex
	entry map[session.SessionID]cacheEntry
}

type cacheEntry struct {
	seq    int64
	said   string
	open   app.UnfinishedTurn
	isOpen bool
	plan   []session.Todo
}

// seen returns what was cached for a session and the sequence it was built at.
func (c *Cache) seen(sid session.SessionID) (cacheEntry, bool) {
	if c == nil {
		return cacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entry[sid]
	return e, ok
}

func (c *Cache) put(sid session.SessionID, e cacheEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entry == nil {
		c.entry = map[session.SessionID]cacheEntry{}
	}
	c.entry[sid] = e
}

// State is what an agent is doing, as far as anyone outside its process can tell.
type State string

const (
	// Working: the socket answers and a turn is open.
	Working State = "working"
	// Idle: the socket answers and the last turn finished.
	Idle State = "idle"
	// Waiting: the daemon is blocked on a human — a permission prompt or a question. This is the
	// one state that cannot be read from the log (see the note below), and the one that means
	// nothing moves until somebody comes.
	Waiting State = "waiting"
	// Abandoned: nobody is listening and a turn was left open — a crash, a kill, a closed laptop.
	// Every other view renders this identically to a finished session, which is why it is here.
	Abandoned State = "abandoned"
	// Stopped: nobody is listening and the last turn finished.
	Stopped State = "stopped"
	// Remote: on another machine, known from a sighting somebody passed along.
	//
	// Its own state cannot be read from here — the socket is a path on their filesystem and the
	// log is a directory on their disk — so this is not one of the four dressed up as a guess. It
	// is the honest fifth thing: it exists, this is when it was last seen, and that is all.
	//
	// A separate state rather than reusing Stopped, which is what it would have fallen through to.
	// "Stopped" asserts nobody is listening, which nothing here established, and the surfaces that
	// grey out a stopped agent would have greyed out every companion on every other machine.
	Remote State = "remote"
)

// Four of the five come from the log and the socket, which outlive the process that made them, so a
// dead daemon is still described. Waiting cannot: a permission prompt is a question about what
// should happen rather than a record of what did, so it is never written down, and the event that
// announced it went to the bus of the process that is blocked. Guessing it from "an open turn that
// has not moved in a while" would flag every slow build. So it is ASKED — the dial that proves a
// daemon alive asks while the connection is open — and it is the only state that needs the daemon
// to be there to be reported, which is right: a daemon that is gone is not waiting for you.

// Agent is one running (or lately running) magi.
type Agent struct {
	// Peer names the console this companion was reported by — empty when it is on this machine.
	// A federated view is several consoles read at once, and the pair (peer, socket) is what
	// identifies a companion once more than one machine is in the list.
	Peer    string `json:"peer,omitempty"`
	Socket  string `json:"socket"`
	Workdir string `json:"workdir"`
	Name    string `json:"name"` // the workspace's base directory — what a person calls it
	Session string `json:"session"`
	PID     int    `json:"pid"`
	// Role is what this companion is FOR, in the words of whoever set the workspace up. Empty for
	// a companion that never declared one, which is most of them and is fine — a role only means
	// something once there is a team to have one within.
	Role string `json:"role,omitempty"`
	// Team and Hub are how a group is addressed: a team name reaches its hub, and the hub is the
	// companion that answers for the rest of them.
	Team    string `json:"team,omitempty"`
	Hub     bool   `json:"hub,omitempty"`
	Host    string `json:"host"` // the machine it runs on — the only thing telling two ssh tabs apart
	Addr    string `json:"addr"`
	Live    bool   `json:"live"`
	State   State  `json:"state"`
	Asking  string `json:"asking"`  // what it is blocked on, when State is waiting
	AskID   string `json:"askId"`   // the call id an answer must carry
	AskKind string `json:"askKind"` // "permission" | "question"
	// AskIndex and AskTotal place a question in the run its call is asking, counting from one.
	// Answering one of three and being handed the next without warning is a different experience
	// from answering one of one, and the row is where a reader finds out which they are in.
	AskIndex int `json:"askIndex,omitempty"`
	AskTotal int `json:"askTotal,omitempty"`
	// Options are the answers the agent offered, when it offered any.
	//
	// A question with a list is a different question: the agent has decided the answer is one of
	// these, and typing a fourth thing is not an answer to what was asked. Carried here because
	// the console has nowhere else to learn them — the prompt is transient and belongs to the
	// daemon's bus — and without them a page drew a free-text box for a multiple choice, which
	// asks somebody to guess the wording of an option that was on screen in the terminal.
	AskOptions []string `json:"askOptions,omitempty"`
	// Report is what the agent wrote for the person to decide on. Carried whole rather than
	// summarised: a supervisor deciding for a companion on another machine has this and the
	// question, and nothing else — clipping it here would clip the grounds, which is the one part
	// that must arrive intact.
	Report []report.Filled `json:"report,omitempty"`
	Task   string          `json:"task"` // what the open turn asked for, or the last thing said
	// Waiting is how much handed-over work it has taken and not started.
	//
	// It is not derived from State, and cannot be: State is read from the session a person
	// attaches to, and handed-over work runs in conversations of its own — so a companion busy
	// with three people's requests reads as Idle, correctly, and this is the number that says
	// otherwise. From a sighting it is as old as the sighting.
	Waiting int `json:"waiting,omitempty"`
	// Handling is whether it is in the middle of a piece of handed-over work. State cannot say so:
	// that is read from the session a person attaches to, and this runs elsewhere.
	Handling bool `json:"handling,omitempty"`
	Steps    int  `json:"steps"` // tool calls the open turn has made — what a crash would cost
	// Doing is what a long-running tool inside the open turn last reported: "check 6, 4m elapsed,
	// still running". Only ever set on a WORKING agent — the note is a live one, and on an idle or
	// stopped agent it would be the last thing said before the turn ended, dressed up as news.
	//
	// This is the difference between "working" and knowing whether to intervene. Steps answers it
	// for a turn making calls; it answers nothing for a turn that has been inside one call for ten
	// minutes, which is exactly when somebody starts wondering if it is stuck.
	Doing string `json:"doing,omitempty"`
	// Permission is the approval mode this companion is on right now — ask, auto, allow or deny.
	// It changes at runtime and decides both what gets a prompt and how long that prompt waits, so
	// a console offering to change it has to be able to say what it is changing from.
	Permission string `json:"permission,omitempty"`
	// User is what this companion calls the person, when a plugin has renamed them (an SSO bridge
	// puts the authenticated username there). Empty means nobody has, and every surface says the
	// same ordinary word for the person instead of each inventing its own.
	User string `json:"user,omitempty"`
	// PlanDone and PlanTotal are the agent's own todo list, counted. "working" says it is alive;
	// "working · 3/7" says whether it is getting anywhere, which is the question somebody has when
	// they look twice in ten minutes.
	PlanDone  int  `json:"planDone,omitempty"`
	PlanTotal int  `json:"planTotal,omitempty"`
	Idle      int  `json:"idle"` // seconds since the last event in the log; -1 if unknown
	Here      bool `json:"here"` // the daemon in the directory the caller is standing in
	// Does names what this companion advertises being able to do, and Can is how many there are —
	// more than Does lists when it has a lot. A sample and a count, not a list and its length: the
	// names are capped for the wire (see cluster.MaxDoes) and the count is not.
	Does []string `json:"does,omitempty"`
	Can  int      `json:"can,omitempty"`
}

// List describes every daemon published under configDir, newest first. here is the socket the
// caller considers its own (empty if none); it is only marked, never filtered on.
func List(ctx context.Context, r Reader, configDir, here string) ([]Agent, error) {
	return ListCached(ctx, r, configDir, here, nil)
}

// ListCached is List with somewhere to keep the part that does not change between polls.
func ListCached(ctx context.Context, r Reader, configDir, here string, cache *Cache) ([]Agent, error) {
	found, err := daemon.List(configDir)
	if err != nil {
		return nil, err
	}
	// One ListSessions per distinct workspace, not per daemon: several daemons in one tree is a
	// normal thing to do, and re-reading the directory for each of them is the same answer again.
	seen := map[string][]session.SessionMeta{}
	out := make([]Agent, 0, len(found))
	for _, in := range found {
		a := Agent{
			Socket: in.Socket, Workdir: in.Workdir, Name: nameOf(in),
			Session: in.Session, PID: in.PID, Role: in.Role, Team: in.Team, Hub: in.Hub,
			Host: in.Host, Addr: in.Addr, Does: in.Does, Can: in.Can, Waiting: in.Waiting, Handling: in.Handling,
			Permission: in.Permission, User: in.User,
			Live: in.Live, Here: here != "" && in.Socket == here,
			Idle: -1,
		}
		sid := session.SessionID(in.Session)
		metas, ok := seen[in.Workdir]
		if !ok {
			metas, _ = r.ListSessions(ctx, in.Workdir)
			seen[in.Workdir] = metas
		}
		for _, m := range metas {
			if m.ID == sid && !m.LastActivity.IsZero() {
				a.Idle = int(time.Since(m.LastActivity).Seconds())
			}
		}
		open, isOpen, said, plan := fromLog(ctx, r, cache, sid)
		a.PlanDone, a.PlanTotal = app.PlanProgress(plan)
		switch {
		case in.Live && in.Asking != nil:
			// Blocked on a person beats anything the log says: the log shows an open turn, which
			// is true and is not the thing that needs doing about it.
			a.State, a.Steps, a.Asking = Waiting, open.Steps, describeAsk(in.Asking)
			a.AskID, a.AskKind = in.Asking.ID, in.Asking.Kind
			a.AskIndex, a.AskTotal = in.Asking.Index, in.Asking.Total
			a.AskOptions = in.Asking.Options
			a.Report = in.Asking.Report
			a.Task = Clip(theWork(open.Text), 160)
		case in.Live && isOpen:
			a.State, a.Steps, a.Task = Working, open.Steps, Clip(theWork(open.Text), 160)
			a.Doing = Clip(in.Doing, 160)
		case in.Live:
			a.State = Idle
		case isOpen:
			a.State, a.Steps, a.Task = Abandoned, open.Steps, Clip(theWork(open.Text), 160)
		default:
			a.State = Stopped
		}
		if a.Task == "" {
			a.Task = Clip(said, 160)
		}
		out = append(out, a)
	}
	now := time.Now()
	out = append(out, elsewhere(configDir, now, out)...)
	electHubs(out, daemon.Known(configDir, now), now)
	return out, nil
}

// elsewhere turns the companions on other machines into rows.
//
// Everything a local row gets from a log or a dial is absent here and stays absent: no task, no
// step count, no plan, no last thing said. All of those live on their machine. Inventing any of
// them — "idle", "0 steps" — would put a claim on the screen that nothing established, and the
// reader has no way to tell it from a claim that was.
func elsewhere(configDir string, now time.Time, seen []Agent) []Agent {
	ms := daemon.Elsewhere(configDir, now)
	out := make([]Agent, 0, len(ms))
	for _, m := range ms {
		if amongst(seen, m) {
			continue
		}
		name := m.Name
		if name == "" {
			name = filepath.Base(m.Workdir)
		}
		out = append(out, Agent{
			Socket: m.Socket, Workdir: m.Workdir, Name: name,
			Role: m.Role, Team: m.Team, Hub: m.Hub, Host: m.Host,
			Does: m.Does, Can: m.Can, Waiting: m.Waiting, Handling: m.Handling,
			// Live is "believed reachable", and for a companion on another machine the evidence
			// is a sighting rather than a dial. Weaker, and named as such by the state beside it:
			// anything acting on this has to look at State too, and Remote is not a state anything
			// treats as ordinary.
			Live:  m.Fresh(now),
			State: Remote,
			Idle:  int(now.Sub(m.Seen).Seconds()),
		})
	}
	return out
}

// amongst reports whether a sighting describes a companion this machine has already listed by
// reading it directly.
//
// It happens wherever a config directory is shared — two containers with a mount in common, two
// workstations with one network home. Sockets live in the config directory, so each of them reads
// the other's published record straight out of the directory AND hears about it round the cluster,
// and the same companion comes back twice.
//
// Not a cosmetic problem. An address matching two rows is refused as ambiguous, and both rows
// answer to the same name — so the refusal offers two identical candidates and there is nothing
// the reader can do with it. A companion becomes unaddressable by being seen too well.
//
// The socket alone is not identity: the same path on two machines is two different companions,
// which is the whole reason work never travels to a path. The socket AND the machine the record
// names is. Matched, this is the record read directly rather than heard about, which is strictly
// better evidence; unmatched, the path here belongs to somebody else and both rows are real.
func amongst(seen []Agent, m cluster.Member) bool {
	for _, a := range seen {
		if a.Socket == m.Socket && strings.EqualFold(a.Host, m.Host) {
			return true
		}
	}
	return false
}

// electHubs replaces what each companion DECLARED about being a hub with who actually answers for
// its team right now.
//
// Read from config, Hub was a claim that outlived the process making it: the declared hub goes
// down and its team stops being addressable, because every reader is still looking for a companion
// that is not there. Computed, the team keeps a speaker — and the moment the declared one is seen
// again it takes the role back, because nothing was stored to un-store.
//
// Over the whole cluster and not just this machine, which is the point of having one: a team split
// across two hosts has one hub between them, not one each.
func electHubs(rows []Agent, members []cluster.Member, now time.Time) {
	teams := map[string]bool{}
	for _, a := range rows {
		if a.Team != "" {
			teams[strings.ToLower(a.Team)] = true
		}
	}
	for team := range teams {
		who, _, ok := cluster.Speaker(members, team, now)
		for i := range rows {
			if !strings.EqualFold(rows[i].Team, team) {
				continue
			}
			// A team whose members have all gone quiet elects nobody, and every one of them stops
			// claiming the role. Leaving the declaration standing would be the old failure with an
			// election bolted on: a hub that is not there, still being addressed.
			rows[i].Hub = ok && rows[i].Host == who.Host && rows[i].Socket == who.Socket
		}
	}
}

// Resolve finds the companions an address names: an exact name, or a role that contains it.
//
// Here rather than in either caller because a console and a companion must not disagree about who
// "design" is. Two resolvers would not differ on the day they were written; they would differ the
// first time one of them learned to match something the other did not, and the symptom would be
// work arriving somewhere nobody chose.
//
// A chosen name beats a role: it was picked to address this one companion, while a role is a
// sentence that may well describe several. A companion named "design" and another whose role
// mentions design in passing is not an ambiguity — the first one is the answer.
//
// Returning every match rather than the best one is deliberate. Ranking would make the caller's
// refusal impossible, and refusing is the right behaviour: the cost of guessing is a turn running
// in somebody else's workspace.
func Resolve(list []Agent, to string) []Agent {
	want := strings.ToLower(strings.TrimSpace(to))
	if want == "" {
		return nil
	}
	var byName, byTeam, byRole []Agent
	for _, a := range list {
		switch {
		case strings.EqualFold(a.Name, want):
			byName = append(byName, a)
		case a.Team != "" && strings.EqualFold(a.Team, want):
			byTeam = append(byTeam, a)
		case a.Role != "" && strings.Contains(strings.ToLower(a.Role), want):
			byRole = append(byRole, a)
		}
	}
	if len(byName) > 0 {
		return byName
	}
	// A team name reaches whichever member has the least on it, and the hub when they are level.
	//
	// It used to reach the hub, always, and that made a team of five work like a queue of one: a
	// companion cannot pass handed-over work on — the no-chaining rule stops it — so everything
	// addressed to the team piled up behind whoever had been elected. Which is the opposite of what
	// somebody who starts a second copy because the first keeps refusing is trying to buy.
	//
	// The hub still wins a tie, so a team that is doing nothing behaves exactly as it did: one
	// stable address, elected rather than declared (see electHubs), so a team stops being
	// addressable when everybody in it stops and never because nobody typed a word in a config file.
	// When nobody is running the members come back for the caller to choose between, and that
	// refusal is the honest answer: there is nobody to hand it to.
	if len(byTeam) > 0 {
		if free := lightest(byTeam); len(free) == 1 {
			return free
		}
		return byTeam
	}
	return byRole
}

// lightest is the running member with the least handed over to it, or nothing if none is running.
//
// Load is counted as pieces, in hand and behind: a companion doing one thing with nothing queued is
// ahead of one doing one thing with two queued, and both are behind one doing nothing. Sorting on
// the same numbers the roster shows is deliberate — a person who reads that list and picks the
// emptiest name should get what the fleet would have picked for them.
//
// The hub breaks a tie, which is what keeps an idle team behaving exactly as it always has: one
// address, and the same one every time. Name breaks the rest, so the choice does not depend on
// which order the sockets happened to be read in.
func lightest(team []Agent) []Agent {
	var best Agent
	found := false
	for _, a := range team {
		if !a.Live {
			continue // load 0 because it is not there, which would win every time
		}
		if !found || lighter(a, best) {
			best, found = a, true
		}
	}
	if !found {
		return nil
	}
	return []Agent{best}
}

func lighter(a, b Agent) bool {
	if la, lb := load(a), load(b); la != lb {
		return la < lb
	}
	if a.Hub != b.Hub {
		return a.Hub
	}
	return a.Name < b.Name
}

// load is how many pieces of handed-over work a companion has on it.
func load(a Agent) int {
	n := a.Waiting
	if a.Handling {
		n++
	}
	return n
}

// Carrying is what a companion has in hand and behind it, in the words an asker needs.
//
// Exported because the shell listing says the same thing, and two spellings of one fact is how a
// person comes to think they are two facts.
//
// Both halves or neither: one piece in hand and nothing queued means the next request waits for one
// turn, while three queued behind it means something else entirely, and a reader given only one of
// the two numbers cannot tell those apart. Silent when there is nothing, because "0 waiting" on
// every idle companion is a column of noise.
func Carrying(a Agent) string {
	var parts []string
	if a.Handling {
		parts = append(parts, "1 in hand")
	}
	if a.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", a.Waiting))
	}
	return strings.Join(parts, ", ")
}

// RosterLines is the roster as a list, one companion per line, for a tool DESCRIPTION.
//
// Separate from Roster because the two are read in different places and one shape does not serve
// both. Roster's comma-run is a sentence inside a refusal, where it is one clause of an answer.
// This goes into a description a model reads while deciding, where a run of five names and five
// parenthesised roles is a wall — and being a list is what makes it read as "these are the
// options" rather than as prose about companions.
//
// here is this companion's own socket, and it is left OUT: a list that offers you yourself is an
// invitation to a refusal. A companion that is not running is left out for the same reason.
//
// A companion on another machine IS offered, and its host is on the line. Both halves matter: work
// crosses now, so leaving it out would hide a companion that can do the job — and the machine has
// to be visible because it is the difference between a name that is unique here and one that is
// unique in the cluster. Two hosts can each have a "design".
func RosterLines(list []Agent, here string) string {
	found := Addressable(list, here)
	out := make([]string, 0, len(found))
	for _, a := range found {
		line := "  " + a.Name
		if a.State == Remote {
			line += " (on " + a.Host + ")"
		}
		if a.Team != "" {
			line += " [" + a.Team + "]"
		}
		if a.Role != "" {
			line += " — " + Clip(a.Role, 100)
		}
		if can := abilities(a); can != "" {
			line += " · can: " + can
		}
		// Shown, never sorted on. A list in load order is a recommendation, and choosing who does
		// the work is the model's. Absent when nothing is waiting, which is most of the time.
		if load := Carrying(a); load != "" {
			line += " · " + load
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

// Addressable is who a roster offers: named, running, and not the caller.
//
// Its own function because the count matters as much as the lines do. A description that lists one
// companion is a list with no choice in it, and anything that exists to inform a choice — how past
// hand-offs went, most obviously — is pure weight in every prompt of every step when there is only
// one candidate. The weight of what a model is shown while deciding is a cost this tree has
// measured; a caller cannot weigh it against a number it has to guess by counting lines.
func Addressable(list []Agent, here string) []Agent {
	out := make([]Agent, 0, len(list))
	for _, a := range list {
		if a.Name == "" || !a.Live || (here != "" && a.Socket == here) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// abilities is the capability names on a roster line, as much of them as a line can hold.
//
// Names and no descriptions, because that is all that crosses a machine — and because this goes
// into a tool description a model reads while deciding, where twelve one-line descriptions per
// companion would be a wall rather than a list of options. What each one means is fetched from the
// companion that has it, when somebody wants to know.
//
// The count is shown when it is larger than the sample, so "+4" says the list is not the whole
// answer rather than quietly implying it is.
func abilities(a Agent) string {
	if len(a.Does) == 0 {
		return ""
	}
	shown, width := make([]string, 0, len(a.Does)), 0
	for _, d := range a.Does {
		if width+len(d) > 90 {
			break
		}
		width += len(d) + 2
		shown = append(shown, d)
	}
	if len(shown) == 0 {
		return ""
	}
	out := strings.Join(shown, ", ")
	if more := a.Can - len(shown); more > 0 {
		out += fmt.Sprintf(" (+%d)", more)
	}
	return out
}

// Roster is who is here, for the message when an address matched nobody: the next thing anybody
// does is address one of them, and they cannot if they do not know who there is.
func Roster(list []Agent) string {
	if len(list) == 0 {
		return "(nobody — no companion is published here)"
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Role != "" {
			out = append(out, a.Name+" ("+Clip(a.Role, 60)+")")
			continue
		}
		out = append(out, a.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Names is who matched, for the message when several did. Peer-qualified, because two machines can
// each have a "design" and the whole point of the refusal is that the difference matters.
//
// Anything still spelled the same as something else gets its workspace, which is the one thing that
// is never shared. A refusal saying "design and design — name one of them" asks for a distinction
// it has just declined to make, and there is nothing the reader can do with it.
func Names(list []Agent) string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, qualified(a))
	}
	seen := map[string]int{}
	for _, n := range out {
		seen[n]++
	}
	for i, a := range list {
		if seen[out[i]] > 1 && a.Workdir != "" {
			out[i] += " (" + a.Workdir + ")"
		}
	}
	sort.Strings(out)
	return strings.Join(out, " and ")
}

// qualified is a name with the machine in front of it, when it is on another one.
func qualified(a Agent) string {
	if a.Peer != "" {
		return a.Peer + "/" + a.Name
	}
	if a.Host != "" && a.State == Remote {
		return a.Host + "/" + a.Name
	}
	return a.Name
}

// nameOf is what a person calls this companion: what it declared, or failing that its directory.
//
// A declared name wins because it is the one somebody else can address, and a directory name is
// only ever a guess at it. The fallbacks are for a workspace at "/" or a relative path, where the
// base name is a character nobody could pick out of a list.
func nameOf(in daemon.Info) string {
	if n := strings.TrimSpace(in.Name); n != "" {
		return n
	}
	base := filepath.Base(in.Workdir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return in.Workdir
	}
	return base
}

// fromLog is everything about a row the log alone decides, answered from the cache while the
// session is not moving and rebuilt when it is.
//
// One question per session per poll. Asked FROM the sequence the cached answer was built at, which
// is what makes it cheap: the store answers with a binary search over the tail rather than a read
// of the log. Asking from zero would be the same answer at two hundred times the price, and this
// runs for every agent on every refresh.
func fromLog(ctx context.Context, r Reader, cache *Cache, sid session.SessionID) (app.UnfinishedTurn, bool, string, []session.Todo) {
	from := int64(0)
	if e, ok := cache.seen(sid); ok {
		seq, changed, err := r.NewSince(ctx, sid, e.seq)
		if err == nil && !changed {
			return e.open, e.isOpen, e.said, e.plan
		}
		if err == nil {
			return rebuild(ctx, r, cache, sid, seq)
		}
		// The probe failed. Derive it anyway — a listing that dropped a row because one cheap
		// question errored would be a dashboard that hides the agent whose store is in trouble.
		from = e.seq
	}
	seq, _, err := r.NewSince(ctx, sid, from)
	if err != nil {
		// Uncached: without a sequence there is no key, so this pays full price every time rather
		// than caching an answer it cannot invalidate.
		open, isOpen := r.UnfinishedTurnOf(ctx, sid)
		plan, _ := r.PlanOf(ctx, sid)
		return open, isOpen, lastSaid(ctx, r, sid), plan
	}
	return rebuild(ctx, r, cache, sid, seq)
}

func rebuild(ctx context.Context, r Reader, cache *Cache, sid session.SessionID, seq int64) (app.UnfinishedTurn, bool, string, []session.Todo) {
	open, isOpen := r.UnfinishedTurnOf(ctx, sid)
	said := lastSaid(ctx, r, sid)
	plan, _ := r.PlanOf(ctx, sid) // a plan that cannot be read is no plan, not a reason to drop the row
	cache.put(sid, cacheEntry{seq: seq, said: said, open: open, isOpen: isOpen, plan: plan})
	return open, isOpen, said, plan
}

// lastSaid is the final piece of text in a session — what an idle agent left behind.
func lastSaid(ctx context.Context, r Reader, sid session.SessionID) string {
	msgs, _, err := r.SessionState(ctx, sid)
	if err != nil {
		return ""
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		for j := len(msgs[i].Parts) - 1; j >= 0; j-- {
			if p := msgs[i].Parts[j]; p.Kind == session.PartText && p.Text != "" {
				return p.Text
			}
		}
	}
	return ""
}

// Moment is one intervention with the companion it happened in.
type Moment struct {
	Companion string `json:"companion"`
	// Socket identifies the companion for a promotion to be aimed at: a name is what a person
	// reads and a socket is what the console resolves.
	Socket   string `json:"socket"`
	Peer     string `json:"peer,omitempty"`
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	At       string `json:"at"`       // RFC3339
	AfterSec int    `json:"afterSec"` // how far into the turn the person stepped in
}

// Interventions gathers what a person had to step in and say, across every companion, newest first.
//
// The supervisor's evening question — what did I correct today — cannot be answered one companion at
// a time: the whole value is seeing that the SAME correction went to three of them, which is what
// makes it a rule rather than a remark. So it is collected here, where the list of companions
// already is.
func Interventions(ctx context.Context, r Reader, configDir string, since time.Duration) ([]Moment, error) {
	found, err := daemon.List(configDir)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-since)
	// An empty result is an empty ARRAY, never null. A JSON list endpoint that answers null where
	// a client expects a list is a trap for every one of them — the page iterates what it gets, and
	// the very first supervisor with nothing to promote would have seen a blank screen. Seen exactly
	// that way, live, before anybody had steered anything.
	out := []Moment{}
	for _, in := range found {
		if in.Session == "" {
			continue
		}
		ivs, ierr := r.Interventions(ctx, session.SessionID(in.Session))
		if ierr != nil {
			continue // a companion whose log cannot be read is not a reason to answer nothing
		}
		name := nameOf(in)
		for _, iv := range ivs {
			if since > 0 && iv.At.Before(cutoff) {
				continue
			}
			out = append(out, Moment{
				Companion: name, Socket: in.Socket, Kind: iv.Kind, Text: Clip(iv.Text, 400),
				At: iv.At.UTC().Format(time.RFC3339), AfterSec: iv.AfterSec,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out, nil
}

// DispatchMark opens the sentence that says a turn was started by another companion.
//
// It is three things at once, deliberately: the label a person reads, the fact the no-chaining rule
// is read off, and the thing this package greps for to reconstruct who handed what to whom. A
// marker with no meaning to a human is one that gets "cleaned up" by somebody who does not know
// what it is for; a marker with three readers must have exactly one spelling.
const DispatchMark = "— asked by "

// theWork is what a companion is doing, with the attribution line taken off the front.
//
// A handed-over request is a label saying who asked, a blank line, and then the request. The label
// is for a person reading the transcript; it is not the work. Left on, every companion busy with
// handed-over work described itself with the same sentence — "design is mid-turn (— asked by master
// on mini, whose companions you cannot reach)" — which says everything except what is being done,
// in the refusal whose whole job is to tell you whether to wait or ask somebody else.
//
// Only the mark is stripped, never a general first line: an ordinary request whose first paragraph
// happened to be short would otherwise be reported by its second.
func theWork(text string) string {
	if !strings.HasPrefix(text, DispatchMark) {
		return text
	}
	if _, rest, ok := strings.Cut(text, "\n\n"); ok && strings.TrimSpace(rest) != "" {
		return rest
	}
	return text
}

// DispatchedBy renders the label.
func DispatchedBy(who string) string {
	return DispatchMark + who + ", another companion on this machine. Answer it here; they will " +
		"read what you say from your transcript, and you can reach them with mcp__" + who +
		"__ask if you need something from them to do it."
}

// DispatchedFrom is the label on work handed over from another MACHINE.
//
// The same mark, so the handoff view and the no-chaining rule keep reading one spelling — and a
// different sentence after it, because both halves of the local one are false here. They are not
// "on this machine", and there is no mcp__<who>__ask to reply through: the ear is a peer attached
// at startup from this machine's own config, and a companion two hosts away is not in it.
//
// Saying so is the point. The failure this tree has written down is a description that names a way
// to do something that does not exist — a model told to reach somebody it cannot will spend a turn
// finding that out. So the label says plainly that the asker cannot be reached, and what to do
// instead: answer in the transcript, and if the work cannot proceed, say what is missing and stop.
func DispatchedFrom(who, host string) string {
	where := "another machine"
	if strings.TrimSpace(host) != "" {
		where = host
	}
	return DispatchMark + who + " on " + where + ", another companion in this cluster. They are " +
		"reading your transcript for the answer and you cannot reach them from here — there is no " +
		"reply channel across machines. Answer it here. If you cannot proceed without something " +
		"only they know, say exactly what is missing and stop; they will read that."
}

// WordMark opens a message a companion put straight into this one's conversation, rather than a
// piece of work it handed over.
//
// A DIFFERENT marker on purpose, and not a variation of the one above. The no-chaining rule and the
// handoff view both read DispatchMark to mean "this turn was started by somebody else", and a
// mid-exchange question carrying that meaning would freeze the receiver out of handing anything on
// for the rest of its turn — because of a sentence, not because of anything it did.
const WordMark = "— from "

// WordFrom renders the label on a message that arrived through this companion's ear.
func WordFrom(who string) string {
	return WordMark + who + ", another companion on this machine, while you are both working. " +
		"It is part of an exchange already under way, not a new request: fold it into what you are " +
		"doing. Reply with mcp__" + who + "__ask if it wants an answer."
}

// Handoff is one piece of work a companion handed to another, and what became of it.
//
// Derived from the transcripts, like everything else here: the label is in the receiver's log, so
// the record exists without anybody having written a handoff down. Which matters more than it
// sounds — it means the asker does not have to be running, or to have kept anything in memory, for
// the answer to be findable.
type Handoff struct {
	From    string `json:"from"`    // who asked, as the label names them
	To      string `json:"to"`      // the companion it was handed to
	Socket  string `json:"socket"`  // …and how to open it
	Request string `json:"request"` // what was asked, as it was written
	// No timestamp. A rebuilt transcript carries no per-message time — that lives on the events,
	// which this package's Reader deliberately does not expose — and inventing one from the
	// session's last activity would date every handoff to the same moment. What a person actually
	// wants from this view is "is it done and what did it say", and both of those are here; the
	// receiver's idle time on its own row says how fresh the answer is.
	// State and Answer are where it got to: the receiver's state now, and — when it is idle — the
	// last thing it said, which IS the answer. There is no reply channel; this is the reply.
	State  State  `json:"state"`
	Answer string `json:"answer,omitempty"`
}

// Handoffs reads what was handed to the companions here, newest first.
//
// from is optional: naming it answers "what did I hand out and what came back", which is the
// question an orchestrator has. Leaving it empty answers "what is being handed around here", which
// is the question a supervisor has.
func Handoffs(ctx context.Context, r Reader, configDir, from string, cache *Cache) ([]Handoff, error) {
	agents, err := ListCached(ctx, r, configDir, "", cache)
	if err != nil {
		return nil, err
	}
	out := []Handoff{}
	for _, a := range agents {
		if a.Session == "" {
			continue
		}
		msgs, _, merr := r.SessionState(ctx, session.SessionID(a.Session))
		if merr != nil {
			continue // one unreadable log is not a reason to answer nothing
		}
		for _, m := range msgs {
			if m.Role != session.RoleUser {
				continue
			}
			for _, p := range m.Parts {
				who, req, ok := parseHandoff(p.Text)
				if !ok || (from != "" && !strings.EqualFold(who, from)) {
					continue
				}
				h := Handoff{From: who, To: a.Name, Socket: a.Socket, Request: Clip(req, 400),
					State: a.State}
				// The answer only when the work is over. A line taken mid-turn is whatever it
				// happened to be saying, which reads as a conclusion and is not one.
				if a.State == Idle || a.State == Stopped {
					h.Answer = Clip(a.Task, 400)
				}
				out = append(out, h)
			}
		}
	}
	// Grouped by who has it, and in transcript order within each — the order they were asked in,
	// which is the only ordering the seam actually knows.
	sort.SliceStable(out, func(i, j int) bool { return out[i].To < out[j].To })
	return out, nil
}

// parseHandoff splits a labelled request back into who asked and what they asked.
func parseHandoff(text string) (who, request string, ok bool) {
	if !strings.HasPrefix(text, DispatchMark) {
		return "", "", false
	}
	rest := strings.TrimPrefix(text, DispatchMark)
	who, after, cut := strings.Cut(rest, ",")
	if !cut {
		return "", "", false
	}
	// The request is what follows the label's own paragraph. Everything before the blank line is
	// the sentence this package wrote; everything after is the caller's words, untouched.
	_, req, split := strings.Cut(after, "\n\n")
	if !split {
		return "", "", false
	}
	return strings.TrimSpace(who), req, true
}

// Clip shortens s to at most n bytes on a rune boundary, marking that it was cut.
func Clip(s string, n int) string { return text.Clip(s, n) }

// commandOf digs the human-meaningful field out of a tool call's arguments. Best effort by design:
// an unrecognised shape falls back to the tool name rather than dumping JSON onto a card.
func commandOf(args []byte) string {
	if len(args) == 0 {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return ""
	}
	for _, k := range []string{"command", "cmd", "url", "path", "file_path"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// describeAsk turns a pending prompt into the line a person reads on a card.
func describeAsk(w *daemon.Waiting) string {
	if w == nil {
		return ""
	}
	switch w.Kind {
	case "permission":
		// The command, not just the tool. "permission: bash" asks somebody to approve a category;
		// the decision is about what it is going to run.
		if cmd := commandOf(w.Args); cmd != "" {
			if w.Reason != "" {
				return w.What + ": " + Clip(cmd, 100) + "  (" + w.Reason + ")"
			}
			return w.What + ": " + Clip(cmd, 120)
		}
		return "permission: " + w.What
	case "question":
		return Clip(w.What, 120)
	}
	return Clip(w.What, 120)
}
