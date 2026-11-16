//go:build !windows

package main

import (
	"path/filepath"
	"testing"
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
