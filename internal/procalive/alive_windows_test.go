//go:build windows

package procalive

import (
	"os"
	"os/exec"
	"testing"
)

// 이 판정 위에 파괴적인 행동이 둘 걸려 있다(claim 지우기, 빌드 되돌리기). 그래서 「살아 있다」보다
// **「확실히 죽었다」를 말할 수 있는가**가 이 파일의 물음이다 — 그것을 못 말하면 저널의 되돌리기는
// 영영 일어나지 않는다.

func TestThisProcessIsAlive(t *testing.T) {
	alive, known := Alive(os.Getpid())
	if !alive || !known {
		t.Fatalf("도는 프로세스를 살았다고 못 한다: alive=%v known=%v", alive, known)
	}
}

// 끝나고 거둬진 프로세스는 **죽었다**고 답해야 한다 — 「모른다」가 아니라. 옛 판(os.FindProcess)은
// 여기서 「모른다」를 냈고, 그것이 윈도우에서 안정 창 되돌리기를 통째로 못 돌게 했다.
func TestAReapedProcessIsKnownDead(t *testing.T) {
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	pid := c.Process.Pid
	_ = c.Wait()
	alive, known := Alive(pid)
	if alive || !known {
		t.Fatalf("거둬진 프로세스: alive=%v known=%v — 「확실히 죽었다」여야 한다", alive, known)
	}
}

// 핸들을 아직 누가 쥐고 있어도 답은 같다. 프로세스 객체가 열린다는 것과 프로세스가 돈다는 것은
// 다른 말이고, 그 둘을 섞은 것이 옛 판이었다.
func TestAnEndedProcessWhoseHandleIsStillHeldIsDead(t *testing.T) {
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	pid := c.Process.Pid
	// os.FindProcess 는 윈도우에서 핸들을 연다. 놓지 않고 들고 있으면 프로세스 객체는 살아남는다.
	held, err := os.FindProcess(pid)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	_ = c.Wait()
	if alive, known := Alive(pid); alive || !known {
		t.Fatalf("핸들이 열려 있다고 끝난 프로세스를 살았다고 한다: alive=%v known=%v", alive, known)
	}
}

// 종료 코드 259 는 윈도우에서 STILL_ACTIVE 와 같은 값이다. 종료 코드로 묻는 구현은 이 프로세스를
// 영원히 살아 있다고 답한다 — 핸들의 시그널 상태로 물으면 그런 일이 없다.
func TestAProcessThatExitedWith259IsDead(t *testing.T) {
	c := exec.Command("cmd", "/c", "exit", "259")
	if err := c.Start(); err != nil {
		t.Skipf("cmd 를 띄우지 못했다: %v", err)
	}
	pid := c.Process.Pid
	_ = c.Wait()
	if alive, known := Alive(pid); alive || !known {
		t.Fatalf("259 로 끝난 프로세스를 살았다고 한다: alive=%v known=%v", alive, known)
	}
}

func TestAPidNobodyCouldHaveIsNotKnown(t *testing.T) {
	if alive, known := Alive(0); alive || known {
		t.Errorf("0 번은 물을 수 있는 pid 가 아니다: alive=%v known=%v", alive, known)
	}
	if alive, known := Alive(-3); alive || known {
		t.Errorf("음수 pid: alive=%v known=%v", alive, known)
	}
}
