package idebridge

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeDaemon answers on a unix socket the way a companion does: one request per line, one reply
// per line. reply is called with the raw request bytes so a test can assert what CROSSED, which is
// the only way to catch a pass-through that quietly re-encodes.
type fakeDaemon struct {
	t     *testing.T
	path  string
	ln    net.Listener
	mu    sync.Mutex
	seen  []string
	reply func(raw string) string
}

func listen(t *testing.T, reply func(string) string) *fakeDaemon {
	t.Helper()
	// Short, because a unix address holds ~100 bytes and t.TempDir() under a long test name is
	// past it — the failure would be "invalid argument" and say nothing about length.
	dir, err := os.MkdirTemp("", "idb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	p := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	d := &fakeDaemon{t: t, path: p, ln: ln, reply: reply}
	go d.accept()
	t.Cleanup(func() { ln.Close() })
	return d
}

func (d *fakeDaemon) accept() {
	for {
		c, err := d.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			sc := bufio.NewScanner(c)
			sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
			for sc.Scan() {
				raw := sc.Text()
				d.mu.Lock()
				d.seen = append(d.seen, raw)
				d.mu.Unlock()
				io.WriteString(c, d.reply(raw)+"\n")
			}
		}()
	}
}

func (d *fakeDaemon) requests() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.seen...)
}

// run drives the bridge over pipes the way an editor does and returns one reply per input line.
func run(t *testing.T, sock string, lines ...string) []map[string]any {
	t.Helper()
	b := &bridge{workspace: "/ws", socket: sock}
	var out strings.Builder
	b.out = &out
	defer b.hangUp()
	if code := b.serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), io.Discard); code != 0 {
		t.Fatalf("serve = %d, want 0", code)
	}
	var got []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("reply %q is not JSON: %v", l, err)
		}
		got = append(got, m)
	}
	return got
}

// The whole point of the forwarding door: a field this build's structs never named must reach the
// daemon, and its answer must come back whole.
//
// This is the failure that would not announce itself. encoding/json drops unknown keys without a
// word, so a bridge that decoded into daemon.Request and re-encoded would send a request missing
// whatever the editor asked for, and the daemon would answer a DIFFERENT, valid question. No error
// anywhere; the editor just sees a door that does nothing.
func TestForwardKeepsFieldsThisBuildDoesNotKnow(t *testing.T) {
	d := listen(t, func(raw string) string {
		return `{"ok":true,"out":"done","futureField":{"deep":[1,2]}}`
	})
	got := run(t, d.path,
		`{"id":1,"method":"daemon","req":{"method":"submit","text":"hi","notInAnyStruct":"keep me","nested":{"a":1}}}`)
	if len(got) != 1 {
		t.Fatalf("got %d replies, want 1", len(got))
	}
	sent := d.requests()
	if len(sent) != 1 {
		t.Fatalf("daemon saw %d requests, want 1", len(sent))
	}
	for _, want := range []string{`"notInAnyStruct":"keep me"`, `"nested":{"a":1}`, `"text":"hi"`} {
		if !strings.Contains(sent[0], want) {
			t.Errorf("the daemon never saw %s\n  it saw: %s", want, sent[0])
		}
	}
	resp, _ := json.Marshal(got[0]["resp"])
	for _, want := range []string{`"futureField"`, `"deep"`, `"out":"done"`} {
		if !strings.Contains(string(resp), want) {
			t.Errorf("the reply lost %s\n  it carried: %s", want, resp)
		}
	}
}

// A refusal is an answer, not a failure of the bridge. If ok:false were turned into a bridge error
// the client would lose the daemon's own sentence about why — which is the sentence worth showing.
func TestRefusalIsPassedOnAsTheDaemonWroteIt(t *testing.T) {
	d := listen(t, func(string) string { return `{"ok":false,"error":"this daemon cannot complete code"}` })
	got := run(t, d.path, `{"id":4,"method":"daemon","req":{"method":"complete"}}`)
	if got[0]["ok"] != true {
		t.Fatalf("the bridge reported its own failure for a daemon refusal: %v", got[0])
	}
	resp, _ := json.Marshal(got[0]["resp"])
	if !strings.Contains(string(resp), "this daemon cannot complete code") {
		t.Errorf("the daemon's reason did not survive: %s", resp)
	}
}

