package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A daemon that cannot take the path says WHICH magi has it.
//
// The whole message used to be "another magi is starting or running on <path>", and the obvious
// next question had no answer in it. Measured twice in one session: the reader went to ps and
// lsof, could not match a process to the path, decided the lock was a leftover, and deleted the
// lock file — the one move the claim exists to prevent. flock lives on the inode, so deleting the
// file frees nothing and lets the next daemon lock a different one; two processes then hold "the"
// lock on one socket path.
func TestAClaimThatFailsSaysWhoHasIt(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mghold")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	path := filepath.Join(home, "d.sock")

	d, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()
	// The record a running daemon publishes. This process is the holder, so its own pid is the
	// honest thing to put in it.
	stop, err := Publish(path, "/some/where", "s1", Identity{Name: "held"})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	_, err = Listen(path)
	if err == nil {
		t.Fatal("a second daemon took a path another one is holding")
	}
	msg := err.Error()
	for _, want := range []string{strconv.Itoa(os.Getpid()), "/some/where", "magi --stop"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not mention %q, so there is no way to check it without "+
				"guessing at ps: %s", want, msg)
		}
	}
}

// With no record beside the socket, the refusal says the holder is probably still starting — and
// does not invite anybody to delete anything.
//
// A daemon publishes right after it claims, so the gap is a second or two wide. Reading "no record"
// as "no owner" is what the deleting starts from.
func TestAClaimWithNoRecordSaysTheHolderIsStarting(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgheld2")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	path := filepath.Join(home, "d.sock")

	d, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Stop()

	_, err = Listen(path)
	if err == nil {
		t.Fatal("a second daemon took a held path")
	}
	if !strings.Contains(err.Error(), "still starting") {
		t.Errorf("with no record the refusal reads %q, which says nothing about what to do next",
			err.Error())
	}
}

// A pid that is not running is reported as not running, and one that is is not reported as gone.
//
// The direction that matters is the second: a live holder called dead is what sends somebody to
// the lock file. Anything the check cannot answer comes back as "not known" rather than dead.
func TestProcessAliveDoesNotCallALiveProcessGone(t *testing.T) {
	if alive, known := processAlive(os.Getpid()); !known || !alive {
		t.Errorf("this very process reports alive=%v known=%v", alive, known)
	}
	if _, known := processAlive(0); known {
		t.Error("pid 0 came back as a knowable answer")
	}
	if _, known := processAlive(-1); known {
		t.Error("a negative pid came back as a knowable answer")
	}
}
