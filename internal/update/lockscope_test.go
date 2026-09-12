package update

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for a magi install.
//
// The update path runs the binary it is about (`Verify` shells out to `<target> --version`), so
// measuring it needs a real, runnable executable at that path — and the shell scripts the rest of
// this package uses are POSIX-only, which is why `journal_test.go` and `rollback_test.go` carry
// `!windows` and the whole transaction has no coverage on the one platform where replacing a file in
// use is hard. A copy of this binary is a runnable stand-in everywhere.
//
// Keyed on the ARGUMENT, not on an environment variable: the process that performs the update passes
// its whole environment to the pre-flight it spawns, so an env switch would turn that process into a
// stub as well and it would print a version instead of doing the work.
//
// Checked BEFORE `m.Run`, because `--version` is not a flag the testing package knows and it would
// exit 2 on the parse.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		if d, err := time.ParseDuration(os.Getenv("MAGI_STUB_SLEEP")); err == nil {
			time.Sleep(d)
		}
		fmt.Println("magi", stubVersion())
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func stubVersion() string {
	if v := os.Getenv("MAGI_STUB_VERSION"); v != "" {
		return v
	}
	return "v0.0.0-stub"
}

// exeName is the file name an install must have to be RUNNABLE.
//
// ⚠ **Windows resolves a command through its extension list, so a file called `magi-install` is not
// an executable there at all** — the pre-flight fails with "executable file not found in %PATH%"
// for a file it is looking straight at. Measured while writing this test: without the suffix the
// child's Commit rolled itself back and the window under test never happened.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// selfAs copies this test binary to path, so the update path has something it can actually run.
func selfAs(t *testing.T, path string) []byte {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Skipf("no test binary path to copy: %v", err)
	}
	b, err := os.ReadFile(self)
	if err != nil {
		t.Skipf("cannot read the test binary: %v", err)
	}
	if err := os.WriteFile(path, b, 0o755); err != nil {
		t.Fatal(err)
	}
	return b
}

// The child half: hold the install lock in a real Commit while the parent tries to salvage.
func TestCommitInAChildProcess(t *testing.T) {
	target := os.Getenv("MAGI_TEST_COMMIT_TARGET")
	if target == "" {
		t.Skip("the child half of TestSalvageDoesNotRobALiveCommit")
	}
	b, err := os.ReadFile(os.Getenv("MAGI_TEST_COMMIT_SOURCE"))
	if err != nil {
		t.Fatal(err)
	}
	// Same bytes as the target would be idempotent and never write a .prev, so make it differ.
	if err := Commit(append(b, '\n'), target, Versions{From: "v1.0.0", To: "v2.0.0"}); err != nil {
		t.Fatalf("child commit: %v", err)
	}
}

// A `.prev` beside a binary means two opposite things, and only the install lock tells them apart.
//
// ⚠ **`Salvage` robbed a live `Commit`.** Commit writes the backup, replaces the binary, pre-flights
// it, and only THEN writes the journal — so for the whole length of the pre-flight there is a `.prev`
// on disk and no record beside it. That is exactly the state Salvage is built to act on: "a
// replacement interrupted before it was recorded, put the backup back". A second daemon starting in
// that window restores the OLD build over the new one and deletes the backup, so the update is undone
// mid-flight and the rollback source the transaction depends on is gone as well.
//
// The lock existed and was held only across `Commit` (review R10): `Salvage`, `Resume`, `Confirm`,
// `Retry` and `LeftCleanly` took nothing, so the one thing that could have told a live transaction
// from an abandoned one was not consulted by any of the five.
//
// Two real processes, because that is the shape: one process's mutex is not the other's, and a
// same-process test would be measuring `commitMu` instead of the file lock.
//
// ⚠ **On Windows it goes wrong in TWO ways, and neither is quiet enough to notice.** Reverting the
// fix here produced both: sometimes the restore lands and the update is silently undone with its
// backup deleted, and sometimes it fails with `rename …: Access is denied` — because the pre-flight
// is EXECUTING the target at that moment and a running image cannot be replaced. So the same race is
// data loss on one run and a startup that reports "an interrupted update could not be undone" on the
// next, for a machine where nothing was interrupted at all.
func TestSalvageDoesNotRobALiveCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, exeName("magi-install"))
	source := filepath.Join(dir, exeName("source"))
	selfAs(t, target)
	selfAs(t, source)

	// The child's Commit pre-flights the new build by running it; the stub sleeps there, which is
	// the window this test needs. Real pre-flights are shorter and the window is still real —
	// a process start is never free.
	child := exec.Command(os.Args[0], "-test.run", "TestCommitInAChildProcess", "-test.v")
	child.Env = append(os.Environ(),
		"MAGI_TEST_COMMIT_TARGET="+target,
		"MAGI_TEST_COMMIT_SOURCE="+source,
		"MAGI_STUB_VERSION=v2.0.0",
		"MAGI_STUB_SLEEP=3s",
	)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Wait() }()

	// Wait for the child to be INSIDE the window: backup written, journal not yet.
	prev := target + ".prev"
	var inWindow bool
	for i := 0; i < 400 && !inWindow; i++ {
		time.Sleep(25 * time.Millisecond)
		if _, err := os.Stat(prev); err != nil {
			continue
		}
		if _, err := os.Stat(journalOf(target)); os.IsNotExist(err) {
			inWindow = true
		}
	}
	if !inWindow {
		t.Skip("never observed the child inside the record-less window — nothing to measure")
	}

	put, err := Salvage(target)
	if err != nil {
		t.Fatalf("salvage: %v", err)
	}
	if put != "" {
		t.Errorf("salvage undid a live commit: it restored %s while another process held the install "+
			"lock, which also deletes the .prev that transaction needs to roll back", put)
	}
	if _, err := os.Stat(prev); err != nil {
		t.Errorf("the live transaction's backup is gone (%v) — a rollback now has nothing to go back to", err)
	}
}

