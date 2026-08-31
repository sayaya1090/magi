package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The successor must not be told to detach again, or it detaches, and so on.
func TestTheSuccessorIsNotToldToDetach(t *testing.T) {
	for _, tc := range []struct {
		in   []string
		want []string
	}{
		{[]string{"--daemon", "--detach"}, []string{"--daemon"}},
		{[]string{"-detach", "-daemon"}, []string{"-daemon"}},
		{[]string{"--detach=true", "--daemon"}, []string{"--daemon"}},
		{[]string{"-detach=false", "--daemon"}, []string{"--daemon"}},
		// A word that merely contains it, or is a value rather than a flag, stays.
		{[]string{"--daemon", "--task", "detach the thing"}, []string{"--daemon", "--task", "detach the thing"}},
		{[]string{"--daemon", "--detach-nothing"}, []string{"--daemon", "--detach-nothing"}},
	} {
		got := argvWithoutDetach(tc.in)
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("argvWithoutDetach(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The attributes are the whole mechanism: without them this is an ordinary child that dies with
// its parent, and the command would report a success that lasts as long as the caller does. What
// exactly was asked for is platform-shaped, so each platform asserts its own (detach_attr_*_test).
func TestSomethingIsAskedForAtAll(t *testing.T) {
	if detachAttr() == nil {
		t.Fatal("nothing was asked for — the child would stay in the caller's group")
	}
}

// A daemon that dies before it listens must be reported as dead, with what it said.
//
// The alternative is what a naive waiter does: wait the full bound and report a timeout. The
// workspace was already claimed, or the socket path was too long — the daemon wrote the reason and
// exited in milliseconds, and the caller is told the wrong thing thirty seconds later.
func TestAStillbornDaemonIsReportedWithItsOwnWords(t *testing.T) {
	// Runs everywhere: the thing it spawns is this test binary, told to run one helper and exit.
	dir := t.TempDir()
	sock := filepath.Join(dir, "daemon-x.sock")
	// Nothing will ever listen: the executable this spawns is magi's own test binary with
	// arguments that make it exit at once.
	t.Setenv("MAGI_DETACH_TEST_EXIT", "1")
	var out bytes.Buffer
	was := detachWait
	detachWait = 3 * time.Second
	defer func() { detachWait = was }()

	code := startDetached(sock, []string{"-test.run", "TestDetachHelperExits", "-test.v=false"},
		func(string) error { return errors.New("nobody is listening") }, &out)
	if code == 0 {
		t.Fatal("a daemon that never listened must not be reported as up")
	}
	if !strings.Contains(out.String(), "exited before it was listening") {
		t.Errorf("the report must say it died, not that it timed out: %q", out.String())
	}
	if took := out.String(); strings.Contains(took, "after 3s") {
		t.Error("it waited the whole bound for a process that was already gone")
	}
}

// TestDetachHelperExits is the stillborn daemon: the test binary re-runs itself with this name and
// this environment, writes a line, and exits.
func TestDetachHelperExits(t *testing.T) {
	if os.Getenv("MAGI_DETACH_TEST_EXIT") == "" {
		t.Skip("only runs as the spawned child of the test above")
	}
	os.Stderr.WriteString("magi: this workspace is already claimed\n")
	os.Exit(1)
}
