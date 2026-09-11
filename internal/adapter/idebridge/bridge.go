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
	"bytes"
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
	// Session names the conversation a question is about. Optional, and its absence is not a
	// default: `status` fills the model only when the request carries one, so a poll that omits it
	// gets an answer with no model — which means "nobody said which conversation", not "no model".
	Session string `json:"session,omitempty"`
}

// Methods returns what this bridge answers, which is also what `about` advertises.
//
// Advertised from one list rather than written twice: a bridge that answers a method it does not
// name, or names one it does not answer, teaches a client to call a door that is not there. The
// daemon's own handshake makes the same promise for the same reason.
func Methods() []string { return []string{"about", "activity", "daemon"} }

// features is what a client may ASK this binary before it trusts it with anything.
//
// ⚠ **The list is derived, never written down.** A hand-kept list is a second place the truth
// lives, and the failure it produces is the worst kind: a client reads the name, calls the thing,
// and the thing is not there — which is indistinguishable from a broken install. So each entry
// pairs its name with a predicate that asks the IMPLEMENTATION, and `featuresOf` drops any whose
// predicate says no. Adding a name without wiring the thing produces an absent feature, not a lie.
//
// Feature names are not version numbers. `protocol` says what SHAPE this output has; a feature
// name carries its own version suffix because features arrive and are replaced one at a time —
// `raw-socket-v2` would be a different thing from `raw-socket-v1`, not a newer protocol.
var features = []struct {
	name string
	// has is asked of this build. It must observe the thing itself — a flag that is registered, a
	// function that is reachable — and never repeat a constant from elsewhere in this file.
	has func(fs *flag.FlagSet) bool
}{
	// The Windows transport. Node reads a unix socket path as a named pipe, so the extension cannot
	// dial AF_UNIX at all and goes through this relay instead: a client with no way to ask would
	// have to spawn it and watch it fail.
	{"raw-socket-v1", func(fs *flag.FlagSet) bool { return fs.Lookup("raw-socket") != nil }},
}

// featuresOf asks every candidate whether this build actually has it.
//
// Returns a non-nil empty slice when none qualify: `[]` and `null` are different answers on the
// wire, and a client that gets `null` cannot tell "this build has no features" from "this field is
// not implemented here".
func featuresOf(fs *flag.FlagSet) []string {
	out := []string{}
	for _, f := range features {
		if f.has(fs) {
			out = append(out, f.name)
		}
	}
	return out
}

// featuresProtocol is the shape of the --features line, not the daemon's wire version.
//
// Separate from daemon.ProtoVersion on purpose. They are free to move independently: this line is
// answered without a daemon at all, so tying it to the socket protocol's number would make a
// client re-read this probe for a change that cannot affect it.
const featuresProtocol = 1

// answerFeatures writes the one line a client reads before it trusts this binary.
//
// ⚠ **It must not dial, and it must not need a workspace.** The caller is deciding whether this
// binary can be used AT ALL — possibly because no daemon is running, possibly because it is about
// to start one — so a probe that waits on a socket answers the wrong question slowly. Nothing here
// touches the filesystem or the network, which is also how it keeps the five-second bound in
// docs/CLIENT_LIFECYCLE §4 without a timeout of its own.
//
// One line, so a caller can read it with a single ReadString('\n') and not have to know when to
// stop. An older binary has no such flag and its flag package refuses the argument, which is the
// answer "this build does not support features" — a client must read THAT, not scan this text.
func answerFeatures(fs *flag.FlagSet, stdout io.Writer) int {
	line, err := json.Marshal(map[string]any{
		"protocol": featuresProtocol,
		"features": featuresOf(fs),
		// The acceptance record in docs/CLIENT_LIFECYCLE §8 has to name the exact binary a run
		// used, and this is the only probe that answers with no daemon up — so the version rides
		// here rather than making a caller start something to learn it.
		"version": version.String(),
	})
	if err != nil {
		return 1
	}
	fmt.Fprintln(stdout, string(line))
	return 0
}

// Run speaks the bridge protocol on stdin/stdout until stdin closes.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("magi ide-bridge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rawSocket := fs.String("raw-socket", "", "relay stdin/stdout to this daemon socket")
	askFeatures := fs.Bool("features", false, "print what this binary supports as one line of JSON, and exit")
	workspace := fs.String("workspace", "", "the project directory whose companion to speak for (default: the working directory)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	// Answered before anything else, including --raw-socket. This is a question ABOUT the binary,
	// so it cannot be conditional on the binary doing its job first.
	if *askFeatures {
		return answerFeatures(fs, stdout)
	}
	if *rawSocket != "" {
		return relay(*rawSocket, stdin, stdout, stderr)
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
		line := trim(sc.Bytes())
		if len(line) == 0 {
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
	case "activity":
		b.activity(req)
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
	// A UTF-8 BOM in front of the first line, which is what a Windows client writing with the
	// platform's default UTF-8 encoder sends. Measured: without this the first request comes back
	// as "invalid character 'U+FEFF' looking for beginning of value" and every later one is fine —
	// so the symptom is a handshake that fails and a bridge that then works, which gets reported as
	// flakiness rather than as an encoding. Three bytes are cheaper to skip than to explain.
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
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
