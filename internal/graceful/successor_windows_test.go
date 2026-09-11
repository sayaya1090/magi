//go:build windows

package graceful

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// 이 시험 바이너리 자신이 후계 노릇을 한다. 환경변수 하나로 무엇을 할지 받는다 — 곧장 죽기,
// 준비됐다고 적고 버티기, 아무 말 없이 버티기. 진짜 데몬을 띄우지 않는 것은 재는 것이 이전
// 세대의 판단이지 데몬의 기동이 아니기 때문이다.
const (
	childRole = "MAGI_GRACEFUL_CHILD"
	childMark = "MAGI_GRACEFUL_MARK"
)

func TestMain(m *testing.M) {
	switch os.Getenv(childRole) {
	case "":
		os.Exit(m.Run())
	case "die":
		os.Exit(3)
	case "ready":
		_ = os.WriteFile(os.Getenv(childMark), []byte(strconv.Itoa(os.Getpid())), 0o600)
		time.Sleep(time.Minute)
	case "hang":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

// successor starts this test binary as a successor through the real reexec, with exit stubbed so
// the decision to leave is visible and the test binary does not leave with it.
func successor(t *testing.T, role string, ready Ready) (left *int, err error) {
	t.Helper()
	was := exit
	t.Cleanup(func() { exit = was })
	code := -1
	left = &code
	exit = func(c int) { code = c }
	env := append(os.Environ(), childRole+"="+role, childMark+"="+filepath.Join(t.TempDir(), "ready"))
	return left, reexec(os.Args[0], []string{os.Args[0], "-test.run=^$"}, env, ready)
}

// running asks the exit code, not whether the pid can be opened.
//
// ⚠ procalive.Alive is not this question on Windows. It answers "can the process object be opened",
// and an ENDED process's object stays openable for as long as anybody holds a handle to it — here the
// very reexec under test holds one. A killed successor read as alive through that check: measured,
// the mutation that kills the slow successor survived it.
func running(t *testing.T, pid int) bool {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		t.Fatal(err)
	}
	const stillActive = 259
	return code == stillActive
}

func killLater(t *testing.T, pid int) {
	t.Cleanup(func() {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	})
}

// The defect: a successor that dies on its first line. This process used to be gone already; now it
// is still here, says which process died and how, and has not left.
func TestASuccessorThatDiesBeforeServingIsSeen(t *testing.T) {
	left, err := successor(t, "die", func(int) bool { return false })
	var died *SuccessorDied
	if !errors.As(err, &died) {
		t.Fatalf("a successor that died was not reported: %v", err)
	}
	if died.Code != 3 {
		t.Errorf("the exit code did not come through: %d", died.Code)
	}
	if *left != -1 {
		t.Fatalf("the previous generation left (exit %d) with its successor dead", *left)
	}
}

// Serving is what lets it go — not Start() succeeding.
func TestItLeavesOnceTheSuccessorIsServing(t *testing.T) {
	mark := filepath.Join(t.TempDir(), "ready")
	was := exit
	t.Cleanup(func() { exit = was })
	code := -1
	exit = func(c int) { code = c }
	var seen int
	ready := func(pid int) bool {
		b, err := os.ReadFile(mark)
		if err != nil {
			return false
		}
		seen, _ = strconv.Atoi(string(b))
		return seen == pid
	}
	env := append(os.Environ(), childRole+"=ready", childMark+"="+mark)
	err := reexec(os.Args[0], []string{os.Args[0], "-test.run=^$"}, env, ready)
	if seen != 0 {
		killLater(t, seen)
	}
	if err != nil {
		t.Fatalf("a successor that was serving was reported as a failure: %v", err)
	}
	if code != 0 {
		t.Fatalf("the previous generation did not leave once its successor was serving: exit %d", code)
	}
}

// Slow is not dead. The successor is left running — killing a freshly-replaced build because an
// antivirus held it for half a minute would turn a pause into a failed update.
func TestASlowSuccessorIsReportedAndLeftRunning(t *testing.T) {
	was := ReadyWithin
	ReadyWithin = 600 * time.Millisecond
	t.Cleanup(func() { ReadyWithin = was })

	left, err := successor(t, "hang", func(int) bool { return false })
	var slow *NotReady
	if !errors.As(err, &slow) {
		t.Fatalf("a successor that never became ready was not reported as such: %v", err)
	}
	killLater(t, slow.PID)
	if *left != -1 {
		t.Errorf("the decision to leave belongs to the caller here, not to reexec: exit %d", *left)
	}
	// Watched for a while, not asked once: a kill is not finished the instant it is issued.
	for end := time.Now().Add(700 * time.Millisecond); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if !running(t, slow.PID) {
			t.Fatal("the slow successor was killed")
		}
	}
}

// Peeking tells a held pipe from a let-go one without taking anything out of it.
func TestPipeClosedTellsAHeldPipeFromALetGoOne(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if closed, known := PipeClosed(r); closed || !known {
		t.Fatalf("a pipe whose writer is open: closed=%v known=%v", closed, known)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	// Data still waiting is not "let go" — and the peek must not have consumed it.
	if closed, known := PipeClosed(r); !known {
		t.Fatalf("lost the answer with data buffered: closed=%v", closed)
	}
	buf := make([]byte, 1)
	if n, _ := r.Read(buf); n != 1 || buf[0] != 'x' {
		t.Fatal("peeking took the byte the pipe was carrying")
	}
	if closed, known := PipeClosed(r); !closed || !known {
		t.Fatalf("a pipe with no writer left: closed=%v known=%v", closed, known)
	}
}

// Not a pipe is not an answer.
func TestPipeClosedOnAFileIsUnknown(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notapipe")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, known := PipeClosed(f); known {
		t.Fatal("claimed to know whether a plain file's writer let go")
	}
}
