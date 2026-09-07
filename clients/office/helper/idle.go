package office

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
			// 순서가 뜻을 갖는다: 프로그램이 떠 있으면 어댑터를 띄우고, 없으면 컴패니언을 내린다.
			a.adapterTick()
			a.idleTick(now)
		}
	}
}

const (
	idleTickEvery    = 5 * time.Second
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
func processRunning(app *App) (bool, bool) { return imageRunning(app.ProcWin, app.ProcMac) }

// imageRunning 은 그 이름의 프로세스가 하나라도 있는가 — (있나, 잴 수 있었나). 이 OS 몫의 이름이 비면 못 재는 것이다.
func imageRunning(win, mac string) (bool, bool) {
	switch runtime.GOOS {
	case "windows":
		if win == "" {
			return false, false
		}
		out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+win, "/NH", "/FO", "CSV").Output()
		if err != nil {
			return false, false
		}
		return bytes.Contains(bytes.ToUpper(out), []byte(strings.ToUpper(win))), true
	case "darwin":
		if mac == "" {
			return false, false
		}
		err := exec.Command("pgrep", "-x", mac).Run()
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

// Office 가 하나도 없으면 **헬퍼도 없다.**
//
// 컴패니언은 제 프로그램이 없으면 내려간다(위). 남는 것은 헬퍼인데, 그것도 Office 를 안 켠 동안 떠 있을 이유가 없다 —
// 사용자가 그렇게 못박았다(2026-09-07: 「헬퍼랑 데몬은 각 오피스가 켜진 게 있을 때만 켜져 있고, 인스턴스가 없으면
// 종료되고」). 다음에 Office 를 켜면 COM 추가 기능이 다시 띄운다(clients/office/addin-com).
//
// **못 재면 안 끝낸다.** 셋 다 잴 수 없는 OS 에서는 이 감시가 아무 말도 하지 않는다 — 모르는 것을 「없다」로 읽으면
// 사람이 쓰고 있는 헬퍼를 끄는 자리가 된다.
type officeWatch struct {
	// Gone 은 (하나도 없나, 잴 수 있었나). 시험이 채운다.
	Gone  func() (bool, bool)
	After time.Duration
	since time.Time
}

func (w *officeWatch) after() time.Duration {
	if w.After > 0 {
		return w.After
	}
	return idleAfterDefault
}

// tick 은 「지금 끝내야 하나」. 유예 안이거나 하나라도 떠 있으면 거짓이다.
func (w *officeWatch) tick(now time.Time) bool {
	gone, known := w.gone()
	if !known || !gone {
		w.since = time.Time{}
		return false
	}
	if w.since.IsZero() {
		w.since = now
		return false
	}
	return now.Sub(w.since) >= w.after()
}

func (w *officeWatch) gone() (bool, bool) {
	if w.Gone != nil {
		return w.Gone()
	}
	return officeGone()
}

// officeGone 은 세 프로그램이 **하나도** 안 떠 있는가 — (없나, 잴 수 있었나).
func officeGone() (bool, bool) {
	known := false
	for _, app := range Apps {
		running, can := processRunning(app)
		if !can {
			continue
		}
		known = true
		if running {
			return false, true
		}
	}
	return known, known
}

// wakesUpAgain 은 **우리를 다시 띄울 것이 있는가.**
//
// Office 가 없을 때 헬퍼가 끝나도 되는 것은 다음에 Office 를 켤 때 누가 다시 띄워 주기 때문이다. 그것은 COM 추가 기능
// 하나뿐이고(clients/office/addin-com), 그것은 Windows 에만 있다. 없는 자리에서 끝내면 사람이 손으로 띄운 헬퍼를 끄는
// 셈이고, 다음에 Office 를 켜면 리본의 Magi 가 빈 창을 띄운다.
//
// **맥에는 그 자리가 없다.** Office for Mac 은 COM 추가 기능을 안 받고, 남은 길(로그인 항목·launchd 주기 작업)은 전부
// 「상주하거나, 사람이 따로 관리하는 것」이라 요구를 어긴다(사용자, 2026-09-07: 상주 프로세스 없음 · Office 를 켜는 것
// 말고 사람이 관리할 것 없음). 그래서 맥은 **개발용**이고, 거기서는 헬퍼가 스스로 안 끝난다.
//
// 재는 것은 **추가 기능이 실제로 깔렸는가**다(설치기가 헬퍼 옆 `start\` 에 놓는다). 「Windows 니까」로 재면 추가 기능
// 없이 깐 판에서 헬퍼가 스스로 사라진다.
func wakesUpAgain() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	self, err := os.Executable()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(filepath.Dir(self), "start", "magi-office-start.comhost.dll"))
	return err == nil
}
