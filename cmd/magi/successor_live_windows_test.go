//go:build windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/procalive"
	"github.com/sayaya1090/magi/internal/shortdir"
	"github.com/sayaya1090/magi/internal/update"
)

// The defect, on the platform it lives on: CLIENT_LIFECYCLE §4 — "the previous generation leaves the
// moment Start() succeeds, so a successor that dies right after it starts, nobody sees."
//
// Staged exactly as an update leaves an install: the build that is running has been moved aside, the
// build it replaced is kept at .prev, the journal holds the transaction — and the candidate now at
// the install path is one that dies on its first line. Then the daemon is asked to restart, which is
// what the update loop does once it is idle.
//
// Before this change the daemon under test exited 0 within milliseconds and the companion was simply
// gone: no daemon, no rollback, and a log whose last line said it was restarting. Every assertion
// below is one of those three.
//
// The candidate that dies is this test binary, copied into place (diesAsEnv): a real bad release fails
// further in — a panic in a migration, a listener that will not bind — but "before it was serving"
// is the same fact to the process watching it, and this one is certain.
//
// It builds magi, so it is skipped under -short.
func TestASuccessorThatDiesOnItsFirstLineIsSeenAndUndone(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cfg, err := shortdir.Make("mgr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	t.Cleanup(func() {
		rows, _ := daemon.List(cfg)
		for _, r := range rows {
			if p, ferr := os.FindProcess(r.PID); ferr == nil && r.PID != 0 {
				_ = p.Kill()
			}
		}
	})

	// "dev" is what buildMagi's build calls itself — the staging update_rollback_live_test.go uses.
	const from, to = "dev", "v9.9.9-livetest"
	exe := buildMagi(t, cfg)
	good, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(cfg, "out.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd := exec.Command(exe, "--daemon", "--no-update-check")
	cmd.Dir = ws
	// The successor inherits this environment. The real build ignores diesAsEnv; the candidate —
	// this test binary, copied into place below — exits 7 on it before doing anything.
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg, diesAsEnv+"=7")
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	first := cmd.Process.Pid
	sock := daemon.SocketPath(cfg, ws)
	waitServing(t, sock, first, logPath)

	// The readiness rule, against a real daemon: its own pid in its own workspace is ready, and
	// nothing else is — not another pid, and not the same pid for a different tree.
	if !successorReady(sock, ws, "")(first) {
		t.Fatal("a daemon that is serving was not read as ready")
	}
	if successorReady(sock, ws, "")(first + 4) {
		t.Error("another pid was read as ready — a daemon that won the gap would pass for the successor")
	}
	if successorReady(sock, cfg, "")(first) {
		t.Error("ready for a workspace it is not serving")
	}
	if successorReady(sock, ws, "someone")(first) {
		t.Error("ready for a lineage it does not belong to")
	}

	// What Commit leaves on Windows: the running image cannot be overwritten, only moved.
	if err := os.Rename(exe, exe+".old"); err != nil {
		t.Fatal(err)
	}
	copyFile(t, os.Args[0], exe)
	if err := os.WriteFile(exe+".prev", good, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := update.Began(exe, update.Versions{From: from, To: to}); err != nil {
		t.Fatal(err)
	}

	c, err := daemon.DialWithin(sock, 2*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Restart(); err != nil {
		t.Fatal(err)
	}
	c.Close()

	ended := make(chan error, 1)
	go func() { ended <- cmd.Wait() }()
	select {
	case <-ended:
	case <-time.After(90 * time.Second):
		t.Fatalf("the previous generation never finished deciding\n%s", readLog(logPath))
	}
	said := readLog(logPath)

	// 1. Seen: the death is in the log, with its code.
	if !strings.Contains(said, "exited with code 7 before it was serving") {
		t.Fatalf("the successor's death was not reported:\n%s", said)
	}
	// 2. Undone: the build that works is back at the install path, and the candidate is refused.
	on, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(on, good) {
		t.Error("the previous build is not back on disk")
	}
	if r := update.Refused(exe); r != to {
		t.Errorf("the candidate was not refused, so the update loop walks into it again: %q", r)
	}
	// 3. Not gone: the previous build is serving again, as a new process.
	if code := cmd.ProcessState.ExitCode(); code != 0 {
		t.Errorf("the previous generation ended with %d after recovering\n%s", code, said)
	}
	rec, ok := serving(sock)
	if !ok {
		t.Fatalf("no daemon is serving after the recovery\n%s", said)
	}
	if rec.PID == first {
		t.Error("the record still names the generation that asked to restart")
	}
	if rec.Version != from {
		t.Errorf("the daemon serving is %q, want the previous build %q", rec.Version, from)
	}
	if alive, _ := procalive.Alive(first); alive {
		t.Error("the previous generation is still running after it decided")
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func waitServing(t *testing.T, sock string, pid int, logPath string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok && rec.PID == pid {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("the daemon never came up\n%s", readLog(logPath))
}

func readLog(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}
