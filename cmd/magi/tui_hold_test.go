//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Recovery writes into logs, so a terminal recovers only while no other terminal here is alive: the
// second one to start finds the first's hold and recovers nothing. Once the first is gone — however
// it died, the kernel drops a flock — the next start is alone again.
func TestOnlyALoneTerminalMayRecover(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	release1, sole1, share1, _ := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	share1()
	release2, sole2, _, _ := holdTUI(sock)
	if sole2 {
		t.Fatal("a second terminal must not recover while the first is alive")
	}
	release2()
	release1()
	release3, sole3, _, _ := holdTUI(sock)
	defer release3()
	if !sole3 {
		t.Fatal("with the others gone, a terminal is alone again")
	}
}

// A terminal started while another holds the hold exclusively (recovering, for a moment) waits and
// then holds it shared; it does not run unheld. Otherwise, once the first ends, a third terminal would
// find itself alone while the second is still running, and recover a turn the second is in.
func TestATerminalStartedDuringRecoveryStillHoldsItsPlace(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	release1, sole1, share1, _ := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	type hold struct {
		release func()
		sole    bool
	}
	second := make(chan hold, 1)
	go func() {
		r, s, _, _ := holdTUI(sock)
		second <- hold{r, s}
	}()
	time.Sleep(200 * time.Millisecond)
	share1()
	var h2 hold
	select {
	case h2 = <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("the second terminal never got its place")
	}
	defer h2.release()
	if h2.sole {
		t.Fatal("the second terminal thinks it is alone")
	}
	release1()
	release3, sole3, _, _ := holdTUI(sock)
	defer release3()
	if sole3 {
		t.Fatal("a third terminal finds itself alone while the second still runs")
	}
}

// A recovery that runs longer than the wait is no reason to run without a place: the second terminal
// says why it waits and keeps waiting, and once the first is gone a third still sees the second.
func TestATerminalWaitsOutALongRecoveryAndStillHoldsItsPlace(t *testing.T) {
	defer func(w time.Duration) { tuiShareWait = w }(tuiShareWait)
	tuiShareWait = 20 * time.Millisecond
	sock := filepath.Join(t.TempDir(), "d.sock")
	release1, sole1, share1, _ := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	type hold struct {
		release func()
		sole    bool
	}
	second := make(chan hold, 1)
	go func() {
		r, s, _, _ := holdTUI(sock)
		second <- hold{r, s}
	}()
	time.Sleep(300 * time.Millisecond) // well past the wait
	share1()
	var h2 hold
	select {
	case h2 = <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("the second terminal never got its place")
	}
	defer h2.release()
	if h2.sole {
		t.Fatal("the second terminal thinks it is alone")
	}
	release1()
	release3, sole3, _, _ := holdTUI(sock)
	defer release3()
	if sole3 {
		t.Fatal("after a long recovery, a third terminal finds itself alone while the second still runs")
	}
}

