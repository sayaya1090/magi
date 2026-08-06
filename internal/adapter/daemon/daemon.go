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
}

// Response is the reply. Err is a STRING rather than a bool: a client told only that something
// failed cannot tell a rejected session id from a dead engine, and would retry both.
type Response struct {
	OK  bool   `json:"ok"`
	Err string `json:"error,omitempty"`
	// Waiting answers the status method: absent when the engine is not blocked on anybody.
	Waiting *Waiting `json:"waiting,omitempty"`
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
	Since   string          `json:"since"` // RFC3339
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

// Path is where this daemon is listening.
func (d *Daemon) Path() string { return d.path }

// Close drops the socket without ever having served on it — for a caller that binds and then fails
// at something else before it can start.
func (d *Daemon) Close() error {
	err := d.ln.Close()
	os.Remove(d.path)
	d.release()
	return err
}

// Serve accepts connections until ctx is done, then removes the socket and gives up the claim.
func (d *Daemon) Serve(ctx context.Context, eng Engine) error {
	defer func() {
		d.ln.Close()
		os.Remove(d.path)
		d.release()
	}()

	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		d.ln.Close() // unblocks Accept
	}()
	for {
		conn, err := d.ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				wg.Wait() // let in-flight requests finish before the socket goes
				return nil
			}
			return fmt.Errorf("daemon: accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			serveConn(ctx, eng, conn)
		}()
	}
}

// serveConn reads requests until the peer hangs up. One bad request answers with an error and the
// connection stays open: a UI that mistypes a method should get told, not disconnected.
func serveConn(ctx context.Context, eng Engine, conn net.Conn) {
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
					Reason: ask.Reason, Options: ask.Options,
					Since: ask.Since.UTC().Format(time.RFC3339),
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
	}
	// Name what IS accepted. A client told only "unknown" cannot tell a typo from a version skew,
	// and the two want different reactions.
	return fmt.Errorf("unknown method %q — this daemon accepts: submit, steer, interrupt, permission, answer, status", r.Method)
}

// Client talks to a daemon. One connection, one request at a time: these are user actions, and the
// serialisation is what keeps a steer from overtaking the submit it belongs to.
type Client struct {
	mu   sync.Mutex
	conn net.Conn
	enc  *json.Encoder
	sc   *bufio.Scanner
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
	return &Client{conn: conn, enc: json.NewEncoder(conn), sc: sc, deadline: deadline}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Status asks what the daemon is blocked on, if anything. nil means nothing.
func (c *Client) Status(sid string) (*Waiting, error) {
	resp, err := c.exchange(Request{Method: "status", Session: sid})
	if err != nil {
		return nil, err
	}
	return resp.Waiting, nil
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
	if c.deadline > 0 {
		// Covers the write AND the read: a peer that stops reading blocks the write once the
		// socket buffer fills, which is the same hang one step earlier.
		if err := c.conn.SetDeadline(time.Now().Add(c.deadline)); err != nil {
			return Response{}, fmt.Errorf("daemon: %w", err)
		}
		defer c.conn.SetDeadline(time.Time{})
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
	if !resp.OK {
		return resp, errors.New(resp.Err)
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
	PID     int    `json:"pid"`
	Started string `json:"started"` // RFC3339
	// Live is filled in by List, not by the daemon: a file cannot say whether the process that
	// wrote it is still there. Only a dial can.
	Live bool `json:"-"`
	// Asking is what the daemon is blocked on, when it is. Also from List, and for the same
	// reason: it is in the daemon's memory, so the dial that proves it alive asks while it is
	// there. nil when nothing is pending or the daemon is not answering.
	Asking *Waiting `json:"-"`
}

// SessionFile is where a daemon records what it is driving.
func SessionFile(socketPath string) string { return socketPath + ".session" }

// Publish records the daemon and returns a function that removes the record.
func Publish(socketPath, workdir, sid string) (func(), error) {
	b, err := json.Marshal(Info{
		Socket: socketPath, Workdir: workdir, Session: sid,
		PID: os.Getpid(), Started: time.Now().UTC().Format(time.RFC3339),
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
			if ask, serr := cl.Status(sid); serr == nil {
				out[i].Asking = ask
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
