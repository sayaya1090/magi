package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/cluster"
)

func memberJSON(t *testing.T, ms []cluster.Member) string {
	t.Helper()
	b, err := json.Marshal(ms)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exchanged(t *testing.T, cfgDir, stdin string) []cluster.Member {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := exchangeMembers(strings.NewReader(stdin), &out, &errOut, cfgDir); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var ms []cluster.Member
	if err := json.Unmarshal(out.Bytes(), &ms); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	return ms
}

// One call is the whole transport, and it goes both ways.
//
// Joining and refreshing are the same act: a member list arrives on stdin, and what this machine
// knows leaves on stdout. Written as two calls they would drift, and the one that drifted would be
// the one nobody runs by hand.
func TestOneExchangeTeachesBothSides(t *testing.T) {
	cfg := t.TempDir()
	theirs := []cluster.Member{
		{Host: "buildbox", Socket: "/s/d.sock", Name: "design", Team: "frontend", Seen: time.Now()},
		{Host: "mini", Socket: "/s/o.sock", Name: "ops", Seen: time.Now()},
	}
	got := exchanged(t, cfg, memberJSON(t, theirs))

	names := map[string]bool{}
	for _, m := range got {
		names[m.Name] = true
	}
	if !names["design"] || !names["ops"] {
		t.Fatalf("what came back does not include what was sent: %+v", got)
	}
	// And it was written down, or a restart would leave this machine alone in a cluster it joined.
	b, err := os.ReadFile(filepath.Join(cfg, "cluster.json"))
	if err != nil {
		t.Fatalf("nothing was recorded: %v", err)
	}
	if !strings.Contains(string(b), "design") {
		t.Errorf("the file does not hold what was learned:\n%s", b)
	}
	// Asking again with nothing to say still answers — the other half of a symmetric exchange, and
	// what `magi --members` run by hand at a terminal does.
	if again := exchanged(t, cfg, ""); len(again) != len(got) {
		t.Errorf("a read-only exchange returned %d of %d", len(again), len(got))
	}
}

// Nothing this machine learns is written to any config.
//
// The whole point of membership over declaration is that it decays. A companion recorded in
// config.toml is a dependency: it never goes stale on its own, so a roster goes on offering
// somebody who has not run since March.
func TestJoiningWritesNoConfiguration(t *testing.T) {
	cfg := t.TempDir()
	exchanged(t, cfg, memberJSON(t, []cluster.Member{
		{Host: "buildbox", Socket: "/s/d.sock", Name: "design", Seen: time.Now()}}))

	if b, err := os.ReadFile(filepath.Join(cfg, "config.toml")); err == nil {
		t.Fatalf("a join wrote config.toml:\n%s", b)
	}
	entries, err := os.ReadDir(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".toml") {
			t.Errorf("a join wrote %s", e.Name())
		}
	}
}

// A member that nobody has seen for an hour leaves on the next exchange, without anybody deleting
// it. That is the difference between membership and configuration, made concrete.
func TestAForgottenMemberLeavesByItself(t *testing.T) {
	cfg := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	exchanged(t, cfg, memberJSON(t, []cluster.Member{
		{Host: "buildbox", Socket: "/s/d.sock", Name: "design", Seen: old},
		{Host: "mini", Socket: "/s/o.sock", Name: "ops", Seen: time.Now()}}))

	got := exchanged(t, cfg, "")
	for _, m := range got {
		if m.Name == "design" {
			t.Errorf("a member unseen for two hours is still in the cluster: %+v", m)
		}
	}
	if len(got) != 1 || got[0].Name != "ops" {
		t.Errorf("the recent one did not survive: %+v", got)
	}
}

