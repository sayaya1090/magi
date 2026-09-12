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
	"github.com/sayaya1090/magi/internal/shortdir"
	"github.com/sayaya1090/magi/internal/update"
)

// 되돌리기의 윈도우 모양. 유닉스 짝(update_rollback_live_test.go)은 복원될 빌드를 셸 스크립트로
// 세우므로 그 플랫폼에만 있고, 그 파일의 머리말이 「윈도우의 후계는 모양이 다르니 그 모양이 있는
// 자리에서 재야 한다」고 적어 두었다. 이 파일이 그 자리다.
//
// 여기서만 생기는 제약이 하나 있다: **도는 실행 파일은 덮을 수 없다.** 기동 중 되돌리기가 쓰는
// 대상은 방금 뜬 후보, 곧 이 프로세스가 지금 도는 그 이미지다.

// rolledBackWorld builds two magi that can be told apart by the version they publish.
type rolledBackWorld struct {
	cfg, ws, exe string
	prev         []byte
}

const prevVersion = "v0.0.1-prev"

func rollbackSetup(t *testing.T) rolledBackWorld {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary twice")
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
				if p, ferr := os.FindProcess(r.PID); ferr == nil {
					_ = p.Kill()
				}
			}
		}
	})

	exe := buildMagi(t, cfg) // the candidate: this build calls itself "dev"
	// The build it replaced, named so the record it publishes says which one came back. A rollback
	// that only changes bytes on disk proves nothing a person would notice; a daemon SERVING the
	// previous version is the claim.
	other, err := shortdir.Make("mgp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	prevExe := buildMagi(t, other,
		"-ldflags=-X github.com/sayaya1090/magi/internal/version.Version="+prevVersion)
	prev, err := os.ReadFile(prevExe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".prev", prev, 0o755); err != nil {
		t.Fatal(err)
	}
	return rolledBackWorld{cfg: cfg, ws: ws, exe: exe, prev: prev}
}

func (w rolledBackWorld) daemonCmd() *exec.Cmd {
	cmd := exec.Command(w.exe, "--daemon", "--no-update-check")
	cmd.Dir = w.ws
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+w.cfg, "MAGI_SOCKET_DIR="+w.cfg)
	return cmd
}

// say runs a generation to completion and gives back everything it printed.
//
// ⚠ **A file, not CombinedOutput.** A generation that rolls back does not merely exit — it starts a
// successor on the restored build, and that successor inherits these two handles. CombinedOutput
// waits for the pipes to close, so it would wait for the daemon that just came up to go away:
// measured as a ten-minute hang, and on unix the same test cannot see it because there the image is
// replaced rather than handed to a child. Bounded too — the claim is that this start does NOT settle
// in as a daemon on the bad build, and a version that did would otherwise stall the suite instead of
// failing here.
func (w rolledBackWorld) say(t *testing.T, cmd *exec.Cmd, name string, limit time.Duration) string {
	t.Helper()
	path := filepath.Join(w.cfg, name+".log")
	log, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
		_ = cmd.Process.Kill()
		<-done
		b, _ := os.ReadFile(path)
		t.Fatalf("%s: %v 안에 안 끝났다 — 되돌리지 않고 그 자리에 눌러앉았다:\n%s", name, limit, b)
	}
	b, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	return string(b)
}

// A candidate that came up and did NOT stay up is undone by the next start — on Windows, where that
// start has to put a file back over the image it is itself running from.
func TestABuildThatDoesNotStayUpIsRolledBackOnWindows(t *testing.T) {
	w := rollbackSetup(t)
	if err := update.Began(w.exe, update.Versions{From: prevVersion, To: "dev"}); err != nil {
		t.Fatal(err)
	}

	// Generation one comes up and is KILLED, not asked to stop: a signal it cannot handle is what a
	// build falling over looks like from outside, and the only way to leave the transaction open (a
	// deliberate stop notes itself — see LeftCleanly).
	one := w.daemonCmd()
	oneLog, err := os.Create(filepath.Join(w.cfg, "one.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer oneLog.Close()
	one.Stdout, one.Stderr = oneLog, oneLog
	if err := one.Start(); err != nil {
		t.Fatal(err)
	}
	sock := daemon.SocketPath(w.cfg, w.ws)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok && rec.PID == one.Process.Pid {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := one.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = one.Wait()

	// Generation two: the same candidate again, which is the evidence the first did not last.
	said := w.say(t, w.daemonCmd(), "two", 90*time.Second)
	if !strings.Contains(said, "did not stay up") {
		t.Errorf("넘어진 빌드에 대해 아무 말도 안 했다:\n%s", said)
	}

	// The file, and then the thing a person would actually notice: what is serving.
	on, rerr := os.ReadFile(w.exe)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, w.prev) {
		t.Errorf("디스크의 바이너리가 복원된 이전 빌드가 아니다 — 윈도우는 도는 이미지 위로 쓰지 못한다:\n%s", said)
	}
	if r := update.Refused(w.exe); r != "dev" {
		t.Errorf("넘어진 빌드가 거절로 기록되지 않았다: %q", r)
	}
	rec := waitVersion(t, sock, prevVersion, said)
	if rec.PID == 0 {
		t.Fatalf("되돌린 뒤 이전 빌드로 서비스하는 데몬이 없다:\n%s", said)
	}
}

// The other interruption: a replacement that was cut off before anything recorded it — a `.prev` and
// no journal, which is what a process or machine killed inside Commit leaves. The next start has to
// undo it BEFORE publishing, because the image it is running may be one that never finished its
// pre-flight.
func TestAnUnrecordedReplacementIsUndoneOnWindows(t *testing.T) {
	w := rollbackSetup(t)
	// No update.Began: that is the whole point — a backup with nothing beside it to explain it.

	said := w.say(t, w.daemonCmd(), "salvage", 90*time.Second)
	if !strings.Contains(said, "interrupted") {
		t.Errorf("끊긴 교체에 대해 아무 말도 안 했다:\n%s", said)
	}
	on, rerr := os.ReadFile(w.exe)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, w.prev) {
		t.Errorf("백업이 제자리로 안 돌아갔다:\n%s", said)
	}
	if rec := waitVersion(t, daemon.SocketPath(w.cfg, w.ws), prevVersion, said); rec.PID == 0 {
		t.Fatalf("되돌린 뒤 이전 빌드로 서비스하는 데몬이 없다:\n%s", said)
	}
}

// waitVersion waits for a daemon serving this socket that publishes the version asked for.
func waitVersion(t *testing.T, sock, want, said string) daemon.Info {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok {
			if rec.Version == want {
				return rec
			}
			last = rec.Version
		}
		time.Sleep(200 * time.Millisecond)
	}
	if last != "" {
		t.Errorf("서비스하는 데몬의 판이 %q 다 — 복원된 %q 가 아니다:\n%s", last, want, said)
	}
	return daemon.Info{}
}
