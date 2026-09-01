// 이 파일은 유닉스에서만 컴파일된다. 안의 물음(어느 세션에 있나, 그 프로세스가 살아 있나)이
// Kill·Getsid 로만 물을 수 있는 것이라, 런타임 skip 으로는 늦다 — 윈도우에서는 시험이
// 건너뛰는 것이 아니라 **빌드가 깨진다**(`GOOS=windows go vet` 이 잡았다). 윈도우의 떼어내기는
// 잡 오브젝트 모양이고, 그것을 재는 자리는 그 플랫폼에 따로 있어야 한다.
//
// 세션을 묻는 것은 x/sys/unix 로 묻는다. `syscall.Getsid` 는 **darwin 에는 있고 linux 에는
// 없다** — 그래서 이 파일은 맥에서 멀쩡히 컴파일되고 CI(우분투)에서만 `go vet` 이 죽었다.
// 위 문단이 윈도우는 꼼꼼히 따졌는데 유닉스 안에서 갈리는 것은 못 봤다. 이 저장소가 이미
// 한 번 겪은 갈래다(같은 물음에 리눅스와 맥이 다른 답을 준 errno 건).
//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// The claim, measured: a detached daemon outlives the command that started it.
//
// Everything else about this feature can be checked in-process, and none of it would have caught
// the thing it exists for. The mechanism is a property of the PROCESS — whose session it is in,
// whose death reaches it — and the only way to know is to start one, let the starter exit, and ask
// the operating system where the daemon ended up.
//
// It builds magi, so it is skipped under -short.
func TestADetachedDaemonOutlivesItsStarter(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	// Short roots on both: a unix socket path caps near 100 bytes and the config dir is most of a
	// socket path on its own.
	cfg, err := os.MkdirTemp("/tmp", "mgd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := os.MkdirTemp("/tmp", "mgw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })

	// This test starts a process that is BUILT to outlive the thing that started it, which is
	// exactly how a suite ends up leaving daemons behind. So the sweep is registered before
	// anything is started and does not depend on the test getting far enough to learn a pid: it
	// asks the records under this temp config dir who is there, and stops them.
	t.Cleanup(func() {
		rows, _ := daemon.List(cfg)
		for _, r := range rows {
			if r.PID != 0 {
				_ = syscall.Kill(r.PID, syscall.SIGTERM)
			}
		}
	})

	exe := filepath.Join(cfg, "magi")
	if out, berr := exec.Command("go", "build", "-o", exe, "github.com/sayaya1090/magi/cmd/magi").CombinedOutput(); berr != nil {
		t.Fatalf("could not build magi: %v\n%s", berr, out)
	}

	start := exec.Command(exe, "--daemon", "--detach")
	start.Dir = ws
	start.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+cfg)
	out, serr := start.CombinedOutput()
	if serr != nil {
		t.Fatalf("--daemon --detach failed: %v\n%s", serr, out)
	}
	// The starter has EXITED — this is the moment the ordinary child would already be dead.
	sock := daemon.SocketPath(cfg, ws)
	pid := daemonPID(t, sock)

	if !strings.Contains(string(out), sock) {
		t.Errorf("the starter should name the socket it brought up, said: %s", out)
	}
	// Alive, with the starter gone.
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("the daemon did not outlive the command that started it: %v", err)
	}
	// And out of its starter's world: its own session, so a signal to that group cannot reach it.
	sid, serr2 := unix.Getsid(pid)
	if serr2 != nil {
		t.Fatalf("could not read the daemon's session: %v", serr2)
	}
	if mine, _ := unix.Getsid(os.Getpid()); sid == mine {
		t.Errorf("the daemon is still in this test's session (%d) — a group signal would take it", sid)
	}
	if sid != pid {
		t.Errorf("a detached daemon leads its own session; session is %d and it is %d", sid, pid)
	}
	// It is really serving, not merely running.
	c, derr := daemon.Dial(sock)
	if derr != nil {
		t.Fatalf("nothing answers on %s: %v", sock, derr)
	}
	c.Close()
}

// daemonPID reads the pid out of the published record the daemon writes for itself.
func daemonPID(t *testing.T, sock string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows, _ := daemon.List(filepath.Dir(sock))
		for _, r := range rows {
			if r.Socket == sock && r.PID != 0 {
				return r.PID
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no published record for %s (%s rows listed)", sock, strconv.Itoa(len(rows)))
		}
		time.Sleep(100 * time.Millisecond)
	}
}
