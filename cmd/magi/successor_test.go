package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/graceful"
	"github.com/sayaya1090/magi/internal/update"
)

// 이전 세대가 후계의 결과를 받아 무엇을 하는가 — CLIENT_LIFECYCLE §4 「후계 준비 확인」과 §9.3.6 을
// 한 줄씩 옮긴 표다. 데몬 없이 재는 것은 판단이고, 판단이 부르는 셋(재기동·저널·파이프)은 각자의
// 패키지에서 실물로 잰다(graceful 은 시험 바이너리를 후계로 띄워, update 는 파일로).

type seen struct {
	relaunched int
	failedFor  int
	said       strings.Builder
}

type world struct {
	second  error // what the relaunch onto the restored build does
	other   int   // a pid serving the workspace now, 0 for none
	journal update.Recovery
	jerr    error
	gone    bool // the owner has let go of the pipe
}

func (w world) run(t *testing.T, first error) (int, *seen) {
	t.Helper()
	s := &seen{}
	code := afterRelaunch{
		relaunch:    func() error { s.relaunched++; return w.second },
		someoneElse: func() (int, bool) { return w.other, w.other != 0 },
		failed: func(pid int) (update.Recovery, error) {
			s.failedFor = pid
			return w.journal, w.jerr
		},
		ownerGone: func() bool { return w.gone },
		say:       &s.said,
	}.run(first, 0)
	return code, s
}

var rolledBack = update.Recovery{RolledBack: true, From: "v1.0.0", To: "v2.0.0"}

func died(code int) error { return &graceful.SuccessorDied{PID: 4242, Code: code} }

// The defect itself, end to end: the successor falls over, the candidate goes back, the previous
// build comes up again, and nothing about it is silent.
func TestAFallenSuccessorPutsThePreviousBuildBackAndRelaunchesIt(t *testing.T) {
	code, s := world{journal: rolledBack}.run(t, died(2))
	if s.failedFor != 4242 {
		t.Fatalf("the journal was not told which successor died: %d", s.failedFor)
	}
	if s.relaunched != 1 {
		t.Fatalf("the previous build was relaunched %d times, want exactly once", s.relaunched)
	}
	if code != 0 {
		t.Errorf("a recovered companion ended with %d", code)
	}
	for _, want := range []string{"4242", "v2.0.0 did not come up", "v1.0.0 is back"} {
		if !strings.Contains(s.said.String(), want) {
			t.Errorf("the log does not say %q:\n%s", want, s.said.String())
		}
	}
}

// §9.3.6: "relaunch the previous generation only while the owner is still alive. If the IDE has
// already closed, restore the file and do not revive the process."
func TestAClosedWindowGetsItsFileBackButNoProcess(t *testing.T) {
	code, s := world{journal: rolledBack, gone: true}.run(t, died(2))
	if s.failedFor == 0 {
		t.Fatal("the file was not restored")
	}
	if s.relaunched != 0 {
		t.Fatal("a companion was revived for a window that has closed")
	}
	if code == 0 {
		t.Error("ended as if nothing went wrong")
	}
}

// A successor that stopped on purpose — its owner closed the pipe while it was starting, a shutdown
// arrived — ends before it serves too. That is not the candidate failing.
func TestAStopOnPurposeIsNotAFailedUpdate(t *testing.T) {
	code, s := world{journal: rolledBack}.run(t, died(0))
	if s.failedFor != 0 || s.relaunched != 0 {
		t.Fatalf("a deliberate stop rolled the update back (journal=%d relaunch=%d)", s.failedFor, s.relaunched)
	}
	if code != 0 {
		t.Errorf("a deliberate stop ended with %d", code)
	}
}

