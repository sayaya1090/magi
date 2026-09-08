// One copy of what every editor client needs, so there are not three.
//
// Two editor clients derive the same eight things from the same daemon, in two languages, and they
// have already drifted: the JetBrains client sends a whole file either side of the cursor for a
// completion while the VS Code client sends 4,000 characters, and one strips the answer's overlap
// with what is already typed while the other draws it. One person, two editors, two different
// completions — and neither side knows, because nothing compares them. Writing it a third time in
// C# for Visual Studio would make three copies of a rule that has one right answer.
//
// So the rule moves here, into the binary the editors already download, and the editor layer keeps
// what is genuinely its own: where commands go, what draws the conversation, which inlay API to
// call. The contract is docs/IDE_BRIDGE.md.
package idebridge

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/version"
)

// request is what an editor writes: one JSON object per line.
//
// Req is deliberately json.RawMessage. It is forwarded to the daemon byte for byte, so anything
// this build's structs do not name still crosses — see daemon.Client.Raw.
type request struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Req    json.RawMessage `json:"req,omitempty"`
}

// Methods returns what this bridge answers, which is also what `about` advertises.
//
// Advertised from one list rather than written twice: a bridge that answers a method it does not
// name, or names one it does not answer, teaches a client to call a door that is not there. The
// daemon's own handshake makes the same promise for the same reason.
func Methods() []string { return []string{"about", "daemon"} }

// Run speaks the bridge protocol on stdin/stdout until stdin closes.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("magi ide-bridge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	workspace := fs.String("workspace", "", "the project directory whose companion to speak for (default: the working directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	wd := *workspace
	if wd == "" {
		var err error
		if wd, err = os.Getwd(); err != nil {
			fmt.Fprintln(stderr, "magi ide-bridge:", err)
			return 1
		}
	}
	b := &bridge{
		workspace: wd,
		socket:    daemon.SocketPath(platform.OS{}.ConfigDir(), wd),
		out:       stdout,
	}
	defer b.hangUp()
	return b.serve(stdin, stderr)
}

type bridge struct {
	workspace string
	socket    string

	// out is written from one place at a time. Replies are answered in order today, but a
	// subscription's frames arrive on their own schedule, and two writers interleaving would put
	// half of one JSON object inside another — a line that no reader can parse and no error names.
	mu  sync.Mutex
	out io.Writer

	// conn is kept open across requests rather than dialed per call: a companion holds per-
	// connection state (which conversation this caller is on), and a fresh connection each time
	// would silently be a fresh caller. nil until the first request needs it.
	conn *daemon.Client
}

func (b *bridge) serve(stdin io.Reader, stderr io.Writer) int {
	sc := bufio.NewScanner(stdin)
	// A prompt or a buffer can be large, and the default 64KB stops a scan with an error that
	// looks like the editor hung up. Same bound the daemon's own reader uses.
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(trim(line)) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			// No id to answer to — the line that carried it is the line that failed to parse. Say
			// so on the wire anyway: a client that gets nothing back cannot tell a malformed
			// request from a bridge that died.
			b.reply(map[string]any{"ok": false, "error": "malformed request: " + err.Error()})
			continue
		}
		b.dispatch(req)
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(stderr, "magi ide-bridge:", err)
		return 1
	}
	return 0
}

func (b *bridge) dispatch(req request) {
	switch req.Method {
	case "about":
		b.about(req)
	case "daemon":
		b.forward(req)
	default:
		// Named, so a typo is a sentence rather than a silence. A client that called a method this
		// build does not have would otherwise wait for a reply that is never coming.
		b.fail(req.ID, fmt.Sprintf("no such bridge method %q — this build answers %v", req.Method, Methods()))
	}
}

// about answers what this bridge is and what the companion says it can do.
//
// The two halves are separate on purpose. The bridge half is always known. The daemon half needs a
// companion to be running, and when none is, `daemon` is null and `why` says so — "we could not
// ask" is a different fact from "it advertised nothing", and folding them would have a client
// conclude a door is missing when nobody was home to name it.
func (b *bridge) about(req request) {
	resp := map[string]any{
		"id":        req.ID,
		"ok":        true,
		"version":   version.String(),
		"methods":   Methods(),
		"workspace": b.workspace,
		"socket":    b.socket,
		"daemon":    nil,
	}
	c, err := b.dial()
	if err != nil {
		resp["why"] = err.Error()
		b.reply(resp)
		return
	}
	peer, err := c.Hello()
	if err != nil {
		b.hangUp()
		resp["why"] = err.Error()
		b.reply(resp)
		return
	}
	caps := peer.Caps
	if caps == nil {
		// [] and null mean different things to a client checking a door: an empty advertisement is
		// a peer that named none, and null would read as "not asked". This peer WAS asked.
		caps = []string{}
	}
	resp["daemon"] = map[string]any{"version": peer.Version, "proto": peer.Proto, "caps": caps}
	b.reply(resp)
}

// forward hands the editor's request to the companion unchanged and returns its answer unchanged.
//
// Deliberately dumb, and that is what makes it worth having: every door the daemon has — including
// the fifteen no editor client has ported — is reachable without inventing a second vocabulary for
// each one. A translating pass-through would be a new contract that has to be kept in step with
// the old one, which is the drift this package exists to end.
func (b *bridge) forward(req request) {
	if len(trim(req.Req)) == 0 {
		b.fail(req.ID, "the daemon method needs a req: {\"method\":\"daemon\",\"req\":{\"method\":\"status\"}}")
		return
	}
	c, err := b.dial()
	if err != nil {
		b.fail(req.ID, err.Error())
		return
	}
	raw, err := c.Raw(req.Req)
	if err != nil {
		// The connection is the suspect, not this request: a send that failed or a reply that
		// never came leaves the stream out of step, and reusing it would hand the NEXT caller this
		// call's answer. Drop it and let the following request dial again.
		b.hangUp()
		b.fail(req.ID, err.Error())
		return
	}
	b.reply(map[string]any{"id": req.ID, "ok": true, "resp": json.RawMessage(raw)})
}

func (b *bridge) dial() (*daemon.Client, error) {
	if b.conn != nil {
		return b.conn, nil
	}
	c, err := daemon.Dial(b.socket)
	if err != nil {
		return nil, err
	}
	b.conn = c
	return c, nil
}

func (b *bridge) hangUp() {
	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}
}

func (b *bridge) fail(id int, why string) {
	b.reply(map[string]any{"id": id, "ok": false, "error": why})
}

func (b *bridge) reply(v map[string]any) {
	line, err := json.Marshal(v)
	if err != nil {
		// Nothing sensible to send about a reply that will not encode, and a half-written line
		// would break the framing for every reply after it.
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.out.Write(append(line, '\n'))
}

func trim(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\r' || b[0] == '\n') {
		b = b[1:]
	}
	for len(b) > 0 {
		c := b[len(b)-1]
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}
