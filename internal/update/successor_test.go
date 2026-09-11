package update

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// 이 파일은 윈도우에서도 돈다 — journal_test.go 와 달리 아무것도 실행하지 않기 때문이다. 재는 것은
// 이전 세대가 후계의 죽음을 목격했을 때 저널이 어떻게 움직이는가이고, 그 목격은 윈도우에서만
// 일어난다(유닉스는 제 이미지를 바꿔 끼우므로 지켜볼 이전 세대가 남지 않는다).

// staged writes the state Commit leaves: the candidate at target, the build it replaced beside it,
// and a pending transaction. Plain bytes — nothing here runs them.
func staged(t *testing.T, from, to string) (target string, prev []byte) {
	t.Helper()
	target = filepath.Join(t.TempDir(), "magi.exe")
	if err := os.WriteFile(target, []byte("candidate "+to), 0o755); err != nil {
		t.Fatal(err)
	}
	prev = []byte("previous " + from)
	if err := os.WriteFile(target+".prev", prev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Began(target, Versions{From: from, To: to}); err != nil {
		t.Fatal(err)
	}
	return target, prev
}

// reaped is a pid that certainly is not running: this test binary, started with nothing to run, and
// waited for. Made rather than invented, for the reason journal_test.go's gone gives.
func reaped(t *testing.T) int {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	pid := c.Process.Pid
	_ = c.Wait()
	return pid
}

func watchedBy(t *testing.T, target string, pid int) {
	t.Helper()
	l, err := readLedger(resolveInstall(target))
	if err != nil || l.Pending == nil {
		t.Fatalf("no pending record: %v", err)
	}
	l.Pending.Starts, l.Pending.Watcher = 1, pid
	if err := writeLedger(resolveInstall(target), l); err != nil {
		t.Fatal(err)
	}
}

func onDisk(t *testing.T, target string) []byte {
	t.Helper()
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The gap itself: the successor died before Resume ever ran, so the record says nobody started.
// Resume would read the next start as the FIRST one and watch the same bad build all over again.
func TestASuccessorThatDiedBeforeResumeIsRolledBack(t *testing.T) {
	target, prev := staged(t, "v1.0.0", "v2.0.0")

	got, err := SuccessorFailed(target, "v1.0.0", reaped(t))
	if err != nil {
		t.Fatal(err)
	}
	if !got.RolledBack || got.From != "v1.0.0" || got.To != "v2.0.0" {
		t.Fatalf("a candidate that never came up was not rolled back: %+v", got)
	}
	if !bytes.Equal(onDisk(t, target), prev) {
		t.Error("the previous build is not back on disk")
	}
	if _, err := os.Stat(target + ".prev"); !os.IsNotExist(err) {
		t.Error("the saved copy was left behind after it was restored")
	}
	if r := Refused(target); r != "v2.0.0" {
		t.Errorf("the candidate was not refused, so the automatic path takes it again (U05): %q", r)
	}
}

// Died after Resume: it took the watch, and it is the process that is gone. Its own watch is not
// "somebody else is running the candidate".
func TestASuccessorThatTookTheWatchAndDiedIsRolledBack(t *testing.T) {
	target, prev := staged(t, "v1.0.0", "v2.0.0")
	succ := reaped(t)
	watchedBy(t, target, succ)

	got, err := SuccessorFailed(target, "v1.0.0", succ)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RolledBack || !bytes.Equal(onDisk(t, target), prev) {
		t.Fatalf("the successor's own watch kept a dead candidate installed: %+v", got)
	}
}

// Its pid can still read as running for a moment after it ended — a handle held open, a check that
// cannot tell. The watch being the successor's own is what decides, not whether the number answers:
// otherwise the one process known to have failed would count as "somebody running the candidate".
func TestTheSuccessorsOwnWatchNeverCountsAsSomeoneElse(t *testing.T) {
	target, prev := staged(t, "v1.0.0", "v2.0.0")
	still := os.Getpid() // alive by construction, standing in for a pid that has not read as gone yet
	watchedBy(t, target, still)

	got, err := SuccessorFailed(target, "v1.0.0", still)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RolledBack || !bytes.Equal(onDisk(t, target), prev) {
		t.Fatalf("the failed successor's own watch kept the candidate: %+v", got)
	}
}

// Review R9, from this side: another workspace's companion is up on the candidate. One workspace
// falling over is not the candidate falling over, and rolling back would pull the file out from
// under a daemon that is serving fine.
func TestAnotherLiveWatcherKeepsTheCandidate(t *testing.T) {
	target, _ := staged(t, "v1.0.0", "v2.0.0")
	watchedBy(t, target, os.Getpid()) // this test process is alive by construction

	got, err := SuccessorFailed(target, "v1.0.0", reaped(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.RolledBack {
		t.Fatal("rolled back a candidate another workspace is running")
	}
	if !bytes.Equal(onDisk(t, target), []byte("candidate v2.0.0")) {
		t.Error("the candidate was replaced while a daemon was running it")
	}
	if r := Refused(target); r != "" {
		t.Errorf("a build nobody rejected was recorded as refused: %q", r)
	}
}

// A restart onto the SAME build has no transaction to judge, and a transaction that is not a
// replacement of this build is somebody else's. Both leave the files exactly as they are.
func TestNotThisBuildsTransactionIsLeftAlone(t *testing.T) {
	t.Run("nothing pending", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "magi.exe")
		if err := os.WriteFile(target, []byte("same"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := SuccessorFailed(target, "v1.0.0", reaped(t))
		if err != nil || got.RolledBack {
			t.Fatalf("a plain restart was read as a failed update: %+v %v", got, err)
		}
	})
	t.Run("replacing some other build", func(t *testing.T) {
		target, _ := staged(t, "v0.9.0", "v2.0.0")
		got, err := SuccessorFailed(target, "v1.0.0", reaped(t))
		if err != nil || got.RolledBack {
			t.Fatalf("judged a transaction this build is not part of: %+v %v", got, err)
		}
		if !bytes.Equal(onDisk(t, target), []byte("candidate v2.0.0")) {
			t.Error("the files moved anyway")
		}
	})
}

// A confirm under way means the candidate already lasted its window. Whatever just died, it was not
// the build failing to come up — undoing it would throw away a build that proved itself.
func TestAConfirmUnderWayIsNotUndone(t *testing.T) {
	target, _ := staged(t, "v1.0.0", "v2.0.0")
	l, err := readLedger(resolveInstall(target))
	if err != nil || l.Pending == nil {
		t.Fatal(err)
	}
	l.Pending.Stage = stageConfirming
	if err := writeLedger(resolveInstall(target), l); err != nil {
		t.Fatal(err)
	}
	got, err := SuccessorFailed(target, "v1.0.0", reaped(t))
	if err != nil || got.RolledBack {
		t.Fatalf("undid a confirm: %+v %v", got, err)
	}
}
