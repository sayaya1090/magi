// Package daemon lets a magi engine outlive the screen in front of it.
//
// Until now the two were one process: the terminal UI held the App by pointer, and quitting the UI
// ended the work. That is the right shape for one person at one terminal, and the wrong one for a
// process meant to stay up — you cannot look away, you cannot look from somewhere else, and you
// cannot watch two at once.
//
// # What crosses the socket, and why it is five things
//
// Almost everything the screen asks the engine is a READ, and both processes can do their own: the
// session log is an append-only file and the store is already a port, so an attached UI builds its
// own App over the same directory and reconstructs the transcript itself. That is not a hole in the
// boundary — it is the store port used from a second process, which is what the ports having been
// separated was for.
//
// What cannot be done twice is anything touching the RUN: the goroutine driving the loop, the
// cancel function, the tool waiting for an answer. Those live in one process's memory and only that
// process can act on them. So they are what the socket carries, and there are five:
//
//	submit / steer     — start work, or steer work already running
//	interrupt          — cancel the live turn
//	permission / answer — reply to a tool that is blocked waiting
//
// Everything else stayed local, which is why this file is short. Wrapping all 42 methods of the
// screen's boundary would have been about fifteen hundred lines of envelope for thirty-seven
// answers both sides could already work out.
//
// # Unix socket, on purpose
//
// A filesystem path with filesystem permissions, not a port. It cannot be reached from the network
// by accident, it cannot collide with another service, and it disappears with the directory. Remote
// access is `ssh -L` or `ssh host magi attach`, which means the authentication and the encryption
// are OpenSSH's rather than something written here — the part of a control channel that is easiest
// to get subtly wrong.
//
// This is NOT magi.serve widened. That is a plugin's application server, loopback-bound for browser
// callbacks and an LLM proxy, and its handler runs inside the plugin's single Lua state serialised
// with tool calls. It could not be a control channel whatever address it bound.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	osuser "os/user"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/version"
)

// Engine is the part of the app a daemon exposes: the calls that only the process holding the run
// can make. Declared here rather than imported so this package depends on behaviour, not on App.
type Engine interface {
	Submit(ctx context.Context, c command.SubmitPrompt) error
	Steer(ctx context.Context, c command.SubmitPrompt) error
	Interrupt(ctx context.Context, c command.Interrupt) error
	RespondPermission(ctx context.Context, c command.RespondPermission) error
	RespondQuestion(ctx context.Context, c command.RespondQuestion) error
	// Waiting is the one READ that crosses, and it earns the crossing the same way the writes do:
	// a prompt the engine is blocked on exists only in that process's memory. It is not in the log
	// (it is a question about what should happen, not a record of what did) and the event that
	// announced it went to that process's bus. From outside, an agent waiting for a human is
	// indistinguishable from one running a slow build — the single most important difference for
	// somebody watching a fleet.
	Waiting(sid session.SessionID) (app.Ask, bool)
	// Doing is the other half of that answer, and is here rather than behind an optional
	// interface because it is the same KIND of fact: a long-running tool's progress note rides a
	// transient event, which is delivered to the engine's own bus and never written to the log.
	// Split from Waiting it would be a second mechanism answering one question — "what is this
	// daemon on right now" — and the two would drift the first time one of them was fixed.
	Doing(sid session.SessionID) (string, bool)
}

// Jobs is the work a companion has running beside its turn: commands it started in the background,
// and children it spawned.
//
// # Why this crosses at all
//
// Both registers live in the memory of the process that started them — a background command is a
// PID this process is waiting on, and the child register is what a session log cannot tell you: a
// log does not know it is over. A viewer holding its own App read its own empty registers, so the
// strip along the bottom of the terminal — the one place a five-minute build or a spawned child is
// visible while it runs — was empty in every attached window and on every page.
//
// The children's CONTENT does not cross and must not: a child writes its own session, both
// processes can read it, and sending its transcript down this socket would be a second copy of
// something already shared. What crosses is the fact of it — which children exist, and which just
// ended.
type Jobs struct {
	Background []BackgroundJob `json:"background,omitempty"`
	Children   []ChildJob      `json:"children,omitempty"`
	// Queued is what will run NEXT, in the order it will run: the person's own parked words first,
	// then work handed over by somebody else.
	//
	// Two queues, deliberately — they are different contracts, one refuses past a handful and the
	// other never refuses the person in front of the thing — but one ORDER, because there is one
	// agent and it does one thing at a time. A screen that showed only the handovers said a
	// companion had nothing waiting while the correction you typed sat in the other queue.
	Queued []QueuedWork `json:"queued,omitempty"`
}

// QueuedWork is one thing waiting for the workspace.
type QueuedWork struct {
	// Kind is "person" — typed here, into a turn that was already running — or "handover", asked
	// by another companion. It decides the order and it is the thing a reader needs first.
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	// From names the asker, for a handover.
	From string `json:"from,omitempty"`
}

// BackgroundJob is one command the agent left running, with the tail of what it has written.
//
// The tail rides along rather than being a second call: the strip shows one for every job on every
// poll, so fetching them separately would be a round trip per job per tick.
type BackgroundJob struct {
	ID      string `json:"id"`
	Command string `json:"command,omitempty"`
	Running bool   `json:"running,omitempty"`
	Killed  bool   `json:"killed,omitempty"`
	Exit    int    `json:"exit,omitempty"`
	Started string `json:"started,omitempty"`
	Tail    string `json:"tail,omitempty"`
}

// ChildJob is one agent this companion spawned.
type ChildJob struct {
	ID      string `json:"id"`
	Tool    string `json:"tool,omitempty"`
	Task    string `json:"task,omitempty"`
	Started string `json:"started,omitempty"`
	Ended   string `json:"ended,omitempty"`
	Running bool   `json:"running,omitempty"`
	Steps   int    `json:"steps,omitempty"`
	Err     string `json:"err,omitempty"`
}

// JobRunner is an engine that is running things beside the turn.
//
// Optional, like the rest: an engine that has no such register answers with nothing, and a viewer
// draws no strip rather than failing.
type JobRunner interface {
	BackgroundJobs() []app.BackgroundJob
	BackgroundTail(id string, max int) string
	SubagentJobs() []app.SubagentJob
	// QueuedWork is what has not started yet, in the order it will. Both queues, because there is
	// one agent — see Jobs.Queued.
	QueuedWork() []QueuedWork
}

// jobTailBytes is how much of a background command's output rides on one reply. The strip shows a
// tail, so more could not be displayed; the cap is what keeps a runaway log off the socket four
// times a second.
const jobTailBytes = 8 << 10

// ModelLister is an engine that can say which models it could run on.
//
// Over the socket for the same reason the roster is: the list comes from the backend this daemon is
// configured against — asked of it, live — so a viewer built from its own config would offer models
// this companion cannot reach and omit the ones it can. Optional: a daemon that cannot say leaves
// the list empty, and the screen then shows the model it is on without offering to change it.
type ModelLister interface {
	ListModels(ctx context.Context) ([]string, error)
}

// ToolLister is an engine that can say which tools it is running with.
//
// Asked over the socket rather than assembled by the reader, because the roster is built at startup
// from the config, the plugins that loaded and the MCP servers that answered — a console listing
// the built-ins would be describing a companion that does not exist. Optional, like the rest: one
// that cannot say says nothing, and the screen reports that rather than inventing a list.
type ToolLister interface {
	ToolNames() []string
}

// ToolServerHost is an engine that can attach a tool server while it runs.
//
// Two products arrived at this door independently — a slide add-in and an editor plugin, each of
// which IS a tool server that starts and stops on the person's clock rather than the operator's.
// The set of servers is otherwise fixed at startup from a config file, which cannot describe an
// application that was not running when the daemon read it.
//
// Optional like the rest: a daemon that cannot attach says so, and the caller can tell that from a
// server that refused.
type ToolServerHost interface {
	// AttachToolServer connects to an HTTP MCP server and answers with the tool names it
	// registered — evidence, not an ack.
	AttachToolServer(ctx context.Context, name, url string, headers map[string]string) ([]string, error)
	// DetachToolServer removes one by name: false when there was none to remove, an error when
	// there was one this caller may not remove (a server the operator declared in config).
	DetachToolServer(name string) (bool, error)
}

// UserNamer is an engine that knows what to call the person it is talking to.
//
// A plugin can rename them — an SSO bridge puts the authenticated username there through
// magi.set_user_label — and the name lives in that process's memory, announced on its own bus as a
// transient event. Which means a viewer that attached afterwards never heard it, and a console
// reading the log could not have: the label is not in the log, because it is not a record of what
// happened. So the same conversation was headed by a person's name in one window and by "you" in
// the next, with neither able to tell you the other existed.
//
// Optional, and asserted for where the status is assembled: an engine that cannot answer simply
// does not, and the surfaces fall back to the word they already had.
type UserNamer interface {
	UserLabel(sid session.SessionID) string
}

// Controller is the part of an engine that CHANGES HOW IT RUNS, rather than what it is doing now.
//
// Optional, and asserted for at dispatch: an engine that does not implement it refuses these and
// says why. Keeping them out of Engine matters — Engine is what every fake in every package must
// satisfy, and a control surface that grows is not a reason to touch four test doubles.
//
// Why they cross at all. The rule for this socket is that only what CANNOT be done in a second
// process goes over it, and these qualify twice over. An attached viewer holds its own throwaway
// App, so /model there changed the viewer's copy while the daemon kept generating with the old one
// — and the screen showed the new name, which is the worst kind of control: one that reports
// success and does nothing. Rewind and Compact are worse than useless locally: they rewrite the log
// the daemon owns, under a process whose sequence counter and in-memory turn state know nothing
// about it.
type Controller interface {
	Rewind(ctx context.Context, sid session.SessionID, n int) (int64, error)
	Compact(ctx context.Context, c command.Compact) error
	SetModel(sid session.SessionID, modelID string)
	// UseBackend points the default backend at a base URL for the rest of this run. It carries an
	// error where SetModel does not, because a backend that cannot be redirected has to say so:
	// the console offers the providers that are serving, and a switch that reported success and
	// changed nothing would leave somebody talking to the wrong model.
	//
	// It takes the session because a backend change is a MODEL change: backends do not share a
	// vocabulary, so the name the companion is on is usually not one the new backend serves, and
	// leaving it produced pairings like one backend holding a model only another one serves.
	UseBackend(sid session.SessionID, base string) error
	SetPermission(p string)
	// Permission is what SetPermission last set, or what the process started on. A setter without
	// a getter is a control a second viewer can only fire blind: the console offers the four modes
	// and has to be able to say which one is on.
	Permission() string
	// Backend is the base URL this companion's LLM requests go to right now, or "" when nothing has
	// redirected it. Same shape of gap as Permission had: UseBackend is a setter the console offers
	// against a roster, and without a getter the provider select opened blank on every companion —
	// it could name every backend that is serving and not the one being used.
	Backend() string
}

