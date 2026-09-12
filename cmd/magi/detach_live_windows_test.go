//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// 떼어내기의 윈도우 쪽 주장들을 실물로 잰다. 지금까지 이 자리에 있던 것은 `detachAttr` 의 플래그를
// 세어 보는 단위 시험뿐이었다 — 플래그를 물어본 것이지, **운영체제가 그래서 무엇을 했는지**를 잰
// 것이 아니다. 그 차이가 2026-09-06 에 검은 `magi.exe` 콘솔 창으로 나타났다: 플래그는 그대로였고,
// 콘솔 없는 부모가 콘솔 프로그램을 띄우면 윈도우가 **새 콘솔을 만들어 준다**는 쪽이 빠져 있었다.
//
// 유닉스 짝은 detach_live_test.go 이고 첫 시험만 같은 물음이다(세션 대신 프로세스 생존으로).

const (
	consoleOfEnv = "MAGI_TEST_CONSOLE_OF"
	gateEnv      = "MAGI_TEST_GATE" // the file whose appearance releases the gated role
	gateExeEnv   = "MAGI_TEST_GATE_EXE"
	gateWSEnv    = "MAGI_TEST_GATE_WS"
	gateCfgEnv   = "MAGI_TEST_GATE_CFG"
)

func init() { testRoles = append(testRoles, consoleProbeRole, gatedStartRole) }