// Rubbish on stdin is a caller that only wanted to ask, not a reason to answer nothing.
func TestSomethingUnreadableOnStdinStillGetsAnAnswer(t *testing.T) {
	cfg := t.TempDir()
	var out, errOut bytes.Buffer
	if code := exchangeMembers(strings.NewReader("not json at all"), &out, &errOut, cfg); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !json.Valid(out.Bytes()) {
		t.Errorf("the answer is not JSON: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "ignoring") {
		t.Errorf("nothing was said about the unreadable input: %q", errOut.String())
	}
}

// A round records what came back, which is the only way the third companion is ever heard of.
//
// This is the gap the pull exists to close, and it is worth stating as the scenario rather than as
// a property: A and B are in a cluster, C joins through A, and NOTHING in the join told B. Without
// a round, a cluster of three is two pairs — and the member missing from B's list is the newest
// one, which is the one somebody is most likely to be looking for.
func TestARoundTeachesThisMachineSomebodyItHadNeverMet(t *testing.T) {
	cfg := t.TempDir()
	seedMembers(t, cfg, []cluster.Member{
		{Host: "buildbox", Socket: "/s/b.sock", Name: "build", Seen: time.Now()},
	})
	trade := func(_ context.Context, host string, mine []cluster.Member) ([]cluster.Member, error) {
		return []cluster.Member{
			{Host: "mini", Socket: "/s/c.sock", Name: "third", Seen: time.Now()},
		}, nil
	}
	gossipRound(context.Background(), cfg, trade, nil, testRand(), map[string]bool{})
	if !hasName(daemon.Known(cfg, time.Now()), "third") {
		t.Fatal("a round with buildbox did not leave this machine knowing the companion it named")
	}
}

// One ssh per machine, not per companion.
//
// Several companions on one host answer out of one membership file, so asking it three times is the
// same answer twice over at two extra ssh handshakes — every minute, forever.
func TestOneMachineIsAskedOnceHoweverManyCompanionsItRuns(t *testing.T) {
	cfg := t.TempDir()
	seedMembers(t, cfg, []cluster.Member{
		{Host: "buildbox", Socket: "/s/a.sock", Name: "one", Seen: time.Now()},
		{Host: "buildbox", Socket: "/s/b.sock", Name: "two", Seen: time.Now()},
		{Host: "buildbox", Socket: "/s/c.sock", Name: "three", Seen: time.Now()},
	})
	var asked []string
	trade := func(_ context.Context, host string, _ []cluster.Member) ([]cluster.Member, error) {
		asked = append(asked, host)
		return nil, nil
	}
	gossipRound(context.Background(), cfg, trade, nil, testRand(), map[string]bool{})
	if len(asked) != 1 {
		t.Fatalf("three companions on one machine caused %d exchanges: %v", len(asked), asked)
	}
}

// A round never ssh's to the machine it is running on.
//
// It would be pointless at best — the answer is a file this process can read — and on most hosts it
// fails outright, because a machine that can ssh to itself is the exception.
func TestThisMachineIsNotOneOfTheHostsItReachesOutTo(t *testing.T) {
	here := daemon.Host()
	if here == "" {
		t.Skip("this machine cannot say its own name")
	}
	cfg := t.TempDir()
	seedMembers(t, cfg, []cluster.Member{{Host: here, Socket: "/s/x.sock", Name: "mine", Seen: time.Now()}})
	var asked []string
	trade := func(_ context.Context, host string, _ []cluster.Member) ([]cluster.Member, error) {
		asked = append(asked, host)
		return nil, nil
	}
	gossipRound(context.Background(), cfg, trade, nil, testRand(), map[string]bool{})
	if len(asked) != 0 {
		t.Fatalf("reached out to itself: %v", asked)
	}
}

// A machine that is down costs one log line, not one a minute.
//
// And the round goes on to the next host: one unreachable machine must not be able to stop this
// one from hearing about anybody else.
func TestAnUnreachableMachineIsSaidOnceAndDoesNotStopTheRound(t *testing.T) {
	cfg := t.TempDir()
	seedMembers(t, cfg, []cluster.Member{
		{Host: "gone", Socket: "/s/a.sock", Name: "one", Seen: time.Now()},
		{Host: "here2", Socket: "/s/b.sock", Name: "two", Seen: time.Now()},
	})
	reached := 0
	trade := func(_ context.Context, host string, _ []cluster.Member) ([]cluster.Member, error) {
		if host == "gone" {
			return nil, errors.New("connection refused")
		}
		reached++
		return nil, nil
	}
	var said []string
	warn := func(line string) { said = append(said, line) }
	quiet := map[string]bool{}
	for i := 0; i < 3; i++ {
		gossipRound(context.Background(), cfg, trade, warn, testRand(), quiet)
	}
	if reached != 3 {
		t.Fatalf("the reachable machine was traded with %d times out of 3", reached)
	}
	if len(said) != 1 {
		t.Fatalf("three rounds against a dead machine said %d things: %v", len(said), said)
	}
	if !strings.Contains(said[0], "gone") {
		t.Fatalf("the complaint does not name the machine: %q", said[0])
	}
}

// A big cluster costs the same round as a small one.
func TestARoundTalksToAFewMachinesHoweverManyThereAre(t *testing.T) {
	cfg := t.TempDir()
	var ms []cluster.Member
	for i := 0; i < 20; i++ {
		ms = append(ms, cluster.Member{
			Host:   fmt.Sprintf("host%02d", i),
			Socket: "/s/a.sock", Name: fmt.Sprintf("c%02d", i), Seen: time.Now(),
		})
	}
	seedMembers(t, cfg, ms)
	var asked []string
	trade := func(_ context.Context, host string, _ []cluster.Member) ([]cluster.Member, error) {
		asked = append(asked, host)
		return nil, nil
	}
	gossipRound(context.Background(), cfg, trade, nil, testRand(), map[string]bool{})
	if len(asked) != gossipFanout {
		t.Fatalf("twenty machines caused %d exchanges, want %d: %v", len(asked), gossipFanout, asked)
	}
}

// Every machine is eventually reached, which is what makes a fanout safe.
//
// Picking a few at random is only cheap if it is not also blind: a host that never comes up in the
// draw is a companion this machine would never hear from again.
func TestEveryMachineComesUpInTheDrawSoonEnough(t *testing.T) {
	cfg := t.TempDir()
	var ms []cluster.Member
	for i := 0; i < 8; i++ {
		ms = append(ms, cluster.Member{
			Host: fmt.Sprintf("host%d", i), Socket: "/s/a.sock", Seen: time.Now(),
		})
	}
	seedMembers(t, cfg, ms)
	got := map[string]bool{}
	trade := func(_ context.Context, host string, _ []cluster.Member) ([]cluster.Member, error) {
		got[host] = true
		return nil, nil
	}
	rng := testRand()
	for i := 0; i < 40; i++ { // forty minutes of rounds, against a one-hour memory
		gossipRound(context.Background(), cfg, trade, nil, rng, map[string]bool{})
	}
	if len(got) != 8 {
		t.Fatalf("only %d of 8 machines were ever reached: %v", len(got), got)
	}
}

// The spread is a spread, not a fixed offset dressed up as one.
func TestRoundsAreSpreadOutRatherThanInStep(t *testing.T) {
	rng := testRand()
	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		d := jitter(rng, gossipEvery)
		if d <= 0 || d > gossipEvery*2 {
			t.Fatalf("a wait of %v is not a jittered minute", d)
		}
		seen[d] = true
	}
	if len(seen) < 10 {
		t.Fatalf("fifty waits took %d distinct values — daemons started together would stay in step", len(seen))
	}
}

func seedMembers(t *testing.T, cfgDir string, ms []cluster.Member) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cfgDir, "cluster.json"), []byte(memberJSON(t, ms)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasName(ms []cluster.Member, name string) bool {
	for _, m := range ms {
		if m.Name == name {
			return true
		}
	}
	return false
}

func testRand() *rand.Rand { return rand.New(rand.NewSource(1)) }
