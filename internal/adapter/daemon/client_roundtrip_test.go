package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// One served daemon, one dialled client, every wrapper crossed once: the test that keeps the
// Client's spelling of each method and serveConn's reading of it from drifting apart — the same
// reason the socket name and the tool-name rule have goldens.
func TestClientWrappersRoundTrip(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
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
	home, err := os.MkdirTemp(shortRoot(), "mgi")
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
	home, err := os.MkdirTemp(shortRoot(), "mgi")
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
	home, err := os.MkdirTemp(shortRoot(), "mgi")
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
	home, err := os.MkdirTemp(shortRoot(), "mgi")
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
	home, err := os.MkdirTemp(shortRoot(), "mgi")
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

// A plain file where a socket belongs is not a daemon that died. The dial's errno cannot tell
// them apart — a leftover file is ENOTSOCK on macOS and ECONNREFUSED on Linux — so the answer
// comes from the file's TYPE, which no kernel has an opinion about.
func TestAPlainFileAtASocketPathIsNotADeadDaemon(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	path := filepath.Join(home, "daemon-x.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Dial(path)
	if err == nil {
		t.Fatal("dialling a plain file succeeded")
	}
	if !errors.Is(err, ErrNotASocket) {
		t.Errorf("a plain file must be said as itself, got %v", err)
	}
	if !errors.Is(err, ErrGone) {
		t.Errorf("it is still unreachable, and a caller asking only that must keep working: %v", err)
	}
	if strings.Contains(err.Error(), "died") {
		t.Errorf("a path no daemon ever listened on must not report a crash: %v", err)
	}
	// And a socket FILE nobody is listening on still reads as a daemon that died. Built by hand
	// so the file outlives its listener: Go unlinks a unix socket on close unless told not to.
	sock := filepath.Join(home, "daemon-y.sock")
	addr, err := net.ResolveUnixAddr("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
	if fi, serr := os.Stat(sock); serr != nil || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("the fixture did not leave a socket file: %v", serr)
	}
	if _, derr := Dial(sock); derr == nil {
		t.Fatal("a socket with no listener answered")
	} else if errors.Is(derr, ErrNotASocket) || !errors.Is(derr, ErrGone) {
		t.Errorf("a dead daemon must read as one: %v", derr)
	}
}

// Starting a daemon does not delete what it did not make.
//
// The dial that precedes the remove proves nothing is LISTENING, not that the file is a leftover
// socket — a plain file fails to dial the same way. magi never writes a plain file at this path
// (bind makes a socket inode; a crash leaves a socket), so anything else there is somebody else's,
// and the cost is asymmetric: refusing is a sentence, removing is not recoverable.
func TestListenDoesNotDeleteWhatItDidNotMake(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })

	plain := filepath.Join(home, "daemon-a.sock")
	if err := os.WriteFile(plain, []byte("somebody's file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, lerr := Listen(plain); lerr == nil {
		d.Stop()
		t.Fatal("a daemon bound over a plain file")
	} else if !strings.Contains(lerr.Error(), "not a socket") {
		t.Errorf("the refusal must say what is in the way: %v", lerr)
	}
	if b, rerr := os.ReadFile(plain); rerr != nil || string(b) != "somebody's file" {
		t.Fatalf("the file was destroyed: %v %q", rerr, b)
	}

	// A symlink is judged as a LINK, not as what it points at: removing it on the strength of a
	// live socket at the other end would take somebody's link away.
	live := filepath.Join(home, "real.sock")
	d, err := Listen(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	link := filepath.Join(home, "daemon-b.sock")
	if err := os.Symlink(live, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if d2, lerr := Listen(link); lerr == nil {
		d2.Stop()
		t.Fatal("a daemon bound over a symlink")
	}
	if _, lerr := os.Lstat(link); lerr != nil {
		t.Fatalf("the symlink was removed: %v", lerr)
	}

	// And a genuine leftover socket is still cleared, which is what the remove is for.
	stale := filepath.Join(home, "daemon-c.sock")
	addr, err := net.ResolveUnixAddr("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
	d3, err := Listen(stale)
	if err != nil {
		t.Fatalf("a leftover socket must not stop a daemon starting: %v", err)
	}
	d3.Stop()
}

// Status answers which model the conversation is on, beside the approval mode and the backend it
// already answered — one question, one moment, the three facts only the running process knows.
func TestStatusSaysWhichModel(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-m.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	go func() { _ = d.Serve(context.Background(), &modelSayingEngine{}) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	st, err := c.Status("s_1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Model != "qwen3-coder" {
		t.Errorf("status answered model %q", st.Model)
	}
	// A daemon whose engine cannot say leaves it empty rather than inventing one.
	if plain, perr := Dial(sock); perr == nil {
		defer plain.Close()
	}
}

type modelSayingEngine struct{ omniEngine }

func (*modelSayingEngine) ModelOf(session.SessionID) string { return "qwen3-coder" }

// kidEngine answers the children door over a real connection.
type kidEngine struct {
	omniEngine
	asked string
}

func (e *kidEngine) ChildrenOf(_ context.Context, parent string) ([]session.SessionMeta, error) {
	e.asked = parent
	return []session.SessionMeta{
		{ID: "s_room", Agent: "spawn", Origin: "meeting", Title: "which store for the queue"},
	}, nil
}

// The children door crosses the socket, and the round trip is what the wrappers exist for: the
// client's spelling of the method, the server's reading of `session` as the PARENT, and the row
// coming back with its role intact. A field that survives a struct but not the wire is the shape
// of defect this file was written for.
func TestTheChildrenDoorCrossesTheSocket(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-kid.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	eng := &kidEngine{}
	go func() { _ = d.Serve(context.Background(), eng) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	kids, err := c.Children("s_parent")
	if err != nil {
		t.Fatalf("children over the socket: %v", err)
	}
	if eng.asked != "s_parent" {
		t.Fatalf("the wire's session reaches the engine as the parent, got %q", eng.asked)
	}
	if len(kids) != 1 || kids[0].ID != "s_room" {
		t.Fatalf("the row crosses whole, got %+v", kids)
	}
	// Origin is the field a screen reads to tell a meeting room from a delegate; a wire that
	// dropped it would still pass every assertion above and leave that screen unable to say
	// which is which. (Agent is "spawn" for every child — measured against a live meeting.)
	if kids[0].Origin != "meeting" || kids[0].Agent != "spawn" {
		t.Fatalf("origin/agent crossed as %q/%q", kids[0].Origin, kids[0].Agent)
	}
	// And the handshake says the door is there, so a client knows before it calls.
	hi, err := c.Hello()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(hi.Caps, "children") {
		t.Fatalf("the door advertises itself in the handshake, caps were %v", hi.Caps)
	}
}

// wireCronEngine answers both cron halves over a real connection.
type wireCronEngine struct {
	omniEngine
	jobs []app.ScheduledJobInfo
	got  CronEdit
}

func (e *wireCronEngine) ScheduledHere() []app.ScheduledJobInfo { return e.jobs }
func (e *wireCronEngine) EditCron(c CronEdit) (string, error) {
	e.got = c
	e.jobs = append(e.jobs, app.ScheduledJobInfo{Name: c.Name, Schedule: c.Schedule,
		Prompt: c.Prompt, Enabled: c.Enabled == nil || *c.Enabled})
	return "set " + c.Name, nil
}

// The cron edit door crosses the socket whole. Two fields are new here and both are the kind that
// survives a struct and dies on the wire: `schedule`, and `enabled` as a POINTER — a bool that
// arrived as false when it was meant to be absent would switch a job off on an edit that only
// changed its words.
func TestTheCronEditDoorCrossesTheSocket(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-cron.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	eng := &wireCronEngine{}
	go func() { _ = d.Serve(context.Background(), eng) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	rows, msg, err := c.SetCron("nightly", "0 3 * * *", "read yesterday's commits", nil)
	if err != nil {
		t.Fatalf("cron-set over the socket: %v", err)
	}
	if eng.got.Name != "nightly" || eng.got.Schedule != "0 3 * * *" ||
		eng.got.Prompt != "read yesterday's commits" {
		t.Fatalf("the edit crossed as %+v", eng.got)
	}
	if eng.got.Enabled != nil {
		t.Fatal("an omitted switch must stay omitted — a false here silently switches jobs off")
	}
	if eng.got.Remove {
		t.Fatal("cron-set is not a removal")
	}
	if len(rows) != 1 || rows[0].Prompt != "read yesterday's commits" {
		t.Fatalf("the answer is the new listing, with the words, got %+v", rows)
	}
	if msg == "" {
		t.Fatal("what the engine said comes back too — it is the only place a nuance can live")
	}
	// The switch, when it IS meant.
	off := false
	if _, _, err := c.SetCron("nightly", "", "", &off); err != nil {
		t.Fatal(err)
	}
	if eng.got.Enabled == nil || *eng.got.Enabled {
		t.Fatalf("an explicit false crossed as %v", eng.got.Enabled)
	}
	// And the handshake says both doors are there.
	hi, err := c.Hello()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cron-set", "cron-remove"} {
		if !slices.Contains(hi.Caps, want) {
			t.Fatalf("%q is advertised in the handshake, caps were %v", want, hi.Caps)
		}
	}
}