// ToolReader is an engine that can run one of its READ-ONLY tools where it is, outside the turn.
//
// # Why the console does not read the files itself
//
// A workspace belongs to the companion, not to whoever is looking at it. magi-web may be reading
// over an ssh tunnel from another machine, under another account, with no route to that filesystem
// at all — and even where it does have one, a second implementation of "what is in this directory"
// is a second answer that can disagree with the one the agent gets. The daemon is already the
// process that knows where the workspace is and already confines every path to it.
//
// # Why the agent's own tools rather than new code
//
// The confinement, the symlink jail, the line numbering, the too-big-to-show cutoff: all of it
// exists in the tools the agent uses, has tests, and has been wrong and fixed. Calling them is one
// implementation; writing a directory lister here would be a second one that has to learn the same
// lessons again.
//
// # What this must not do
//
// Touch the turn. It runs on the connection's own goroutine, reads no session state and writes
// none, so a person browsing files while the agent works interrupts nothing — which is the whole
// point of it being out of band. And it is READ-only: the allowlist is on the engine side, so a
// caller naming `bash` or `edit` is refused by the process that owns the workspace rather than by
// whatever was in front of it.
type ToolReader interface {
	ReadOnlyTool(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// ToolWriter is an engine that can change a file in its workspace on a person's behalf.
//
// Its own method and its own interface, deliberately separate from ToolReader: the reading door is
// read-only and stays provably so, and a caller that has one has not got the other. What is behind
// this writes to somebody's working tree, so it is worth being able to say, of the method name
// alone, which kind of thing it is.
//
// The engine records the edit in the companion's log as the person's own words — see
// App.WriteTool. That is the half that makes a console edit honest rather than a change the agent
// discovers by finding its file different from the one in its context.
type ToolWriter interface {
	WriteTool(ctx context.Context, name string, args json.RawMessage, ask bool) (string, error)
	// PatchFile applies a unified diff instead of carrying the whole file — and, because a patch
	// carries the context around what it changes, it is also the check for a file that moved under
	// the person while they were typing. See app.PatchFile.
	PatchFile(ctx context.Context, path, patch string, ask bool) error
}

// GitTeller is an engine that can say what git makes of its workspace.
//
// Its own method rather than a tool: the tool registry is what the MODEL sees, and a git_status in
// it would be in front of every agent on every turn to answer a question the console asked. And
// not the shell either — see app.GitFacts: what runs is a fixed argv with nothing from the request
// in it, which is a property of the shape rather than of the escaping.
type GitTeller interface {
	Git(ctx context.Context) (json.RawMessage, error)
	// GitDiff is what changed in one file — staged or not, and an untracked file compared with
	// nothing, which is what a new file's diff is. Read-only: it runs git and passes back what git
	// said, because a diff is easy to produce differently and a screen that reimplemented renames,
	// mode changes and binary detection would show something the repository does not agree with.
	GitDiff(ctx context.Context, path string, staged, untracked bool) (string, error)
}

// Reviewer is an engine that will look at a file somebody is editing and say what is wrong with it.
//
// Not a turn: what travels is a buffer that is not on disk, twenty times in a minute, and putting
// that in the session would fill the agent's context with drafts. See app.LookOver. Nothing is
// written, nothing is recorded, nothing is started — the answer goes to the person who asked.
type Reviewer interface {
	LookOver(ctx context.Context, path, text string) (string, error)
	// OpenPR pushes the branch and opens a pull request for it, answering with the URL. Here
	// rather than with the git verbs because it is not git: it is a tool that may not be installed
	// and a network round trip that can fail in ways a local command cannot.
	OpenPR(ctx context.Context, title, body string) (string, error)
	// PRFacts is what a pull request from this workspace would carry: the base it goes onto, the
	// commits on this branch, and the difference against that base. A read, but it crosses here
	// because it is a read of the DAEMON's workspace — same reason GitDiff does.
	PRFacts(ctx context.Context) (out string, err error)
	// DraftPR is the model writing that request's title and body from those same facts. rules is a
	// one-off house-rules override for this draft only (empty = the saved [templates] pr).
	DraftPR(ctx context.Context, rules string) (string, error)
	// DraftCommit is the same kind of thing about a different subject: what is staged, described.
	// A draft only — the console puts it in a box somebody edits before anything is committed. rules
	// is a one-off override for this draft only (empty = the saved [templates] commit).
	DraftCommit(ctx context.Context, rules string) (string, error)
	// CompleteCode is inline completion text at the cursor: prefix and suffix are the buffer either
	// side of it. The same no-turn shape as LookOver, on a fast routed profile — see app.CompleteCode.
	// Empty text arrives with an app.CompleteReason saying which kind of empty it is, and a call
	// that failed arrives as an error — a dead completer and a quiet one used to look the same.
	CompleteCode(ctx context.Context, path, prefix, suffix string) (string, app.CompleteReason, error)
	// SetOpenFile records the file the editor has open and its unsaved buffer, so the agent's next
	// turn sees it as ambient context. Nothing is generated or recorded — see app.SetOpenFile.
	SetOpenFile(ctx context.Context, path, text string) error
	// SuggestPrompt is the composer's ghost text: how the person is likely to finish the instruction
	// they are typing, on the composer profile — see app.SuggestPrompt. prefix is what they typed.
	SuggestPrompt(ctx context.Context, prefix string) (string, error)
}

// GitDoer is an engine that will run one of a short, closed list of git commands in its workspace.
//
// stage, unstage, discard, commit — each a fixed argv with the path as an argument, never a string
// a shell parses. See app.GitDo, including which of them is written into the companion's log:
// discard throws away what was in a file, and the agent's context still holds it.
type GitDoer interface {
	GitDo(ctx context.Context, what, path, message string, ask bool) (string, error)
}

// FileKeeper is an engine that will make, move and remove files in its workspace.
//
// Apart from ToolWriter because these are acts on the TREE rather than on a file's contents, and
// one of them cannot be undone by doing the opposite. See app.FileDo, including what is written
// into the companion's log — all of them, because all of them change what the agent is holding.
type FileKeeper interface {
	FileDo(ctx context.Context, what, path, to string, ask bool) error
}

// Speaker is an engine that will take part in a meeting.
//
// One call is one contribution: it reads the question and everything said so far, and answers with
// what it has to add or with a pass. It happens in a session of its own with read-only tools — see
// app.MeetingTurn — so a companion mid-turn on its own work can take part without that work being
// touched, and so a meeting cannot change three workspaces while its subject is still being argued
// about.
type Speaker interface {
	// MeetingJoin is the participant getting ready, before the room opens: it reads its own
	// workspace and history and answers with what it brings. The session it makes is the one every
	// turn of this meeting then happens in.
	MeetingJoin(ctx context.Context, meeting, topic string) (ready, room string, err error)
	MeetingTurn(ctx context.Context, meeting, topic, transcript string, closing bool) (Contribution, error)
}

// ShellRunner is an engine that can run a command where IT is, rather than where the caller is.
//
// The distinction is the whole reason this crosses the socket. Everything else a viewer does
// locally is a READ of a shared log, and reading it twice gives the same answer. A command is not:
// run in the viewer it would execute on whichever machine and account happens to be looking, in
// whatever directory that process started in — and the answer somebody wants is what the command
// does in the daemon's workspace, as the daemon's user, beside the files the agent is editing.
//
// It carries no workdir argument for the same reason. The workspace is the daemon's, and a method
// that let the caller name a directory would be a way to run commands anywhere on that machine
// from a page.
type ShellRunner interface {
	RunShellHere(ctx context.Context, cmd string) (out string, exit int, err error)
}

// SessionMover is an engine that can be pointed at a different conversation.
//
// Optional and asserted at dispatch, like CronController and for the same reason: Controller is
// what every fake in every package must satisfy, and this is one call.
//
// Why it has to cross the socket at all. A console does not name a session — it names a companion,
// and reads the session out of the published record (see withClient in the web console). So
// "continue that conversation instead" cannot be a thing the caller decides per request: the
// daemon has to move, republish, and let every reader find out the way they already find things
// out. A viewer that pointed itself somewhere else would be a screen disagreeing with the process
// that owns the log.
type SessionMover interface {
	// Resume points this companion at sid. It refuses while a turn is in flight — a companion
	// cannot leave a conversation it is still speaking in — and refuses a session that is not in
	// its own workspace, which would be this daemon writing into somebody else's log.
	Resume(ctx context.Context, sid session.SessionID) error
}

// ConfigItem is one editable setting as it stands.
//
// The value is the EFFECTIVE one — what the engine will actually use — and Source says which
// layer won it, because a reader that is shown only the file's contents cannot explain a
// companion that is embedding with something the file does not name (an environment variable
// beats both files for some keys). Tier and File are where a write goes by default: the layer
// this value came from, and the path a person can open and read for themselves.
type ConfigItem struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
	// Source is "env", "project", "global", or "" when nothing sets it.
	Source string `json:"source,omitempty"`
	Tier   string `json:"tier,omitempty"`
	File   string `json:"file,omitempty"`
	// Applies is "now" or "next start" — a property of the KEY, not one sentence for all of them:
	// this daemon re-reads some settings per turn and others once at boot, and a screen that says
	// "restart to apply" about a live one teaches people to restart for nothing.
	Applies string `json:"applies,omitempty"`
	// Doc is the one line a screen can put under the field.
	Doc string `json:"doc,omitempty"`
	// Unreadable names a config layer that would not parse, with the reason. A file with a typo
	// in it and a file that says nothing are the same absence to a reader who is only shown
	// values — and this is the door whose promise is that a screen redraws from what the daemon
	// actually read. A read that cannot say "your global file is broken" is the third silence.
	Unreadable string `json:"unreadable,omitempty"`
}

// ProfileChoice is one assignable backend and the layer that defines it.
type ProfileChoice struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// ConfigKeeper is the settings door: read what is editable, change one key, and list what the
// profile-shaped keys may point at.
//
// One door for the settings rather than a door per setting. Seven places decide which model runs
// (the default model, the llm profiles, the two autocomplete profiles, the embedding model, the
// subagent profiles, the templates) and exactly two of them had a method — so every client
// without a config file to edit was stuck writing "not editable here" where a field belongs, and
// each new setting meant the same conversation again.
//
// The keys are a WHITELIST, held by the engine. Arbitrary TOML writing down a socket is a hole,
// not a door: the config file also holds the permission posture and the hooks that run.
type ConfigKeeper interface {
	// ConfigHere lists every editable key with its effective value.
	ConfigHere(ctx context.Context) ([]ConfigItem, error)
	// ConfigSet writes one key and answers with the key as it now stands. An empty value clears
	// it. tier picks the file; empty means the layer the value came from, else the workspace.
	ConfigSet(ctx context.Context, key, value, tier string) (ConfigItem, error)
	// ProfilesHere lists the [llm.profiles.*] a profile-shaped key may name.
	ProfilesHere(ctx context.Context) ([]ProfileChoice, error)
}

// ConversationKeeper is the engine half of the bottom dock's session switcher: list this
// workspace's conversations, and open a fresh one. Local socket only, like the transcript —
// the clients this exists for (the IDE plugin first) hold a socket and nothing else.
type ConversationKeeper interface {
	// SessionsHere lists this workspace's conversations, newest activity first.
	SessionsHere(ctx context.Context) ([]session.SessionMeta, error)
	// NewSession opens a fresh conversation AND moves the companion onto it — one verb, because
	// the caller that wants a new conversation wants to be in it, and a created-but-not-current
	// session is a row nobody asked for. The contract resume deliberately does not offer:
	// resume refuses an id that is not already a conversation of this workspace, so a client
	// must never invent an id — it calls this and reads the one that comes back.
	NewSession(ctx context.Context) (session.SessionID, error)
}

// JobKiller stops a background command — the same stop bash_kill performs, offered to the
// person watching the row. Split from JobRunner the way CronTeller is from CronController:
// showing and stopping are different grants for a fake to give.
type JobKiller interface {
	// KillBackgroundJob reports whether a job with that id existed to kill.
	KillBackgroundJob(id string) bool
}

// CronTeller is the read half of standing work, for a dock that shows what is coming: the TUI
// reads it in-process (scheduledSoon), and a socket client had only the write half (reload-cron).
type CronTeller interface {
	// ScheduledHere answers this workspace's jobs — the runnable with their next instant, and the
	// never-runnable with the reason, which a listing MUST carry since nothing else will ever
	// mention them again.
	ScheduledHere() []app.ScheduledJobInfo
}

// CronController is the part of an engine that holds scheduled work.
//
// Optional and asserted at dispatch, like Controller, and separate from it for the reason Controller
// gives about its own size: an editor in another process writes a job to the config file and then
// has to tell the daemon, and that one call is not worth a method on the interface every test
// double implements.
type CronController interface {
	// ReloadCron re-reads the job definitions. Called after something outside this process has
	// changed them — the console or an attached terminal writing config.toml.
	ReloadCron()
}

// Transcriber is an engine that can read out a conversation: everything already written, then
// whatever happens next, down one connection.
//
// Optional and asserted at dispatch, like Taker and CronController, for the reason they give.
//
// # Why a READ crosses here, when Engine's own comment says most do not
//
// It does not earn the crossing the way Waiting and Doing do. Those are facts that exist only in
// the memory of the process holding the run and are in no log, so nobody else can work them out. A
// transcript is the opposite: it IS the log, and the terminal's --attach and the web console both
// build an App of their own over the same directory and reconstruct it themselves. For those two
// this method would be a second way to learn what they already know.
//
// It earns it because of the readers that cannot do that. The JetBrains plugin is a JVM process
// with a socket and no Go store; a slide add-in outside this module is in the same position. They
// cannot open the log AT ALL — not "would rather not", cannot — and the choice for them is this
// door or no transcript. That is a different argument from the one Waiting makes, and it is the
// only one holding this method up: an in-process reader should still read the log directly.
type Transcriber interface {
	// Subscribe replays the persisted events after fromSeq and then streams live ones, in order.
	// fromSeq 0 (and any negative, which the store treats the same way) means everything.
	Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error)
	// NewSince is here to check a cursor before it is honoured, and for nothing else. Asked with 0
	// it answers the highest seq this session's log holds, which is the one fact that can tell a
	// caller's `since` from a caller's mistake — see answerable. It is a binary search over a
	// cached tail, so asking costs about as much as not asking.
	NewSince(ctx context.Context, sid session.SessionID, seq int64) (latest int64, changed bool, err error)
}

// Request is one line on the wire. One object per line, so a reader needs no framing beyond what
// bufio already does and a person can watch the socket with `nc` and read it.
type Request struct {
	Method  string `json:"method"`
	Session string `json:"session,omitempty"`
	Text    string `json:"text,omitempty"`
	CallID  string `json:"callId,omitempty"`
	// Decision is the permission verdict as the core spells it: allow | deny | always. Carried as
	// the same string rather than translated into booleans here — a second vocabulary for one
	// decision is a place for the two to drift.
	Decision string `json:"decision,omitempty"`
	// Tier says WHICH config file a settings write lands in — "project" (this workspace's
	// .magi/config.toml) or "global" (the account's). Its own field rather than a second meaning
	// for Name because it is a second argument, not a second spelling of the first: a client that
	// read a value out of the global file and wrote it back without saying so would silently mint
	// a project override of a setting the person meant to change everywhere.
	Tier   string `json:"tier,omitempty"`
	Answer string `json:"answer,omitempty"`
	// Name and N carry the control methods' one argument each: a model id, a permission policy, a
	// number of turns to rewind, the label above a handed-over request, the receipt for one.
	// Named generically because the alternative is a field per method and a wire format that grows
	// a column every time the engine gains a knob.
	Name string `json:"name,omitempty"`
	// Looking marks handed-over work the asker says is a question rather than a change: the
	// receiver runs it read-only, and a read-only turn does not wait for the workspace.
	Looking bool `json:"looking,omitempty"`
	// Meeting is which meeting this belongs to, so the daemon can hand the same session back for
	// every turn of it. Without an id every contribution was a new child: three companions over
	// five rounds put fifteen of them on the strip, each one starting cold and knowing nothing of
	// what this participant had already said.
	Meeting string `json:"meeting,omitempty"`
	N       int    `json:"n,omitempty"`
	// URL and Headers are the attach door's arguments: which HTTP MCP server, and what to send
	// with every request to it. There is deliberately no command field — this door's safety
	// argument is that it spawns nothing, and an argument kept in the signature cannot be lost to
	// convenience later (a caller that needs a process needs a different door).
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Args is the tool method's arguments, verbatim, as the tool's own schema spells them. Raw
	// JSON rather than a field per argument: the caller and the tool already agree on a schema,
	// and re-declaring it here would be a third copy to keep in step with the other two.
	Args json.RawMessage `json:"args,omitempty"`
	// Ask says the caller wants the companion to ANSWER what it is being told, rather than only be
	// told. It rides with the console's own changes — an edit, a git command — and turns the note
	// they leave in the log from a record into a steer.
	Ask bool `json:"ask,omitempty"`
	// Refs are the files a submit/steer attaches (command.FileRef): the IDE's selection, the
	// composer's paperclip. The core renders and persists the excerpts; the wire carries only
	// names and ranges.
	Refs []command.FileRef `json:"refs,omitempty"`
	// Since is the transcript method's cursor: send what came AFTER this seq. Zero — which is what
	// an absent field decodes to — and any negative both mean everything, because that is what the
	// store already means by them (its filterFrom only cuts when fromSeq > 0, and seqs start at 1)
	// and a second reading of one number is how two processes come to disagree about where a
	// conversation starts.
	//
	// Its own field rather than N, which is an int and would have truncated a seq on a 32-bit
	// client, and whose comment says it carries a number of turns to rewind.
	Since int64 `json:"since,omitempty"`
}

// Response is the reply. Err is a STRING rather than a bool: a client told only that something
// failed cannot tell a rejected session id from a dead engine, and would retry both.
type Response struct {
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
	// Waiting answers the status method: absent when the engine is not blocked on anybody.
	Waiting *Waiting `json:"waiting,omitempty"`
	// Doing answers the same method with the opposite news: the latest progress note from a tool
	// that is still running. Empty when nothing has reported, which is most of the time — a turn
	// making ordinary steady progress has nothing to say beyond the log, and only the tools that
	// spend minutes inside one call (a wait, a compaction, a stalled stream) speak up.
	Doing string `json:"doing,omitempty"`
	// Out and Exit answer the shell method. Exit is a pointer so that a zero — which is the answer
	// a caller most wants to be able to trust — is distinguishable from a reply that carried no
	// exit code at all.
	Out  string `json:"out,omitempty"`
	Exit *int   `json:"exit,omitempty"`
	// Permission is the approval mode the engine is on RIGHT NOW, not the one it started in — it
	// changes at runtime and a viewer that offers to change it has to show what it is changing
	// from. Only on the status answer, where every other "what is it doing" fact lives.
	Permission string `json:"permission,omitempty"`
	// Backend is the base URL its requests go to now — a runtime fact for the same reason
	// Permission is one, and read by the console to say which provider is in use.
	Backend string `json:"backend,omitempty"`
	// User is what to call the person, when a plugin has renamed them. Same reason as Permission:
	// it is set at runtime, in the memory of the process holding the run, and nowhere else.
	User string `json:"user,omitempty"`
	// Reason answers the complete method when Out is empty: WHICH empty it is (app.CompleteReason
	// — off, unrouted, nothing-asked, no-answer). A failed completion is Err, not a reason. Without
	// it every one of those arrived as an empty string, so an editor could not tell a completer
	// that had nothing to say from one that was switched off, misconfigured, or never asked.
	// Absent on a completion that produced text, and absent on every other method.
	Reason string `json:"reason,omitempty"`
	// Jobs answers the jobs method: the work running BESIDE the turn.
	Jobs *Jobs `json:"jobs,omitempty"`
	// Tools answers the tools method: the roster this companion is actually running with, which
	// only the process holding it can say.
	Tools []string `json:"tools,omitempty"`
	// Removed answers mcp-detach: whether there was a server to remove. Its own field because "no"
	// here is not a failure — a helper reconnecting after a crash detaches first, and being told
	// it was already clean is the answer it wanted. Reported as Err, it was indistinguishable from
	// a refusal.
	Removed bool `json:"removed,omitempty"`
	// Models answers the models method: what this daemon's backend says it could run on.
	Models []string `json:"models,omitempty"`
	// Why carries a reason with an otherwise-empty answer — the backend refused, or timed out —
	// so a caller can tell "nothing to offer" from "could not ask".
	Why string `json:"why,omitempty"`
	// Session is the conversation an answer was produced in, when the caller has a use for it.
	//
	// The meeting methods and session-new set it, and the use is one the screen has: a participant speaks from
	// a session of its own, and what it thought and read on the way to a sentence is in there. The
	// console cannot know that id any other way — the room is opened inside the daemon — so a
	// viewer that wanted to show the working had nothing to ask for.
	Session string `json:"session,omitempty"`
	// Handover answers hand-state. Its own object rather than four more columns here, because
	// "not finished, and here is why not" is one fact with parts and reading it out of flat
	// fields would let a caller act on half of it.
	Handover *Handover `json:"handover,omitempty"`
	// Version, Proto and Caps answer the `about` handshake: who the far side is and what it speaks,
	// so a caller can negotiate rather than guess. Version is the binary's (e.g. "v0.22.2"), Proto
	// the wire-protocol version (ProtoVersion), Caps the negotiable capabilities (Caps()). A daemon
	// that predates the handshake sets none of them; a caller reads that as proto 0 / no caps and
	// holds to the pre-negotiation behaviour. Additive and omitempty — an older client ignores them.
	Version string   `json:"version,omitempty"`
	Proto   int      `json:"proto,omitempty"`
	Caps    []string `json:"caps,omitempty"`
	// Event is one frame of a transcript: the log's own event, whole and unrenamed.
	//
	// Whole rather than a diff, and the same shape the store holds rather than a rendering. A
	// client that has the log's vocabulary can do everything the console does with it; one that
	// only had a rendering could not, and two spellings of one stream drift the first time either
	// is fixed. A pointer so an absent event is absent — a frame that carries only a sentence (see
	// Why) is a real frame with nothing to draw, not an event with every field zeroed.
	Event *event.Event `json:"event,omitempty"`
	// Roster answers the `roster` method: every companion this machine can name — its own
	// (measurements, with a session id to subscribe to) and other machines' (signed sightings,
	// with their age). Empty list = an empty fleet; the field absent = some other method.
	Roster []RosterRow `json:"roster,omitempty"`
	// Sessions answers the `sessions` method: this workspace's conversations, newest first.
	Sessions []SessionRow `json:"sessions,omitempty"`
	// Config answers config-get and config-set: what a settings key is, where the value came
	// from, and when a change to it takes effect.
	Config []ConfigItem `json:"config,omitempty"`
	// Profiles answers the method of that name: the backends a settings field may point at.
	Profiles []ProfileChoice `json:"profiles,omitempty"`
	// Cron answers the `cron` method: the standing schedule, broken first, then soonest first.
	Cron []CronRow `json:"cron,omitempty"`
}

