package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if code := exchangeMembers(strings.NewReader(stdin), &out, &errOut, cfgDir, nil); code != 0 {
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
	if code := exchangeMembers(strings.NewReader("not json at all"), &out, &errOut, cfg, nil); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !json.Valid(out.Bytes()) {
		t.Errorf("the answer is not JSON: %s", out.String())
	}
	if !strings.Contains(errOut.String(), "ignoring") {
		t.Errorf("nothing was said about the unreadable input: %q", errOut.String())
	}
}
