package fleet

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// Roster is for the message when an address matched nobody: who there IS, or the honest empty.
func TestRosterNamesWhoIsHere(t *testing.T) {
	if got := Roster(nil); !strings.Contains(got, "nobody") {
		t.Fatalf("an empty machine says so, got %q", got)
	}
	got := Roster([]Agent{
		{Name: "zeta"},
		{Name: "api", Role: "serves the API"},
	})
	if !strings.Contains(got, "api (serves the API)") || !strings.Contains(got, "zeta") {
		t.Fatalf("names with their roles, got %q", got)
	}
	if strings.Index(got, "api") > strings.Index(got, "zeta") {
		t.Fatalf("sorted, so the same fleet always reads the same: %q", got)
	}
}

// WordFrom labels a mid-exchange message: it must carry the mark the no-chaining rule reads, name
// the sender, and say how to answer — and it must NOT be the dispatch mark, which would freeze the
// receiver out of handing anything on.
func TestWordFromCarriesTheMarkAndTheWayBack(t *testing.T) {
	got := WordFrom("design")
	if !strings.HasPrefix(got, WordMark+"design") {
		t.Fatalf("the mark opens it, got %q", got)
	}
	if !strings.Contains(got, "mcp__design__ask") {
		t.Fatalf("the way to answer rides the label, got %q", got)
	}
}

// lightRow builds a row from the record and the dial alone: the daemon's own state vocabulary
// when it wrote one, the minimum claim when it did not, and Stopped for the dead — whether a turn
// was left open lives in the log, which the light list never reads.
func daemonInfo(state string, live bool) daemon.Info {
	return daemon.Info{Socket: "/s/a", Workdir: "/w/a", Session: "s_a", State: state, Live: live}
}

func TestLightRowClaimsOnlyWhatItRead(t *testing.T) {
	live := lightRow(daemonInfo("waiting", true))
	if live.State != Waiting || !live.Live {
		t.Fatalf("the daemon said waiting: %+v", live)
	}
	if quiet := lightRow(daemonInfo("", true)); quiet.State != Idle {
		t.Fatalf("alive and saying nothing is the minimum claim, got %v", quiet.State)
	}
	if dead := lightRow(daemonInfo("working", false)); dead.State != Stopped {
		t.Fatalf("dead is Stopped in the light list — the log is not read here, got %v", dead.State)
	}
	if live.Task != "" || live.PlanTotal != 0 {
		t.Fatal("a light row must not carry claims only a log could establish")
	}
}

// A word the vocabulary does not know — a newer daemon's — draws as the minimum claim, never as
// another machine's row (which is what stateHeard's unknown answer would have made of it).
func TestLightRowUnknownWordIsTheMinimumClaim(t *testing.T) {
	if got := lightRow(daemonInfo("reviewing", true)); got.State != Idle {
		t.Fatalf("an unknown live state is the minimum claim, got %v", got.State)
	}
}

// ListLight itself: a machine of corpses still deserves its list (the fallback path), and an
// unreadable directory answers an error — never an empty fleet asserted by nobody.
func TestListLightFallbackAndHonestError(t *testing.T) {
	// A short home, because a unix socket path has a hard length limit (~104 bytes on macOS) and
	// the default temp name spends most of it. (The package's own shortTempDir lives in the
	// external test package and cannot be reached from here.)
	home, herr := os.MkdirTemp("/tmp", "flt")
	if herr != nil {
		t.Fatal(herr)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	// A corpse is a SOCKET nobody is listening on — bind() makes a socket inode and a crash
	// leaves one. This wrote an empty plain file, which magi never creates at that path and
	// which the daemon now refuses to touch rather than tidy away: the fixture would have been
	// standing in for something that does not happen. Go unlinks on close unless told not to.
	deadPath := filepath.Join(home, "daemon-dead.sock")
	addr, aerr := net.ResolveUnixAddr("unix", deadPath)
	if aerr != nil {
		t.Fatal(aerr)
	}
	ln, lerr := net.ListenUnix("unix", addr)
	if lerr != nil {
		t.Fatal(lerr)
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if fi, serr := os.Lstat(deadPath); serr != nil || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("the fixture did not leave a socket behind: %v", serr)
	}
	if _, err := daemon.Publish(deadPath, "/w/dead", "s_d", daemon.Identity{Name: "dead"}); err != nil {
		t.Fatal(err)
	}
	rows, err := ListLight(home, "")
	if err != nil || len(rows) != 1 || rows[0].Name != "dead" || rows[0].State != Stopped {
		t.Fatalf("the corpse draws Stopped via the fallback: (%+v, %v)", rows, err)
	}

	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := ListLight(locked, ""); err == nil {
		t.Skip("this filesystem lets us read an unreadable directory (root?)")
	}
}
