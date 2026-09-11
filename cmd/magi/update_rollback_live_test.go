// The rollback that only a real process can prove. The journal's own tests (internal/update) move the
// record by calling the functions; this one kills a daemon and starts another, because the claim is
// about what SURVIVES a process — and the failure it guards against is precisely a build that answers
// `--version`, comes up, and then does not last.
//
// The restored build here is a shell script, so: unix only. The Windows successor is a different
// shape (no execve) and is measured where that shape exists.
//go:build !windows

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
	"github.com/sayaya1090/magi/internal/update"
)

func TestABuildThatDoesNotStayUpIsRolledBackOnTheNextStart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cfg, err := shortdir.Make("mgu")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	t.Cleanup(func() {
		rows, _ := daemon.List(cfg)
		for _, r := range rows {
			if r.PID != 0 {
				_ = syscall.Kill(r.PID, syscall.SIGTERM)
			}
		}
	})

	exe := buildMagi(t, cfg)
	// The state an update leaves behind: the candidate in place ("dev", what this build calls
	// itself), and the build it replaced beside it.
	previous := []byte("#!/bin/sh\necho 'the restored build is running'\nexit 0\n")
	if err := os.WriteFile(exe+".prev", previous, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := update.Began(exe, update.Versions{From: "v0.0.1", To: "dev"}); err != nil {
		t.Fatal(err)
	}

	// Generation one: it comes up — and is KILLED, not asked to stop. A signal it cannot handle is
	// how a build that falls over looks from the outside, and it is the only way to leave the
	// transaction open (a deliberate stop notes itself; see LeftCleanly).
	one := exec.Command(exe, "--daemon")
	one.Dir = ws
	one.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg)
	if err := one.Start(); err != nil {
		t.Fatal(err)
	}
	waitForDaemonUp(t, daemon.SocketPath(cfg, ws), one.Process)
	if err := one.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = one.Wait()

	// Generation two: the same candidate, started again. That is the evidence — the first one did
	// not last its window.
	// Bounded on purpose. The whole claim is that this start does NOT become a daemon — it rolls the
	// file back and continues onto the restored build, which exits. Without a deadline, a version of
	// the code that simply serves on the bad image would hang here until the suite's own timeout,
	// ten minutes later, and report as a stall rather than as the defect it is.
	tctx, tcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer tcancel()
	two := exec.CommandContext(tctx, exe, "--daemon")
	two.Dir = ws
	two.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg)
	out, _ := two.CombinedOutput()
	if tctx.Err() != nil {
		t.Fatalf("the start on a candidate that fell over kept serving instead of rolling back:\n%s", out)
	}
	said := string(out)

	if !strings.Contains(said, "did not stay up") {
		t.Errorf("the second start said nothing about the build that fell over:\n%s", said)
	}
	// Not just "the file changed" — the process CONTINUED onto the restored build. A rollback that
	// leaves the bad image running would satisfy a file check and nothing a user cares about.
	if !strings.Contains(said, "the restored build is running") {
		t.Errorf("it did not restart onto the restored build:\n%s", said)
	}
	on, rerr := os.ReadFile(exe)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, previous) {
		t.Error("the binary on disk is not the build that was restored")
	}
	if r := update.Refused(exe); r != "dev" {
		t.Errorf("the build that fell over was not recorded as refused: %q", r)
	}
}

