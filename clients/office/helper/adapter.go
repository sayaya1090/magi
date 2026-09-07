package office

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/sayaya1090/magi/internal/quietconsole"
)

// PowerPoint 2021 의 편집 어댑터는 **헬퍼가 띄운다.**
//
// 2021 의 작업창은 편집을 못 해(PowerPointApi 1.2) COM 어댑터가 대신 고친다. 그 어댑터는 PowerPoint 가 이미 떠 있어야
// 붙으므로 누군가 때를 봐서 띄워야 하는데, 2026-09-07 까지는 그것이 PowerShell 감시기였다 — 로그인 때 뜨는 등록을 하나
// 더 쓰고, 관리자 창에서 뜨면 보통 권한 PowerPoint 의 COM 을 못 보고, 열린 덱을 세어 프로세스를 여럿 건사했다.
//
// 사용자가 그 자리를 짚었다: 「2021 은 그런 거 없어도 되잖아, 직접 프로세스 띄울 수 있지 않아」. 맞다 — 헬퍼는 창이
// 붙는 자리라 어차피 떠 있고, 프로세스를 띄우는 일은 이미 한다(컴패니언 데몬). 열린 덱을 세는 일은 COM 이 손에 있는
// 어댑터 자신이 더 잘한다(hand-com 의 Supervisor). 그래서 여기서는 **하나만** 본다: PowerPoint 가 떠 있는데 어댑터가
// 없으면 띄운다.
//
// **볼륨 판 갈림은 실행 파일의 존재로 진다.** 설치기는 볼륨 판에서만 어댑터를 짓는다(M365 는 작업창이 그대로 손이고,
// 거기에 COM 어댑터가 붙으면 헬퍼가 덱 둘을 보고 호출이 엉뚱한 쪽으로 간다 — 2026-09-06 에 겪은 그 결함이다). 없으면
// 안 띄우는 것이 곧 그 갈림이다.

// adapterTick 은 폴마다 한 번. 띄웠으면 true — 시험이 그것을 읽는다.
func (a *API) adapterTick() bool {
	if a.App == nil || a.App.AdapterExe == "" {
		return false // 어댑터가 있는 프로그램은 파워포인트뿐이다
	}
	running, known := a.programRunning()
	if !known || !running {
		return false // PowerPoint 가 없으면 붙을 데가 없다. 모르면 안 띄운다.
	}
	exe := a.adapterExe()
	if exe == "" {
		return false // 이 설치본에는 어댑터가 없다(M365, 또는 .NET SDK 없이 깐 판)
	}
	if a.adapterAlive() {
		return false
	}
	if err := a.spawnAdapter(exe); err != nil {
		log.Printf("[%s] 편집 어댑터를 못 띄웠습니다(%s): %v", a.App.Key, exe, err)
		return false
	}
	log.Printf("[%s] %s 가 떠 있고 편집 어댑터가 없어 띄웠습니다: %s", a.App.Key, a.App.Product, exe)
	return true
}

func (a *API) adapterExe() string {
	if a.AdapterExe != nil {
		return a.AdapterExe()
	}
	return adapterBeside(a.App)
}

func (a *API) adapterAlive() bool {
	if a.AdapterAlive != nil {
		return a.AdapterAlive()
	}
	alive, known := imageRunning(a.App.AdapterExe, "")
	// 못 재는 자리에서는 **떠 있다고 본다** — 모르는 것을 「없다」로 읽으면 폴마다 하나씩 띄운다.
	return alive || !known
}

func (a *API) spawnAdapter(exe string) error {
	if a.SpawnAdapter != nil {
		return a.SpawnAdapter(exe)
	}
	cmd := exec.Command(exe, "--helper", Origin(a.Port)+a.App.Base())
	cmd.Dir = filepath.Dir(exe)
	// 헬퍼는 콘솔 없이 떠 있다. 그 자식인 콘솔 프로그램은 창을 새로 열므로 숨긴다(own.go 의 같은 자리).
	quietconsole.Apply(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // 좀비를 안 남긴다. 기다리는 것 말고는 할 일이 없다.
	return nil
}

// adapterBeside 는 헬퍼 실행 파일 **옆**의 어댑터. 설치기가 `<Dest>\hand\magi-ppt-hand.exe` 에 놓는다.
// 없으면 빈 문자열이다 — 지어내지 않는다.
func adapterBeside(app *App) string {
	if runtime.GOOS != "windows" || app == nil || app.AdapterExe == "" {
		return ""
	}
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	at := filepath.Join(filepath.Dir(self), "hand", app.AdapterExe)
	if _, err := os.Stat(at); err != nil {
		return ""
	}
	return at
}