// A lock that can never be had (a filesystem without flock) is not another terminal recovering:
// waiting would never end. The terminal says why and starts without recovering.
func TestATerminalThatCannotLockStartsWithoutRecoveringInsteadOfWaiting(t *testing.T) {
	defer func(f func(*os.File, int) error, w interface{ Write([]byte) (int, error) }) {
		tuiFlock, tuiStderr = f, w
	}(tuiFlock, tuiStderr)
	tuiFlock = func(*os.File, int) error { return unix.EOPNOTSUPP }
	var said bytes.Buffer
	tuiStderr = &said
	sock := filepath.Join(t.TempDir(), "d.sock")
	type hold struct {
		release func()
		sole    bool
	}
	got := make(chan hold, 1)
	go func() {
		r, s, _, _ := holdTUI(sock)
		got <- hold{r, s}
	}()
	select {
	case h := <-got:
		h.release()
		if h.sole {
			t.Fatal("a terminal that cannot lock must not think it is alone")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a terminal that cannot lock waited as if another were recovering")
	}
	if msg := said.String(); !strings.Contains(msg, unix.EOPNOTSUPP.Error()) || strings.Contains(msg, "still recovering") {
		t.Fatalf("the note must name the cause, not another terminal: %q", msg)
	}
}

// A shortage of locks (ENOLCK) can pass while terminals run, so it is waited out like another
// terminal's recovery: a second terminal that ran unheld through one would be invisible to a third
// that finds the lock again once the first is gone.
func TestATerminalWaitsOutALockShortageAndStillHoldsItsPlace(t *testing.T) {
	defer func(f func(*os.File, int) error, w interface{ Write([]byte) (int, error) }) {
		tuiFlock, tuiStderr = f, w
	}(tuiFlock, tuiStderr)
	var said bytes.Buffer
	tuiStderr = &said
	sock := filepath.Join(t.TempDir(), "d.sock")
	release1, sole1, share1, _ := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	share1()
	var n int32
	tuiFlock = func(f *os.File, how int) error {
		if atomic.AddInt32(&n, 1) <= 3 {
			return unix.ENOLCK
		}
		return unix.Flock(int(f.Fd()), how)
	}
	type hold struct {
		release func()
		sole    bool
	}
	second := make(chan hold, 1)
	go func() {
		r, s, _, _ := holdTUI(sock)
		second <- hold{r, s}
	}()
	var h2 hold
	select {
	case h2 = <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("the second terminal never got its place after the shortage passed")
	}
	defer h2.release()
	if h2.sole {
		t.Fatal("the second terminal thinks it is alone")
	}
	tuiFlock = func(f *os.File, how int) error { return unix.Flock(int(f.Fd()), how) }
	release1()
	release3, sole3, _, _ := holdTUI(sock)
	defer release3()
	if sole3 {
		t.Fatal("after a lock shortage, a third terminal finds itself alone while the second still runs")
	}
}

func realFlock(f *os.File, how int) error { return unix.Flock(int(f.Fd()), how) }

// A lock failure that is neither contention, a shortage nor a known lack of flock (an I/O error) is
// no reason to run unseen: the terminal does not start, and says why.
func TestATerminalRefusesToStartOnALockFailureItCannotPlace(t *testing.T) {
	defer func(f func(*os.File, int) error, w interface{ Write([]byte) (int, error) }) {
		tuiFlock, tuiStderr = f, w
	}(tuiFlock, tuiStderr)
	tuiFlock = func(*os.File, int) error { return unix.EIO }
	tuiStderr = &bytes.Buffer{}
	sock := filepath.Join(t.TempDir(), "d.sock")
	done := make(chan error, 1)
	go func() {
		release, sole, _, err := holdTUI(sock)
		release()
		if sole {
			err = errors.New("sole")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, unix.EIO) {
			t.Fatalf("an unplaced lock failure must stop the start, naming it: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an unplaced lock failure was waited on")
	}
}

// A terminal that cannot even open its hold file would run unseen by every other terminal: it does
// not start.
func TestATerminalRefusesToStartWhenItsHoldFileCannotBeOpened(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "x")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	release, sole, _, err := holdTUI(filepath.Join(blocker, "d.sock"))
	release()
	if err == nil || sole {
		t.Fatalf("a hold file that cannot be made must stop the start (err=%v sole=%v)", err, sole)
	}
}

// flock(2) turns an exclusive lock into a shared one by letting go and taking it again. A terminal
// that starts in that gap must not find itself alone and recover while the first one runs.
func TestATerminalStartedInTheHandoverGapIsNotAlone(t *testing.T) {
	defer func(f func(*os.File, int) error) { tuiFlock = f }(tuiFlock)
	sock := filepath.Join(t.TempDir(), "d.sock")
	type hold struct {
		release func()
		sole    bool
	}
	probe := make(chan hold, 1)
	var fired int32
	tuiFlock = func(f *os.File, how int) error {
		if how == unix.LOCK_SH|unix.LOCK_NB && strings.HasSuffix(f.Name(), ".tui.lock") && atomic.CompareAndSwapInt32(&fired, 0, 1) {
			// What the kernel does inside the conversion: the exclusive lock is gone for a moment.
			_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
			go func() {
				r, s, _, _ := holdTUI(sock)
				probe <- hold{r, s}
			}()
			time.Sleep(200 * time.Millisecond)
		}
		return realFlock(f, how)
	}
	release1, sole1, share1, err := holdTUI(sock)
	if err != nil || !sole1 {
		t.Fatalf("the first terminal is alone (err=%v)", err)
	}
	defer release1()
	share1()
	select {
	case h := <-probe:
		defer h.release()
		if h.sole {
			t.Fatal("a terminal started in the handover gap thinks it is alone and would recover")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal started in the gap never got its place")
	}
}
