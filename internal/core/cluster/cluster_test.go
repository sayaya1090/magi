package cluster

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return now.Add(d) }

func m(host, sock, name string, seen time.Time) Member {
	return Member{Host: host, Socket: sock, Name: name, Seen: seen}
}

func names(ms []Member) string {
	out := make([]string, 0, len(ms))
	for _, x := range ms {
		out = append(out, x.Host+"/"+x.Name)
	}
	return strings.Join(out, " ")
}

// Three companions know each other after two joins, and nobody was told about the third.
//
// This is the whole point of merging rather than configuring. C joins A; A now knows C. B, which
// has never met C, learns it the next time it asks A — and A learned B the same way. Nobody wrote
// anybody into a file, and there is no registry that has to be up for it to work.
func TestKnowingSomebodyYouNeverMet(t *testing.T) {
	a := m("studio", "/s/a.sock", "master", at(0))
	b := m("mini", "/s/b.sock", "api", at(0))
	c := m("buildbox", "/s/c.sock", "design", at(0))

	// A has met B. C joins A, so A holds all three.
	aKnows := Merge([]Member{a, b}, []Member{c}, at(time.Second))
	if got := names(aKnows); got != "buildbox/design mini/api studio/master" {
		t.Fatalf("A knows %q", got)
	}
	// B asks A, and learns C without ever having met it.
	bKnows := Merge([]Member{b, a}, aKnows, at(2*time.Second))
	if got := names(bKnows); got != "buildbox/design mini/api studio/master" {
		t.Fatalf("B knows %q — it never learned about C", got)
	}
}

// A member nobody has seen for long enough is dropped, and one seen recently is not.
//
// The two thresholds are not symmetrical on purpose. Dropping early loses a companion that was
// restarting and sends its work somewhere worse; keeping too long offers one that is not there and
// costs a turn every time somebody tries. So it is said quickly and forgotten slowly.
func TestAMemberDecaysRatherThanBeingDeleted(t *testing.T) {
	fresh := m("studio", "/s/a.sock", "master", at(-time.Minute))
	quiet := m("mini", "/s/b.sock", "api", at(-10*time.Minute))
	gone := m("buildbox", "/s/c.sock", "design", at(-2*time.Hour))

	out := Merge([]Member{fresh, quiet, gone}, nil, now)
	if got := names(out); got != "mini/api studio/master" {
		t.Fatalf("after expiry: %q", got)
	}
	// Quiet is kept and marked, not removed: it may be a companion that is restarting.
	for _, x := range out {
		switch x.Name {
		case "master":
			if !x.Fresh(now) {
				t.Error("a companion seen a minute ago is not fresh")
			}
		case "api":
			if x.Fresh(now) {
				t.Error("a companion unseen for ten minutes is still offered as fresh")
			}
		}
	}
	// Exactly at the boundary it is already forgotten, so the constant means what it says.
	if out := Merge([]Member{m("h", "/s", "x", at(-Forget))}, nil, now); len(out) != 0 {
		t.Errorf("a member at exactly Forget survived: %+v", out)
	}
}

// The later sighting wins, whichever side it came from.
func TestTheFresherSightingWins(t *testing.T) {
	old := m("studio", "/s/a.sock", "master", at(-time.Hour+time.Minute))
	recent := m("studio", "/s/a.sock", "master", at(-time.Minute))

	for _, order := range [][2][]Member{
		{{old}, {recent}},
		{{recent}, {old}},
	} {
		out := Merge(order[0], order[1], now)
		if len(out) != 1 {
			t.Fatalf("one companion merged into %d entries", len(out))
		}
		if !out[0].Seen.Equal(recent.Seen) {
			t.Errorf("kept the older sighting (%v)", out[0].Seen)
		}
	}
}

