package fleet

import (
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