// The other direction, and it is the one that would quietly undo good updates: a daemon stopped ON
// PURPOSE inside the window is not a build that fell over. Stopping a companion a minute after it
// updated is an ordinary thing to do, and without the clean-stop note the next start would read it
// as a crash and roll the update back.
func TestADeliberateStopInsideTheWindowIsNotARollback(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cfg, err := shortdir.Make("mgs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	t.Cleanup(func() {
		rows, _ := daemon.List(cfg)
		for _, r := range rows {
			if r.PID != 0 {
				_ = syscall.Kill(r.PID, syscall.SIGTERM)
			}
		}
	})

	exe := buildMagi(t, cfg)
	candidate, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".prev", []byte("#!/bin/sh\necho 'the restored build is running'\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := update.Began(exe, update.Versions{From: "v0.0.1", To: "dev"}); err != nil {
		t.Fatal(err)
	}
	sock := daemon.SocketPath(cfg, ws)

	// Up, then asked to stop the way a service is asked.
	one := exec.Command(exe, "--daemon")
	one.Dir = ws
	one.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg)
	if err := one.Start(); err != nil {
		t.Fatal(err)
	}
	waitForDaemonUp(t, sock, one.Process)
	if err := one.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = one.Wait()
	// Its socket goes with it, so the wait below cannot be satisfied by the one that just left.
	for i := 0; i < 100; i++ {
		if _, serr := os.Stat(sock); os.IsNotExist(serr) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The next start is on the same candidate, but the previous generation left on purpose.
	two := exec.Command(exe, "--daemon")
	two.Dir = ws
	two.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg)
	var said bytes.Buffer
	two.Stdout, two.Stderr = &said, &said
	if err := two.Start(); err != nil {
		t.Fatal(err)
	}
	// Watch for the daemon coming up AND for it leaving, because leaving is exactly the defect: a
	// start that rolls the update back exits instead of serving, and waiting only for a socket would
	// report that as "never came up" — true, and the wrong sentence to hand whoever reads it.
	//
	// ⚠ **`cmd.Wait`, not `Process.Wait`.** The buffer below is filled by the goroutines `exec` starts
	// to copy the child's output, and only `cmd.Wait` joins them. Reaping the PID directly leaves them
	// running, so reading the buffer races the copy — which is what `-race` said on CI (and only
	// there: a local run without `-race` passed every time).
	left := make(chan struct{})
	go func() { _ = two.Wait(); close(left) }()
	deadline := time.After(30 * time.Second)
	up := false
	for !up {
		select {
		case <-left:
			t.Fatalf("the start after a deliberate stop rolled a good update back instead of serving:\n%s", said.String())
		case <-deadline:
			_ = two.Process.Kill()
			t.Fatal("the daemon never came up, so nothing was on trial")
		default:
			if _, serr := os.Stat(sock); serr == nil {
				up = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	_ = two.Process.Signal(syscall.SIGTERM)
	<-left

	if strings.Contains(said.String(), "did not stay up") {
		t.Errorf("a deliberate shutdown was read as a build that could not stay up:\n%s", said.String())
	}
	on, rerr := os.ReadFile(exe)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, candidate) {
		t.Error("a good update was rolled back because somebody stopped the daemon")
	}
	if r := update.Refused(exe); r != "" {
		t.Errorf("a build nobody rejected was recorded as refused: %q", r)
	}
}

// waitForDaemonUp blocks until the daemon has published its socket, or fails the test.
func waitForDaemonUp(t *testing.T, sock string, p *os.Process) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if _, err := os.Stat(sock); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = p.Kill()
	t.Fatal("the daemon never came up, so nothing was on trial")
}

// A replacement interrupted before anything recorded it — a `.prev` and no journal, which is what a
// process or machine killed inside `update.Commit` leaves. The daemon has to undo it on the way up,
// BEFORE it publishes anything, because the binary it is running may be one that never finished its
// pre-flight.
func TestAnUnrecordedReplacementIsUndoneOnTheNextStart(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cfg, err := shortdir.Make("mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgj")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	t.Cleanup(func() {
		rows, _ := daemon.List(cfg)
		for _, r := range rows {
			if r.PID != 0 {
				_ = syscall.Kill(r.PID, syscall.SIGTERM)
			}
		}
	})

	exe := buildMagi(t, cfg)
	previous := []byte("#!/bin/sh\necho 'the restored build is running'\nexit 0\n")
	if err := os.WriteFile(exe+".prev", previous, 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliberately no journal: that absence IS the case under test.

	tctx, tcancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer tcancel()
	start := exec.CommandContext(tctx, exe, "--daemon")
	start.Dir = ws
	start.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg)
	out, _ := start.CombinedOutput()
	if tctx.Err() != nil {
		t.Fatalf("it served on a binary that may never have passed its pre-flight:\n%s", out)
	}
	said := string(out)

	if !strings.Contains(said, "interrupted before it was recorded") {
		t.Errorf("the start said nothing about the interrupted replacement:\n%s", said)
	}
	if !strings.Contains(said, "the restored build is running") {
		t.Errorf("it did not restart onto the restored build:\n%s", said)
	}
	on, rerr := os.ReadFile(exe)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, previous) {
		t.Error("the binary on disk is not the one that was put back")
	}
}