// gatedStartRole is a process that starts a detached daemon, but not until it is told to.
//
// The waiting is the point: the test has to get this process INTO a job object before it spawns
// anything, or the daemon it spawns may exist before the job does and the test would be measuring
// nothing. Its exit code is the start command's.
func gatedStartRole() {
	gate := os.Getenv(gateEnv)
	if gate == "" {
		return
	}
	for i := 0; i < 600; i++ {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd := exec.Command(os.Getenv(gateExeEnv), "--daemon", "--detach", "--no-update-check")
	cmd.Dir = os.Getenv(gateWSEnv)
	cmd.Env = append(os.Environ(),
		gateEnv+"=", // the daemon must not take this role itself
		"MAGI_CONFIG_DIR="+os.Getenv(gateCfgEnv),
		"MAGI_SOCKET_DIR="+os.Getenv(gateCfgEnv))
	if err := cmd.Run(); err != nil {
		os.Exit(13)
	}
	os.Exit(0)
}

var (
	procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	procFreeConsole   = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")
)

// consoleProbeRole answers, from a process of its own, whether another process owns a console.
//
// It cannot be asked from the test process: AttachConsole refuses when the caller already has one,
// and `go test` may well be run from a terminal. Dropping the test's own console to ask would change
// what every later test in this binary is running in. So a separate process drops ITS console and
// attaches to the target's — the only answer the operating system gives about somebody else's
// console — and says which it was in its exit code.
func consoleProbeRole() {
	raw := os.Getenv(consoleOfEnv)
	if raw == "" {
		return
	}
	pid, err := strconv.Atoi(raw)
	if err != nil {
		os.Exit(9)
	}
	procFreeConsole.Call() // this process may have inherited one; attaching needs none
	ok, _, callErr := procAttachConsole.Call(uintptr(pid))
	switch {
	case ok != 0:
		os.Exit(consoleYes)
	case errors.Is(callErr, windows.ERROR_INVALID_HANDLE):
		// The documented answer for "that process has no console attached".
		os.Exit(consoleNo)
	case errors.Is(callErr, windows.ERROR_INVALID_PARAMETER):
		os.Exit(consoleGone)
	default:
		os.Exit(9)
	}
}

const (
	consoleYes  = 0
	consoleNo   = 3
	consoleGone = 4
)

// hasConsole asks the role above about pid.
func hasConsole(t *testing.T, pid int) bool {
	t.Helper()
	probe := exec.Command(os.Args[0], "-test.run=^$")
	probe.Env = append(os.Environ(), consoleOfEnv+"="+strconv.Itoa(pid))
	err := probe.Run()
	code := 0
	if probe.ProcessState != nil {
		code = probe.ProcessState.ExitCode()
	}
	switch code {
	case consoleYes:
		return true
	case consoleNo:
		return false
	case consoleGone:
		t.Fatalf("콘솔을 물어볼 프로세스(%d)가 이미 없다", pid)
	}
	t.Fatalf("콘솔을 묻지 못했다: 종료 코드 %d (%v)", code, err)
	return false
}

// detachWorld is a temp config tree, a workspace and a built magi, with the sweep registered first.
type detachWorld struct{ cfg, ws, exe string }

func detachSetup(t *testing.T) detachWorld {
	t.Helper()
	if testing.Short() {
		t.Skip("builds the binary")
	}
	cfg, err := shortdir.Make("mgd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	ws, err := shortdir.Make("mgw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
	// This test starts processes BUILT to outlive whoever started them, which is exactly how a suite
	// leaves daemons behind. Registered before anything starts, and not dependent on the test getting
	// far enough to learn a pid — the unix file's reasoning.
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
	return detachWorld{cfg: cfg, ws: ws, exe: buildMagi(t, cfg)}
}

// start runs `magi --daemon --detach` and waits for that command to finish, which is the whole point
// of the flag: the thing you ran exits and the daemon stays. Returns the record of what is serving.
func (w detachWorld) start(t *testing.T, extra ...string) daemon.Info {
	t.Helper()
	args := append([]string{"--daemon", "--detach", "--no-update-check"}, extra...)
	starter := exec.Command(w.exe, args...)
	starter.Dir = w.ws
	starter.Env = append(os.Environ(), "MAGI_CONFIG_DIR="+w.cfg, "MAGI_SOCKET_DIR="+w.cfg)
	out, err := starter.CombinedOutput()
	if err != nil {
		t.Fatalf("떼어내기 기동이 실패했다: %v\n%s", err, out)
	}
	rec := w.serving(t)
	if rec.PID == starter.ProcessState.Pid() {
		t.Fatal("떼어냈다면서 명령 그 자신이 데몬으로 남았다")
	}
	return rec
}

func (w detachWorld) serving(t *testing.T) daemon.Info {
	t.Helper()
	sock := daemon.SocketPath(w.cfg, w.ws)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok {
			return rec
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("떼어낸 데몬이 안 떴다:\n%s", w.startLog())
	return daemon.Info{}
}

// startLog is where a detached daemon's own words go — beside the socket, by detachLogSuffix.
func (w detachWorld) startLog() string {
	b, _ := os.ReadFile(daemon.SocketPath(w.cfg, w.ws) + detachLogSuffix)
	return string(b)
}

// The claim the unix file measures with sessions, measured here with processes: the command you ran
// is gone and the companion is still serving.
func TestADetachedDaemonOutlivesItsStarterOnWindows(t *testing.T) {
	w := detachSetup(t)
	rec := w.start(t)
	if !running(rec.PID) {
		t.Fatal("기동 명령이 끝나자 데몬도 없다")
	}
	// And it is reachable, not merely alive: a process that survived without its socket is not a
	// companion anybody can use.
	if _, ok := serving(daemon.SocketPath(w.cfg, w.ws)); !ok {
		t.Errorf("살아 있지만 아무도 못 부른다:\n%s", w.startLog())
	}
}

// The black window, from the front: a detached daemon must own no console at all.
func TestADetachedDaemonHasNoConsole(t *testing.T) {
	w := detachSetup(t)

	// ⚠ **The probe has to be able to say yes**, or "no console" is what this test would report about
	// anything at all, including a daemon sitting behind a black window. CREATE_NO_WINDOW is a
	// process that HAS a console and shows no window for it — exactly the distinction being made.
	control := exec.Command(os.Args[0], "-test.run=^$")
	control.Env = append(os.Environ(), gateEnv+"="+filepath.Join(w.cfg, "never-appears"))
	control.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := control.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Process.Kill() })
	if !hasConsole(t, control.Process.Pid) {
		t.Fatal("탐침이 콘솔을 가진 프로세스도 없다고 한다 — 이 시험은 아무것도 못 잰다")
	}

	rec := w.start(t)
	if hasConsole(t, rec.PID) {
		t.Error("떼어낸 데몬이 콘솔을 쥐고 있다 — 작업 표시줄에 검은 창이 남는 그 상태다")
	}
}

// The black window, from the side it actually appeared on (2026-09-06): the daemon restarts itself
// onto a new build, and a console program started by a console-LESS parent is handed a brand-new
// visible console by Windows unless DETACHED_PROCESS is asked for again. The flags test next door
// cannot see this — it reads the flags this repository passes, not what the operating system did
// with them.
func TestTheSuccessorOfADetachedDaemonHasNoConsoleEither(t *testing.T) {
	w := detachSetup(t)
	first := w.start(t)
	if hasConsole(t, first.PID) {
		t.Fatal("전제가 깨졌다: 첫 세대부터 콘솔을 쥐고 있다")
	}

	sock := daemon.SocketPath(w.cfg, w.ws)
	c, err := daemon.DialWithin(sock, 2*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Restart(); err != nil {
		t.Fatal(err)
	}
	c.Close()

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
		t.Fatalf("후계가 서비스를 시작하지 않았다:\n%s", w.startLog())
	}
	if hasConsole(t, next.PID) {
		t.Error("후계가 콘솔을 쥐고 있다 — 재기동마다 검은 창이 하나씩 남는다(2026-09-06)")
	}
}

// The flag that matters for an IDE: an editor runs inside a job object, and closing that job kills
// everything in it. CREATE_BREAKAWAY_FROM_JOB is what keeps the companion out of that, and nothing
// here measured it — the unit test beside this one only asks whether the bit was set.
func TestADetachedDaemonSurvivesTheJobItWasStartedIn(t *testing.T) {
	w := detachSetup(t)

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(job)
	// An editor's job is this shape: everything in it dies with it, and children may break away.
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags =
		windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		t.Fatal(err)
	}

	// The starter waits for a file before it starts anything, so it is in the job BEFORE the daemon
	// it spawns exists — assigning afterwards would be a race this test could pass by luck.
	gate := filepath.Join(w.cfg, "go")
	inJob := exec.Command(os.Args[0], "-test.run=^$")
	inJob.Env = append(os.Environ(),
		gateEnv+"="+gate,
		gateExeEnv+"="+w.exe,
		gateWSEnv+"="+w.ws,
		gateCfgEnv+"="+w.cfg)
	if err := inJob.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if inJob.ProcessState == nil {
			_ = inJob.Process.Kill()
		}
	})
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false,
		uint32(inJob.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := inJob.Wait(); err != nil {
		t.Fatalf("잡 안의 기동이 실패했다: %v\n%s", err, w.startLog())
	}

	rec := w.serving(t)
	// Closing the job kills every process still inside it. The daemon asked to leave; this is where
	// that either happened or did not.
	if err := windows.TerminateJobObject(job, 1); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
		if !running(rec.PID) {
			t.Fatalf("잡이 닫히자 컴패니언이 같이 죽었다 — IDE 가 데몬을 데려가는 그 결함이다:\n%s",
				w.startLog())
		}
	}
	if _, ok := serving(daemon.SocketPath(w.cfg, w.ws)); !ok {
		t.Error("잡이 닫힌 뒤 살아 있지만 아무도 못 부른다")
	}
}
