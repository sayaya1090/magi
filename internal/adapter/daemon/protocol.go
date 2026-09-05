// The wire: what a request and a reply are made of.
package daemon

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/report"
	"github.com/sayaya1090/magi/internal/core/session"
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
	// owner names the conversation the tools belong to; empty is the whole daemon, which is what
	// every caller sent before this field existed and what an older client still sends.
	AttachToolServer(ctx context.Context, owner, name, url string, headers map[string]string) ([]string, error)
	// DetachToolServer removes one by name: false when there was none to remove, an error when
	// there was one this caller may not remove (a server the operator declared in config).
	DetachToolServer(owner, name string) (bool, error)
}

// ConversationOpener opens a conversation WITHOUT moving the companion onto it.
//
// Its own optional interface rather than a second argument on ConversationKeeper: that one is what
// every fake in every package must satisfy, and a control surface which grows is not a reason to
// touch four test doubles (the same reasoning `reload-cron` records).
type ConversationOpener interface {
	// NewSessionKeeping opens a conversation and leaves the companion where it is.
	NewSessionKeeping(ctx context.Context) (session.SessionID, error)
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

// ModelNamer answers which model a conversation is on right now.
//
// Beside Permission and Backend in the status answer, because it is the same kind of fact and the
// same reader wants all three: a screen that offers to change the model has to show what it is
// changing from. It rode nothing before — the fleet listing filled it by opening the workspace's
// session metadata, which the LIGHT listing refuses to do, so a console reading the light list
// drew an empty model field and could not say what the companion was on.
type ModelNamer interface {
	ModelOf(sid session.SessionID) string
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

// CouncilTeller answers whether a working turn here must end by declaring to a council.
//
// Separate from Controller because it is a fact, not a control: nothing outside sets it, and an
// engine that has no council still answers — with false.
type CouncilTeller interface {
	HasCouncil() bool
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
	MeetingJoin(ctx context.Context, meeting, topic string, room []Seat) (ready, roomID string, err error)
	MeetingTurn(ctx context.Context, meeting, topic, transcript, minutes string, room []Seat,
		closing bool) (Contribution, error)
}

// Seat is one participant of a meeting, as the companion that convened it knows them.
//
// Name is what utterances are attributed to. Role is what that companion publishes itself as being
// for, and Does is what it advertises being able to do — the sample-and-count string that
// fleet.abilities renders, not a list, because what the reader needs is "roughly what is this one
// good for" and the full list is a wall.
//
// A person has a name and neither of the others: they are in the room, and there is nothing they
// advertise. Told apart by Role and Does both being empty is NOT safe — a companion that declared
// neither looks the same — so the prompt says which one is the person by position, not by shape.
type Seat struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	Does string `json:"does,omitempty"`
	// Person marks the human who called the meeting. A field rather than an inference for the
	// reason above: absence of a role is not evidence of being a person.
	Person bool `json:"person,omitempty"`
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
	// Profile marks a key whose value must name an [llm.profiles.*], so a screen offers the ones
	// that exist instead of a text box.
	//
	// Carried rather than left to the client to know. Every client that hardcodes which keys are
	// profile-shaped is a copy of a list that lives here — the web console has one today, and the
	// IDE would have grown a second the moment it drew these fields. The set the value may take
	// comes from the `profiles` method; this says which fields should ask for it.
	Profile bool `json:"profile,omitempty"`
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

// ChildLister is the engine half of the `children` door: the subagent conversations a session
// spawned, read out of the log.
//
// Separate from ConversationKeeper because it answers a different question, and separate from
// JobRunner for a sharper reason: that one keeps a live register of children running now, which
// a session log cannot know ("a session log does not know it is over"), and this one knows the
// past, which the register cannot ("gone when the daemon restarts"). Neither is the other's
// cache. A client that wants both asks both, and the shapes say which is which.
//
// Local socket only, like sessions and transcript — the clients this exists for hold a socket
// and nothing else. The web console reads the same fact from its own store because it has the
// files; a plugin in a JVM has no store, and re-deriving "what is a child session" in a second
// language would be a second place that decides it.
type ChildLister interface {
	// ChildrenOf lists the subagent sessions spawned by parent, newest activity first. An id
	// with no children answers an empty list, not an error — "none" is a real answer here.
	ChildrenOf(ctx context.Context, parent string) ([]session.SessionMeta, error)
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
	// Keep asks session-new to open a conversation WITHOUT moving the companion onto it.
	//
	// Opening one has always meant "and go there" — the console's button means both. A client that
	// serves several conversations from one daemon means only the first: the PowerPoint helper gives
	// each open deck its own, and moving on each one writes "the companion left this conversation"
	// into the deck that was working a second ago (measured 2026-09-05). Omitted keeps the old
	// meaning, so every existing caller and every older client is unchanged.
	Keep   bool   `json:"keep,omitempty"`
	Text   string `json:"text,omitempty"`
	CallID string `json:"callId,omitempty"`
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
	// Room is who is in this meeting and what each of them is for.
	//
	// On meet-join it is what a participant reads while getting ready. On meet it is what the
	// MINUTES need: the record says who was in the room, and that is a fact the convener holds —
	// a model asked to remember it from its preparation turn would eventually write down a room
	// that is not the one it is in.
	//
	// Safe for an older daemon to drop. encoding/json ignores a field it does not declare, and a
	// participant that never learns the roster prepares exactly as it did before this existed —
	// the prompt omits a section. That is why this is a field rather than a capability: there is
	// no screen to draw or withhold on the answer, and nothing to tell an old build apart from.
	Room []Seat `json:"room,omitempty"`
	// Minutes is the meeting's document as it stands, carried on meet so the speaker reads what has
	// already been agreed before adding to it. What comes back on Contribution is its revision.
	//
	// The whole text each way. It is written in bullets and stays small; a diff would save little
	// and cost the one thing that matters here — a patch that does not apply corrupts the record
	// without saying so.
	Minutes string `json:"minutes,omitempty"`
	// Schedule and Enabled are the cron edit doors' own two fields. Enabled is a pointer because
	// the switch is three-valued on the wire: absent must mean "leave it alone", or an edit that
	// only changes the words would silently switch a job back on.
	Schedule string `json:"schedule,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
	// Command and Timeout are the cron edit doors' fields for a job that runs instead of asking.
	Command string `json:"command,omitempty"`
	Timeout string `json:"timeout,omitempty"`
	N       int    `json:"n,omitempty"`
	// URL and Headers are the attach door's arguments: which HTTP MCP server, and what to send
	// with every request to it. There is deliberately no command field — this door's safety
	// argument is that it spawns nothing, and an argument kept in the signature cannot be lost to
	// convenience later (a caller that needs a process needs a different door).
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Owner is the session these tools belong to. **Omitted means the whole daemon** — an older
	// client sends nothing here and keeps exactly the behaviour it had. A client that serves two
	// conversations from one daemon (the PowerPoint helper with two decks open) names the session,
	// and then a tool call can say which conversation it came from — which is the one thing a
	// tool call could not say before (internal/adapter/mcp/SESSION_SCOPE.md).
	Owner string `json:"owner,omitempty"`
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
	// Minutes is the meeting document as the speaker just revised it, on a meet reply. Its own
	// field rather than folded into Out, which carries what was SAID: a screen draws those two in
	// different places, and one field meaning "the sentence, or the document, depending" is a wire
	// that has to be read twice to be understood.
	Minutes string `json:"minutes,omitempty"`
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
	// Model is the model this conversation is on, on the status answer for the reason Permission
	// is there: it changes at runtime, and a viewer that offers to change it must show what from.
	Model string `json:"model,omitempty"`
	// User is what to call the person, when a plugin has renamed them. Same reason as Permission:
	// it is set at runtime, in the memory of the process holding the run, and nowhere else.
	User string `json:"user,omitempty"`
	// Council says whether this companion ends a working turn by declaring to a council. It is here
	// for the reason Permission is: it is settable per companion, and something OUTSIDE the daemon
	// has to be able to tell the model the truth about it.
	//
	// The gap it closes was measured (2026-09-04). The PowerPoint helper appends a clause to every
	// tool description telling the model to finish with `council{complete:true}` — the only place a
	// clause can reach that model, because the MCP handshake's instructions are dropped. On a
	// companion with council switched off that clause names a tool that is not there, and the model
	// called it and got `unknown tool: council`. It was rewritten twice, in prose, and called
	// anyway: a name repeated in forty-two descriptions gets called whatever the sentence around it
	// says. The fix is not a better sentence, it is not writing the sentence when it is not true —
	// and the only process that knows is this one.
	Council bool `json:"council,omitempty"`
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
	// Children answers the `children` method: the subagent conversations a session spawned, as
	// the LOG holds them — newest first.
	//
	// Not the same list as `jobs.children`, and the difference is the reason this door exists.
	// That one is a live register: what is running now and what just ended, in memory, bounded,
	// gone when the daemon restarts. This one is a fact of the log, so it answers for a child
	// that finished last week and for a companion that has since been restarted — which is
	// exactly when somebody asks what a subagent actually did.
	Children []SessionRow `json:"children,omitempty"`
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

// Reasons a gated door carries no capability. The handshake exists for decisions a client makes
// BEFORE anybody presses anything — whether to draw a panel at all — and a name in `caps` is what
// lets it tell an old build from a refusing engine. A door that answers a press is different: the
// refusal arrives at a moment that already has somewhere to show it.
//
// So this is not a backlog. A name here is a decision, and the reason is the value.
const (
	onPress     = "answers a press: no screen is drawn or withheld on it, and the refusal lands on the control that asked"
	isTheAnswer = "this IS the handshake — advertising it inside its own reply says nothing"
)

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
	// Command is what the job RUNS when it comes round, for a job that runs instead of asking.
	// Exclusive with Prompt — a row carries one or the other.
	Command string `json:"command,omitempty"`
	// Timeout bounds one run of Command, as written ("10m"). Empty means the default.
	Timeout string `json:"timeout,omitempty"`
	// Prompt is what the job ASKS when it comes round.
	//
	// It was dropped here while the engine was already handing it over (ScheduledHere answers
	// app.ScheduledJobInfo, which carries it), and dropping it is what made an editing screen
	// impossible: a client could show that a job exists and when it next runs, and could not show
	// what it would do — so "edit this job" had nowhere to start and every writer had to keep its
	// own copy of the words.
	Prompt string `json:"prompt,omitempty"`
}

// CronEdit is one change to the standing schedule, as the wire carries it.
//
// A shape of this package's own rather than port.ScheduleChange: the wire's vocabulary is this
// package's business, and an engine converts at its edge like it does for every other door.
type CronEdit struct {
	Name     string
	Schedule string
	Prompt   string
	// Command and Timeout describe a job that runs instead of asking. Exclusive with Prompt.
	Command string
	Timeout string
	// Enabled is three-valued on purpose: nil leaves the switch alone, which is what an edit that
	// only changes the words must do.
	Enabled *bool
	// Remove deletes the job instead of writing it. One shape for both verbs because they are one
	// read-decide-write against one file, and the gate that serialises them is one gate.
	Remove bool
}

// CronEditor is the engine half of the write doors: add, change or delete one standing job.
//
// Separate from CronTeller for the reason the read/write split has everywhere here — a build can
// answer what is scheduled without accepting changes to it — and separate from CronController
// (reload) because that one exists for an editor that wrote the FILE ITSELF and now has to tell
// the daemon. This door is the alternative to that: the client says what it wants and the daemon,
// which already owns the schema and the three config layers, writes it. A client that composes
// TOML is a client that has to know where a workspace's file lives, how a name becomes a table
// header, and which layer wins — and every client that learns it is one more place that can be
// wrong about it.
type CronEditor interface {
	// EditCron applies one change and answers what the engine said about it. A refusal comes back
	// as prose with a nil error (a schedule that will not parse is not a failure of this call);
	// err is for a change that could not be written at all.
	EditCron(c CronEdit) (string, error)
}

// SessionRow is one conversation as the sessions door reports it: what a picker needs and no
// more. The title is the first prompt's first line, which is what the store already derives.
type SessionRow struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	// Agent marks a CHILD session — every child records the same word ("spawn"), so this says
	// "something else asked for this conversation" and nothing more. It is deliberately not the
	// discriminator: a live run proved the point (a meeting room came back as agent="spawn"),
	// and `spawnAgentName` is a constant in internal/app/spawn.go.
	Agent string `json:"agent,omitempty"`
	// Origin is WHO opened it, and this is what tells one child from another — "meeting" for a
	// room a participant holds, the spawning tool's actor otherwise. The web console already
	// keyed on it before this door existed ("nothing new has to be recorded to tell them apart"),
	// which is the strongest argument for carrying it rather than inventing a second field.
	Origin       string   `json:"origin,omitempty"`
	Model        string   `json:"model,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Created      string   `json:"created,omitempty"`
	LastActivity string   `json:"lastActivity,omitempty"`
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
	// Model is the model the conversation is on now — the same kind of runtime fact as the two
	// above, and the one a model select has to show before it offers to change it.
	Model string
	// Council says whether a working turn here ends by declaring to a council. Anything that tells
	// the model what to do at the end of a turn has to read this first, or it names a tool that
	// is not there — which is what happened, twice, before this field existed.
	Council bool
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
	// Minutes is the meeting's document as this speaker rewrote it, whole. Empty when this daemon
	// does not keep minutes (an older build, or a turn whose minutes pass failed) — which the
	// convener reads as "leave the document alone", never as "erase it".
	Minutes string
}

// completeArgs is the cursor-sides payload for a "complete" request.
type completeArgs struct {
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
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
