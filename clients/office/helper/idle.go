package office

import (
	"bytes"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// 프로그램이 없으면 컴패니언도 없다.
//
// 헬퍼가 마련한 컴패니언(magi --daemon)은 PowerPoint·Excel·Word 를 다 꺼도 남아 있었다 — 로그인부터 로그아웃까지
// 셋이 늘 떠 있고, 사람은 그것을 좀비로 읽었다(2026-09-07 제보). 사용자 결정: **각 프로그램 인스턴스가 없으면 종료.**
// 기준은 작업창이 아니라 **프로그램 프로세스**다 — 창을 안 연 채 PowerPoint 를 켜 둔 사람의 컴패니언을 내리면 다음
// 창 열기가 몇 초 늦을 뿐이지만, 그 반대(프로그램은 껐는데 컴패니언이 남는 것)가 이 제보다.
//
// 재는 것은 OS 다: Windows 는 tasklist, macOS 는 pgrep. 둘 다 못 하는 곳(리눅스·시험)에서는 「모른다」이고, 모르면
// 안 내린다 — 모르는 것을 「없다」로 읽으면 멀쩡한 컴패니언을 죽인다. 잠깐의 빈틈(껐다 바로 켬)은 유예(IdleAfter)가
// 덮는다. 내린 뒤는 `Work.Forget()` — 다음 `/api/own` 이 처음처럼 다시 마련한다.

// idleTick 은 감시기가 tick 마다 도는 것. 시험이 시계를 넣어 부른다. 내렸으면 true.
func (a *API) idleTick(now time.Time) bool {
	held := a.Work.Now()
	if held.Phase != OwnReady || held.Socket == "" {
		a.idleSince = time.Time{}
		return false
	}
	running, known := a.programRunning()
	if !known || running {
		a.idleSince = time.Time{}
		return false
	}
	if a.idleSince.IsZero() {
		a.idleSince = now
		return false
	}
	if now.Sub(a.idleSince) < a.idleAfter() {
		return false
	}
	// **내린다.** shutdown 문은 답하고 나서 끝난다(daemon doors). 못 두드려도(이미 죽었다) 기록은 잊는다 — 다음 own 이
	// 생애를 재서 어차피 다시 마련한다.
	if err := a.stopCompanion(held.Socket); err != nil {
		log.Printf("[%s] %s 가 없어 컴패니언을 내리려 했는데 못 두드렸다(%s): %v", a.App.Key, a.App.Product, held.Socket, err)
	} else {
		log.Printf("[%s] %s 가 %s 동안 없어 컴패니언을 내렸다(%s)", a.App.Key, a.App.Product, a.idleAfter(), held.Socket)
	}
	a.Work.Forget()
	a.idleSince = time.Time{}
	return true
}

// watchProgram 은 헬퍼가 사는 동안 돈다(mount 가 띄운다).
func (a *API) watchProgram(stop <-chan struct{}) {
	t := time.NewTicker(idleTickEvery)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			a.idleTick(now)
		}
	}
}

const (
	idleTickEvery    = 10 * time.Second
	idleAfterDefault = 60 * time.Second
)

func (a *API) idleAfter() time.Duration {
	if a.IdleAfter > 0 {
		return a.IdleAfter
	}
	return idleAfterDefault
}

// programRunning 은 (돌고 있나, 알 수 있나). 시험은 Running 을 넣는다.
func (a *API) programRunning() (bool, bool) {
	if a.Running != nil {
		return a.Running()
	}
	return processRunning(a.App)
}

func (a *API) stopCompanion(socket string) error {
	if a.Stop != nil {
		return a.Stop(socket)
	}
	cl, err := daemon.DialWithin(socket, aliveTimeout, aliveTimeout)
	if err != nil {
		return err
	}
	defer cl.Close()
	return cl.Shutdown()
}

// processRunning 은 그 프로그램의 프로세스가 하나라도 있는가. 둘째 값이 거짓이면 이 OS 에서 못 잰 것이다.
func processRunning(app *App) (bool, bool) {
	switch runtime.GOOS {
	case "windows":
		if app.ProcWin == "" {
			return false, false
		}
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+app.ProcWin, "/NH", "/FO", "CSV").Output()
		if err != nil {
			return false, false
		}
		return bytes.Contains(bytes.ToUpper(out), []byte(strings.ToUpper(app.ProcWin))), true
	case "darwin":
		if app.ProcMac == "" {
			return false, false
		}
		err := exec.Command("pgrep", "-x", app.ProcMac).Run()
		if err == nil {
			return true, true
		}
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, true // pgrep 1 = 없음
		}
		return false, false
	default:
		return false, false
	}
}
