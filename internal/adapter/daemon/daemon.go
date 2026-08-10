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
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

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
	SetPermission(p string)
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
	Answer   string `json:"answer,omitempty"`
	// Name and N carry the control methods' one argument each: a model id, a permission policy, a
	// number of turns to rewind, the label above a handed-over request, the receipt for one.
	// Named generically because the alternative is a field per method and a wire format that grows
	// a column every time the engine gains a knob.
	Name string `json:"name,omitempty"`
	N    int    `json:"n,omitempty"`
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
	// Handover answers hand-state. Its own object rather than four more columns here, because
	// "not finished, and here is why not" is one fact with parts and reading it out of flat
	// fields would let a caller act on half of it.
	Handover *Handover `json:"handover,omitempty"`
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
	// Report is the grounds a question was asked on. It crosses the socket for the same reason the
	// options do: a console in another process draws the prompt, and a prompt whose grounds stayed
	// behind is the one this exists to stop.
	Report []report.Filled `json:"report,omitempty"`
	Since  string          `json:"since"` // RFC3339
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
			CallID: w.ID, Question: w.What, Options: w.Options, Report: w.Report, Index: 1, Total: 1})
	default:
		typ = event.TypePermissionRequested
		data, err = json.Marshal(event.PermissionRequestedData{
			CallID: w.ID, Name: w.What, Args: w.Args, Reason: w.Reason})
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
	return filepath.Join(configDir, "daemon-"+sanitize(filepath.Base(abs))+"-"+shortHash(abs)+".sock")
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
	// Nobody answered, so the file is a leftover.
	//
	// This Remove is only safe BECAUSE the dial above proved nothing is listening. A unix socket
	// is an ordinary directory entry: removing a LIVE one succeeds, silently orphans the running
	// daemon's listener, and leaves two engines writing one store while every client reaches only
	// the newer. The probe is what stands between here and that, so the two lines stay together.
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
	return &Daemon{ln: ln, path: path, release: release}, nil
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
		d.stop = make(chan struct{})
	}

	var wg sync.WaitGroup
	go func() {
		select {
		case <-ctx.Done():
		case <-d.stop:
		}
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
			serveConn(ctx, eng, conn, d.Stop)
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
func serveConn(ctx context.Context, eng Engine, conn net.Conn, stop func()) {
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
		// status is answered here rather than in dispatch: it is the only method with a payload,
		// and giving dispatch a return value for the sake of one caller would make every write
		// site pretend to produce something.
		var resp Response
		if req.Method == "status" {
			resp = Response{OK: true}
			if ask, ok := eng.Waiting(session.SessionID(req.Session)); ok {
				resp.Waiting = &Waiting{
					ID: ask.ID, Kind: ask.Kind, What: ask.What, Args: ask.Args,
					Reason: ask.Reason, Options: ask.Options, Report: ask.Report,
					Since: ask.Since.UTC().Format(time.RFC3339),
				}
			}
			resp.Doing, _ = eng.Doing(session.SessionID(req.Session))
			if enc.Encode(resp) != nil {
				return
			}
			continue
		}
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
		// about is answered here rather than in dispatch, like status and shell, because it has a
		// payload. It is also the whole point of the relay: whoever is asking connected to THIS
		// companion, so there is no name to resolve and no config directory to read — the process
		// that knows answers about itself.
		if req.Method == "about" {
			d, ok := eng.(Describer)
			if !ok {
				resp = Response{Err: "this daemon cannot describe its companion"}
			} else {
				resp = Response{OK: true, Out: d.About()}
			}
			if enc.Encode(resp) != nil {
				return
			}
			continue
		}
		// hand and hand-state are answered here for the same reason about is, and they are the
		// reason the relay exists at all. Whoever is asking has connected to THIS companion, so
		// there is no name to resolve against a config directory that may belong to another
		// account and may not exist at all inside a container. The process doing the work says
		// what became of it.
		if req.Method == "hand" || req.Method == "hand-state" {
			taker, ok := eng.(Taker)
			switch {
			case !ok:
				resp = Response{Err: "this daemon cannot be handed work"}
			case req.Method == "hand":
				id, herr := taker.Hand(ctx, req.Name, req.Text)
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
			if enc.Encode(resp) != nil {
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
		// shell is answered here rather than in dispatch, like status, because it has a payload:
		// dispatch returns only an error, and giving it a return value for one caller would make
		// every other write site pretend to produce something.
		if req.Method == "shell" {
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
			if enc.Encode(resp) != nil {
				return
			}
			continue
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

// Describer is an engine that can say what its companion is for and what it can be asked to do.
//
// Optional, like CronController and ShellRunner, for the reason those are: Engine is what every
// fake in every package must satisfy, and a test double has no workspace to describe.
type Describer interface {
	// About renders the same description the MCP `about` tool gives — one renderer, so the answer
	// does not depend on which door it came through.
	About() string
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
	Hand(ctx context.Context, label, request string) (receipt string, err error)
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
func (c *Client) Hand(label, request string) (string, error) {
	resp, err := c.exchange(Request{Method: "hand", Name: label, Text: request})
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

func dispatch(ctx context.Context, eng Engine, r Request) error {
	sid := session.SessionID(r.Session)
	parts := []session.Part{{Kind: session.PartText, Text: r.Text}}
	actor := event.Actor{Kind: event.ActorUser, ID: "attach"}
	switch r.Method {
	case "submit":
		return eng.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: parts, Actor: actor})
	case "steer":
		return eng.Steer(ctx, command.SubmitPrompt{SessionID: sid, Parts: parts, Actor: actor})
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
	case "rewind", "compact", "set-model", "set-permission":
		return control(ctx, eng, r, sid)
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
	return fmt.Errorf("unknown method %q — this daemon accepts: submit, steer, interrupt, permission, answer, status, rewind, compact, set-model, set-permission, reload-cron, shell, about, hand, hand-state, watch, shutdown", r.Method)
}

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
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") ||
			strings.Contains(err.Error(), "connect: no such") {
			return nil, fmt.Errorf("no magi daemon at %s — start one with `magi --daemon` in that workspace", path)
		}
		if strings.Contains(err.Error(), "connection refused") {
			return nil, fmt.Errorf("a socket is at %s but nothing is listening — the daemon died; "+
				"start one with `magi --daemon`", path)
		}
		return nil, fmt.Errorf("daemon: %w", err)
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

// Status asks what the daemon is blocked on and what it is on, if anything. A nil Waiting means it
// is blocked on nobody; an empty note means no running tool has reported.
//
// Both in one exchange because they are one question asked at one moment. Two calls could return a
// prompt and a progress note taken half a second apart, which is a state the daemon was never in.
func (c *Client) Status(sid string) (*Waiting, string, error) {
	resp, err := c.exchange(Request{Method: "status", Session: sid})
	if err != nil {
		return nil, "", err
	}
	return resp.Waiting, resp.Doing, nil
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

// Shutdown asks the daemon to stop.
//
// The daemon answers before it acts, so a nil error means it accepted — not that it has finished
// unwinding. What follows on its side is the ordinary teardown: in-flight requests finish, the
// socket goes, the published record is removed, and the scheduled work stops with the process that
// was running it. That last part is why removing a companion is one call and not two: a record
// deleted while its daemon kept running would leave work happening that nothing on screen could
// account for.
func (c *Client) Shutdown() error { return c.call(Request{Method: "shutdown"}) }

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
	return c.call(Request{Method: "submit", Session: string(cmd.SessionID), Text: textOf(cmd.Parts)})
}

func (c *Client) Steer(_ context.Context, cmd command.SubmitPrompt) error {
	return c.call(Request{Method: "steer", Session: string(cmd.SessionID), Text: textOf(cmd.Parts)})
}

func (c *Client) Interrupt(_ context.Context, cmd command.Interrupt) error {
	return c.call(Request{Method: "interrupt", Session: string(cmd.SessionID)})
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
}

// SessionFile is where a daemon records what it is driving.
func SessionFile(socketPath string) string { return socketPath + ".session" }

// Publish records the daemon and returns a function that removes the record.
func Publish(socketPath, workdir, sid string, id Identity) (func(), error) {
	// Host(), not os.Hostname(): one spelling of this machine's name enters the system here and
	// nothing downstream has to normalise. A record written with the raw name and a member built
	// from the lowercased one compare unequal, and an election over the two decides that the
	// companion it just elected is not on any row it can see.
	host := Host()
	b, err := json.Marshal(Info{
		Socket: socketPath, Workdir: workdir, Session: sid,
		Name: id.Name, Role: id.Role, Team: id.Team, Hub: id.Hub, Can: id.Can, Does: id.Does,
		PID: os.Getpid(), Started: time.Now().UTC().Format(time.RFC3339),
		Host: host, Addr: primaryAddr(),
	})
	if err != nil {
		return func() {}, fmt.Errorf("daemon: publishing: %w", err)
	}
	f := SessionFile(socketPath)
	if err := os.WriteFile(f, b, 0o600); err != nil {
		return func() {}, fmt.Errorf("daemon: publishing: %w", err)
	}
	return func() { os.Remove(f) }, nil
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
	in, err := Published(socketPath)
	if err != nil {
		return nil
	}
	if in.Waiting == waiting && in.Handling == handling {
		return nil // nothing changed; do not rewrite a file readers are polling
	}
	in.Waiting, in.Handling = waiting, handling
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return os.WriteFile(SessionFile(socketPath), b, 0o600)
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
	// Probed in parallel. Serially, a listing costs the SUM of every daemon's latency and one
	// wedged process delays every entry after it — and the reason to run a listing is usually that
	// something is wrong. In parallel it costs the slowest one, which probeTimeout bounds.
	var wg sync.WaitGroup
	for i, s := range socks {
		in, err := Published(s)
		if err != nil {
			// A socket with no readable record: still worth showing, because something is there.
			in = Info{Socket: s, Workdir: "(unknown — no record)"}
		}
		in.Socket = s
		out[i] = in
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
			if ask, doing, serr := cl.Status(sid); serr == nil {
				out[i].Asking, out[i].Doing = ask, doing
			}
			// A daemon too old to know the method answers with an error naming what it does
			// accept. That is a version skew, not a fault: it is alive, and everything else about
			// it is still true. A TIMEOUT lands here too, and means the same thing for the entry:
			// alive, and not saying.
		}(i, s, in.Session)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Started > out[j].Started })
	return out, nil
}
