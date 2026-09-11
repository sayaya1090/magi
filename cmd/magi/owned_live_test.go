//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// The claim, measured: an owned daemon lives exactly as long as the pipe its owner holds.
//
// Nothing in-process can check this. The mechanism is a property of the PIPE — who holds the write
// end, and what the operating system does when the last one closes — so the only way to know is to
// start one, hold the end, let go, and watch. The same reason the detach test beside this one
// starts a real process.
//
// It builds magi, so it is skipped under -short.
func TestAnOwnedDaemonDiesWithItsOwnersPipe(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cfg, err := shortdir.Make("mgo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	// Registered before anything starts and not dependent on this test getting far enough to learn
	// a pid — the neighbouring detach test's reasoning, and it applies to any test that spawns one.
	t.Cleanup(func() {
		rows, _ := daemon.List(cfg)
		for _, r := range rows {
			if r.PID != 0 {
				_ = syscall.Kill(r.PID, syscall.SIGTERM)
			}
		}
	})

	exe := buildMagi(t, cfg)

	cmd := exec.Command(exe, "--daemon", "--client-owned")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg)
	pipe, err := cmd.StdinPipe() // the owner's end, and the only thing here carrying authority
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(cfg, "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	var in daemon.Info
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := daemon.List(cfg)
		if len(rows) == 1 && rows[0].PID != 0 {
			in = rows[0]
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if in.PID == 0 {
		b, _ := os.ReadFile(filepath.Join(cfg, "out.log"))
		t.Fatalf("소유 데몬이 안 떴다:\n%s", b)
	}
	// The lineage and the process, both published. A client reads these to tell "the daemon I own
	// updated itself" from "the daemon I own is gone" — docs/CLIENT_LIFECYCLE §4.
	if in.Owner == "" {
		t.Error("소유 데몬인데 기록이 계보를 말하지 않는다")
	}
	if in.Instance == "" {
		t.Error("기록이 어느 프로세스인지 말하지 않는다")
	}

	// Alive while the pipe is held. Without this the test would pass on a daemon that never came up
	// properly and exited on its own a moment later.
	time.Sleep(2 * time.Second)
	if cmd.ProcessState != nil {
		t.Fatal("파이프를 쥐고 있는데 죽었다")
	}

	// And gone once it is let go. docs/CLIENT_LIFECYCLE §5: five seconds from EOF, including
	// clearing the published record — a socket file left behind is a daemon every client still
	// believes in.
	start := time.Now()
	if err := pipe.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("파이프를 닫았는데 10초가 지나도 안 끝났다 — 계약은 5초다")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("종료까지 %v — 계약은 5초다", took)
	}
	if rows, _ := daemon.List(cfg); len(rows) != 0 {
		t.Errorf("떠났는데 기록이 %d 개 남았다 — 클라이언트는 아직 있다고 믿는다", len(rows))
	}
	// The ending it prints must name the pipe. It unwinds down the SAME path as the socket's
	// `shutdown`, and that path's sentence says "asked to stop over the socket" — measured
	// 2026-09-11, printed for an owner nobody had asked through. A person reading that log goes
	// looking for a client that does not exist.
	b, _ := os.ReadFile(filepath.Join(cfg, "out.log"))
	if !strings.Contains(string(b), "the owner closed its pipe") {
		t.Errorf("끝맺음이 파이프를 이름 대지 않는다:\n%s", b)
	}
}
