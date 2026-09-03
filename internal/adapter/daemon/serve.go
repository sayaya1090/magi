// Listening, and the loop that answers: three tables, consulted in order.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sayaya1090/magi/internal/core/session"
)

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
	if err := removeSocket(path); err != nil && !os.IsNotExist(err) {
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
	// Belt and braces where the OS has a mode to confirm, and nothing where it does not — the
	// difference is behind the same seam that opened the socket (secureSocket).
	//
	// This used to be an unconditional os.Chmod, and the comment beside it called Windows "the
	// whole story" there. Windows answers a chmod on an AF_UNIX socket with "The file cannot be
	// accessed by the system", so the line it was said to be for is the one it broke: `magi
	// --daemon` could not start on Windows at all, and the error named a permission bit rather
	// than the platform.
	if err := secureSocket(path); err != nil {
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
	removeSocket(d.path)
	d.release()
	return err
}

// Serve accepts connections until ctx is done or a client asks it to stop, then removes the socket
// and gives up the claim.
func (d *Daemon) Serve(ctx context.Context, eng Engine) error {
	defer func() {
		d.ln.Close()
		removeSocket(d.path)
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
		// Three tables, in this order. A method is in exactly one of them (a guard says so), so the
		// order is not a precedence — it is just the cheapest lookup first.
		//
		// This was a chain of `if req.Method == "…"` down a 265-line function, with a comment at
		// each one saying why it was there. Those reasons now live in the tables' own `why`, where
		// a reader looking for "where does my new method go" finds them by looking at the table
		// instead of by reading the loop.
		if d, ok := answers[req.Method]; ok {
			// The lockstep write is here rather than repeated at the end of all thirty-four.
			if enc.Encode(d.run(ctx, eng, req)) != nil {
				return // the peer is gone
			}
			continue
		}
		if st, ok := streams[req.Method]; ok {
			if st.run(ctx, eng, req, wire{enc: enc, sc: sc, home: home, stop: stop, restart: restart}) == done {
				return
			}
			continue
		}
		// The acts: they DO something and answer only whether it worked, so the Response is built
		// here from the error rather than by each of them.
		err := dispatch(ctx, eng, req)
		resp := Response{OK: err == nil}
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
	// Read-decide-write against one config file — the shape this gate exists for. Two editors
	// landing together would each write the file they read, and the loser's job vanishes with
	// both callers told it worked.
	"cron-set": true, "cron-remove": true,
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

// act is a method that DOES something and answers only whether it worked — no payload, so it
// cannot live in the `answers` table beside the doors that return one.
//
// A table for the same reason that one is: this was two switch statements and a hand-written list
// of their names in acceptedMethods, and the list is what went stale. Now the names exist once.
type act struct {
	run func(context.Context, Engine, Request, session.SessionID) error
	// needs is a nil pointer to the interface this act requires — the same shape as door.needs, so
	// the same guard reads it, and for the same reason: written as a predicate, a gate naming the
	// wrong interface agrees with the body on every engine that implements neither.
	needs any
	// why is what run answers when needs is false, checked against the real answer by a guard.
	why string
}

var acts map[string]act

// can reports whether this engine satisfies the act's declared gate.
func (a act) can(e Engine) bool {
	if a.needs == nil {
		return true
	}
	t := reflect.TypeOf(e)
	return t != nil && t.Implements(reflect.TypeOf(a.needs).Elem())
}

func dispatchNow(ctx context.Context, eng Engine, r Request) error {
	if a, ok := acts[r.Method]; ok {
		return a.run(ctx, eng, r, session.SessionID(r.Session))
	}
	// Name what IS accepted. A client told only "unknown" cannot tell a typo from a version skew,
	// and the two want different reactions.
	return fmt.Errorf("unknown method %q — this daemon accepts: %s", r.Method, acceptedMethods())
}

// acceptedMethods is every method serveConn answers, derived from the three tables that answer
// them rather than kept as a sentence.
//
// The sentence drifted once already: it listed the roster as of the day it was written, and the
// answers map had since grown a dozen methods (tool, edit-file, git, the meeting verbs, the draft
// verbs) the refusal then denied existed — a typo-correcting message that itself needed correcting.
// Half of it stayed a sentence after that fix: eighteen names, hand-written, for the methods that
// were not in a table because there was no table for them. There is now, so this is derived whole,
// and a guard counts what it returns against what the tables hold.
var acceptedMethods = sync.OnceValue(func() string {
	names := map[string]bool{}
	for m := range answers {
		names[m] = true
	}
	for m := range acts {
		names[m] = true
	}
	for m := range streams {
		names[m] = true
	}
	out := make([]string, 0, len(names))
	for m := range names {
		out = append(out, m)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
})
