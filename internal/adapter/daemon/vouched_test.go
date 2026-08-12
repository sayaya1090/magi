package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/identity"
	"github.com/sayaya1090/magi/internal/core/cluster"
)

// what a far machine would send: a record signed by the key that machine holds.
func sent(t *testing.T, keyDir string, ms ...cluster.Member) []cluster.Member {
	t.Helper()
	out := Vouch(keyDir, ms)
	if len(out) > 0 && out[0].Sig == "" {
		t.Fatal("the fixture did not sign anything, so nothing below is being tested")
	}
	return out
}

func names(ms []cluster.Member) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

// A peer may CARRY a record and may not WRITE one.
//
// Gossip is transitive, so everything a machine hears has been through somebody. Before this, an
// admitted peer could put a companion on a host it has never touched — or move a real one to a
// socket of its choosing — and what arrived was indistinguishable from a sighting.
func TestARecordNobodyVouchedForIsNotBelieved(t *testing.T) {
	cfg := t.TempDir()
	far := t.TempDir()
	now := time.Now()

	invented := cluster.Member{Host: "buildbox", Socket: "/s/ghost.sock", Name: "ghost", Seen: now}
	real1 := sent(t, far, cluster.Member{Host: "buildbox", Socket: "/s/d.sock", Name: "design",
		Account: "you", Seen: now})[0]

	got := Vouched(cfg, []cluster.Member{invented, real1}, func(string, ...any) {})
	if len(got) != 1 || got[0].Name != "design" {
		t.Fatalf("an unsigned record was believed: %v", names(got))
	}

	// And a signed record that was EDITED on the way. The signature covers where a companion
	// answers, which is the field a relay would want to change.
	moved := real1
	moved.Socket = "/s/mine.sock"
	if got := Vouched(cfg, []cluster.Member{moved}, func(string, ...any) {}); len(got) != 0 {
		t.Errorf("a record whose socket was rewritten still verified: %+v", got)
	}
	// And the account it says it belongs to. Moving a companion into somebody else's instance is
	// how a row comes to say two people's work is one fleet.
	borrowed := real1
	borrowed.Account = "somebody-else"
	if got := Vouched(cfg, []cluster.Member{borrowed}, func(string, ...any) {}); len(got) != 0 {
		t.Errorf("a record whose account was rewritten still verified: %+v", got)
	}
	// The same for what it claims to be able to do, which is what a roster is read for.
	louder := real1
	louder.Does = append([]string{"deploy-production"}, louder.Does...)
	louder.Can = 9
	if got := Vouched(cfg, []cluster.Member{louder}, func(string, ...any) {}); len(got) != 0 {
		t.Errorf("a record whose capabilities were rewritten still verified: %+v", got)
	}
}

// Signing alone proves nothing about WHO — anybody can mint a key. What makes it an identity is
// that the key does not change under a companion this machine has already seen.
func TestACompanionThatSignsWithANewKeyIsRefused(t *testing.T) {
	cfg := t.TempDir()
	first, second := t.TempDir(), t.TempDir()
	now := time.Now()
	one := cluster.Member{Host: "buildbox", Socket: "/s/d.sock", Name: "design", Seen: now}

	if got := Vouched(cfg, sent(t, first, one), nil); len(got) != 1 {
		t.Fatalf("the first sighting was refused: %v", names(got))
	}
	// Remembered on disk, so a restart does not forget who it met.
	seen := filepath.Join(cfg, seenKeysFile)
	body, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("nothing was written down: %v", err)
	}
	if !strings.Contains(string(body), "/s/d.sock") {
		t.Errorf("the file does not name the companion it is about:\n%s", body)
	}

	var said []string
	got := Vouched(cfg, sent(t, second, one),
		func(f string, a ...any) { said = append(said, fmt.Sprintf(f, a...)) })
	if len(got) != 0 {
		t.Fatal("somebody claiming to be a companion this machine had already met was believed")
	}
	// And the refusal says how to clear it, because a machine really can be rebuilt and the person
	// reading the log is the one who would know.
	if len(said) == 0 || !strings.Contains(strings.Join(said, " "), seenKeysFile) {
		t.Errorf("the refusal does not say which file to edit: %q", said)
	}

	// The one it did learn still passes, so the refusal is about the key and not about the member.
	if got := Vouched(cfg, sent(t, first, one), nil); len(got) != 1 {
		t.Error("the key it first met stopped being accepted")
	}
}