// A clock running fast cannot make a dead companion immortal.
//
// The timestamps come from other machines and clocks disagree. Five minutes is a threshold a few
// minutes of skew can cross, and the two errors are not equal: aged early shows as "unseen" and the
// next direct sighting fixes it, while trusted-from-the-future keeps a companion that stopped
// yesterday looking alive, with nothing later able to correct it.
func TestASightingFromTheFutureIsNotTrusted(t *testing.T) {
	out := Merge(nil, []Member{m("mini", "/s/b.sock", "api", at(3*time.Hour))}, now)
	if len(out) != 1 {
		t.Fatalf("the entry was dropped entirely: %+v", out)
	}
	if out[0].Seen.After(now) {
		t.Fatalf("a sighting from the future was kept as-is: %v", out[0].Seen)
	}
	// And it is still a real member — clamped, not discarded: the entry is evidence the companion
	// exists, and only its freshness was wrong.
	if !out[0].Fresh(now) {
		t.Error("clamping to now made it stale, which is the opposite of what the clock claimed")
	}
}

// A fresher sighting that carries no name does not blank the name somebody recorded.
//
// A sighting can be thin — somebody answered on this socket — while an older entry holds the role
// and team it declared. Taking the fresher entry whole would turn a described companion into an
// anonymous one every time it was seen by somebody who knew less about it.
func TestAThinSightingDoesNotEraseWhatIsKnown(t *testing.T) {
	known := Member{Host: "studio", Socket: "/s/a.sock", Name: "design",
		Role: "the design system", Team: "frontend", Workdir: "/w/d", Seen: at(-time.Minute)}
	thin := Member{Host: "studio", Socket: "/s/a.sock", Seen: at(0)}

	out := Merge([]Member{known}, []Member{thin}, now)
	if len(out) != 1 {
		t.Fatalf("%d entries", len(out))
	}
	got := out[0]
	if got.Name != "design" || got.Role != "the design system" || got.Team != "frontend" || got.Workdir != "/w/d" {
		t.Errorf("a thin sighting erased what was known: %+v", got)
	}
	if !got.Seen.Equal(thin.Seen) {
		t.Errorf("the fresher time was lost: %v", got.Seen)
	}
}

// An entry with no host or no socket is not a member: there is nothing to merge it against and
// nothing to reach.
func TestAnEntryWithNoIdentityIsNotAMember(t *testing.T) {
	out := Merge(nil, []Member{
		{Name: "nowhere", Seen: now},
		{Host: "studio", Name: "nosocket", Seen: now},
		{Socket: "/s/a.sock", Name: "nohost", Seen: now},
	}, now)
	if len(out) != 0 {
		t.Errorf("kept %+v", out)
	}
}

// What arrived is a hostname. What runs is built here.
//
// This is the line the package is arranged around, and the one that lets membership travel at all:
// join refuses to copy an [mcp] entry between workspaces because it is a command this process would
// later run, and that refusal stands. A member says WHERE a companion is; it can never say what to
// execute.
func TestAMemberSaysWhereNotWhatToRun(t *testing.T) {
	remote := Member{Host: "buildbox", Socket: "/s/d.sock", Name: "design"}
	cmd, args := Reach(remote, "studio", "master", "/usr/local/bin/magi")
	if cmd != "ssh" {
		t.Fatalf("a remote member is reached with %q", cmd)
	}
	want := "buildbox /usr/local/bin/magi --mcp design --mcp-as master"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("args %q, want %q", got, want)
	}

	// On this machine there is no ssh: a host that cannot reach itself over ssh is most of them,
	// and going through one to talk to a neighbour would fail for no reason.
	local := Member{Host: "studio", Socket: "/s/a.sock", Name: "api"}
	cmd, args = Reach(local, "studio", "master", "/usr/local/bin/magi")
	if cmd != "/usr/local/bin/magi" {
		t.Errorf("a local member is reached with %q %v", cmd, args)
	}
	if strings.Contains(strings.Join(args, " "), "ssh") {
		t.Errorf("ssh crept into a local reach: %v", args)
	}
}
