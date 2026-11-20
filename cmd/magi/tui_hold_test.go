//go:build !windows

package main

import (
	"path/filepath"
	"testing"
	"time"
)

// Recovery writes into logs, so a terminal recovers only while no other terminal here is alive: the
// second one to start finds the first's hold and recovers nothing. Once the first is gone — however
// it died, the kernel drops a flock — the next start is alone again.
func TestOnlyALoneTerminalMayRecover(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	release1, sole1, share1 := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	share1()
	release2, sole2, _ := holdTUI(sock)
	if sole2 {
		t.Fatal("a second terminal must not recover while the first is alive")
	}
	release2()
	release1()
	release3, sole3, _ := holdTUI(sock)
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
	release1, sole1, share1 := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	type hold struct {
		release func()
		sole    bool
	}
	second := make(chan hold, 1)
	go func() {
		r, s, _ := holdTUI(sock)
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
	release3, sole3, _ := holdTUI(sock)
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
	release1, sole1, share1 := holdTUI(sock)
	if !sole1 {
		t.Fatal("the first terminal is alone")
	}
	type hold struct {
		release func()
		sole    bool
	}
	second := make(chan hold, 1)
	go func() {
		r, s, _ := holdTUI(sock)
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
	release3, sole3, _ := holdTUI(sock)
	defer release3()
	if sole3 {
		t.Fatal("after a long recovery, a third terminal finds itself alone while the second still runs")
	}
}
