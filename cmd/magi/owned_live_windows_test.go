//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// 소유 파이프가 수명을 쥔다는 주장을, 그 주장이 가장 중요한 플랫폼에서 실물로 잰다.
//
// 유닉스 짝은 owned_live_test.go 이고 첫 시험은 같은 것을 묻는다. 나머지 둘은 **윈도우에만 있는
// 물음**이다 — execve 가 없어 재기동이 프로세스를 갈아 끼우므로 「창이 쥔 파이프가 후계까지
// 따라가는가」가 생기고, 소유자가 스스로 닫지 못하고 죽는 경우(확장 호스트 강제 종료)에 운영체제가
// 대신 닫아 주는가가 생긴다. CLIENT_LIFECYCLE §2.5 는 이 셋을 「구현자가 실제 Windows 코어에서
// 확인했습니다」로 적고 있었다 — 보고이지, 이 저장소에서 도는 시험이 아니었다.

// ownedWorld is one temp config tree, one workspace, one built magi, and the cleanup that kills
// whatever is still running under it.
type ownedWorld struct {
	cfg, ws, exe, log string
}

func ownedSetup(t *testing.T) ownedWorld {
	t.Helper()
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
	// Registered before anything starts, and not dependent on this test getting far enough to learn
	// a pid — the unix file's reasoning, and it applies to any test that spawns one.
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
	return ownedWorld{cfg: cfg, ws: ws, exe: buildMagi(t, cfg), log: filepath.Join(cfg, "out.log")}
}

// startOwned starts an owned daemon whose stdin is a pipe THIS process holds the write end of, and
// returns that end. os.Pipe rather than cmd.StdinPipe on purpose: exec closes a StdinPipe when the
// child it made it for is waited on, so the moment this test waited for a predecessor the successor
// would lose its owner — the same trap CLIENT_LIFECYCLE §9.3 records for Node's ChildProcess.
func (w ownedWorld) startOwned(t *testing.T) (*exec.Cmd, *os.File) {
	t.Helper()
	r, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(w.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	cmd := exec.Command(w.exe, "--daemon", "--client-owned", "--no-update-check")
	cmd.Dir = w.ws
	cmd.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+w.cfg, "MAGI_SOCKET_DIR="+w.cfg)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r, log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r.Close()
	t.Cleanup(func() { write.Close() })
	return cmd, write
}

