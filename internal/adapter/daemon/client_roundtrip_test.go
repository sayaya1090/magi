package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
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
	// The dock's verbs, across the wire once each.
	if rows, err := c.Sessions(); err != nil || len(rows) != 2 || rows[0].ID != "s_new" {
		t.Fatalf("Sessions: (%+v, %v)", rows, err)
	}
	if sid, err := c.NewSession(); err != nil || sid != "s_fresh" {
		t.Fatalf("NewSession: (%q, %v)", sid, err)
	}
	if jobs, err := c.Cron(); err != nil || len(jobs) != 3 || jobs[0].Name != "cursed" {
		t.Fatalf("Cron: (%+v, %v)", jobs, err)
	}
	if had, err := c.KillJob("j1"); err != nil || !had {
		t.Fatalf("KillJob: (%v, %v)", had, err)
	}

	// Watch LAST among the exchanges: it gives the connection over to a stream, and the daemon
	// closes it when the stream ends — every wrapper after it would write into a hung-up pipe.
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

// An invented conversation id gets a refusal in words from the transcript door — it used to get
// infinite silence, indistinguishable from an empty session, because the store answers unknown
// with emptiness. The engine's own listing (unborn current included) is what tells them apart.
func TestTranscriptRefusesAnInventedConversation(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-tr.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	go func() { _ = d.Serve(context.Background(), &omniEngine{}) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Transcript("s_invented", 0, nil, func(event.Event) bool { return true })
	var refused Refused
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "sessions") {
		t.Fatalf("an invented id is refused in the door's words, got %v", err)
	}
	// A LISTED conversation still streams (and a clean end is not an error).
	if err := c.Transcript("s_new", 0, nil, func(event.Event) bool { return true }); err != nil {
		t.Fatalf("a listed conversation streams: %v", err)
	}
}

// childAwareEngine mimics production: the listing is top-level only, but the store knows a child
// session the moment it has events. The transcript door must serve that id, not refuse it — the
// same socket advertises it through jobs.
type childAwareEngine struct{ omniEngine }

func (e *childAwareEngine) NewSince(_ context.Context, sid session.SessionID, _ int64) (int64, bool, error) {
	if sid == "s_child" {
		return 3, true, nil // on disk, three events — a subagent's log
	}
	return 0, false, nil
}

func TestTranscriptServesAChildSessionTheListingOmits(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-ch.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	go func() { _ = d.Serve(context.Background(), &childAwareEngine{}) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Transcript("s_child", 0, nil, func(event.Event) bool { return true }); err != nil {
		t.Fatalf("a child session with events on disk must stream, not be refused: %v", err)
	}
}

// settingsEngine answers the settings door and nothing else it does not have to.
type settingsEngine struct {
	omniEngine
	items []ConfigItem
	set   []string
}

func (e *settingsEngine) ConfigHere(context.Context) ([]ConfigItem, error) { return e.items, nil }
func (e *settingsEngine) ConfigSet(_ context.Context, key, value, tier string) (ConfigItem, error) {
	e.set = append(e.set, key+"="+value+"@"+tier)
	return ConfigItem{Key: key, Value: value, Tier: tier, Source: tier, Applies: "next start"}, nil
}
func (e *settingsEngine) ProfilesHere(context.Context) ([]ProfileChoice, error) {
	return []ProfileChoice{{Name: "fast", Tier: "global"}}, nil
}

// The settings cross the socket: a client reads what is editable, changes one key and is told what
// it now is, and the door advertises itself so a screen knows whether to draw the fields at all.
func TestTheSettingsDoorCrossesTheSocket(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-set.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	eng := &settingsEngine{items: []ConfigItem{{Key: "embed_model", Value: "nomic", Source: "global",
		Tier: "global", File: "/x/config.toml", Applies: "next start"}}}
	go func() { _ = d.Serve(context.Background(), eng) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "embed_model" || got[0].Source != "global" || got[0].Applies != "next start" {
		t.Fatalf("the reading arrived as %+v", got)
	}
	item, err := c.SetSetting("autocomplete.code_profile", "fast", "project")
	if err != nil {
		t.Fatal(err)
	}
	if item.Value != "fast" || item.Tier != "project" {
		t.Fatalf("the write answered %+v", item)
	}
	if len(eng.set) != 1 || eng.set[0] != "autocomplete.code_profile=fast@project" {
		t.Fatalf("the engine was asked for %v", eng.set)
	}
	profiles, err := c.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "fast" || profiles[0].Tier != "global" {
		t.Fatalf("the picker arrived as %+v", profiles)
	}
	if _, err := c.Hello(); err != nil {
		t.Fatal(err)
	}
	if !c.PeerSupports("settings") {
		t.Error("a daemon that answers the settings door does not advertise it")
	}
}

// A daemon whose engine has no settings door refuses in words rather than by silence, and does not
// advertise a screen's fields into existence.
func TestADaemonWithoutTheSettingsDoorSaysSo(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-nos.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	go func() { _ = d.Serve(context.Background(), &omniEngine{}) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Settings(); err == nil {
		t.Error("a daemon with no settings door answered as if it had one")
	}
	if _, err := c.Hello(); err != nil {
		t.Fatal(err)
	}
	if c.PeerSupports("settings") {
		t.Error("the door was advertised by a daemon that does not have it")
	}
}
