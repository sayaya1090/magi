package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/bus"
)

// A fleet on disk: real published records behind real sockets, which is what a listing reads.
type earsFixture struct {
	t      *testing.T
	cfgDir string
	reader *app.App
	broken error // set to make the listing fail the way a bad moment on this machine would
}

func newEarsFixture(t *testing.T) *earsFixture {
	t.Helper()
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &earsFixture{t: t, cfgDir: shortSockDir(t),
		reader: app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})}
}

// running publishes a companion with somebody answering its socket, and returns the way to stop it.
func (f *earsFixture) running(name, sid string) func() {
	f.t.Helper()
	sock := filepath.Join(f.cfgDir, "daemon-"+sid+".sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		f.t.Fatal(err)
	}
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			c.Close()
		}
	}()
	unpublish, err := daemon.Publish(sock, f.t.TempDir(), sid, daemon.Identity{Name: name})
	if err != nil {
		f.t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		unpublish()
		ln.Close()
		os.Remove(sock)
	}
	f.t.Cleanup(stop)
	return stop
}

// recordedPeers stands in for the MCP manager. Attaching really means spawning a subprocess that
// speaks the protocol; what is under test here is which companions should be attached at all.
type recordedPeers struct {
	added   []string
	removed []string
	fail    map[string]error
}

func (r *recordedPeers) AddStdio(_ context.Context, name, _ string, _, _ []string) error {
	if err := r.fail[name]; err != nil {
		return err
	}
	r.added = append(r.added, name)
	return nil
}
func (r *recordedPeers) Remove(name string) { r.removed = append(r.removed, name) }

func (f *earsFixture) ears(mgr *recordedPeers, out *bytes.Buffer) *companionEars {
	cfg := config.Config{}
	cfg.Companion.MCPPeers = true
	return newCompanionEars(mgr, f.listing, cfg, "/bin/magi", "master", out)
}

// listing is the real one, over the real records on disk — with a way to make it fail, which is
// the one thing a directory of sockets cannot be asked to do on purpose.
func (f *earsFixture) listing(ctx context.Context) ([]fleet.Agent, error) {
	if f.broken != nil {
		return nil, f.broken
	}
	// A socket nothing published, so nothing in the listing is "us".
	return fleet.List(ctx, f.reader, f.cfgDir, filepath.Join(f.cfgDir, "daemon-nobody.sock"))
}

// A companion that comes up later is attached, and one that stops is detached.
//
// Attached once at startup, the set was frozen for the life of the process. The roster now
// advertises a companion that appeared later, so a model can hand it work — and without this it
// would then have no way to ask it anything. The other direction matters more: a stopped
// companion left two tools in the list that could only fail, which is worse than not being there.
func TestPeersFollowTheCompanionsThatAreActuallyRunning(t *testing.T) {
	f := newEarsFixture(t)
	stopDesign := f.running("design", "d")
	var out bytes.Buffer
	mgr := &recordedPeers{}
	e := f.ears(mgr, &out)

	e.reconcile(context.Background())
	if got := sorted(mgr.added); len(got) != 1 || got[0] != "design" {
		t.Fatalf("the first pass attached %v", got)
	}

	// Nothing changed: a second pass must not attach the same companion twice, which would leave
	// two subprocesses and two sets of identically named tools.
	e.reconcile(context.Background())
	if got := sorted(mgr.added); len(got) != 1 {
		t.Fatalf("an unchanged fleet was attached again: %v", got)
	}

	f.running("builder", "b")
	e.reconcile(context.Background())
	if got := sorted(mgr.added); len(got) != 2 || got[1] != "design" || got[0] != "builder" {
		t.Fatalf("a companion that came up later was not attached: %v", got)
	}
	if len(mgr.removed) != 0 {
		t.Errorf("attaching one detached another: %v", mgr.removed)
	}

	stopDesign()
	e.reconcile(context.Background())
	if got := sorted(mgr.removed); len(got) != 1 || got[0] != "design" {
		t.Fatalf("a companion that stopped kept its tools in the list: %v", got)
	}
	if got := sorted(mgr.added); len(got) != 2 {
		t.Errorf("detaching one re-attached another: %v", got)
	}
}

// A listing that failed changes nothing.
//
// It is this machine having a bad moment, not the cluster emptying. Detaching every peer over it
// would take away tools that work — a worse answer than a set that is briefly stale.
func TestAFailedListingDetachesNobody(t *testing.T) {
	f := newEarsFixture(t)
	f.running("design", "d")
	var out bytes.Buffer
	mgr := &recordedPeers{}
	e := f.ears(mgr, &out)
	e.reconcile(context.Background())
	if len(mgr.added) != 1 {
		t.Fatalf("setup attached %v", mgr.added)
	}

	f.broken = errors.New("the daemon records could not be read")
	e.reconcile(context.Background())
	if len(mgr.removed) != 0 {
		t.Errorf("a listing that could not be done detached %v", mgr.removed)
	}
}

// A peer that will not start is said once, and tried again.
//
// Said every turn it would bury the log of a long session; said never, an empty peer set looks
// like a companion that knows nobody. Not marked attached, so a companion that starts working is
// picked up without a restart.
func TestAPeerThatWillNotStartIsSaidOnceAndRetried(t *testing.T) {
	f := newEarsFixture(t)
	stop := f.running("design", "d")
	var out bytes.Buffer
	mgr := &recordedPeers{fail: map[string]error{"design": errWontStart}}
	e := f.ears(mgr, &out)

	for i := 0; i < 3; i++ {
		e.reconcile(context.Background())
	}
	if n := strings.Count(out.String(), errWontStart.Error()); n != 1 {
		t.Errorf("it complained %d times over three turns:\n%s", n, out.String())
	}
	mgr.fail = nil
	e.reconcile(context.Background())
	if len(mgr.added) != 1 {
		t.Fatalf("a peer that started working was never retried: %v", mgr.added)
	}

	// And a companion that stops, comes back, and fails again is reported again. Detaching is the
	// only way back to being attempted, so it is where the complaint has to be forgotten — silent
	// the second time, an empty peer set looks like a companion that knows nobody.
	stop()
	e.reconcile(context.Background())
	f.running("design", "d2")
	mgr.fail = map[string]error{"design": errWontStart}
	e.reconcile(context.Background())
	if n := strings.Count(out.String(), errWontStart.Error()); n != 2 {
		t.Errorf("a peer that broke a second time was said %d times:\n%s", n, out.String())
	}
}

var errWontStart = errors.New("that subprocess will not start")

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}