// Somebody took the workspace in the gap. The successor lost a race; undoing the update for it would
// punish the build for a timing accident.
func TestLosingTheWorkspaceToAnotherDaemonIsNotAFailedUpdate(t *testing.T) {
	code, s := world{journal: rolledBack, other: 7}.run(t, died(1))
	if s.failedFor != 0 || s.relaunched != 0 {
		t.Fatal("rolled back an update because another daemon took the workspace")
	}
	if code != 0 || !strings.Contains(s.said.String(), "pid 7") {
		t.Errorf("code %d, log:\n%s", code, s.said.String())
	}
}

// Nothing to go back to — a restart onto the same build, or a candidate another workspace is running.
// No loop, no silence: an error code, so whatever started this counts the failure.
func TestNoPreviousBuildEndsWithAnErrorAndNoLoop(t *testing.T) {
	code, s := world{journal: update.Recovery{}}.run(t, died(2))
	if s.relaunched != 0 {
		t.Fatal("relaunched with nothing new on disk — that is the retry loop U05 forbids")
	}
	if code == 0 {
		t.Error("a companion that is not running ended with success")
	}
}

// The previous build will not come up either. Once was the allowance; the rest is a person's.
func TestThePreviousBuildFailingTooStopsAndSaysHow(t *testing.T) {
	code, s := world{journal: rolledBack, second: died(2)}.run(t, died(2))
	if s.relaunched != 1 {
		t.Fatalf("relaunched %d times", s.relaunched)
	}
	if code == 0 || !strings.Contains(s.said.String(), "magi --daemon") {
		t.Errorf("code %d, and the way out is not named:\n%s", code, s.said.String())
	}
}

// Slow is not dead: left to come up, and the process leaves cleanly (graceful.NotReady).
func TestASlowSuccessorIsLeftToComeUp(t *testing.T) {
	code, s := world{journal: rolledBack}.run(t, &graceful.NotReady{PID: 4242})
	if s.failedFor != 0 || s.relaunched != 0 {
		t.Fatal("a successor that was only slow was treated as a failure")
	}
	if code != 0 {
		t.Errorf("ended with %d", code)
	}
}

// 세대 비교는 클라이언트가 이미 고친 규칙이다(`ee92a23b`). 코어의 준비 판정에 같은 구멍이 남아
// 있었다 — 한쪽만 세대를 대는 것을 구형 호환으로 흘려보내고 있었고, 그것이 이 검사가 막으려던 바로
// 그 경우다(재기동 틈에 다른 데몬이 워크스페이스를 차지한 것).
func TestOnlyNeitherSideNamingAGenerationFallsBackToThePid(t *testing.T) {
	if !sameGeneration("", "") {
		t.Error("둘 다 구형이면 pid 로 떨어져야 한다")
	}
	if !sameGeneration("i-1", "i-1") {
		t.Error("같은 세대를 다르다고 했다")
	}
	if sameGeneration("i-1", "i-2") {
		t.Error("다른 세대를 같다고 했다")
	}
	if sameGeneration("i-1", "") {
		t.Error("기록은 세대를 대는데 답이 안 댔다 — 답한 것은 다른 프로세스다")
	}
	if sameGeneration("", "i-1") {
		t.Error("답은 세대를 대는데 기록이 안 댔다 — 그 기록은 다른 프로세스의 것이다")
	}
}

// A relaunch that never started keeps the old rule: say so and end with run()'s own code.
func TestARelaunchThatNeverStartedKeepsRunsCode(t *testing.T) {
	s := &seen{}
	code := afterRelaunch{
		relaunch:    func() error { s.relaunched++; return nil },
		someoneElse: func() (int, bool) { return 0, false },
		failed:      func(int) (update.Recovery, error) { s.failedFor = 1; return update.Recovery{}, nil },
		ownerGone:   func() bool { return false },
		say:         &s.said,
	}.run(errors.New("exec: file not found"), 5)
	if code != 5 || s.failedFor != 0 || s.relaunched != 0 {
		t.Fatalf("code %d, journal %d, relaunch %d", code, s.failedFor, s.relaunched)
	}
}
