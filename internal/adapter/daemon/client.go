// Calling a daemon: one connection, one request at a time.
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
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

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

// DialWithin is Dial with both bounds set, for a caller that cannot afford to wait forever.
//
// Dial has no timeouts on purpose: an attached view is meant to sit on its daemon. Every OTHER
// caller — anything that asks a question and has a person waiting on the answer — needs the
// bounds dialProbe documents below, and until now the only way to get them was to be inside this
// package. The PowerPoint helper was outside it, called Dial, and hung: a socket file whose owner
// is gone in a way that leaves connect hanging is exactly the state that helper exists to survive,
// and on Windows under %APPDATA% it is a state that happens.
//
// Both bounds, both different failures. connectTimeout is nobody accepting; deadline is a daemon
// that accepted and then never answered.
func DialWithin(path string, connectTimeout, deadline time.Duration) (*Client, error) {
	return dialProbe(path, connectTimeout, deadline)
}

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
// owner names the conversation the tools belong to; empty is the whole daemon — what this call
// meant before the field existed.
func (c *Client) AttachMCP(owner, name, url string, headers map[string]string) ([]string, error) {
	resp, err := c.exchange(Request{Method: "mcp-attach", Owner: owner, Name: name, URL: url, Headers: headers})
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
func (c *Client) DetachMCP(owner, name string) (bool, error) {
	resp, err := c.exchange(Request{Method: "mcp-detach", Owner: owner, Name: name})
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

// SetCron writes one standing job — adding it, or changing the one with that name.
func (c *Client) SetCron(name, schedule, prompt string, enabled *bool) ([]CronRow, string, error) {
	return c.editCron(Request{Method: "cron-set", Name: name, Schedule: schedule,
		Text: prompt, Enabled: enabled})
}

// RemoveCron deletes one standing job.
func (c *Client) RemoveCron(name string) ([]CronRow, string, error) {
	return c.editCron(Request{Method: "cron-remove", Name: name})
}

func (c *Client) editCron(r Request) ([]CronRow, string, error) {
	// exchange already carries a refusal out as Refused — a second !OK check here would be
	// unreachable, and if it ever were reached it would DOWNGRADE that type to a plain error,
	// which is what withClient reads to tell "the daemon said no" from "the pipe is dead".
	resp, err := c.exchange(r)
	if err != nil {
		return nil, "", err
	}
	return resp.Cron, resp.Out, nil
}

// Children lists the subagent conversations a session spawned, newest activity first — what the
// live `jobs` register cannot answer once a child has ended or the daemon has restarted.
func (c *Client) Children(sid string) ([]SessionRow, error) {
	resp, err := c.exchange(Request{Method: "children", Session: sid})
	if err != nil {
		return nil, err
	}
	return resp.Children, nil
}

// NewSession opens a fresh conversation on the companion and moves it there, answering with the
// new id. The one way to get a new conversation: resume refuses invented ids on purpose.
func (c *Client) NewSession() (string, error) { return c.newSession(false) }

// NewSessionKeeping opens a conversation and leaves the companion on the one it is serving. A
// client that holds several conversations at once wants this one — see Request.Keep.
func (c *Client) NewSessionKeeping() (string, error) { return c.newSession(true) }

func (c *Client) newSession(keep bool) (string, error) {
	resp, err := c.exchange(Request{Method: "session-new", Keep: keep})
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
		Backend: resp.Backend, User: resp.User, Model: resp.Model, Council: resp.Council}, nil
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

// ContextState answers what fills the conversation's context window — usage against the window
// and the five parts it is made of. The add-ins draw a meter from it; the terminal has /context.
func (c *Client) ContextState(sid session.SessionID) (app.ContextState, error) {
	resp, err := c.exchange(Request{Method: "context", Session: string(sid)})
	if err != nil {
		return app.ContextState{}, err
	}
	if resp.Context == nil {
		return app.ContextState{}, errors.New("the daemon answered without a context state")
	}
	return *resp.Context, nil
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
func (c *Client) Join(meeting, topic string, room []Seat) (ready, roomID string, err error) {
	resp, err := c.exchange(Request{Method: "meet-join", Meeting: meeting, Name: topic, Room: room})
	if err != nil {
		return "", "", err
	}
	return resp.Out, resp.Session, nil
}

func (c *Client) Meet(meeting, topic, transcript, minutes string, room []Seat, closing bool) (Contribution, error) {
	which := ""
	if closing {
		which = "closing"
	}
	resp, err := c.exchange(Request{Method: "meet", Meeting: meeting, Name: topic, Text: transcript,
		Minutes: minutes, Room: room, Decision: which})
	if err != nil {
		return Contribution{}, err
	}
	return Contribution{Said: resp.Out, Pass: resp.Exit != nil && *resp.Exit == 1,
		Room: resp.Session, Minutes: resp.Minutes}, nil
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