// One request per line is the whole framing. A newline smuggled inside a string would be read by
// the daemon as the start of a second request — and the reply to that phantom would be handed to
// whoever asked next.
func TestForwardCannotSmuggleASecondRequest(t *testing.T) {
	d := listen(t, func(string) string { return `{"ok":true}` })
	run(t, d.path, `{"id":1,"method":"daemon","req":{"method":"submit","text":"a\nb"}}`)
	sent := d.requests()
	if len(sent) != 1 {
		t.Fatalf("the daemon saw %d requests, want 1: %q", len(sent), sent)
	}
}

// "We could not ask" and "it advertised nothing" are different facts. A client that read a missing
// companion as an empty capability list would conclude a door is gone when nobody was home.
func TestAboutSaysWhenNobodyIsHome(t *testing.T) {
	got := run(t, filepath.Join(t.TempDir(), "nothing.sock"), `{"id":9,"method":"about"}`)
	if got[0]["ok"] != true {
		t.Fatalf("about failed with no daemon: %v", got[0])
	}
	if got[0]["daemon"] != nil {
		t.Errorf("daemon = %v, want null when none is running", got[0]["daemon"])
	}
	if why, _ := got[0]["why"].(string); why == "" {
		t.Error("no why: a client cannot tell 'not running' from 'advertised nothing'")
	}
	if got[0]["version"] == "" || got[0]["version"] == nil {
		t.Error("about must say what the bridge is even when the daemon is absent")
	}
}

// Every method about names must actually answer, or the advertisement teaches a client to call a
// door that is not there — the trap the daemon's own handshake exists to avoid.
func TestAboutAdvertisesOnlyMethodsThatAnswer(t *testing.T) {
	got := run(t, filepath.Join(t.TempDir(), "nothing.sock"), `{"id":1,"method":"about"}`)
	list, ok := got[0]["methods"].([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("about advertised no methods: %v", got[0]["methods"])
	}
	for _, m := range list {
		name, _ := m.(string)
		reply := run(t, filepath.Join(t.TempDir(), "nothing.sock"),
			`{"id":2,"method":`+strconv.Quote(name)+`}`)
		if e, _ := reply[0]["error"].(string); strings.Contains(e, "no such bridge method") {
			t.Errorf("about advertises %q but the bridge does not answer it", name)
		}
	}
}

// A typo must come back as a sentence. Silence would leave the client waiting for a reply that is
// never coming, and "the extension hangs" is the report that reaches a person.
func TestUnknownMethodIsNamed(t *testing.T) {
	got := run(t, "/nowhere.sock", `{"id":3,"method":"transcript"}`)
	if got[0]["ok"] != false {
		t.Fatalf("unknown method reported ok: %v", got[0])
	}
	if e, _ := got[0]["error"].(string); !strings.Contains(e, "transcript") {
		t.Errorf("the error does not name the method: %q", e)
	}
}

// A Windows client writing with the platform's default UTF-8 encoder puts three bytes in front of
// the first line. Measured against the real bridge: without this, `about` came back as "invalid
// character 'U+FEFF' looking for beginning of value" and every request after it was fine — a
// handshake that fails and a bridge that then works, which gets reported as flakiness.
func TestAByteOrderMarkOnTheFirstLineIsNotAMalformedRequest(t *testing.T) {
	// Escaped rather than typed: a literal BOM in a Go source file is a compile error, and one
	// pasted into a comment is invisible to whoever reads this next.
	got := run(t, filepath.Join(t.TempDir(), "nothing.sock"), "\ufeff"+`{"id":1,"method":"about"}`)
	if got[0]["ok"] != true {
		t.Fatalf("a BOM made the first request malformed: %v", got[0])
	}
	if got[0]["version"] == nil {
		t.Error("the reply is not an about answer")
	}
}

// A line that is not JSON has no id to answer to, but it must still get a reply: a client that
// gets nothing back cannot tell a bad request from a bridge that died.
func TestMalformedLineStillAnswers(t *testing.T) {
	got := run(t, "/nowhere.sock", `{not json`, `{"id":2,"method":"about"}`)
	if len(got) != 2 {
		t.Fatalf("got %d replies for 2 lines, want 2: %v", len(got), got)
	}
	if got[0]["ok"] != false {
		t.Errorf("a malformed line was reported ok: %v", got[0])
	}
}