// Waiting is a prompt the daemon is blocked on, as it travels.
type Waiting struct {
	ID   string `json:"id"`   // the call id an answer must carry
	Kind string `json:"kind"` // "permission" | "question"
	What string `json:"what"`
	// The rest of the request, so a viewer draws the prompt rather than a description of it: the
	// command being decided on, why the policy stopped, and the picks a question offers.
	Args    json.RawMessage `json:"args,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	Options []string        `json:"options,omitempty"`
	// Diff is what approving would change, for edit-class prompts — computed once in the app and
	// carried, never recomputed by a viewer (PermissionRequestedData.Diff).
	Diff string `json:"diff,omitempty"`
	// Report is the grounds a question was asked on. It crosses the socket for the same reason the
	// options do: a console in another process draws the prompt, and a prompt whose grounds stayed
	// behind is the one this exists to stop.
	Report []report.Filled `json:"report,omitempty"`
	// Index and Total place this question in the run its call is asking. A viewer on another
	// machine has no other way to know that answering this one leads to another.
	Index int    `json:"index,omitempty"`
	Total int    `json:"total,omitempty"`
	Since string `json:"since"` // RFC3339
}

// Event turns a pending prompt back into the event a UI already knows how to draw.
//
// A viewer in another process cannot receive the original: it is transient, published to the bus of
// the daemon that is blocked, and it never leaves that process. So the request's fields cross the
// wire and this rebuilds the same payload on the other side — the same call id above all, because
// that is what an answer is addressed to. A prompt rendered from a summary is one nobody can reply
// to.
//
// It lives here, next to the wire type it converts, rather than in whichever client happens to need
// it: a second client would otherwise write its own version, and a viewer that filled in three of
// the four fields would show a prompt that looks right and answers nothing.
func (w *Waiting) Event(sid session.SessionID) (event.Event, error) {
	if w == nil {
		return event.Event{}, errors.New("daemon: no pending prompt to draw")
	}
	var (
		typ  event.Type
		data []byte
		err  error
	)
	switch w.Kind {
	case "question":
		typ = event.TypeQuestionRequested
		data, err = json.Marshal(event.QuestionRequestedData{
			CallID: w.ID, Question: w.What, Options: w.Options, Report: w.Report,
			Index: w.Index, Total: w.Total})
	default:
		typ = event.TypePermissionRequested
		data, err = json.Marshal(event.PermissionRequestedData{
			CallID: w.ID, Name: w.What, Args: w.Args, Reason: w.Reason, Diff: w.Diff})
	}
	if err != nil {
		return event.Event{}, fmt.Errorf("daemon: rebuilding the prompt: %w", err)
	}
	return event.Event{
		SessionID: sid, Type: typ, Data: data,
		Actor: event.Actor{Kind: event.ActorSystem, ID: "daemon"},
	}, nil
}

// SocketPath is where a workspace's daemon listens.
//
// Derived from the workspace so two projects do not fight over one path, and placed under the
// config directory rather than the workspace so it never lands in a deliverable tree or a git
// status. The name carries the base directory so `ls` is readable by a person looking for theirs.
func SocketPath(configDir, workdir string) string {
	return filepath.Join(configDir, "daemon-"+WorkspaceKey(workdir)+".sock")
}

// WorkspaceKey names a workspace in one short, stable string: its base directory, so `ls` is
// readable by a person looking for theirs, and a hash of the whole path, so two checkouts of one
// repo are two companions.
//
// One definition because there are two users now — the socket above, and the per-companion config
// directory beside it — and a key spelled two ways would give one companion two identities: it
// would answer on one socket and read settings written for another.
func WorkspaceKey(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = workdir
	}
	// Resolve symlinks, or one directory gets two names and the daemon and the attach look at
	// different sockets. Go's os.Getwd prefers $PWD when it points at the same place, so a shell
	// that did `cd /tmp/x` reports the logical path while a process that chdir'd itself reports
	// /private/tmp/x — same directory, different hash, "no daemon here". Observed exactly that way.
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return sanitize(filepath.Base(abs)) + "-" + shortHash(abs)
}

// maxSocketPath is what the OS allows in a unix address: 104 bytes on macOS, 108 on Linux. Past it
// both bind and connect fail with "invalid argument", which says nothing about the length — so the
// check is here, where the reason can be given.
const maxSocketPath = 100

// tooLong reports a path the OS will refuse, with the reason it will not give.
func tooLong(path string) error {
	if len(path) <= maxSocketPath {
		return nil
	}
	return fmt.Errorf("daemon: the socket path is %d bytes and the OS allows about %d — "+
		"set MAGI_CONFIG_DIR to somewhere shorter: %s", len(path), maxSocketPath, path)
}

// Serve accepts connections until ctx is done. It removes the socket on the way out, and on the way
// IN removes a stale one — a daemon killed with SIGKILL leaves the file behind, and refusing to
// start because of a path nobody is listening on would need a manual delete every crash.
func Serve(ctx context.Context, eng Engine, path string) error {
	d, err := Listen(path)
	if err != nil {
		return err
	}
	return d.Serve(ctx, eng)
}

// Daemon is a bound control socket, before anything is served on it.
//
// Binding and serving are separate because the caller has work to do in between, and the ORDER of
// that work matters. Publishing the record before the socket is claimed means a second magi that is
// about to lose the race still writes its own session id over the winner's, and then removes the
// file on its way out — leaving a running daemon that no viewer can find. Seen exactly that way
// with four simultaneous starts: one daemon serving, no record at all.
//
// So: claim first, then create the session and publish, then serve.
type Daemon struct {
	ln      net.Listener
	path    string
	release func()
	// stop is closed when a client asks the daemon to shut down. It ends Serve by the same route a
	// cancelled context does — the listener closes, in-flight requests finish, the socket and the
	// claim go — so there is one way a daemon stops and not two.
	stop     chan struct{}
	stopOnce sync.Once
	// restart marks that the daemon was asked to relaunch onto a new binary rather than simply stop,
	// so the process re-execs once Serve has drained and returned and the socket and claim are
	// released — the same drain as a shutdown, a different ending. Read after Serve returns.
	restart atomic.Bool
	// conns are the connections currently being served. Stop closes them as well as the listener:
	// closing a listener does not touch what has already been accepted, and Serve waits for its
	// handlers — so a client that asked to shut down and then sat there holding the connection open
	// would keep the daemon alive by doing nothing at all.
	connMu sync.Mutex
	conns  map[net.Conn]struct{}
}

// Listen claims a workspace's socket path and binds it, or says who has it.
func Listen(path string) (*Daemon, error) {
	if err := tooLong(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon: %w", err)
	}
	// Claim the path before looking at it. The three steps below — ask who is listening, remove
	// what is left over, bind — have two gaps in them, and two daemons starting together fall
	// through both: each finds nothing, each removes what the other just bound, and one engine
	// ends up orphaned while both write the same store. Measured at 25 of 300 simultaneous starts.
	// The claim makes the sequence one step as far as any other process is concerned.
	release, err := claimPath(path)
	if err != nil {
		return nil, err
	}
	if c, derr := net.Dial("unix", path); derr == nil {
		c.Close()
		release()
		// A live listener holding no claim: a daemon from a version before the claim existed, which
		// is what an upgrade looks like from here.
		return nil, fmt.Errorf("daemon: another magi is already listening on %s", path)
	}
	// Nobody answered — which says nothing about WHAT is there.
	//
	// The dial proves only that nothing is listening; a plain file fails to dial too (ECONNREFUSED
	// on Linux, ENOTSOCK on macOS — the same platform split Dial's own branch records). Reading
	// that failure as "the file is a leftover socket" is the inference this package just removed
	// from the reading side, and here it ends in an irreversible delete rather than a wrong
	// sentence. magi never creates a plain file at this path: bind() makes a socket inode and a
	// crash leaves a socket, so anything else here is by definition not ours. Owning the
	// directory means our own leavings are ours to clear, not that somebody else's file is.
	//
	// Lstat, not Stat, because the delete targets the ENTRY: a symlink pointing at a live socket
	// elsewhere reads as a socket through Stat, and removing it would take somebody's link away
	// on the strength of what it points at. A link is not something magi made either.
	if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&os.ModeSocket == 0 {
		release()
		return nil, fmt.Errorf("daemon: %s is not a socket — something else holds that name (%s); "+
			"move it aside and start again", path, fi.Mode().Type())
	}
	// Now it is a leftover socket, and this Remove is safe BECAUSE the dial above proved nothing
	// is listening. A unix socket is an ordinary directory entry: removing a LIVE one succeeds,
	// silently orphans the running daemon's listener, and leaves two engines writing one store
	// while every client reaches only the newer. The probe is what stands between here and that,
	// so the two lines stay together.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		release()
		return nil, fmt.Errorf("daemon: a stale socket is at %s and could not be removed: %w", path, err)
	}
	// Owner only, from the instant it exists. The socket is a control channel: anything that can
	// write to it can make this engine act, in this workspace, with this workspace's permissions.
	ln, err := listenOwnerOnly(path)
	if err != nil {
		release()
		return nil, err
	}
	// Belt and braces, and the whole story on Windows, where there is no umask to set: a mode that
	// is already right costs one syscall to confirm.
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		release()
		return nil, fmt.Errorf("daemon: %w", err)
	}
	// The stop channel is made HERE, not lazily in Serve: Stop/Restart/stopped are reachable from
	// other goroutines (the auto-update loop holds Restart before Serve is even entered), and a lazy
	// init raced them — a Stop that won the race consumed the sync.Once against a nil channel and
	// the daemon became unstoppable over the socket.
	return &Daemon{ln: ln, path: path, release: release, stop: make(chan struct{})}, nil
}

// Close drops the socket without ever having served on it — for a caller that binds and then fails
// at something else before it can start.
func (d *Daemon) Close() error {
	err := d.ln.Close()
	os.Remove(d.path)
	d.release()
	return err
}

// Serve accepts connections until ctx is done or a client asks it to stop, then removes the socket
// and gives up the claim.
func (d *Daemon) Serve(ctx context.Context, eng Engine) error {
	defer func() {
		d.ln.Close()
		os.Remove(d.path)
		d.release()
	}()
	if d.stop == nil {
		d.stop = make(chan struct{}) // zero-value Daemon in a test; Listen always makes it
	}

	var wg sync.WaitGroup
	go func() {
		select {
		case <-ctx.Done():
		case <-d.stop:
		}
		// Both endings close the OPEN CONNECTIONS as well as the listener, and that is the whole
		// of it: Ctrl-C used to close only the listener, and then wg.Wait below waited for every
		// serveConn goroutine — each of which sits in Scan until its peer hangs up.
		//
		// A console holds one open per daemon it has ever touched (magi-web caches its clients),
		// and an attached terminal holds one for as long as it is attached. So a daemon anybody
		// had looked at did not stop on Ctrl-C. It printed nothing and it did not exit; the only
		// way out was to kill it, which is how it was reported — from Windows, though nothing
		// about it is Windows.
		//
		// A connection blocked on Scan is not work in flight: closing it ends the read, the
		// goroutine returns, and a request that IS mid-answer still finishes, because Stop closes
		// the socket rather than the goroutine and the write it is doing completes or fails on its
		// own. wg.Wait then means what it says.
		d.Stop()
		d.ln.Close() // unblocks Accept
	}()
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || d.stopped() {
				wg.Wait() // let in-flight requests finish before the socket goes
				return nil
			}
			return fmt.Errorf("daemon: accept: %w", err)
		}
		d.track(conn)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer d.untrack(conn)
			defer conn.Close()
			serveConn(ctx, eng, conn, homeOf(d.path), d.Stop, d.Restart)
		}()
	}
}

// Stop ends Serve. Safe to call more than once and from any goroutine: two clients asking at the
// same moment is a normal thing for a console with two tabs open.
//
// Open connections are closed too. A request in flight on another one loses its reply, which is the
// right trade for a shutdown somebody asked for — the alternative is a daemon that stays up because
// a console in a background tab never hung up.
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() {
		if d.stop != nil {
			close(d.stop)
		}
		d.connMu.Lock()
		for c := range d.conns {
			c.Close()
		}
		d.connMu.Unlock()
	})
}

// Restart drains the daemon exactly like Stop, but marks that the process should relaunch onto the
// binary now on disk once Serve has returned and the socket and claim are released — the same drain,
// a different ending. The caller (the daemon loop) reads Restarting after Serve to choose re-exec
// over exit. Ordering is deliberate: set the intent before draining, so Serve cannot return and the
// flag be read before it is set.
//
// A daemon already stopping is left stopping: the auto-update loop polls for an idle moment and then
// calls this, and without the guard that poll could land during the drain a SHUTDOWN started — the
// flag would flip after the fact and a daemon the operator asked to stop would come back up. Stop
// wins; the committed binary waits on disk for the next start.
func (d *Daemon) Restart() {
	if d.stopped() {
		return
	}
	d.restart.Store(true)
	d.Stop()
}

// Restarting reports whether Restart, rather than Stop, ended the daemon. Read after Serve returns.
func (d *Daemon) Restarting() bool { return d.restart.Load() }

func (d *Daemon) track(c net.Conn) {
	d.connMu.Lock()
	defer d.connMu.Unlock()
	if d.conns == nil {
		d.conns = map[net.Conn]struct{}{}
	}
	d.conns[c] = struct{}{}
	// Stop may have run between Accept and here, in which case nothing is coming to close this one.
	if d.stopped() {
		c.Close()
	}
}

func (d *Daemon) untrack(c net.Conn) {
	d.connMu.Lock()
	defer d.connMu.Unlock()
	delete(d.conns, c)
}

func (d *Daemon) stopped() bool {
	if d.stop == nil {
		return false
	}
	select {
	case <-d.stop:
		return true
	default:
		return false
	}
}

// serveConn reads requests until the peer hangs up. One bad request answers with an error and the
// connection stays open: a UI that mistypes a method should get told, not disconnected.
func serveConn(ctx context.Context, eng Engine, conn net.Conn, home string, stop, restart func()) {
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // a steer can be long; the default 64K is not enough
	enc := json.NewEncoder(conn)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			// Tell the peer and keep reading. If even that write fails the peer is gone, which is
			// the same conclusion the encode below draws — so it ends the connection rather than
			// looping on a socket nobody is holding.
			if enc.Encode(Response{Err: "malformed request: " + err.Error()}) != nil {
				return
			}
			continue
		}
		// roster is answered here rather than from the answers map: that map's shape is
		// (ctx, Engine, Request), and who is out there is a fact about the MACHINE — the home
		// directory the listener sits in — which no Engine method answers. See roster.go.
		if req.Method == "roster" {
			if enc.Encode(answerRoster(home)) != nil {
				return // the peer is gone
			}
			continue
		}
		// The methods that ANSWER: one function each, and the lockstep write is here rather than
		// repeated at the end of every one of them.
		if answer, ok := answers[req.Method]; ok {
			if enc.Encode(answer(ctx, eng, req)) != nil {
				return // the peer is gone
			}
			continue
		}
		// status is answered here rather than in dispatch: it is the only method with a payload,
		// and giving dispatch a return value for the sake of one caller would make every write
		// site pretend to produce something.
		var resp Response
		// shutdown is answered here, like status, and the reply goes out BEFORE the stop.
		//
		// Not because the reply would otherwise be lost — closing a listener does not touch
		// connections already accepted, and Serve waits for in-flight handlers before it returns,
		// so the write is safe either way. It is the order that is honest: OK means "accepted",
		// and answering after unwinding had begun would be claiming rather more than that.
		//
		// The stop itself must NOT be deferred to the end of this function. Deferring it waits for
		// the peer to hang up, so a client that asked to shut down and then kept its connection
		// open would leave the daemon running — which is precisely the state this call exists to
		// get out of.
		if req.Method == "shutdown" {
			if stop == nil {
				resp = Response{Err: "this daemon cannot be stopped remotely"}
				if enc.Encode(resp) != nil {
					return
				}
				continue
			}
			wrote := enc.Encode(Response{OK: true}) == nil
			stop()
			if !wrote {
				return
			}
			continue
		}
		// restart is shutdown with a successor: the same drain, then the process re-execs onto the
		// binary now on disk instead of exiting. Answered before the drain for the same honesty as
		// shutdown — OK means "accepted, and I am on my way down to come back up". The relaunch itself
		// happens in the daemon loop after Serve returns and the socket and claim are released.
		if req.Method == "restart" {
			if restart == nil {
				resp = Response{Err: "this daemon cannot be restarted remotely"}
				if enc.Encode(resp) != nil {
					return
				}
				continue
			}
			wrote := enc.Encode(Response{OK: true}) == nil
			restart()
			if !wrote {
				return
			}
			continue
		}
		// update runs a self-update and, if it committed a new build, restarts onto it — B1 wiring
		// B5 (Commit, with rollback) to B2 (restart). Answered inline because it does I/O (a download)
		// and the caller waits for the outcome. Not on the fleet-door allowlist, so the narrowed
		// remote entry cannot carry it; what remains is the local socket and anything the operator
		// has deliberately piped to it (--relay over their own ssh) — the same boundary shutdown has.
		//
		// The restart here is IMMEDIATE, unlike the auto loop's idle-gated one, and that asymmetry is
		// chosen: this is an operator pressing a button and watching, and holding their reply hostage
		// to an idle moment that may be minutes away would read as a hang. A restart mid-turn costs
		// the in-flight step; the log keeps the rest and the session resumes.
		if req.Method == "update" {
			u, ok := eng.(Updater)
			if !ok {
				if enc.Encode(Response{Err: "this daemon cannot update itself"}) != nil {
					return
				}
				continue
			}
			res, uerr := u.Update(ctx)
			if uerr != nil {
				if enc.Encode(Response{Err: uerr.Error()}) != nil {
					return
				}
				continue
			}
			if res.Updated && restart != nil {
				// "or on the next start": Restart refuses when a stop is already draining (stop wins),
				// and this reply has already gone out by then — so it must not promise more than the
				// binary being on disk guarantees.
				wrote := enc.Encode(Response{OK: true, Out: "updated " + res.From + " → " + res.To +
					" — restarting (or on the next start, if this daemon is already stopping)"}) == nil
				restart()
				if !wrote {
					return
				}
				continue
			}
			msg := res.Message
			switch {
			case res.Updated:
				// Updated but nobody wired a restarter (an embedder without one): saying "up to
				// date" would hide that the binary on disk changed and this process did not.
				msg = "updated " + res.From + " → " + res.To + ", but this daemon cannot restart " +
					"itself — restart it by hand to run the new build"
			case msg == "":
				msg = "already up to date"
			}
			if enc.Encode(Response{OK: true, Out: msg}) != nil {
				return
			}
			continue
		}
		// watch turns this connection into a stream, which no other method does.
		//
		// One request, then a frame every time something changes, then the end. The daemon writes
		// without being asked again — which is the whole point: the answer to handed-over work
		// arrives when it arrives, and the alternative was the asking side spawning a process
		// across a network every three seconds for up to two hours to find out.
		//
		// It does not disturb the lockstep every other caller relies on, because a watcher gives
		// this connection over to it and sends nothing else down it. A UI's connection, which
		// interleaves calls under one mutex, never sees an unsolicited frame.
		if req.Method == "watch" {
			taker, ok := eng.(Taker)
			if !ok {
				// Refused before the connection is given over to anything, so it is still an
				// ordinary exchange and stays open like any other refusal.
				if enc.Encode(Response{Err: "this daemon cannot be handed work"}) != nil {
					return
				}
				continue
			}
			// The peer hanging up is the only thing that ends a watch nothing is happening in.
			// Read for it in the background: without this, a stream whose link died holds a
			// goroutine until the daemon stops, because there is nothing to write and therefore
			// nothing to fail. Anything actually read is discarded — a watcher has said its piece.
			wctx, hungUp := context.WithCancel(ctx)
			go func() {
				for sc.Scan() { //nolint:revive // draining, not reading
				}
				hungUp()
			}()
			werr := taker.Watch(wctx, req.Name, func(h Handover) error {
				return enc.Encode(Response{OK: true, Handover: &h})
			})
			hungUp()
			if werr != nil {
				// Said the way every other refusal is said, and the only discarded write in this
				// file. It is discarded because this connection ends on the next line whatever
				// happens: a write that fails means the peer left before hearing why, which is
				// where it was going anyway. Checking it would be a check with one outcome.
				_ = enc.Encode(Response{Err: werr.Error()})
			}
			return // this connection was a stream; it ends with it
		}
		// transcript is the second method that turns a connection into a stream, and it is framed
		// like watch on purpose: one request line, then the connection is given over, then it ends
		// when the peer hangs up. Two streaming styles in one protocol would be two things to get
		// right in every client that speaks it.
		//
		// It carries a conversation — everything already in the log, then everything that happens
		// next — for the clients that have no way to read the log themselves. See Transcriber for
		// why a READ is on this socket at all.
		if req.Method == "transcript" {
			tr, ok := eng.(Transcriber)
			if !ok {
				// Refused before the connection is given over, so it is still an ordinary exchange
				// and this connection stays open, like every other refusal in this loop.
				if enc.Encode(Response{Err: "this daemon cannot read out a transcript"}) != nil {
					return
				}
				continue
			}
			sid := session.SessionID(req.Session)
			// An invented id used to stream infinite silence — the store answers an unknown
			// session with emptiness, not an error, because "no events yet" is also what the
			// CURRENT conversation looks like before its first words (born-lazy). The one party
			// that can tell those apart is the engine's own listing, which includes the unborn
			// current on top; a daemon without that door keeps the old behaviour.
			if k, ok := eng.(ConversationKeeper); ok {
				metas, kerr := k.SessionsHere(ctx)
				if kerr == nil && !slices.ContainsFunc(metas, func(m session.SessionMeta) bool { return m.ID == sid }) {
					// Off the listing is not yet "not there": the listing is TOP-LEVEL only, and
					// this same socket advertises child session ids (jobs), whose transcripts are
					// on disk and readable. So the store is asked second — a session with any
					// events is real, whoever its parent is — and only an id that is neither
					// listed nor ever written to is refused. An unborn CHILD (spawned, no events
					// yet) would land here too; today a spawn's first act is appending, so the
					// window is the same one the born-lazy current already accepts.
					// And a store that COULD NOT answer (nerr != nil) serves rather than
					// refuses — a real child must not be refused over a transient read
					// failure, at the accepted price that an invented id under the same
					// failure streams the old silence.
					if _, known, nerr := tr.NewSince(ctx, sid, 0); nerr == nil && !known {
						if enc.Encode(Response{Err: fmt.Sprintf("no conversation %q in this workspace — `sessions` lists them", sid)}) != nil {
							return
						}
						continue
					}
				}
			}
			since, note := answerable(ctx, tr, sid, req.Since)
			// The peer hanging up is the only thing that ends a transcript nothing is happening
			// in, exactly as for watch: with no reader for the hang-up, a stream whose link died
			// holds a goroutine until the daemon stops, because there is nothing to write and so
			// nothing to fail. Anything actually read is discarded — a reader has said its piece.
			rctx, hungUp := context.WithCancel(ctx)
			go func() {
				for sc.Scan() { //nolint:revive // draining, not reading
				}
				hungUp()
			}()
			evs, unsubscribe, serr := tr.Subscribe(rctx, sid, since)
			if serr != nil {
				hungUp()
				_ = enc.Encode(Response{Err: serr.Error()})
				return
			}
			// Said BEFORE the first event, because it changes what the events that follow mean: a
			// client that asked for a tail and is being sent a whole conversation has to know, or
			// it appends the beginning of the session to the end of what it is showing.
			if note != "" && enc.Encode(Response{OK: true, Why: note}) != nil {
				hungUp()
				unsubscribe()
				return
			}
			for e := range evs {
				frame := e
				if enc.Encode(Response{OK: true, Event: &frame}) != nil {
					break // the peer is gone
				}
			}
			hungUp()
			unsubscribe()
			return // this connection was a stream; it ends with it
		}
		err := dispatch(ctx, eng, req)
		resp = Response{OK: err == nil}
		if err != nil {
			resp.Err = err.Error()
		}
		if enc.Encode(resp) != nil {
			return // the peer is gone
		}
	}
}

// answerable settles which cursor a transcript is really sent from, and returns a sentence when
// that is not the one the caller asked for.
//
// # The case this exists for
//
// The console opens a session at -1 and RESETS to -1 when the companion moves to another
// conversation, because carrying a cursor across a conversation boundary blinds you to the start of
// the new one. A client on the other side of this socket has the same problem and less to go on: it
// reconnects after a daemon restart holding a number it read in a conversation that is no longer the
// one it is being given, and every seq it holds is a plausible seq somewhere.
//
// So the number is checked against the log it names. `since` past the end of that log is a cursor no
// event in this session can account for — nothing after it has happened, and nothing after it is
// going to have happened before whatever comes next. Honouring it would hand back an empty replay
// followed by live events, which is indistinguishable, on the client's screen, from a conversation
// that started where it happens to be looking. That silent missing span is the thing this whole
// tree is built to avoid, so the cursor is refused OUT LOUD and the conversation is sent whole.
//
// What this does NOT catch, and cannot: a stale cursor that happens to land INSIDE the new
// session's range. Seq 40 from yesterday's conversation is a real position in today's, and no fact
// on this side distinguishes them — the wire carries a number, not which log it was counted in. A
// client that wants that guarantee has to hold the session id beside the seq and send both, which
// is what the console does locally when it resets. Naming the limit rather than implying it is
// covered: the check catches the reconnect-after-restart case and no other.
func answerable(ctx context.Context, tr Transcriber, sid session.SessionID, since int64) (int64, string) {
	// 0 and negative already mean everything, to the store and therefore here. There is nothing to
	// distrust in a cursor that is asking for the whole thing.
	if since <= 0 {
		return since, ""
	}
	// Asked with 0, NewSince answers the highest seq the log holds (0 for a session with none).
	latest, _, err := tr.NewSince(ctx, sid, 0)
	if err != nil {
		// Could not check. Subscribe is about to read the same log and will refuse in words if it
		// is unreadable; throwing away a good cursor over a transient failure to stat a file would
		// resend a whole conversation for no reason a client could act on.
		return since, ""
	}
	if since <= latest {
		return since, ""
	}
	return 0, fmt.Sprintf("since %d is past the end of this session's log, which ends at %d — that "+
		"cursor was counted in some other conversation, so it is refused rather than honoured; "+
		"replaying %s from the beginning", since, latest, sid)
}

// The methods that answer with something, one function each.
//
// serveConn used to hold all of them inline: nineteen blocks of `if req.Method == …`, each ending
// in the same four lines that write the response and continue. That trailer was the bug surface —
// a block that forgot it would answer nothing and then read the next request as though it had —
// and 466 lines in one function is a place where the next method gets added by copying the one
// above it.
//
// The three that are NOT here are the three that do more than answer: shutdown replies and then
// stops the daemon, and watch and transcript each give the connection over to a stream and never
// return to the loop.
var answers = map[string]func(context.Context, Engine, Request) Response{
	"status":      answerStatus,
	"models":      answerModels,
	"tools":       answerTools,
	"jobs":        answerJobs,
	"about":       answerAbout,
	"hand":        answerHand,
	"hand-state":  answerHand,
	"tool":        answerTool,
	"edit-file":   answerEditFile,
	"git":         answerGit,
	"file-do":     answerFileDo,
	"git-diff":    answerGitDiff,
	"git-do":      answerGitDo,
	"meet-join":   answerMeetJoin,
	"meet":        answerMeet,
	"git-pr":      answerGitPR,
	"pr-facts":    answerPRFacts,
	"pr-msg":      answerPRFacts,
	"git-msg":     answerGitMsg,
	"look-over":   answerLookOver,
	"complete":    answerComplete,
	"open-file":   answerOpenFile,
	"suggest":     answerSuggest,
	"shell":       answerShell,
	"config-get":  answerConfigGet,
	"config-set":  answerConfigSet,
	"profiles":    answerProfiles,
	"mcp-attach":  answerMCPAttach,
	"mcp-detach":  answerMCPDetach,
	"sessions":    answerSessions,
	"session-new": answerSessionNew,
	"cron":        answerCron,
	"job-kill":    answerJobKill,
}

// answerJobKill stops one background command. Removed answers whether the id named a job — a ✕
// pressed twice must read "already gone", not "failure".
func answerJobKill(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(JobKiller)
	if !ok {
		return Response{Err: "this daemon cannot stop background commands"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return Response{Err: "no job named"}
	}
	return Response{OK: true, Removed: k.KillBackgroundJob(req.Name)}
}

// CronRow is one standing job as the dock draws it: when it runs next, or why it never will.
type CronRow struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule,omitempty"`
	Enabled  bool   `json:"enabled,omitempty"`
	// Next is RFC3339, and empty when the job never runs — switched off, or Problem says why.
	Next string `json:"next,omitempty"`
	// Problem is why this job can NEVER run, in words. A row carrying one is the row a dock must
	// mark: nothing else on any screen will mention it again.
	Problem string `json:"problem,omitempty"`
}

