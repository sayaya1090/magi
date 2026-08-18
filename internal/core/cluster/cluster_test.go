package cluster

import (
	"reflect"
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

// What arrived is a hostname. What runs is built elsewhere, from the reaching machine's template.
//
// This is the line the package is arranged around, and the one that lets membership travel at all:
// join refuses to copy an [mcp] entry between workspaces because it is a command this process would
// later run, and that refusal stands. A member says WHERE a companion is; it can never say what to
// execute.
//
// It used to be checked against Reach, which assembled `ssh host magi --mcp name` here. Crossing is
// the daemon protocol over a door now (cmd/magi's reachCompanion and doorTo, where the host is also
// refused if it is shaped like an ssh option), and Reach went with the design that needed it. The
// rule did not go with it, so this asks the rule directly: is there anywhere on a Member for a
// command to ride in? A new field that could carry one has to be added deliberately, in sight of
// this test, rather than arriving as a convenience.
func TestAMemberSaysWhereNotWhatToRun(t *testing.T) {
	t3 := reflect.TypeOf(Member{})
	// The names a command would arrive under. Substring, so Command/Cmd/ExecPath and their like are
	// all caught by one entry.
	for _, bad := range []string{"cmd", "command", "exec", "run", "argv", "shell", "script", "template"} {
		for i := 0; i < t3.NumField(); i++ {
			if strings.Contains(strings.ToLower(t3.Field(i).Name), bad) {
				t.Errorf("Member.%s can carry something to execute, which is what this package "+
					"exists to keep off the wire", t3.Field(i).Name)
			}
		}
	}
	// And the fields that DO travel are identity and sightings — every one of them a fact about
	// where a companion is or what it was doing, never about how to start anything.
	m := Member{Host: "buildbox", Socket: "/s/d.sock", Name: "design"}
	if m.Host == "" || m.Socket == "" {
		t.Fatal("a member is a host and a socket")
	}
}

// A team keeps a speaker when the one who declared itself hub goes quiet.
//
// Addressing a team is addressing its hub, and a hub is the only companion allowed to split work
// it was given across its own team. Both stopped dead when the declared hub went away, so a team
// whose lead was restarting was a team nobody could reach and whose members could not share out
// what they had been handed.
func TestATeamKeepsASpeakerWhenTheHubGoesQuiet(t *testing.T) {
	hub := Member{Host: "studio", Socket: "/s/a.sock", Name: "lead", Team: "frontend", Hub: true, Seen: at(-time.Minute)}
	one := Member{Host: "mini", Socket: "/s/b.sock", Name: "api", Team: "frontend", Seen: at(-time.Minute)}
	two := Member{Host: "buildbox", Socket: "/s/c.sock", Name: "design", Team: "frontend", Seen: at(-time.Minute)}

	// While it is there, the declaration wins and nothing is "acting".
	who, acting, ok := Speaker([]Member{hub, one, two}, "frontend", now)
	if !ok || acting || who.Name != "lead" {
		t.Fatalf("with the hub present: who=%q acting=%v ok=%v", who.Name, acting, ok)
	}

	// Gone quiet: somebody else speaks, and is marked as standing in.
	hub.Seen = at(-10 * time.Minute)
	who, acting, ok = Speaker([]Member{hub, one, two}, "frontend", now)
	if !ok || !acting {
		t.Fatalf("nobody speaks for the team: who=%q acting=%v ok=%v", who.Name, acting, ok)
	}
	if who.Name != "design" { // lowest key: buildbox < mini
		t.Errorf("elected %q", who.Name)
	}

	// Every companion works this out from its own list with no message passing, so the answer must
	// not depend on the order the list happens to be in.
	for _, order := range [][]Member{{two, one, hub}, {one, hub, two}, {hub, two, one}} {
		if got, _, _ := Speaker(order, "frontend", now); got.Name != who.Name {
			t.Errorf("a different order elected %q instead of %q", got.Name, who.Name)
		}
	}

	// And it goes back the moment the declared hub is seen again. Nothing was stored, so nothing
	// has to be undone.
	hub.Seen = at(-time.Second)
	if got, acting, _ := Speaker([]Member{hub, one, two}, "frontend", now); got.Name != "lead" || acting {
		t.Errorf("the hub came back and %q is still speaking (acting=%v)", got.Name, acting)
	}
}

// Two companions preferring themselves is a field of two candidates, not a conflict.
//
// Declaring `hub = true` is a preference, the way MongoDB's replica-set priority is: it moves a
// member to the front of the queue, and two members may share it because the election still
// returns one. An earlier version treated it as a claim and refused — which made a team
// unaddressable because somebody typed one word twice, and left its members unable to share out
// work they had been handed. The worse outcome, for the sake of surfacing a smaller mistake.
func TestTwoPreferredHubsAreElectedBetween(t *testing.T) {
	a := Member{Host: "studio", Socket: "/s/a.sock", Name: "lead", Team: "frontend", Hub: true, Seen: at(0)}
	b := Member{Host: "mini", Socket: "/s/b.sock", Name: "other", Team: "frontend", Hub: true, Seen: at(0)}
	plain := Member{Host: "buildbox", Socket: "/s/c.sock", Name: "worker", Team: "frontend", Seen: at(0)}

	who, acting, ok := Speaker([]Member{a, b, plain}, "frontend", now)
	if !ok {
		t.Fatal("a team with two willing hubs has nobody to speak for it")
	}
	// One of the two who asked for it, never the one who did not — a preference that loses to a
	// bare member is not a preference.
	// mini < studio: the key leads with the HOST, so the tie-break is by machine first. Arbitrary
	// and stable, which is the whole requirement.
	if who.Name != "other" {
		t.Errorf("elected %q; want other (lowest key among the preferred)", who.Name)
	}
	if acting {
		t.Error("a companion that declared itself hub is not standing in for anybody")
	}
	// And it does not depend on the order the list arrived in, because every companion computes
	// this alone and they have to agree.
	for _, order := range [][]Member{{b, plain, a}, {plain, a, b}} {
		if got, _, _ := Speaker(order, "frontend", now); got.Name != who.Name {
			t.Errorf("a different order elected %q", got.Name)
		}
	}
}

// A team whose members have all gone has no speaker — an election among nobody is not an answer.
func TestATeamWithNobodyLeftHasNoSpeaker(t *testing.T) {
	stale := Member{Host: "studio", Socket: "/s/a.sock", Name: "lead", Team: "frontend", Seen: at(-30 * time.Minute)}
	if _, _, ok := Speaker([]Member{stale}, "frontend", now); ok {
		t.Error("a team of one unseen member still has a speaker")
	}
	if _, _, ok := Speaker(nil, "frontend", now); ok {
		t.Error("an empty cluster has a speaker")
	}
	// The empty team name is not a team. Companions that declared none would otherwise all be
	// swept into one — and the first of them elected to speak for a group that does not exist.
	// The fixture is FRESH and teamless on purpose: a stale one is filtered before the question
	// is even reached, which is how this check passed while meaning nothing.
	loner := Member{Host: "studio", Socket: "/s/z.sock", Name: "solo", Seen: at(0)}
	if who, _, ok := Speaker([]Member{loner}, "", now); ok {
		t.Errorf("the empty team name elected %q", who.Name)
	}
}

// Among equals, the one that can do the most speaks.
//
// The hub is where team-addressed work lands and the only companion allowed to split it up. Give it
// to one that can do little and it forwards everything: a hop, a second paraphrase of the request —
// which this tree has confirmed loses the identifiers a task is graded on — and a roundtrip, for
// nothing.
func TestTheCompanionThatCanDoMostSpeaks(t *testing.T) {
	// deep sorts LAST by key (studio > buildbox > mini), so a key-only rule would never pick it.
	deep := Member{Host: "studio", Socket: "/s/a.sock", Name: "deep", Team: "frontend", Can: 21, Seen: at(0)}
	thin := Member{Host: "buildbox", Socket: "/s/b.sock", Name: "thin", Team: "frontend", Can: 2, Seen: at(0)}
	none := Member{Host: "mini", Socket: "/s/c.sock", Name: "none", Team: "frontend", Seen: at(0)}

	who, acting, ok := Speaker([]Member{thin, none, deep}, "frontend", now)
	if !ok || !acting {
		t.Fatalf("who=%q acting=%v ok=%v", who.Name, acting, ok)
	}
	if who.Name != "deep" {
		t.Errorf("elected %q; the one that can do the most should hold the work", who.Name)
	}

	// A person's declaration still beats the count. The number is a tie-break for when nobody has
	// said anything, not a vote against somebody who has.
	thin.Hub = true
	if got, acting, _ := Speaker([]Member{thin, none, deep}, "frontend", now); got.Name != "thin" || acting {
		t.Errorf("a capability count overrode a declaration: %q (acting=%v)", got.Name, acting)
	}

	// Equal counts fall back to the stable key, so companions computing this alone still agree.
	deep.Hub, thin.Hub = false, false
	deep.Can, thin.Can = 5, 5
	first, _, _ := Speaker([]Member{deep, thin}, "frontend", now)
	second, _, _ := Speaker([]Member{thin, deep}, "frontend", now)
	if first.Name != second.Name {
		t.Errorf("equal counts elected %q and %q depending on order", first.Name, second.Name)
	}
}

// A companion that has gone quiet keeps what it could do, and keeps the count with it.
//
// A later sighting is often a bare one — somebody passing along "this exists at this address" with
// no capabilities attached. Letting that blank the names would make a companion look useless
// precisely when it went quiet, which is when somebody is deciding whether to wait for it.
//
// Names and count together or neither: three names beside a count of nine, taken from two different
// sightings, would advertise a companion nobody ever saw.
func TestACompanionThatGoesQuietKeepsWhatItCanDo(t *testing.T) {
	now := time.Now()
	known := []Member{{Host: "buildbox", Socket: "/s/d.sock", Name: "design",
		Can: 9, Does: []string{"tokens", "layout", "contrast"}, Seen: now.Add(-10 * time.Minute)}}
	bare := []Member{{Host: "buildbox", Socket: "/s/d.sock", Seen: now}}

	got := Merge(known, bare, now)
	if len(got) != 1 {
		t.Fatalf("%d members after a merge of one", len(got))
	}
	if len(got[0].Does) != 3 || got[0].Can != 9 {
		t.Fatalf("a bare sighting erased what it can do: can=%d does=%v", got[0].Can, got[0].Does)
	}
}

// One machine is one member, however its name happens to be capitalised.
//
// Key is host plus socket, so two spellings of a hostname are two companions: the cluster carries
// both, every roster offers both, and work goes to whichever the model picked. Reachable without
// anybody doing anything odd — os.Hostname() reports different casing on macOS depending on which
// of its several host names was last set, so one reboot is enough.
func TestOneMachineIsOneMemberHoweverItIsCapitalised(t *testing.T) {
	now := time.Now()
	got := Merge(
		[]Member{{Host: "buildbox", Socket: "/s/d.sock", Name: "design", Seen: now}},
		[]Member{{Host: "BuildBox", Socket: "/s/d.sock", Name: "design", Seen: now}},
		now)
	if len(got) != 1 {
		t.Fatalf("one companion became %d: %+v", len(got), got)
	}
}

// A signed record is not "improved" by an older sighting of the same companion.
//
// Merge borrows identity fields onto an entry that is missing them, which is right for a bare
// sighting and wrong for a record its owner signed: the signature covers those fields, so what the
// owner did not say is absent rather than missing. Borrowing would also make the bytes stop
// matching the signature — and this machine relays what it stores, so the improvement would arrive
// at the next machine as a forgery.
func TestASignedRecordIsNotFilledInFromAnOlderOne(t *testing.T) {
	now := time.Now()
	old := Member{
		Host: "buildbox", Socket: "/s/d.sock", Name: "design", Role: "the design system",
		Team: "frontend", Can: 3, Does: []string{"tokens"}, Seen: now.Add(-time.Minute),
	}
	// The same companion, said again by the machine it belongs to, which has since dropped the
	// team and the role. Signed — the bytes here stand in for a real signature; what matters is
	// that Merge sees a record that carries one.
	fresh := Member{
		Host: "buildbox", Socket: "/s/d.sock", Name: "design", Seen: now,
		By: "a-key", Sig: "a-signature",
	}
	got := Merge([]Member{old}, []Member{fresh}, now)
	if len(got) != 1 {
		t.Fatalf("one companion became %d", len(got))
	}
	if got[0].Role != "" || got[0].Team != "" {
		t.Errorf("the signed record was filled in from an older one: role=%q team=%q",
			got[0].Role, got[0].Team)
	}
	if len(got[0].Does) != 0 || got[0].Can != 0 {
		t.Errorf("capabilities were borrowed onto a signed record: can=%d does=%v",
			got[0].Can, got[0].Does)
	}
	// An UNSIGNED record still is, because a bare sighting is exactly what that behaviour is for.
	bare := Member{Host: "buildbox", Socket: "/s/d.sock", Seen: now}
	if got := Merge([]Member{old}, []Member{bare}, now); got[0].Role == "" {
		t.Error("a bare sighting stopped being filled in, which is what it is for")
	}
}

// Signable must cover Waiting and Handling: they drive team-address routing (fleet.load), so if the
// signature ignored them an admitted relay could forge a companion's load — starve a victim or steal
// its team work — with the signature still verifying. Reverting the two puts makes these equal.
func TestSignableCoversTheRoutingLoad(t *testing.T) {
	base := Member{Host: "h", Socket: "s", Name: "n"}
	hi := base
	hi.Waiting = 5
	busy := base
	busy.Handling = true
	if string(Signable(base)) == string(Signable(hi)) {
		t.Error("Signable ignores Waiting — a relay could forge team-routing load")
	}
	if string(Signable(base)) == string(Signable(busy)) {
		t.Error("Signable ignores Handling — a relay could forge team-routing load")
	}
}

// Version must NOT be in Signable, or adding it to the roster would make an older daemon's records —
// signed before the field existed — fail verification on a newer daemon and vanish during the rolling
// upgrade, exactly when the roster is meant to show both versions. It is advisory, so it travels
// unsigned: setting it must not change the signed bytes.
func TestSignableIgnoresVersionSoARollingUpgradeSurvives(t *testing.T) {
	base := Member{Host: "h", Socket: "s", Name: "n"}
	newer := base
	newer.Version = "v0.23.0"
	if string(Signable(base)) != string(Signable(newer)) {
		t.Error("Signable covers Version — an older peer's records would fail verification and drop " +
			"out of the roster mid-upgrade, which is exactly what carrying the version unsigned avoids")
	}
}