// published waits for a record on this workspace's socket that a handshake agrees with.
func (w ownedWorld) published(t *testing.T) daemon.Info {
	t.Helper()
	sock := daemon.SocketPath(w.cfg, w.ws)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok {
			return rec
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("소유 데몬이 안 떴다:\n%s", readLog(w.log))
	return daemon.Info{}
}

// running asks the exit code rather than whether the pid can be opened — procalive.Alive answers the
// second question on Windows, and an ended process stays openable while anyone holds a handle to it.
func running(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}

// goneWithin waits for the daemon at pid to end and its record to be cleared, and says how long it
// took. CLIENT_LIFECYCLE §5: five seconds from EOF, publication included — a record left behind is a
// daemon every client still believes in.
func (w ownedWorld) goneWithin(t *testing.T, pid int, limit time.Duration) {
	t.Helper()
	start := time.Now()
	for time.Since(start) < limit+5*time.Second {
		rows, _ := daemon.List(w.cfg)
		if !running(pid) && len(rows) == 0 {
			if took := time.Since(start); took > limit {
				t.Errorf("종료까지 %v — 계약은 %v 다", took, limit)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	rows, _ := daemon.List(w.cfg)
	t.Fatalf("파이프가 놓였는데 %v 가 지나도 살아 있다(기록 %d 개):\n%s",
		time.Since(start), len(rows), readLog(w.log))
}

// The claim, on Windows: an owned daemon lives exactly as long as the pipe its owner holds.
func TestAnOwnedDaemonDiesWithItsOwnersPipeOnWindows(t *testing.T) {
	w := ownedSetup(t)
	cmd, pipe := w.startOwned(t)

	in := w.published(t)
	// The lineage and the process, both published: a client reads these to tell "the daemon I own
	// updated itself" from "the daemon I own is gone" (§4).
	if in.Owner == "" {
		t.Error("소유 데몬인데 기록이 계보를 말하지 않는다")
	}
	if in.Instance == "" {
		t.Error("기록이 어느 프로세스인지 말하지 않는다")
	}

	// Alive while the pipe is held — otherwise this would pass on a daemon that came up and quit on
	// its own a moment later.
	time.Sleep(2 * time.Second)
	if !running(in.PID) {
		t.Fatalf("파이프를 쥐고 있는데 죽었다:\n%s", readLog(w.log))
	}

	pipe.Close()
	w.goneWithin(t, in.PID, 5*time.Second)
	_ = cmd.Wait()

	// The ending it prints must name the pipe: it unwinds down the same path as the socket's
	// `shutdown`, whose sentence would send a person looking for a client that does not exist.
	if said := readLog(w.log); !strings.Contains(said, "the owner closed its pipe") {
		t.Errorf("끝맺음이 파이프를 이름 대지 않는다:\n%s", said)
	}
}

// Windows only: the daemon that replaced itself is a DIFFERENT process, and the owner never touched
// it. The pipe has to have crossed, or an update quietly turns an owned companion into one nobody
// can stop — §4 says the read end and the lineage both go over, and this is that, measured.
func TestTheSuccessorOfAnOwnedDaemonIsStillOwned(t *testing.T) {
	w := ownedSetup(t)
	cmd, pipe := w.startOwned(t)
	first := w.published(t)

	sock := daemon.SocketPath(w.cfg, w.ws)
	c, err := daemon.DialWithin(sock, 2*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Restart(); err != nil {
		t.Fatal(err)
	}
	c.Close()

	// The predecessor now waits for the successor to be serving and then leaves; what is on the
	// socket afterwards is a process this test never started.
	var next daemon.Info
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok && rec.PID != first.PID {
			next = rec
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if next.PID == 0 {
		t.Fatalf("후계가 서비스를 시작하지 않았다:\n%s", readLog(w.log))
	}
	_ = cmd.Wait() // the predecessor; with our own pipe this does not close the owner's end
	if next.Owner != first.Owner {
		t.Errorf("계보가 교체에서 끊겼다: %q → %q — 창은 제 컴패니언이 사라졌다고 읽는다",
			first.Owner, next.Owner)
	}
	if next.Instance == first.Instance {
		t.Error("교체됐는데 실행 세대가 그대로다 — 클라이언트가 교체를 못 가린다")
	}
	// U06 의 앞 절반: **이전 자식의 exit 는 후계를 죽이지 않는다.** 한 번 보고 끝내면 늦게 오는
	// 죽음을 놓치므로 잠시 지켜본다 — 이 자리가 바로 `cmd.StdinPipe` 였다면 무너지는 곳이다
	// (exec 가 기다린 자식의 파이프를 닫아 버려 소유자의 쓰기 끝이 사라진다).
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if !running(next.PID) {
			t.Fatalf("이전 세대가 떠나자 후계가 따라 죽었다 — 소유 채널이 그 자식의 것이었다:\n%s",
				readLog(w.log))
		}
	}

	// And the owner's end still ends it, though the owner never spoke to this process.
	pipe.Close()
	w.goneWithin(t, next.PID, 5*time.Second)
}

// Windows only, and the case a graceful close cannot reach: the owner is KILLED — an extension host
// force-stopped — so nothing it would have done on the way out runs. Only the operating system
// closing its handles is left, and that has to be enough.
func TestKillingTheOwnerStopsTheDaemonItStarted(t *testing.T) {
	w := ownedSetup(t)

	owner := exec.Command(os.Args[0], "-test.run=^$")
	owner.Env = append(os.Environ(),
		ownsEnv+"="+w.exe,
		ownsWSEnv+"="+w.ws,
		ownsCfgEnv+"="+w.cfg,
		ownsLogEnv+"="+w.log)
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if owner.ProcessState == nil {
			_ = owner.Process.Kill()
		}
	})

	in := w.published(t)
	if in.Owner == "" {
		t.Error("소유 데몬인데 기록이 계보를 말하지 않는다")
	}
	if in.PID == owner.Process.Pid {
		t.Fatal("소유자 자신을 데몬으로 셌다")
	}

	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	// The daemon is nobody's child now — killing the owner does not kill it on Windows, there is no
	// job object in this arrangement. If it goes, it goes because the last write end went with its
	// owner.
	w.goneWithin(t, in.PID, 5*time.Second)
}