// answerCron reads the standing schedule out: broken jobs first (they are the ones nobody else
// mentions), then soonest first — the same order the TUI's own panel draws.
func answerCron(ctx context.Context, eng Engine, req Request) Response {
	t, ok := eng.(CronTeller)
	if !ok {
		return Response{Err: "this daemon cannot read its schedule"}
	}
	jobs := t.ScheduledHere()
	sort.SliceStable(jobs, func(i, k int) bool {
		if (jobs[i].Problem != "") != (jobs[k].Problem != "") {
			return jobs[i].Problem != "" // broken first: nothing else will mention them again
		}
		// Then the runnable, soonest first. A job with no next instant and no problem is switched
		// OFF — it belongs after everything that will actually run, not (as zero-before-anything
		// would put it) ahead of the whole schedule.
		ni, nk := !jobs[i].Next.IsZero(), !jobs[k].Next.IsZero()
		if ni != nk {
			return ni
		}
		return jobs[i].Next.Before(jobs[k].Next)
	})
	rows := make([]CronRow, 0, len(jobs))
	for _, j := range jobs {
		r := CronRow{Name: j.Name, Schedule: j.Schedule, Enabled: j.Enabled, Problem: j.Problem}
		if !j.Next.IsZero() {
			r.Next = j.Next.UTC().Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	return Response{OK: true, Cron: rows}
}

// SessionRow is one conversation as the sessions door reports it: what a picker needs and no
// more. The title is the first prompt's first line, which is what the store already derives.
type SessionRow struct {
	ID           string   `json:"id"`
	Title        string   `json:"title,omitempty"`
	Model        string   `json:"model,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Created      string   `json:"created,omitempty"`
	LastActivity string   `json:"lastActivity,omitempty"`
}

// answerSessions lists this workspace's conversations, newest activity first. Whether a turn is
// OPEN in one is deliberately not here: answering it costs reading every log whole, and the one
// conversation it could matter for is the current one — which the caller already has from the
// roster row and can ask status about.
func answerSessions(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(ConversationKeeper)
	if !ok {
		return Response{Err: "this daemon cannot list its conversations"}
	}
	metas, err := k.SessionsHere(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	sort.SliceStable(metas, func(i, j int) bool { return metas[i].LastActivity.After(metas[j].LastActivity) })
	rows := make([]SessionRow, 0, len(metas))
	for _, m := range metas {
		r := SessionRow{ID: string(m.ID), Title: m.Title, Model: m.Model, Labels: m.Labels}
		if !m.Created.IsZero() {
			r.Created = m.Created.UTC().Format(time.RFC3339)
		}
		if !m.LastActivity.IsZero() {
			r.LastActivity = m.LastActivity.UTC().Format(time.RFC3339)
		}
		rows = append(rows, r)
	}
	return Response{OK: true, Sessions: rows}
}

// answerSessionNew opens a fresh conversation and answers with its id — see ConversationKeeper
// for why creating and moving are one verb.
func answerSessionNew(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(ConversationKeeper)
	if !ok {
		return Response{Err: "this daemon cannot open a new conversation"}
	}
	sid, err := k.NewSession(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Session: string(sid)}
}

// answerConfigGet reads the editable settings.
func answerConfigGet(ctx context.Context, eng Engine, _ Request) Response {
	k, ok := eng.(ConfigKeeper)
	if !ok {
		return Response{Err: "this daemon cannot read out its settings"}
	}
	items, err := k.ConfigHere(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Config: items}
}

// answerConfigSet changes one setting and answers with the key as it now stands — so a screen
// redraws from the daemon's own reading rather than from what it hoped the write did.
func answerConfigSet(ctx context.Context, eng Engine, req Request) Response {
	k, ok := eng.(ConfigKeeper)
	if !ok {
		return Response{Err: "this daemon cannot change its settings"}
	}
	item, err := k.ConfigSet(ctx, req.Name, req.Text, req.Tier)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Config: []ConfigItem{item}}
}

// answerProfiles lists what a profile-shaped setting may point at.
func answerProfiles(ctx context.Context, eng Engine, _ Request) Response {
	k, ok := eng.(ConfigKeeper)
	if !ok {
		return Response{Err: "this daemon cannot list its backends"}
	}
	list, err := k.ProfilesHere(ctx)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Profiles: list}
}

// answerMCPAttach opens the runtime door: attach an HTTP MCP server to this companion.
//
// The reply carries the tool names rather than a bare ok. A caller told "ok" knows the handshake
// worked; a caller told mcp__ppt__render, mcp__ppt__open knows what it may now ask for; a caller
// told nothing at all knows the server answered and offers nothing. One ack flattens three
// different situations into one.
func answerMCPAttach(ctx context.Context, eng Engine, req Request) Response {
	h, ok := eng.(ToolServerHost)
	if !ok {
		return Response{Err: "this daemon cannot attach tool servers"}
	}
	names, err := h.AttachToolServer(ctx, req.Name, req.URL, req.Headers)
	if err != nil {
		return Response{Err: err.Error()}
	}
	// Never nil: a client reading JSON should see an empty list rather than null when a server
	// attached and advertised nothing.
	if names == nil {
		names = []string{}
	}
	return Response{OK: true, Tools: names}
}

// answerMCPDetach is the other half. A helper reconnecting after a crash sends detach first, and
// the answer tells it whether it was cleaning up or was already clean.
func answerMCPDetach(ctx context.Context, eng Engine, req Request) Response {
	h, ok := eng.(ToolServerHost)
	if !ok {
		return Response{Err: "this daemon cannot attach tool servers"}
	}
	removed, err := h.DetachToolServer(req.Name)
	if err != nil {
		return Response{Err: err.Error()} // refused: there is one, and it is not this caller's
	}
	return Response{OK: true, Removed: removed}
}

func answerStatus(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true}
	if ask, ok := eng.Waiting(session.SessionID(req.Session)); ok {
		resp.Waiting = &Waiting{
			ID: ask.ID, Kind: ask.Kind, What: ask.What, Args: ask.Args,
			Reason: ask.Reason, Options: ask.Options, Report: ask.Report,
			Diff:  ask.Diff,
			Index: ask.Index, Total: ask.Total,
			Since: ask.Since.UTC().Format(time.RFC3339),
		}
	}
	resp.Doing, _ = eng.Doing(session.SessionID(req.Session))
	if c, ok := eng.(Controller); ok {
		resp.Permission = c.Permission()
		resp.Backend = c.Backend()
	}
	if n, ok := eng.(UserNamer); ok {
		resp.User = n.UserLabel(session.SessionID(req.Session))
	}
	return resp
}

// models is the list this companion could be put on, asked of its own backend.
func answerModels(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true, Models: []string{}}
	if l, ok := eng.(ModelLister); ok {
		// A backend that is slow or down must not hold the socket: the caller is a screen
		// drawing a menu, and no menu is a better answer than a stuck one.
		mctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		names, err := l.ListModels(mctx)
		cancel()
		if err == nil {
			resp.Models = names
		} else {
			resp.Why = err.Error()
		}
	}
	return resp
}

// tools is answered here too, and for the same reason.
func answerTools(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true, Tools: []string{}}
	if t, ok := eng.(ToolLister); ok {
		resp.Tools = t.ToolNames()
	}
	return resp
}

// jobs is answered here for the same reason status is: it carries a payload.
func answerJobs(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	resp = Response{OK: true, Jobs: &Jobs{}}
	if j, ok := eng.(JobRunner); ok {
		for _, b := range j.BackgroundJobs() {
			resp.Jobs.Background = append(resp.Jobs.Background, BackgroundJob{
				ID: b.ID, Command: b.Command, Running: b.Running, Killed: b.Killed,
				Exit: b.Exit, Started: b.Started.UTC().Format(time.RFC3339),
				Tail: j.BackgroundTail(b.ID, jobTailBytes),
			})
		}
		resp.Jobs.Queued = j.QueuedWork()
		for _, c := range j.SubagentJobs() {
			out := ChildJob{
				ID: c.ID, Tool: c.Tool, Task: c.Task, Running: c.Running,
				Steps: c.Steps, Err: c.Err,
				Started: c.Started.UTC().Format(time.RFC3339),
			}
			if !c.Ended.IsZero() {
				out.Ended = c.Ended.UTC().Format(time.RFC3339)
			}
			resp.Jobs.Children = append(resp.Jobs.Children, out)
		}
	}
	return resp
}

// about is answered here rather than in dispatch, like status and shell, because it has a
// payload. It is also the whole point of the relay: whoever is asking connected to THIS
// companion, so there is no name to resolve and no config directory to read — the process
// that knows answers about itself.
func answerAbout(ctx context.Context, eng Engine, req Request) Response {
	d, ok := eng.(Describer)
	if !ok {
		return Response{Err: "this daemon cannot describe its companion"}
	}
	// The rendered description as before, plus the structured handshake so a caller can negotiate:
	// the wire protocol and capabilities this build speaks, and — when the engine carries it — the
	// binary version. All additive and omitempty, so an older client that only reads Out is unaffected.
	resp := Response{OK: true, Out: d.About(), Proto: ProtoVersion, Caps: capsOf(eng)}
	if v, ok := eng.(Versioner); ok {
		resp.Version = v.Version()
	}
	return resp
}

// hand and hand-state are answered here for the same reason about is, and they are the
// reason the relay exists at all. Whoever is asking has connected to THIS companion, so
// there is no name to resolve against a config directory that may belong to another
// account and may not exist at all inside a container. The process doing the work says
// what became of it.
func answerHand(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	taker, ok := eng.(Taker)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot be handed work"}
	case req.Method == "hand":
		id, herr := taker.Hand(ctx, req.Name, req.Text, req.Looking)
		if herr != nil {
			resp = Response{Err: herr.Error()}
		} else {
			resp = Response{OK: true, Out: id}
		}
	default:
		h, herr := taker.Handed(ctx, req.Name)
		if herr != nil {
			resp = Response{Err: herr.Error()}
		} else {
			resp = Response{OK: true, Handover: &h}
		}
	}
	return resp
}

// A read-only tool, run where the workspace is. Answered here rather than in dispatch for the
// same reason shell is: it has a payload.
func answerTool(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	reader, ok := eng.(ToolReader)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot read its workspace"}
	case strings.TrimSpace(req.Name) == "":
		resp = Response{Err: "no tool named"}
	default:
		out, rerr := reader.ReadOnlyTool(ctx, req.Name, req.Args)
		if rerr != nil {
			resp = Response{Err: rerr.Error()}
		} else {
			resp = Response{OK: true, Out: out}
		}
	}
	return resp
}

// An edit, made where the workspace is, and written into the log as a person's own words.
func answerEditFile(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	writer, ok := eng.(ToolWriter)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot be asked to edit its workspace"}
	case strings.TrimSpace(req.Name) == "":
		resp = Response{Err: "no tool named"}
	case req.Name == "patch":
		if perr := writer.PatchFile(ctx, req.Text, req.Answer, req.Ask); perr != nil {
			resp = Response{Err: perr.Error()}
		} else {
			resp = Response{OK: true}
		}
	default:
		out, werr := writer.WriteTool(ctx, req.Name, req.Args, req.Ask)
		if werr != nil {
			resp = Response{Err: werr.Error()}
		} else {
			resp = Response{OK: true, Out: out}
		}
	}
	return resp
}

func answerGit(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	teller, ok := eng.(GitTeller)
	if !ok {
		resp = Response{Err: "this daemon cannot say what git makes of its workspace"}
	} else if out, gerr := teller.Git(ctx); gerr != nil {
		resp = Response{Err: gerr.Error()}
	} else {
		resp = Response{OK: true, Out: string(out)}
	}
	return resp
}

func answerFileDo(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	keeper, ok := eng.(FileKeeper)
	if !ok {
		resp = Response{Err: "this daemon cannot make or remove files"}
	} else if ferr := keeper.FileDo(ctx, req.Name, req.Text, req.Answer, req.Ask); ferr != nil {
		resp = Response{Err: ferr.Error()}
	} else {
		resp = Response{OK: true}
	}
	return resp
}

func answerGitDiff(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	teller, ok := eng.(GitTeller)
	if !ok {
		resp = Response{Err: "this daemon cannot show a diff"}
	} else if out, derr := teller.GitDiff(ctx, req.Text, req.Decision == "staged",
		req.Decision == "untracked"); derr != nil {
		resp = Response{Err: derr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerGitDo(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	doer, ok := eng.(GitDoer)
	if !ok {
		resp = Response{Err: "this daemon cannot run git commands"}
	} else if out, gerr := doer.GitDo(ctx, req.Name, req.Text, req.Answer, req.Ask); gerr != nil {
		resp = Response{Err: gerr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerMeetJoin(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	sp, ok := eng.(Speaker)
	if !ok {
		resp = Response{Err: "this daemon cannot take part in a meeting"}
	} else if ready, room, jerr := sp.MeetingJoin(ctx, req.Meeting, req.Name); jerr != nil {
		resp = Response{Err: jerr.Error()}
	} else {
		resp = Response{OK: true, Out: ready, Session: room}
	}
	return resp
}

func answerMeet(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	sp, ok := eng.(Speaker)
	if !ok {
		resp = Response{Err: "this daemon cannot take part in a meeting"}
	} else if c, merr := sp.MeetingTurn(ctx, req.Meeting, req.Name, req.Text,
		req.Decision == "closing"); merr != nil {
		resp = Response{Err: merr.Error()}
	} else {
		// A pass travels as a flag rather than as a word in the text, or a contribution
		// that happens to begin with the word would arrive as a silence.
		//
		// The room travels on every turn and not only on the join: a daemon that restarted
		// mid-meeting prepares again in a NEW session, and a viewer holding the old id
		// would show an empty working rather than the one that produced the sentence in
		// front of it.
		resp = Response{OK: true, Out: c.Said, Exit: passFlag(c.Pass), Session: c.Room}
	}
	return resp
}

func answerGitPR(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot open a pull request"}
	} else if url, perr := rev.OpenPR(ctx, req.Name, req.Text); perr != nil {
		resp = Response{Err: perr.Error()}
	} else {
		resp = Response{OK: true, Out: url}
	}
	return resp
}

func answerPRFacts(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot answer about pull requests"}
	} else {
		var out string
		var perr error
		if req.Method == "pr-facts" {
			out, perr = rev.PRFacts(ctx)
		} else {
			out, perr = rev.DraftPR(ctx, req.Text)
		}
		if perr != nil {
			resp = Response{Err: perr.Error()}
		} else {
			resp = Response{OK: true, Out: out}
		}
	}
	return resp
}

func answerGitMsg(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot draft a commit message"}
	} else if out, derr := rev.DraftCommit(ctx, req.Text); derr != nil {
		resp = Response{Err: derr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerLookOver(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	rev, ok := eng.(Reviewer)
	if !ok {
		resp = Response{Err: "this daemon cannot look over a file"}
	} else if out, lerr := rev.LookOver(ctx, req.Name, req.Text); lerr != nil {
		resp = Response{Err: lerr.Error()}
	} else {
		resp = Response{OK: true, Out: out}
	}
	return resp
}

func answerComplete(ctx context.Context, eng Engine, req Request) Response {
	rev, ok := eng.(Reviewer)
	if !ok {
		return Response{Err: "this daemon cannot complete code"}
	}
	var a completeArgs
	if len(req.Args) > 0 {
		if err := json.Unmarshal(req.Args, &a); err != nil {
			return Response{Err: err.Error()}
		}
	}
	out, why, err := rev.CompleteCode(ctx, req.Name, a.Prefix, a.Suffix)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Out: out, Reason: string(why)}
}

func answerOpenFile(ctx context.Context, eng Engine, req Request) Response {
	rev, ok := eng.(Reviewer)
	if !ok {
		return Response{Err: "this daemon cannot track an open file"}
	}
	if err := rev.SetOpenFile(ctx, req.Name, req.Text); err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true}
}

func answerSuggest(ctx context.Context, eng Engine, req Request) Response {
	rev, ok := eng.(Reviewer)
	if !ok {
		return Response{Err: "this daemon cannot suggest a prompt"}
	}
	out, err := rev.SuggestPrompt(ctx, req.Text)
	if err != nil {
		return Response{Err: err.Error()}
	}
	return Response{OK: true, Out: out}
}

// shell is answered here rather than in dispatch, like status, because it has a payload:
// dispatch returns only an error, and giving it a return value for one caller would make
// every other write site pretend to produce something.
func answerShell(ctx context.Context, eng Engine, req Request) Response {
	var resp Response
	runner, ok := eng.(ShellRunner)
	switch {
	case !ok:
		resp = Response{Err: "this daemon cannot run commands"}
	case strings.TrimSpace(req.Text) == "":
		resp = Response{Err: "no command"}
	default:
		out, code, rerr := runner.RunShellHere(ctx, req.Text)
		if rerr != nil {
			resp = Response{Err: rerr.Error()}
		} else {
			resp = Response{OK: true, Out: out, Exit: &code}
		}
	}
	return resp
}

// Describer is an engine that can say what its companion is for and what it can be asked to do.
//
// Optional, like CronController and ShellRunner, for the reason those are: Engine is what every
// fake in every package must satisfy, and a test double has no workspace to describe.
type Describer interface {
	// About renders the same description the MCP `about` tool gives — one renderer, so the answer
	// does not depend on which door it came through.
	About() string
}

// Versioner is an optional Engine capability: the running binary's version, for the `about`
// handshake (Response.Version). Optional like Describer — a test double has no build metadata to
// report, and answerAbout simply omits the field when the engine does not carry it.
type Versioner interface{ Version() string }

// Updater is an optional Engine capability: run a self-update — download the latest release and put
// it on disk with rollback (see internal/update.RunCommit) — and report what happened. The daemon's
// `update` method calls it and, when it actually updated, restarts onto the new binary. Optional like
// the others: a test double has no release to fetch. The `update` method is not on the fleet-door
// allowlist, so the narrowed remote entry cannot carry it; its boundary is the owner-only local
// socket plus whatever the operator deliberately pipes to it (--relay over their own ssh) — the same
// boundary shutdown and restart have.
type Updater interface {
	Update(ctx context.Context) (UpdateResult, error)
}

// UpdateResult is what a self-update did: Updated with From→To when a new build was committed, or a
// Message ("already up to date") when nothing changed. On a failed pre-flight the update rolled back
// and Update returns an error instead.
type UpdateResult struct {
	Updated bool
	From    string
	To      string
	Message string
}

// ProtoVersion is the daemon wire-protocol version, carried in the `about` handshake
// (Response.Proto). It is bumped when the Request/Response shape gains a capability peers negotiate
// on, so a newer and an older instance can each learn what the other speaks and one can down-convert
// for the other. It is NOT the binary version (that is Response.Version).
const ProtoVersion = 1

// Caps names the negotiable protocol capabilities this build speaks, carried in the `about`
// handshake (Response.Caps). A sender checks a peer's advertised set (PeerInfo.Supports / a Client's
// PeerSupports) before using a newer method or field, so it never sends what an older peer would
// silently drop — encoding/json ignores unknown fields, which would turn a shape mismatch into wrong
// behaviour rather than an error. The list grows as gated features land; "handshake" marks a build
// that answers this versioned about at all (every build from this one on).
func Caps() []string { return capsOf(nil) }

// capsOf is Caps for a known engine, which is the only honest way to answer it.
//
// A capability that is a property of the BUILD can be a constant; "tool-servers" is not one. The
// door is an optional interface (ToolServerHost), so whether this daemon accepts mcp-attach is a
// fact about the engine it is running, and a build-level list would advertise it and then refuse —
// reintroducing, one layer down, exactly the distinction the capability exists to make. Two lines
// below, Version already asks the engine; this one did not.
//
// nil engine answers the build-level floor: what any daemon from this build speaks whoever is
// behind it.
func capsOf(eng Engine) []string {
	// "handshake" marks a build that answers this versioned about at all.
	caps := []string{"handshake"}
	// "roster": this daemon answers who is out there (roster.go). Build-level, unlike the two
	// below: the answer is read from the listener's home directory, not from an engine interface,
	// so any daemon from this build speaks it.
	caps = append(caps, "roster")
	// "tool-servers": this daemon accepts mcp-attach / mcp-detach. Asked of the engine because an
	// application that IS a tool server (an editor plugin, a slide add-in) draws a list of
	// companions and decides which ones it can attach to BEFORE attaching — so what it needs from
	// this answer is "this daemon will take it", not "this build contains the code".
	//
	// It is advertised at all because the door landed dispatched and unadvertised, leaving a client
	// nothing to ask: it would have to call the method and read the refusal back, and prose is not
	// a contract — the two refusals differ in wording today (one lists the accepted methods, the
	// other says this daemon cannot attach tool servers) and one of them has already been reworded
	// once in this repository.
	//
	// This repository has a name for advertising that runs ahead of the code. This was the other
	// direction, which is quieter: the code ran ahead of the advertisement, and nothing failed.
	if _, ok := eng.(ToolServerHost); ok {
		caps = append(caps, "tool-servers")
	}
	// "settings": this daemon will read and change the whitelisted config keys. Advertised for
	// the same reason as the doors above — a client draws a settings screen, and "there is no
	// door" and "the door refused" are different screens.
	if _, ok := eng.(ConfigKeeper); ok {
		caps = append(caps, "settings")
	}
	// "transcript": this daemon will read a conversation out down the socket. Asked of the engine
	// for the reason above, and advertised for the reason above: the clients this door exists for
	// are the ones with no log reader, and the only alternative to an answer here is calling the
	// method and reading a sentence back — which cannot tell a build that does not know the method
	// from an engine that will not do it.
	if _, ok := eng.(Transcriber); ok {
		caps = append(caps, "transcript")
	}
	// The dock's verbs, engine-gated like the two above and advertised for the same reason: an
	// IDE deciding whether to draw a picker cannot tell an old build from a refusing engine by
	// calling and reading prose back.
	if _, ok := eng.(ConversationKeeper); ok {
		caps = append(caps, "sessions", "session-new")
	}
	if _, ok := eng.(CronTeller); ok {
		caps = append(caps, "cron")
	}
	if _, ok := eng.(JobKiller); ok {
		caps = append(caps, "job-kill")
	}
	return caps
}

// About asks a companion to describe itself.
func (c *Client) About() (string, error) {
	resp, err := c.exchange(Request{Method: "about"})
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New(resp.Err)
	}
	return resp.Out, nil
}

// Hello runs the `about` handshake and returns what the far side is — its version, wire protocol and
// capabilities. The result is cached on the client, so PeerSupports can gate later sends without a
// second round trip. A daemon that predates the handshake answers with proto 0 and no caps, which the
// caller reads as "hold to the pre-negotiation behaviour."
func (c *Client) Hello() (PeerInfo, error) {
	resp, err := c.exchange(Request{Method: "about"})
	if err != nil {
		return PeerInfo{}, err
	}
	if !resp.OK {
		return PeerInfo{}, errors.New(resp.Err)
	}
	p := PeerInfo{Version: resp.Version, Proto: resp.Proto, Caps: resp.Caps}
	c.mu.Lock()
	c.peer = &p
	c.mu.Unlock()
	return p, nil
}

// PeerSupports reports whether the far side advertised a capability, from the cached handshake. It is
// false before the first Hello and for a peer that predates the handshake — both of which mean "do
// not send the newer form." A sender gates a new method or field on this so it never ships what an
// older peer would silently drop.
func (c *Client) PeerSupports(capability string) bool {
	c.mu.Lock()
	p := c.peer
	c.mu.Unlock()
	return p != nil && p.Supports(capability)
}

// Handover is what became of one piece of work handed to a companion.
//
// Done and Over are both endings and they are not the same one: Done means a turn finished and
// Answer is what was said, Over means nothing is coming and News says why. A caller that collapsed
// them would report a crash as an empty answer.
type Handover struct {
	Done   bool   `json:"done,omitempty"`
	Answer string `json:"answer,omitempty"`
	News   string `json:"news,omitempty"`
	Over   bool   `json:"over,omitempty"`
}

// Taker is an engine that can be handed work by a companion somewhere else.
//
// Optional, like Describer and ShellRunner, for the reason those are: Engine is what every fake in
// every package must satisfy, and a test double has no workspace to take work into.
type Taker interface {
	// Hand takes one piece of work under a label naming who asked, and returns the receipt it is
	// asked about with. A refusal is an error — this companion is mid-turn, or not published —
	// because a refusal is an answer and the wire has one place for sentences a caller reads.
	//
	// looking says the asker declared this a QUESTION: the receiver runs it with the four tools
	// that only read, and because such a turn cannot touch the workspace it need not wait for one
	// that can. It is the receiver that enforces it, not the asker — an asker cannot bind anybody.
	Hand(ctx context.Context, label, request string, looking bool) (receipt string, err error)
	// Handed says what became of the work a receipt stands for. Read-only, and called by whoever
	// is waiting, so it must stay cheap and must never make something happen.
	//
	// Kept alongside Watch rather than replaced by it, and not only for a daemon too old to
	// stream: it is the one question that distinguishes "this daemon does not know that receipt"
	// from "this daemon does not know that method", which are a wait to end and a wait to carry
	// on polling.
	Handed(ctx context.Context, receipt string) (Handover, error)
	// Watch says the same thing when it happens instead of when asked, calling say for each
	// change, and returns when there is nothing more coming. A cancelled ctx is the peer having
	// hung up, and is not an error. An error is a refusal, said the way the other two say theirs.
	Watch(ctx context.Context, receipt string, say func(Handover) error) error
}

// Refused is a daemon's own answer that it will not do the thing.
//
// Distinguished from a transport failure on purpose. Both arrive as an error and they want opposite
// reactions: a refusal is a sentence to show whoever asked, so they can pick somebody else, while a
// broken link is a machine to go and fix. Collapsed into one, the advice for the second ("it needs
// magi on its PATH") ends up printed under a companion that answered perfectly clearly.
type Refused struct{ Why string }

func (r Refused) Error() string { return r.Why }

// ErrGone is a companion whose daemon is not running — as distinct from one that could not be
// reached at all.
//
// The distinction can only be drawn on the far machine. From here a crossing that fails looks the
// same whether the network went, the login failed, or the process died, and those are a wait to
// keep running and a wait to end. So whatever carries the protocol across is expected to report
// this when it learns it, and the wire itself never carries it: a daemon that is not there cannot
// say so.
var ErrGone = errors.New("that companion is not running")

// Hand gives a companion a piece of work and takes the receipt for it.
func (c *Client) Hand(label, request string, looking bool) (string, error) {
	resp, err := c.exchange(Request{Method: "hand", Name: label, Text: request, Looking: looking})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// Watch follows handed-over work until there is nothing more coming, calling each with what the
// far side says as it says it. Returning false from each stops listening.
//
// This connection is given over to the watch: the mutex every other call takes for one exchange is
// held here for as long as the work lasts, so a watcher must open a connection of its own. A clean
// end is not an error — the daemon closing the stream is it saying there is no more.
func (c *Client) Watch(receipt string, each func(Handover) bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enc.Encode(Request{Method: "watch", Name: receipt}); err != nil {
		return fmt.Errorf("daemon: send: %w", err)
	}
	for c.sc.Scan() {
		var resp Response
		if err := json.Unmarshal(c.sc.Bytes(), &resp); err != nil {
			return fmt.Errorf("daemon: malformed reply: %w", err)
		}
		if !resp.OK {
			why := resp.Err
			if why == "" {
				why = "the daemon refused without saying why"
			}
			return Refused{Why: why}
		}
		if resp.Handover == nil {
			continue
		}
		if !each(*resp.Handover) {
			return nil
		}
	}
	return c.sc.Err()
}

// Transcript reads a conversation out of a companion: everything the log holds after since, then
// everything that happens next, one event per call to each. Returning false from each stops
// listening.
//
// This connection is given over to it, exactly as Watch's is — the mutex every other call takes for
// one exchange is held for as long as the caller keeps reading, so a reader opens a connection of
// its own. A clean end is not an error.
//
// since 0 (or any negative) is everything. restart is called, before the first event, when the
// daemon would not honour the cursor and is sending the whole conversation instead: a caller that
// is appending to something must throw that away first, or it stitches the beginning of the session
// onto the end of what it is already showing. nil is fine for a caller that asked for everything.
func (c *Client) Transcript(sid string, since int64, restart func(why string), each func(event.Event) bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.enc.Encode(Request{Method: "transcript", Session: sid, Since: since}); err != nil {
		return fmt.Errorf("daemon: send: %w", err)
	}
	for c.sc.Scan() {
		var resp Response
		if err := json.Unmarshal(c.sc.Bytes(), &resp); err != nil {
			return fmt.Errorf("daemon: malformed reply: %w", err)
		}
		if !resp.OK {
			why := resp.Err
			if why == "" {
				why = "the daemon refused without saying why"
			}
			return Refused{Why: why}
		}
		if resp.Event == nil {
			// A frame with no event is the daemon saying something about the stream rather than
			// carrying a piece of it — today, only that the cursor was refused.
			if resp.Why != "" && restart != nil {
				restart(resp.Why)
			}
			continue
		}
		if !each(*resp.Event) {
			return nil
		}
	}
	return c.sc.Err()
}

// Handed asks what became of work handed over under a receipt.
func (c *Client) Handed(receipt string) (Handover, error) {
	resp, err := c.exchange(Request{Method: "hand-state", Name: receipt})
	if err != nil {
		return Handover{}, err
	}
	if resp.Handover == nil {
		return Handover{}, nil
	}
	return *resp.Handover, nil
}

// controlMu puts the state-changing controls in a queue of one.
//
// Each of them is read-decide-write against state two goroutines can reach — two consoles, a
// console and a phone, a terminal and either. Left to overlap they lose each other's changes in
// ways that are invisible: set-model writes the new id under the App's lock and persists it
// OUTSIDE that lock, so two of them landing together can leave the running process on one model
// and the file that survives a restart on the other, with both callers told they succeeded.
//
// Only the fast ones. `compact` is a model round trip and holding a gate across it would stop
// every other control for as long as a summarisation takes; it also touches nothing these do.
// Submitting, steering, interrupting and answering are not here either: each one carries the
// session it means, and the App is already the one arbiter of running a turn at a time.
var controlMu sync.Mutex

var serialControls = map[string]bool{
	"resume": true, "rewind": true, "set-model": true, "set-permission": true, "reload-cron": true,
	"use-backend": true,
}

func dispatch(ctx context.Context, eng Engine, r Request) error {
	if serialControls[r.Method] {
		controlMu.Lock()
		defer controlMu.Unlock()
	}
	return dispatchNow(ctx, eng, r)
}

// conversationErr translates the store's private vocabulary into the door's: a client that sent
// a prompt to an id nobody minted was told "jsonl: unknown session … (first append must include
// session.created)" — implementation circumstances, with no next move in them. The refusal now
// names the two verbs that ARE the next move, and keeps the cause for whoever debugs.
func conversationErr(sid session.SessionID, err error) error {
	if err == nil || !strings.Contains(err.Error(), "unknown session") {
		return err
	}
	return fmt.Errorf("no conversation %q in this workspace — `sessions` lists them, `session-new` opens one (%v)", sid, err)
}

func dispatchNow(ctx context.Context, eng Engine, r Request) error {
	sid := session.SessionID(r.Session)
	parts := []session.Part{{Kind: session.PartText, Text: r.Text}}
	actor := event.Actor{Kind: event.ActorUser, ID: "attach"}
	switch r.Method {
	case "submit":
		return conversationErr(sid, eng.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: parts, Actor: actor, Refs: r.Refs}))
	case "steer":
		return conversationErr(sid, eng.Steer(ctx, command.SubmitPrompt{SessionID: sid, Parts: parts, Actor: actor, Refs: r.Refs}))
	case "interrupt":
		return eng.Interrupt(ctx, command.Interrupt{SessionID: sid})
	case "permission":
		return eng.RespondPermission(ctx, command.RespondPermission{
			SessionID: sid, CallID: r.CallID, Decision: r.Decision, Actor: actor})
	case "answer":
		return eng.RespondQuestion(ctx, command.RespondQuestion{
			SessionID: sid, CallID: r.CallID, Answer: r.Answer})
	// Named apart from "permission", which ANSWERS a prompt. One word for "decide this call" and
	// "change the policy for every call" would be a wire that means two things.
	case "rewind", "compact", "set-model", "set-permission", "use-backend":
		return control(ctx, eng, r, sid)
	case "resume":
		m, ok := eng.(SessionMover)
		if !ok {
			return fmt.Errorf("this companion cannot be moved to another conversation")
		}
		return m.Resume(ctx, sid)
	case "reload-cron":
		// Its own optional interface rather than another method on Controller. Controller is what
		// every fake in every package must satisfy, and this file already says that a control
		// surface which grows is not a reason to touch four test doubles.
		c, ok := eng.(CronController)
		if !ok {
			return fmt.Errorf("this daemon holds no scheduled work")
		}
		c.ReloadCron()
		return nil
	}
	// Name what IS accepted. A client told only "unknown" cannot tell a typo from a version skew,
	// and the two want different reactions.
	return fmt.Errorf("unknown method %q — this daemon accepts: %s", r.Method, acceptedMethods())
}

// acceptedMethods is every method serveConn answers, derived from the tables that actually answer
// them rather than kept as a sentence. The sentence drifted: it listed the roster as of the day it
// was written, and the answers map had since grown a dozen methods (tool, edit-file, git, the
// meeting verbs, the draft verbs) the refusal then denied existed — a typo-correcting message that
// itself needed correcting.
var acceptedMethods = sync.OnceValue(func() string {
	names := map[string]bool{
		// The methods dispatch and serveConn handle outside the answers map.
		"submit": true, "steer": true, "interrupt": true, "permission": true, "answer": true,
		"rewind": true, "compact": true, "set-model": true, "set-permission": true, "use-backend": true,
		"resume": true, "reload-cron": true, "watch": true, "shutdown": true, "restart": true,
		"update": true, "transcript": true, "roster": true,
	}
	for m := range answers {
		names[m] = true
	}
	out := make([]string, 0, len(names))
	for m := range names {
		out = append(out, m)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
})

// control runs one of the calls that change how the engine behaves.
func control(ctx context.Context, eng Engine, r Request, sid session.SessionID) error {
	c, ok := eng.(Controller)
	if !ok {
		return fmt.Errorf("this daemon cannot be controlled remotely (%s)", r.Method)
	}
	switch r.Method {
	case "rewind":
		_, err := c.Rewind(ctx, sid, r.N)
		return err
	case "compact":
		return c.Compact(ctx, command.Compact{SessionID: sid})
	case "set-model":
		c.SetModel(sid, r.Name)
		return nil
	case "use-backend":
		return c.UseBackend(sid, r.Name)
	case "set-permission":
		c.SetPermission(r.Name)
		return nil
	}
	return fmt.Errorf("unknown control %q", r.Method)
}

// Client talks to a daemon. One connection, one request at a time: these are user actions, and the
// serialisation is what keeps a steer from overtaking the submit it belongs to.
type Client struct {
	mu sync.Mutex
	// rw carries the protocol. Usually a unix socket; also a pipe to a process that is relaying to
	// one on another machine, which is why this is not a net.Conn. The protocol is the same either
	// way — that is the point of a relay being a byte pipe and nothing more.
	rw io.ReadWriteCloser
	// nc is rw when it happens to be a network connection, for deadlines. A pipe has none to set,
	// and the caller that wanted one gets its bound from the process it spawned instead.
	nc  net.Conn
	enc *json.Encoder
	sc  *bufio.Scanner
	// deadline bounds one exchange. Zero for a UI's connection: the person on the other end
	// decides how long a thing takes. Non-zero for a probe, where the caller is asking about a
	// process that may be wedged and must not be dragged down with it.
	deadline time.Duration
	// peer is what the about handshake learned about the far side (Hello), cached so PeerSupports
	// can gate a newer send without a second round trip. nil until the first Hello; a peer that
	// predates the handshake caches as proto 0 / no caps, which reads as "hold to old behaviour".
	peer *PeerInfo
}

// PeerInfo is what the `about` handshake learned about the far side: its binary version, the wire
// protocol it speaks, and the capabilities it advertised. Supports gates a newer method or field so
// a sender never ships what an older peer would silently drop (encoding/json ignores unknown fields,
// turning a shape mismatch into wrong behaviour rather than an error).
type PeerInfo struct {
	Version string
	Proto   int
	Caps    []string
}

// Supports reports whether the far side advertised a capability in its handshake.
func (p PeerInfo) Supports(capability string) bool {
	for _, c := range p.Caps {
		if c == capability {
			return true
		}
	}
	return false
}

// Dial connects to a daemon, or says plainly that none is there.
func Dial(path string) (*Client, error) { return dial(path, 0, 0) }

// dialProbe connects for a single bounded question. Both bounds matter and they are different
// failures: connectTimeout is a socket file whose owner is gone in a way that leaves connect
// hanging, and deadline is a daemon that accepted and then never answered. A listing that can be
// stopped by either is a listing you cannot run while anything is wrong, which is when you run it.
func dialProbe(path string, connectTimeout, deadline time.Duration) (*Client, error) {
	return dial(path, connectTimeout, deadline)
}

func dial(path string, connectTimeout, deadline time.Duration) (*Client, error) {
	if err := tooLong(path); err != nil {
		return nil, err
	}
	var (
		conn net.Conn
		err  error
	)
	if connectTimeout > 0 {
		conn, err = net.DialTimeout("unix", path, connectTimeout)
	} else {
		conn, err = net.Dial("unix", path)
	}
	if err != nil {
		// Refused, not absent. A socket belonging to another account answers "permission denied",
		// and that daemon IS running — telling somebody to start one would send them to fix a
		// machine that is fine.
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("daemon: %w", err)
		}
		// Otherwise the question is only whether anything is at that path, which is a fact this
		// process can check rather than a phrase to match. It used to match phrases, and the
		// phrases differ by platform: a leftover file is "connection refused" on Linux and "socket
		// operation on non-socket" on macOS, so the same absence was ErrGone on one and not on the
		// other — and a caller that asked the honest question got the honest answer on one CI and
		// not the other.
		if fi, serr := os.Stat(path); serr == nil {
			// WHAT is at that path, not merely that something is. The sentence below decides
			// what a screen says and, in the clients that restart on it, what happens next — and
			// "the daemon died" about a path no daemon has ever listened on sends somebody
			// looking for a crash that did not happen. The file's TYPE answers it without asking
			// the kernel for its opinion: the errnos differ by platform (a plain file is ENOTSOCK
			// on macOS and ECONNREFUSED on Linux, which is the same trap this branch already
			// records one paragraph up), and a mode bit does not.
			if fi.Mode()&os.ModeSocket == 0 {
				return nil, notSocket("%s is not a socket — something else is at the path a "+
					"companion's socket would take, so no daemon has ever listened there", path)
			}
			return nil, gone("a socket is at %s but nothing is listening — the daemon died; "+
				"start one with `magi --daemon`", path)
		}
		return nil, gone("no magi daemon at %s — start one with `magi --daemon` in that workspace", path)
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	return &Client{rw: conn, nc: conn, enc: json.NewEncoder(conn), sc: sc, deadline: deadline}, nil
}

// gone is "that companion is not running", said in the words for the case at hand.
//
// Both of the sentences above are ErrGone and neither used to say so: the message was built with
// fmt.Errorf and the syscall underneath was dropped, so a caller could match the WORDS or nothing.
// One did — the console's "is it off?" check keyed on errno and therefore worked on macOS, where
// connecting to a leftover file gives ENOTSOCK and falls through this branch, and failed on Linux,
// where it gives ECONNREFUSED and lands here. That is a difference no caller should have to know.
func gone(format string, a ...any) error { return goneErr{msg: fmt.Sprintf(format, a...)} }

// ErrNotASocket is "there is something at that path and it was never a companion's door".
//
// Its own sentinel, wrapping ErrGone so every caller that only wants "cannot be reached" keeps
// working: the two are the same unreachability and different facts. A dead daemon is a thing to
// restart; a plain file where a socket belongs is a thing to look at, and a client that restarts
// on the first would otherwise do it forever on the second.
var ErrNotASocket = errors.New("that path is not a companion's socket")

func notSocket(format string, a ...any) error {
	return notSocketErr{msg: fmt.Sprintf(format, a...)}
}

type notSocketErr struct{ msg string }

func (e notSocketErr) Error() string { return e.msg }

// Is answers both questions truthfully: this is not a socket, AND nothing is reachable there.
func (e notSocketErr) Is(target error) bool {
	return target == ErrNotASocket || target == ErrGone
}

type goneErr struct{ msg string }

func (e goneErr) Error() string { return e.msg }

// Unwrap, so errors.Is(err, ErrGone) is the question a caller asks, and the sentence stays the one
// a person reads.
func (e goneErr) Unwrap() error { return ErrGone }

// Over speaks the daemon protocol across an already-open pipe.
//
// The pipe reaches a daemon somewhere — over ssh, into a container, through anything that carries
// bytes both ways. Nothing here knows or cares which: a relay is deliberately dumb, so a new kind
// of machine boundary is a new way to make a pipe and not a new way to ask a question.
//
// This is what replaced spawning a subcommand on the far side to re-derive, from that user's config
// directory, what the running daemon already knew about itself.
func Over(rw io.ReadWriteCloser) *Client {
	sc := bufio.NewScanner(rw)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	return &Client{rw: rw, enc: json.NewEncoder(rw), sc: sc}
}

func (c *Client) Close() error { return c.rw.Close() }

// Status asks what the daemon is blocked on and what it is on, if anything. A nil Waiting means it
// is blocked on nobody; an empty note means no running tool has reported.
//
// Both in one exchange because they are one question asked at one moment. Two calls could return a
// prompt and a progress note taken half a second apart, which is a state the daemon was never in.
// Status is the three things only the running process knows: what it is blocked on, what a
// long-running tool last said, and which approval mode it is on right now.
// Status is what a daemon says about itself right now: the four facts that exist only in the
// process holding the run.
//
// A struct rather than a return list. This was three values, then four, and the fifth would have
// made every call site unpack positions it does not use to reach the one it does.
type Status struct {
	// Asking is the prompt it is blocked on, or nil.
	Asking *Waiting
	// Doing is what a still-running tool last reported about itself.
	Doing string
	// Permission is the approval mode it is on now.
	Permission string
	// Backend is the base URL its LLM requests go to now.
	Backend string
	// User is what it calls the person, when a plugin has renamed them.
	User string
}

// Jobs asks what is running beside the turn. An empty answer and no error is the ordinary case:
// nothing spawned, nothing backgrounded.
func (c *Client) Jobs(sid string) (Jobs, error) {
	resp, err := c.exchange(Request{Method: "jobs", Session: sid})
	if err != nil {
		return Jobs{}, err
	}
	if resp.Jobs == nil {
		return Jobs{}, nil
	}
	return *resp.Jobs, nil
}

// Models is what this companion could be put on, from its own backend. Empty when the daemon
// cannot say or the backend did not answer; Reason carries why when there is one.
func (c *Client) Models() ([]string, error) {
	resp, err := c.exchange(Request{Method: "models"})
	if err != nil {
		return nil, err
	}
	if len(resp.Models) == 0 && resp.Why != "" {
		return nil, errors.New(resp.Why)
	}
	return resp.Models, nil
}

// Tools is the roster the daemon is running with. Empty from a daemon that cannot say, which the
// caller must not read as "no tools" — it is "not known from here".
func (c *Client) Tools() ([]string, error) {
	resp, err := c.exchange(Request{Method: "tools"})
	if err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

// AttachMCP asks the daemon to attach an HTTP MCP server and returns the tools it brought.
//
// The caller is an application that IS the server — it starts, tells the companion where to reach
// it, and is expected to detach on the way out. Nothing is written to config: a daemon that
// restarts has forgotten, and the application attaches again when it notices.
func (c *Client) AttachMCP(name, url string, headers map[string]string) ([]string, error) {
	resp, err := c.exchange(Request{Method: "mcp-attach", Name: name, URL: url, Headers: headers})
	if err != nil {
		return nil, err
	}
	return resp.Tools, nil
}

// DetachMCP removes a server this caller attached. An application reconnecting after its own crash
// sends this first: the name is locked while a dead registration holds it.
//
// The bool is whether there was one, and "already clean" is a normal answer rather than a failure.
// An error means the daemon refused — including when the name belongs to a server the operator
// declared in config, which this door does not own.
func (c *Client) DetachMCP(name string) (bool, error) {
	resp, err := c.exchange(Request{Method: "mcp-detach", Name: name})
	if err != nil {
		return false, err
	}
	return resp.Removed, nil
}

// Roster asks who is out there: this machine's companions as measurements (with the session id a
// transcript subscription needs), other machines' as signed sightings (with their age). Discovery
// only — command and conversation go through each row's own socket, which is the boundary the
// door keeps (see roster.go).
func (c *Client) Roster() ([]RosterRow, error) {
	resp, err := c.exchange(Request{Method: "roster"})
	if err != nil {
		return nil, err
	}
	return resp.Roster, nil
}

// Settings reads the editable settings: each key with its effective value, where that value came
// from, and when a change to it takes effect.
func (c *Client) Settings() ([]ConfigItem, error) {
	resp, err := c.exchange(Request{Method: "config-get"})
	if err != nil {
		return nil, err
	}
	return resp.Config, nil
}

// SetSetting changes one key and answers with the key as it now stands. An empty value clears it;
// an empty tier writes it back where the value was read from.
func (c *Client) SetSetting(key, value, tier string) (ConfigItem, error) {
	resp, err := c.exchange(Request{Method: "config-set", Name: key, Text: value, Tier: tier})
	if err != nil {
		return ConfigItem{}, err
	}
	if len(resp.Config) == 0 {
		return ConfigItem{}, fmt.Errorf("the daemon changed %q and said nothing about it", key)
	}
	return resp.Config[0], nil
}

// Profiles lists the backends a profile-shaped setting may point at.
func (c *Client) Profiles() ([]ProfileChoice, error) {
	resp, err := c.exchange(Request{Method: "profiles"})
	if err != nil {
		return nil, err
	}
	return resp.Profiles, nil
}

// Sessions lists the companion's conversations, newest activity first — the bottom dock's
// session picker reads this.
func (c *Client) Sessions() ([]SessionRow, error) {
	resp, err := c.exchange(Request{Method: "sessions"})
	if err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

// NewSession opens a fresh conversation on the companion and moves it there, answering with the
// new id. The one way to get a new conversation: resume refuses invented ids on purpose.
func (c *Client) NewSession() (string, error) {
	resp, err := c.exchange(Request{Method: "session-new"})
	if err != nil {
		return "", err
	}
	return resp.Session, nil
}

// Cron reads the standing schedule: what runs next, and what never will and why.
func (c *Client) Cron() ([]CronRow, error) {
	resp, err := c.exchange(Request{Method: "cron"})
	if err != nil {
		return nil, err
	}
	return resp.Cron, nil
}

// KillJob stops a background command on the companion. The bool is whether the id named one —
// pressing ✕ twice reads "already gone", never "failure".
func (c *Client) KillJob(id string) (bool, error) {
	resp, err := c.exchange(Request{Method: "job-kill", Name: id})
	if err != nil {
		return false, err
	}
	return resp.Removed, nil
}

func (c *Client) Status(sid string) (Status, error) {
	resp, err := c.exchange(Request{Method: "status", Session: sid})
	if err != nil {
		return Status{}, err
	}
	return Status{Asking: resp.Waiting, Doing: resp.Doing, Permission: resp.Permission,
		Backend: resp.Backend, User: resp.User}, nil
}

// Rewind, Compact, SetModel and SetPermission change how the daemon runs, which is why they cross:
// done locally by a viewer they would change a copy nobody is using, and the two log-rewriting ones
// would do it underneath the process that owns the log.
func (c *Client) Rewind(_ context.Context, sid session.SessionID, n int) (int64, error) {
	// The new boundary is not carried back: the caller re-reads the log, which is where the answer
	// lives, and a number returned by one process about another's file is stale on arrival.
	return 0, c.call(Request{Method: "rewind", Session: string(sid), N: n})
}

func (c *Client) Compact(_ context.Context, cmd command.Compact) error {
	return c.call(Request{Method: "compact", Session: string(cmd.SessionID)})
}

func (c *Client) SetModel(sid session.SessionID, modelID string) error {
	return c.call(Request{Method: "set-model", Session: string(sid), Name: modelID})
}

// UseBackend switches which backend this daemon's requests go to, by base URL. Not persisted —
// see App.UseBackend for why the plugin that owns the address gets the next word.
func (c *Client) UseBackend(base string) error {
	return c.call(Request{Method: "use-backend", Name: base})
}

func (c *Client) SetPermission(p string) error {
	return c.call(Request{Method: "set-permission", Name: p})
}

// ReloadCron tells the daemon its scheduled work has changed on disk.
//
// For an editor in another process — the console, an attached terminal. The schedule tool runs
// inside the daemon and calls the engine directly instead.
func (c *Client) ReloadCron() error { return c.call(Request{Method: "reload-cron"}) }

// Shell runs a command in the daemon's workspace, as the daemon's user, and returns what it wrote
// and what it exited with.
//
// A viewer running it locally would run it on whichever machine is looking, in whatever directory
// that process started in. That is a different question with the same spelling.
func (c *Client) Shell(cmd string) (out string, exit int, err error) {
	resp, err := c.exchange(Request{Method: "shell", Text: cmd})
	if err != nil {
		return "", -1, err
	}
	if resp.Exit == nil {
		return resp.Out, -1, nil
	}
	return resp.Out, *resp.Exit, nil
}

// ReadOnlyTool runs one of the companion's read-only tools in ITS workspace and returns what the
// tool produced, as the tool wrote it.
//
// The text is the tool's own result — line-numbered file contents, a directory listing, whatever
// that tool produces for the agent. Not reshaped on the way through: two renderings of one thing
// is how a console comes to show something the agent never saw.
func (c *Client) ReadOnlyTool(name string, args json.RawMessage) (string, error) {
	resp, err := c.exchange(Request{Method: "tool", Name: name, Args: args})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// WriteTool asks the companion to change one of its own files, and to write down that a person did.
// PatchFile applies a unified diff to one file in the companion's workspace. A refusal here is
// usually the file having changed since it was read, which is the whole reason to send a patch.
func (c *Client) PatchFile(path, patch string, ask bool) error {
	_, err := c.exchange(Request{Method: "edit-file", Name: "patch", Text: path, Answer: patch, Ask: ask})
	return err
}

func (c *Client) WriteTool(name string, args json.RawMessage, ask bool) (string, error) {
	resp, err := c.exchange(Request{Method: "edit-file", Name: name, Args: args, Ask: ask})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// Git asks what git makes of the companion's workspace: its branch, and what is not committed.
func (c *Client) Git() (string, error) {
	resp, err := c.exchange(Request{Method: "git"})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// Meet asks the companion for one contribution to a meeting: what it has to add, or a pass.
//
// topic is the question, transcript is everything said so far, and closing asks the other
// question — what this participant will DO about it — which is what a meeting is for.
// Join is this companion getting ready for a meeting: it reads its own workspace and answers with
// what it brings. The session it opens is the one its turns then happen in.
func (c *Client) Join(meeting, topic string) (ready, room string, err error) {
	resp, err := c.exchange(Request{Method: "meet-join", Meeting: meeting, Name: topic})
	if err != nil {
		return "", "", err
	}
	return resp.Out, resp.Session, nil
}

// Contribution is one participant's turn in a meeting: what it said, whether that was a pass, and
// the conversation it said it in.
//
// One value rather than three returns because they are one answer, and because the third was not
// there at all — the room is opened inside the daemon, so a console showing the meeting had no way
// to offer what the participant read and thought on its way to the sentence.
type Contribution struct {
	Said string
	Pass bool
	Room string
}

func (c *Client) Meet(meeting, topic, transcript string, closing bool) (Contribution, error) {
	which := ""
	if closing {
		which = "closing"
	}
	resp, err := c.exchange(Request{Method: "meet", Meeting: meeting, Name: topic, Text: transcript,
		Decision: which})
	if err != nil {
		return Contribution{}, err
	}
	return Contribution{Said: resp.Out, Pass: resp.Exit != nil && *resp.Exit == 1,
		Room: resp.Session}, nil
}

// passFlag carries "this was a pass" in the one field a Response has for a small number. Named
// rather than written inline at the call site, because 0 and 1 mean nothing to a reader of that
// line and "pass" means everything.
func passFlag(pass bool) *int {
	n := 0
	if pass {
		n = 1
	}
	return &n
}

// LookOver asks the companion's model what it makes of a file being edited. Nothing is saved.
func (c *Client) LookOver(path, text string) (string, error) {
	resp, err := c.exchange(Request{Method: "look-over", Name: path, Text: text})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// CompleteCode asks the daemon for inline completion text at the cursor. path in Name, and the two
// sides of the cursor in Args (raw JSON, the same way the tool methods carry their arguments) — a
// completion needs both, and Text alone carries one. The relay for the console editor's ghost text,
// and the endpoint a future IDE extension would call for the same thing.
//
// Three values because empty text is not one outcome: the reason says which empty it is, and a
// completion that FAILED comes back as an error instead of as silence.
func (c *Client) CompleteCode(path, prefix, suffix string) (string, app.CompleteReason, error) {
	args, _ := json.Marshal(completeArgs{Prefix: prefix, Suffix: suffix})
	resp, err := c.exchange(Request{Method: "complete", Name: path, Args: args})
	if err != nil {
		return "", "", err
	}
	return resp.Out, app.CompleteReason(resp.Reason), nil
}

// SetOpenFile tells the daemon which file the editor has open and its unsaved buffer, so the agent's
// next turn sees it (app.SetOpenFile). Fire-and-forget from the caller's view: an empty buffer
// clears it. Errors are the transport's, not the model's — nothing is generated here.
func (c *Client) SetOpenFile(path, text string) error {
	_, err := c.exchange(Request{Method: "open-file", Name: path, Text: text})
	return err
}

// Suggest asks the daemon for the composer's ghost text: how the person is likely to finish the
// instruction whose prefix is given, on the composer profile. The relay for the composer, and the
// endpoint an IDE extension would call for the same.
func (c *Client) Suggest(prefix string) (string, error) {
	resp, err := c.exchange(Request{Method: "suggest", Text: prefix})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// completeArgs is the cursor-sides payload for a "complete" request.
type completeArgs struct {
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

// OpenPR pushes this companion's branch and opens a pull request, answering with its URL.
func (c *Client) OpenPR(title, body string) (string, error) {
	resp, err := c.exchange(Request{Method: "git-pr", Name: title, Text: body})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// PRFacts is what a pull request from this companion's branch would carry, as JSON.
//
// JSON over a string field rather than a shape in this package: the protocol carries text, the
// console decodes it, and a struct here would be a third copy of the same fields to keep in step.
func (c *Client) PRFacts() (string, error) {
	resp, err := c.exchange(Request{Method: "pr-facts"})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// DraftPR asks the companion's model for that request's title and body. Nothing is opened.
func (c *Client) DraftPR(rules string) (string, error) {
	resp, err := c.exchange(Request{Method: "pr-msg", Text: rules})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// DraftCommit asks the companion's model to describe what is staged. Nothing is committed.
func (c *Client) DraftCommit(rules string) (string, error) {
	resp, err := c.exchange(Request{Method: "git-msg", Text: rules})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// FileDo makes, moves or removes a file in the companion's workspace: new-file, new-dir, rename,
// delete. The paths travel as arguments and never as part of a command line.
func (c *Client) FileDo(what, path, to string, ask bool) error {
	_, err := c.exchange(Request{Method: "file-do", Name: what, Text: path, Answer: to, Ask: ask})
	return err
}

// GitDiff is what changed in one file: staged, unstaged, or an untracked file against nothing.
func (c *Client) GitDiff(path string, which string) (string, error) {
	resp, err := c.exchange(Request{Method: "git-diff", Text: path, Decision: which})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// GitDo runs one of the four git commands in the companion's workspace: stage, unstage, discard,
// commit. The path travels as an argument and never as part of a command line.
func (c *Client) GitDo(what, path, message string, ask bool) (string, error) {
	resp, err := c.exchange(Request{Method: "git-do", Name: what, Text: path, Answer: message, Ask: ask})
	if err != nil {
		return "", err
	}
	return resp.Out, nil
}

// Shutdown asks the daemon to stop.
//
// The daemon answers before it acts, so a nil error means it accepted — not that it has finished
// unwinding. What follows on its side is the ordinary teardown: in-flight requests finish, the
// socket goes, the published record is removed, and the scheduled work stops with the process that
// was running it. That last part is why removing a companion is one call and not two: a record
// deleted while its daemon kept running would leave work happening that nothing on screen could
// account for.
func (c *Client) Shutdown() error { return c.call(Request{Method: "shutdown"}) }

// Restart asks the daemon to relaunch onto the binary now on disk — the same drain as Shutdown, then
// a re-exec into the new build instead of an exit. Used to finish a self-update. The reply arrives
// before the drain; the socket then goes briefly as the successor rebinds.
func (c *Client) Restart() error { return c.call(Request{Method: "restart"}) }

// Update asks a same-machine daemon to update itself to the latest release and restart onto it. It
// returns the daemon's one-line account (what it did, or "already up to date"), or an error when the
// update failed and rolled back. It blocks for the download; on a successful update the connection
// then drops as the daemon drains to restart, which is not an error.
func (c *Client) Update() (string, error) {
	resp, err := c.exchange(Request{Method: "update"})
	if err != nil {
		return "", err
	}
	if !resp.OK {
		return "", errors.New(resp.Err)
	}
	return resp.Out, nil
}

func (c *Client) call(r Request) error {
	_, err := c.exchange(r)
	return err
}

// exchange sends one request and returns the whole reply — which call throws away and Status does
// not.
func (c *Client) exchange(r Request) (Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Covers the write AND the read: a peer that stops reading blocks the write once the socket
	// buffer fills, which is the same hang one step earlier. A pipe has no deadline to set; the
	// caller bounds it by killing the process at the other end.
	if c.deadline > 0 && c.nc != nil {
		if err := c.nc.SetDeadline(time.Now().Add(c.deadline)); err != nil {
			return Response{}, fmt.Errorf("daemon: %w", err)
		}
		defer c.nc.SetDeadline(time.Time{})
	}
	if err := c.enc.Encode(r); err != nil {
		return Response{}, fmt.Errorf("daemon: send: %w", err)
	}
	if !c.sc.Scan() {
		if err := c.sc.Err(); err != nil {
			return Response{}, fmt.Errorf("daemon: %w", err)
		}
		return Response{}, io.ErrUnexpectedEOF
	}
	var resp Response
	if err := json.Unmarshal(c.sc.Bytes(), &resp); err != nil {
		return Response{}, fmt.Errorf("daemon: malformed reply: %w", err)
	}
	// Typed, so a caller can tell "it answered, and said no" from "it never answered". Every error
	// above this line is the second kind and every one below is the first; collapsing them is how a
	// companion that refused perfectly clearly gets reported as an unreachable machine.
	if !resp.OK {
		why := resp.Err
		if why == "" {
			why = "the daemon refused without saying why"
		}
		return resp, Refused{Why: why}
	}
	return resp, nil
}

func (c *Client) Submit(_ context.Context, cmd command.SubmitPrompt) error {
	return c.call(Request{Method: "submit", Session: string(cmd.SessionID), Text: textOf(cmd.Parts), Refs: cmd.Refs})
}

func (c *Client) Steer(_ context.Context, cmd command.SubmitPrompt) error {
	return c.call(Request{Method: "steer", Session: string(cmd.SessionID), Text: textOf(cmd.Parts), Refs: cmd.Refs})
}

func (c *Client) Interrupt(_ context.Context, cmd command.Interrupt) error {
	return c.call(Request{Method: "interrupt", Session: string(cmd.SessionID)})
}

// Resume asks the daemon to continue a different conversation of its own.
func (c *Client) Resume(sid session.SessionID) error {
	return c.call(Request{Method: "resume", Session: string(sid)})
}

func (c *Client) RespondPermission(_ context.Context, cmd command.RespondPermission) error {
	return c.call(Request{Method: "permission", Session: string(cmd.SessionID),
		CallID: cmd.CallID, Decision: cmd.Decision})
}

func (c *Client) RespondQuestion(_ context.Context, cmd command.RespondQuestion) error {
	return c.call(Request{Method: "answer", Session: string(cmd.SessionID),
		CallID: cmd.CallID, Answer: cmd.Answer})
}

func textOf(parts []session.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Kind == session.PartText {
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

// shortHash keeps the socket name unique per absolute path while staying inside the ~104-byte limit
// a unix socket path has on macOS — a full path in the name would blow past it.
func shortHash(s string) string {
	var h uint64 = 1469598103934665603
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var out [8]byte
	for i := range out {
		out[i] = digits[h%36]
		h /= 36
	}
	return string(out[:])
}

// The daemon publishes what it is, next to its socket.
//
// An attaching UI has to open the SAME session, and guessing — "the newest in this workspace" — is
// wrong exactly when it matters: a daemon up for days sits behind whatever session someone opened
// since. And once there is more than one daemon, a person looking at a directory of sockets can
// read a base name and a hash and nothing else: not which tree it is, not whether anyone is home.
//
// So the daemon says, in a file that lives and dies with the socket.

// Info is what a daemon publishes about itself.
type Info struct {
	Socket  string `json:"socket"`
	Workdir string `json:"workdir"` // the FULL path; the socket name carries only a base name and a hash
	Session string `json:"session"`
	// Name and Role are what a TEAM is addressed by, declared in the workspace's own
	// .magi/config.toml and published here so everything that lists companions reads one source.
	//
	// Without them a companion is identified by the base name of a directory, which answers "which
	// one is this" and not "which one do I want" — and "which one do I want" is the question
	// somebody coordinating work actually has. A directory called `ds` is not a design specialist
	// until it says so.
	//
	// Both optional. A companion with neither is exactly what companions were before: a workspace.
	Name string `json:"name,omitempty"`
	Role string `json:"role,omitempty"`
	// Team is the group of companions doing related work, and Hub marks the one that answers for
	// it. Addressing, not topology: nothing routes through a hub and nothing is hidden behind one.
	Team string `json:"team,omitempty"`
	Hub  bool   `json:"hub,omitempty"`
	// Can is how many things this companion advertises being able to do, counted by the process
	// that knows: its own skills and its own tool servers. Published rather than worked out by
	// whoever is reading, because a reader on another machine cannot see either.
	//
	// Counted once, when the daemon starts. A skill written this afternoon does not raise it until
	// the companion is next restarted, and that is the right trade: the number decides a tie in a
	// hub election, and an election every companion has to agree on is better served by a value
	// that changes on the scale of days than by one that changes while they are comparing it.
	Can int `json:"can,omitempty"`
	// Does names them, capped. See cluster.Member.Does: names travel, descriptions are fetched.
	Does []string `json:"does,omitempty"`
	// Waiting is how many pieces of handed-over work this companion has taken and not started.
	//
	// The one number here that changes on the scale of SECONDS, where everything around it changes
	// on the scale of days. Read from the file it is current; read from a sighting a minute old it
	// is a minute old, and a reader choosing between companions should treat it as what it is —
	// advisory. The authority is the refusal: a companion that is full says so when asked, and
	// that answer is never stale.
	Waiting int `json:"waiting,omitempty"`
	// Handling is whether a piece of handed-over work is running right now.
	//
	// Separate from Waiting because they are separate facts, and separate from the state a
	// dashboard derives because that is read off the session a person attaches to — and handed-over
	// work runs in conversations of its own. Without this a companion in the middle of somebody
	// else's request is indistinguishable from one with nothing to do.
	//
	// Wrong in one direction only, by construction: a daemon that loses track of a piece clears
	// this rather than leaving it set. Saying "free" when busy costs an asker a wait it did not
	// expect; saying "busy" forever would push every asker away from a companion that is fine.
	Handling bool   `json:"handling,omitempty"`
	PID      int    `json:"pid"`
	Started  string `json:"started"` // RFC3339
	// Host and Addr say WHERE this is running. Everything in one config directory is on one
	// machine, so on a laptop they are the same for every entry and read as noise — until you are
	// looking at three browser tabs forwarded from three hosts over ssh, which is the arrangement
	// this whole split exists for. Then the only thing telling them apart is this.
	Host string `json:"host,omitempty"`
	Addr string `json:"addr,omitempty"`
	// State is what this companion is doing, as the daemon that IS it says so: "waiting" on a
	// person, "working" on a turn, or "idle".
	//
	// It is here because it has to TRAVEL. A console dials the companions in its own directory and
	// works their state out from the answer; nothing dials the ones on other machines, so a roster
	// used to show them as "elsewhere" — a place, in a column about what things are doing. Gossip
	// already carries a sighting every round, which is exactly where Cassandra puts a node's
	// application state, and the record is signed, so a relay can carry this and cannot invent it.
	//
	// It is a SIGHTING, never a live reading: it was true when the record was written, and how long
	// ago that was travels beside it. A screen that shows one without the other is claiming to know
	// something it cannot.
	State string `json:"state,omitempty"`
	// Account is the OS user this daemon runs as, and with Host it names the INSTANCE a companion
	// belongs to.
	//
	// The pair is the unit everything else here is scoped to: two accounts on one machine read two
	// config directories, enforce two policies, keep two session stores, and neither can write the
	// other's files. A roster that said only the host would put two people's companions on one line
	// of provenance and imply they are interchangeable.
	Account string `json:"account,omitempty"`
	// Version is the build this daemon is running, which is not always the build the console
	// reading it is. Upgrading replaces the binary and leaves every daemon already running on the
	// old one until somebody restarts it — so a console showing only its own version answers a
	// question nobody asked, and the one they did ask ("why does this companion not have the thing
	// I just shipped") has no answer on the screen at all.
	Version string `json:"version,omitempty"`
	// Live is filled in by List, not by the daemon: a file cannot say whether the process that
	// wrote it is still there. Only a dial can.
	Live bool `json:"-"`
	// Asking is what the daemon is blocked on, when it is. Also from List, and for the same
	// reason: it is in the daemon's memory, so the dial that proves it alive asks while it is
	// there. nil when nothing is pending or the daemon is not answering.
	Asking *Waiting `json:"-"`
	// Doing is what a still-running tool last reported, and comes back on the same dial as Asking.
	// The pair is the whole of "what is happening in there right now": one says it has stopped and
	// needs a person, the other says it has not.
	Doing string `json:"-"`
	// Permission is the approval mode it is on now. Not in the file either: the mode changes at
	// runtime, so a record written at startup would be the mode it USED to be on.
	Permission string `json:"-"`
	// Backend is the base URL its LLM requests go to now, and is here for exactly the same reason:
	// the console can redirect it mid-run, so a record written at startup would name the endpoint
	// it USED to talk to.
	Backend string `json:"-"`
	// User is what this companion calls the person, when a plugin has renamed them. Not in the
	// file, for the same reason: a plugin can set it on any turn.
	User string `json:"-"`
}

// SessionFile is where a daemon records what it is driving.
func SessionFile(socketPath string) string { return socketPath + ".session" }

// Publish records the daemon and returns a function that removes the record.
// recordMu serialises read-modify-write on a daemon's own record.
//
// Three writers reach it from three goroutines of the same process: the queue's depth (Announce,
// from the drain), the session (Moved, from a serve goroutine), and the initial write. Each reads
// the whole record, changes one field and writes it back, so two of them overlapping loses one of
// the changes — and the losing field is whichever finished first, silently.
//
// Package-level rather than per-daemon: one process serves one socket, the sections it guards are
// microseconds long, and a test running several daemons at once loses nothing by taking turns.
var recordMu sync.Mutex

// writeRecord replaces a daemon's record in one step.
//
// Written to a temporary file and renamed, because the readers are POLLING it: os.WriteFile
// truncates and then writes, so a reader that lands in between gets an empty or half-written file
// and reports the companion as unreadable — a console blinking a daemon out of existence every
// time its queue depth changed. Rename is atomic on both platforms this ships to.
func writeRecord(socketPath string, in Info) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	f := SessionFile(socketPath)
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, f); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func Publish(socketPath, workdir, sid string, id Identity) (func(), error) {
	// Host(), not os.Hostname(): one spelling of this machine's name enters the system here and
	// nothing downstream has to normalise. A record written with the raw name and a member built
	// from the lowercased one compare unequal, and an election over the two decides that the
	// companion it just elected is not on any row it can see.
	host := Host()
	recordMu.Lock()
	err := writeRecord(socketPath, Info{
		Socket: socketPath, Workdir: workdir, Session: sid,
		Name: id.Name, Role: id.Role, Team: id.Team, Hub: id.Hub, Can: id.Can, Does: id.Does,
		PID: os.Getpid(), Started: time.Now().UTC().Format(time.RFC3339),
		Host: host, Addr: primaryAddr(), Account: account(), Version: version.Version,
	})
	recordMu.Unlock()
	if err != nil {
		return func() {}, fmt.Errorf("daemon: publishing: %w", err)
	}
	return func() { os.Remove(SessionFile(socketPath)) }, nil
}

// Announce updates the one part of a published record that changes while the daemon runs: how much
// work is waiting.
//
// Read-modify-write on the daemon's own record, which only it writes. Not a second publishing path:
// everything else in there is fixed when the daemon starts, and threading a number that changes
// every few seconds through the call that writes the fixed parts would make every caller of it
// pass something it does not know.
//
// A missing record is not an error. The daemon is either not published yet or on its way out, and
// in both cases the number describes nothing anybody can act on.
func Announce(socketPath string, waiting int, handling bool) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	in, err := Published(socketPath)
	if err != nil {
		return nil
	}
	if in.Waiting == waiting && in.Handling == handling {
		return nil // nothing changed; do not rewrite a file readers are polling
	}
	in.Waiting, in.Handling = waiting, handling
	return writeRecord(socketPath, in)
}

// NoteState records what this companion is doing, in its own record.
//
// Read-modify-write on the daemon's own file, exactly like Announce, and written only when it
// CHANGED: readers poll this file, and rewriting it every few seconds to say the same thing would
// wake every one of them for nothing.
func NoteState(socketPath, state string) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	in, err := Published(socketPath)
	if err != nil {
		return nil
	}
	if in.State == state {
		return nil
	}
	in.State = state
	return writeRecord(socketPath, in)
}

// Moved rewrites which conversation the published record names.
//
// Read-modify-write on the daemon's own record, exactly like Announce: everything else in there is
// fixed at startup, and the session is now the second thing that is not. It is the record, and not
// a message, that every reader believes — the fleet row, the console's withClient, an attaching
// terminal — so this write IS the move as far as anything outside the process is concerned.
func Moved(socketPath string, sid session.SessionID) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	in, err := Published(socketPath)
	if err != nil {
		return err
	}
	if in.Session == string(sid) {
		return nil // do not rewrite a file readers are polling to say what it already says
	}
	in.Session = string(sid)
	return writeRecord(socketPath, in)
}

// Identity is what this companion calls itself and what it is for.
//
// Passed in rather than read here: this package publishes records and does not know where a
// workspace keeps its config, and a second config reader is a second place for the two to disagree
// about which file wins.
type Identity struct {
	Name string   // "design", "api" — how somebody addresses it
	Role string   // one line: what it is for, in the words of whoever set it up
	Team string   // the group of companions doing related work, if any
	Hub  bool     // whether this one answers for its team
	Can  int      // how many things it can do — skills plus tool servers; a tie-break in an election
	Does []string // and what they are called, capped at cluster.MaxDoes
}

// account is the OS user this process runs as, or empty when it cannot be read — empty rather than
// a guess, since the whole value of the name is that it matches what somebody sees in a shell.
func account() string {
	u, err := osuser.Current()
	if err != nil {
		return ""
	}
	return u.Username
}

// primaryAddr is the address another machine would reach this one at, best effort.
//
// The first routable IPv4 on an interface that is up. Not a guarantee — a host with several NICs
// has several answers and this picks one — which is why it travels beside the hostname rather than
// instead of it. Empty when there is nothing routable, which is the honest answer for a laptop on
// no network.
func primaryAddr() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, in := range ifaces {
		if in.Flags&net.FlagUp == 0 || in.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, aerr := in.Addrs()
		if aerr != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			if v4 := ipn.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

// Published reads what a daemon published.
func Published(socketPath string) (Info, error) {
	b, err := os.ReadFile(SessionFile(socketPath))
	if err != nil {
		return Info{}, fmt.Errorf("daemon: nothing published at %s — is a daemon running there? %w",
			SessionFile(socketPath), err)
	}
	var in Info
	if err := json.Unmarshal(b, &in); err != nil {
		return Info{}, fmt.Errorf("daemon: the record at %s is unreadable: %w", SessionFile(socketPath), err)
	}
	if strings.TrimSpace(in.Session) == "" {
		return Info{}, fmt.Errorf("daemon: the record at %s names no session", SessionFile(socketPath))
	}
	return in, nil
}

// PublishedSession is Published narrowed to the session id, for callers that want only that.
func PublishedSession(socketPath string) (string, error) {
	in, err := Published(socketPath)
	return in.Session, err
}

// probeTimeout bounds each half of a liveness probe: how long to wait for a connect, and how long
// to wait for the answer. Generous for a local unix socket answering from memory (a healthy daemon
// replies in well under a millisecond) and short enough that a listing stays usable when one
// process is wedged — which is exactly when somebody runs it.
const probeTimeout = 700 * time.Millisecond

// Find resolves one published socket under configDir, without probing anything.
//
// Separate from List because the two questions are different. A dashboard asks "what is out there,
// and which of them are alive?", which costs a dial to each. Delivering ONE steer asks "is this
// path one somebody published?", and answering that by probing every daemon on the machine makes a
// keystroke wait on an unrelated wedged process — the cost lands on the one action where a person
// is watching the cursor.
//
// Matched against the published set rather than parsed from the parameter: the path arrives from a
// page, and a path from a page must not become a path this process dials.
func Find(configDir, socket string) (Info, error) {
	socks, err := filepath.Glob(filepath.Join(configDir, "daemon-*.sock"))
	if err != nil {
		return Info{}, fmt.Errorf("daemon: listing: %w", err)
	}
	for _, s := range socks {
		if s != socket {
			continue
		}
		in, perr := Published(s)
		if perr != nil {
			return Info{}, perr
		}
		in.Socket = s
		return in, nil
	}
	return Info{}, fmt.Errorf("no daemon at %s — it is not one of the %d published under %s",
		socket, len(socks), configDir)
}

// List returns every daemon that has published under configDir, newest first.
//
// Each is DIALLED, because the file cannot say whether anybody is home: a daemon killed with
// SIGKILL leaves both the socket and the record behind, and a list that showed those as running
// would send a viewer to a dead endpoint. A dead one is still listed — knowing a workspace has a
// corpse is more useful than the entry silently missing — but it is marked.
func List(configDir string) ([]Info, error) {
	socks, err := filepath.Glob(filepath.Join(configDir, "daemon-*.sock"))
	if err != nil {
		return nil, fmt.Errorf("daemon: listing: %w", err)
	}
	out := make([]Info, len(socks))
	for i, s := range socks {
		in, err := Published(s)
		if err != nil {
			// A socket with no readable record: still worth showing, because something is there.
			in = Info{Socket: s, Workdir: "(unknown — no record)"}
		}
		in.Socket = s
		out[i] = in
	}
	out = Probe(out)
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out, nil
}

// Probe fills in the half of each Info only a dial can answer: liveness, and what the daemon is
// blocked on or doing right now. Split out of List so a listing whose RECORDS came another way —
// the web console's fleet path reads them off the roster door — pays the same dial and reads the
// same truth, instead of trusting a snapshot for the one thing snapshots cannot say.
func Probe(out []Info) []Info {
	// Probed in parallel. Serially, a listing costs the SUM of every daemon's latency and one
	// wedged process delays every entry after it — and the reason to run a listing is usually that
	// something is wrong. In parallel it costs the slowest one, which probeTimeout bounds.
	var wg sync.WaitGroup
	for i := range out {
		s, sid := out[i].Socket, out[i].Session
		out[i].Live, out[i].Asking, out[i].Doing = false, nil, ""
		wg.Add(1)
		go func(i int, s string, sid string) {
			defer wg.Done()
			// The dial that proves it alive also asks what it is waiting for: two questions, one
			// connection, and the second is free at the point the first is being answered.
			cl, derr := dialProbe(s, probeTimeout, probeTimeout)
			if derr != nil {
				return
			}
			defer cl.Close()
			out[i].Live = true
			if sid == "" {
				return
			}
			if st, serr := cl.Status(sid); serr == nil {
				out[i].Asking, out[i].Doing = st.Asking, st.Doing
				out[i].Permission, out[i].User = st.Permission, st.User
				out[i].Backend = st.Backend
			}
			// A daemon too old to know the method answers with an error naming what it does
			// accept. That is a version skew, not a fault: it is alive, and everything else about
			// it is still true. A TIMEOUT lands here too, and means the same thing for the entry:
			// alive, and not saying.
		}(i, s, sid)
	}
	wg.Wait()
	return out
}
