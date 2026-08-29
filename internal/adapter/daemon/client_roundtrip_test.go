package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One served daemon, one dialled client, every wrapper crossed once: the test that keeps the
// Client's spelling of each method and serveConn's reading of it from drifting apart — the same
// reason the socket name and the tool-name rule have goldens.
func TestClientWrappersRoundTrip(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-rt.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	o := &omniEngine{}
	go func() { _ = d.Serve(context.Background(), o) }()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if caps := Caps(); !strings.Contains(strings.Join(caps, ","), "handshake") {
		t.Fatalf("the build floor speaks the handshake: %v", caps)
	}
	if about, err := c.About(); err != nil || about != "a companion" {
		t.Fatalf("About: (%q, %v)", about, err)
	}
	if c.PeerSupports("roster") {
		t.Fatal("before Hello nothing is known, and unknown means do-not-send")
	}
	p, err := c.Hello()
	if err != nil || p.Version != "v-test" || p.Proto != ProtoVersion {
		t.Fatalf("Hello: (%+v, %v)", p, err)
	}
	if !c.PeerSupports("roster") || c.PeerSupports("no-such-cap") {
		t.Fatal("PeerSupports reads the cached handshake, exactly")
	}
	if id, err := c.Hand("design", "count things", true); err != nil || id != "receipt-1" {
		t.Fatalf("Hand: (%q, %v)", id, err)
	}
	if h, err := c.Handed("receipt-1"); err != nil || !h.Done || h.Answer != "42" {
		t.Fatalf("Handed: (%+v, %v)", h, err)
	}
	if jobs, err := c.Jobs("s"); err != nil || len(jobs.Background) != 1 || jobs.Background[0].Tail != "tail:j1" {
		t.Fatalf("Jobs: (%+v, %v)", jobs, err)
	}
	if out, exit, err := c.Shell("ls -la"); err != nil || out != "out" || exit != 3 {
		t.Fatalf("Shell: (%q, %d, %v)", out, exit, err)
	}
	if out, err := c.ReadOnlyTool("read", json.RawMessage(`{"path":"x"}`)); err != nil || out != "read-out" {
		t.Fatalf("ReadOnlyTool: (%q, %v)", out, err)
	}
	if err := c.PatchFile("/f.go", "@@ hunk", true); err != nil {
		t.Fatalf("PatchFile: %v", err)
	}
	if last := o.calls[len(o.calls)-1]; last != "patch:/f.go:@@ hunk" {
		t.Fatalf("the patch and its path must arrive as sent: %v", last)
	}
	if out, err := c.WriteTool("write", json.RawMessage(`{}`), false); err != nil || out != "write-out" {
		t.Fatalf("WriteTool: (%q, %v)", out, err)
	}
	if out, err := c.Git(); err != nil || !strings.Contains(out, "branch") {
		t.Fatalf("Git: (%q, %v)", out, err)
	}
	if out, err := c.LookOver("/f.go", "code"); err != nil || out != "remark" {
		t.Fatalf("LookOver: (%q, %v)", out, err)
	}
	if out, why, err := c.CompleteCode("/f.go", "pre", "suf"); err != nil || out != "" || string(why) != "unrouted" {
		t.Fatalf("CompleteCode: the reason must survive the wire: (%q, %q, %v)", out, why, err)
	}
	if last := o.calls[len(o.calls)-1]; last != "complete:/f.go:pre|suf" {
		t.Fatalf("both cursor sides must arrive in order: %v", last)
	}
	if err := c.SetOpenFile("/f.go", "buffer"); err != nil {
		t.Fatalf("SetOpenFile: %v", err)
	}
	if out, err := c.Suggest("fix the"); err != nil || out == "" {
		t.Fatalf("Suggest: (%q, %v)", out, err)
	}
	if url, err := c.OpenPR("Title", "Body"); err != nil || url != "https://pr/1" {
		t.Fatalf("OpenPR: (%q, %v)", url, err)
	}
	if out, err := c.PRFacts(); err != nil || !strings.Contains(out, "repo") {
		t.Fatalf("PRFacts: (%q, %v)", out, err)
	}
	if out, err := c.DraftPR("rules"); err != nil || !strings.HasPrefix(out, "title") {
		t.Fatalf("DraftPR: (%q, %v)", out, err)
	}
	if out, err := c.DraftCommit("rules"); err != nil || out != "subject" {
		t.Fatalf("DraftCommit: (%q, %v)", out, err)
	}
	if err := c.FileDo("rename", "a.txt", "b.txt", true); err != nil {
		t.Fatalf("FileDo: %v", err)
	}
	if last := o.calls[len(o.calls)-1]; last != "filedo:rename:a.txt:b.txt" {
		t.Fatalf("what/path/to must arrive in order: %v", last)
	}
	if out, err := c.GitDiff("f.go", "staged"); err != nil || out != "diff-out" {
		t.Fatalf("GitDiff: (%q, %v)", out, err)
	}
	if o.flags != [2]bool{true, false} {
		t.Fatalf("staged means staged across the wire too: %v", o.flags)
	}
	if out, err := c.GitDo("commit", "f.go", "msg", false); err != nil || out != "done" {
		t.Fatalf("GitDo: (%q, %v)", out, err)
	}
	if err := c.Watch("receipt-1", func(Handover) bool { return true }); err != nil {
		t.Fatalf("Watch: a clean end is not an error, got %v", err)
	}

	// The doors this engine does not have: the refusal must travel as an error, not as silence.
	if _, err := c.AttachMCP("x", "http://127.0.0.1:9/mcp", nil); err == nil {
		t.Fatal("AttachMCP on a doorless engine must refuse in words")
	}
	if _, err := c.DetachMCP("x"); err == nil {
		t.Fatal("DetachMCP likewise")
	}
	if err := c.UseBackend("http://b:1"); err == nil {
		t.Fatal("UseBackend likewise")
	}
	if err := c.Resume("s_x"); err == nil {
		t.Fatal("Resume on an engine that cannot must surface the refusal")
	}
}

// Over is the byte-pipe constructor — the --relay path, where the connection was made by
// something else (ssh, a test, a pipe) and the client only speaks over it.
func TestOverSpeaksOverAForeignConnection(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-ov.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	go func() { _ = d.Serve(context.Background(), &omniEngine{}) }()

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	c := Over(conn)
	defer c.Close()
	if about, err := c.About(); err != nil || about != "a companion" {
		t.Fatalf("a dumb byte pipe is a full client: (%q, %v)", about, err)
	}
}