// §9.3 이 못 쥔 쪽에 요구하는 것: 기다리지도, 빼앗지도 말고 제 일을 계속하라(U04).
//
// 유닉스 쪽에도 같은 물음이 있지만(journal_test.go) 거기서는 설치 자리에 셸 스크립트를 세우므로 그
// 플랫폼에만 있다 — 그리고 **쓰고 있는 파일을 바꾸는 일이 어려운 쪽은 윈도우다.** 여기 두면 양쪽에서
// 돈다.
func TestAReplacementThatCannotTakeTheInstallDoesNotWaitOrSteal(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, exeName("magi-install"))
	was := selfAs(t, target)

	// 남이 쥔 것처럼 쥔다 — 같은 파일을 따로 여는 것. 두 플랫폼 다 잠금이 **핸들 단위**라, 같은
	// 프로세스의 두 번째 핸들도 다른 프로세스와 똑같이 충돌한다.
	held, ok := holdInstall(target)
	if !ok {
		t.Fatal("설치 잠금을 아예 못 쥐었다")
	}
	freed := false
	defer func() {
		if !freed {
			held()
		}
	}()

	start := time.Now()
	err := Commit(append(was, '\n'), target, Versions{From: "v1.0.0", To: "v2.0.0"})
	took := time.Since(start)
	if !errors.Is(err, ErrInstallBusy) {
		t.Fatalf("남이 설치를 쥐고 있는데 교체가 그냥 진행됐다: %v", err)
	}
	// 줄 서지 않는다. `installWait`(30초)는 **정착 단계**에게 허락된 기다림이지 교체에게가 아니다 —
	// 필요도 없는 갱신을 하려고 줄 서 있는 데몬은 제 일을 안 하는 데몬이다.
	if took > 5*time.Second {
		t.Errorf("못 쥔 교체가 %v 를 기다렸다 — 말하고 제 일을 계속해야 한다", took)
	}
	on, rerr := os.ReadFile(target)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !bytes.Equal(on, was) {
		t.Error("잠금을 못 쥔 프로세스가 바이너리를 갈아치웠다")
	}
	// 빼앗지도 않는다: 두 프로세스가 다툴 파일을 미리 만들어 두지 않는다.
	if _, serr := os.Stat(target + ".prev"); serr == nil {
		t.Error("못 쥔 쪽이 이전 사본을 남겼다 — 그 파일이 바로 둘이 다투게 되는 자리다")
	}
	if _, serr := os.Stat(journalOf(target)); serr == nil {
		t.Error("하지도 않은 트랜잭션을 기록했다")
	}

	// 그리고 놓이면 같은 호출이 통과한다 — 거절이 잠금 때문이었다는 증거다.
	held()
	freed = true
	if err := Commit(append(was, '\n'), target, Versions{From: "v1.0.0", To: "v2.0.0"}); err != nil {
		t.Fatalf("잠금이 풀렸는데도 교체가 안 됐다: %v", err)
	}
}