// A record this machine relays has to arrive verifiable at the NEXT one.
//
// Gossip is transitive, so a machine passes on what it was told. If the signature did not survive
// being written down and read back, every record would be believable exactly one hop from the
// machine it describes, and the check would quietly become a check on the last relay.
func TestWhatThisMachineRelaysStaysCheckable(t *testing.T) {
	cfg := t.TempDir()
	far := t.TempDir()
	now := time.Now()

	heard := sent(t, far, cluster.Member{
		Host: "buildbox", Socket: "/s/d.sock", Name: "design", Role: "the design system",
		Can: 3, Does: []string{"tokens", "review"}, Seen: now})
	if _, err := LearnMembers(cfg, heard, now); err != nil {
		t.Fatal(err)
	}
	out := Elsewhere(cfg, now)
	if len(out) != 1 {
		t.Fatalf("the sighting was not kept: %v", names(out))
	}
	// Read back off disk, through the merge, and checked the way a third machine would check it.
	if !identity.VerifyBy(out[0].By, out[0].Sig, cluster.Signable(out[0])) {
		t.Error("what came back out of the file no longer verifies, so this machine cannot relay it")
	}

	// And a sighting arriving from a machine whose clock runs fast is CLAMPED by the merge — which
	// is why freshness is not signed. If it were, the clamp would break every record from that
	// machine, at the machine the clamp exists to protect.
	ahead := sent(t, far, cluster.Member{
		Host: "buildbox", Socket: "/s/late.sock", Name: "late", Seen: now.Add(2 * time.Hour)})
	if got := Vouched(cfg, ahead, nil); len(got) != 1 {
		t.Fatal("a record from a machine with a fast clock was refused")
	}
	if _, err := LearnMembers(cfg, ahead, now); err != nil {
		t.Fatal(err)
	}
	for _, m := range Elsewhere(cfg, now) {
		if m.Name != "late" {
			continue
		}
		if m.Seen.After(now) {
			t.Error("the clamp stopped happening")
		}
		if !identity.VerifyBy(m.By, m.Sig, cluster.Signable(m)) {
			t.Error("clamping a sighting broke the signature it arrived with")
		}
	}
}

// This machine signs its own companions, wherever they are on their way to.
//
// Signed in Mine rather than in whichever code path is doing the sending: there are four of those
// — a join, a round, a restore, the door — and a record signed only by the one somebody remembered
// to change arrives unsigned at the far end, where it is dropped without ceremony.
func TestThisMachinesOwnCompanionsLeaveSigned(t *testing.T) {
	cfg := shortDir(t)
	publishFake(t, cfg, "design", "s1", acceptSilently)

	mine := Mine(cfg, time.Now())
	if len(mine) != 1 {
		t.Fatalf("this machine published %d companions", len(mine))
	}
	if mine[0].By == "" || mine[0].Sig == "" {
		t.Fatal("a record left this machine unsigned, so nothing out there will accept it")
	}
	if !identity.VerifyBy(mine[0].By, mine[0].Sig, cluster.Signable(mine[0])) {
		t.Error("this machine's own signature does not check out")
	}
	// The key it signs with is the one it is admitted BY, or a peer would have admitted a
	// fingerprint that says nothing about the records arriving from that machine.
	id, err := identity.Load(cfg, Host())
	if err != nil {
		t.Fatal(err)
	}
	if identity.FingerprintOfKey(mine[0].By) != id.Fingerprint() {
		t.Errorf("records are signed by %s and the machine is admitted as %s",
			identity.FingerprintOfKey(mine[0].By), id.Fingerprint())
	}
}

// The check is at the door, not at the four call sites in front of it.
//
// LearnMembers is where every record from another machine ends up: an exchange, a round, a restore
// from a peer. A check anywhere else is a check somebody can walk around by adding a fifth caller.
func TestNothingUnvouchedForIsEverWrittenDown(t *testing.T) {
	cfg := t.TempDir()
	now := time.Now()
	if _, err := LearnMembers(cfg, []cluster.Member{
		{Host: "buildbox", Socket: "/s/ghost.sock", Name: "ghost", Seen: now},
	}, now); err != nil {
		t.Fatal(err)
	}
	if got := Elsewhere(cfg, now); len(got) != 0 {
		t.Fatalf("an unsigned record was written down: %v", names(got))
	}
	if b, err := os.ReadFile(clusterFile(cfg)); err == nil && strings.Contains(string(b), "ghost") {
		t.Errorf("the file holds a companion nobody vouched for:\n%s", b)
	}
}
