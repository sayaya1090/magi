package office

import "testing"

// **PowerPoint 2021 의 편집 어댑터는 헬퍼가 띄운다**(adapter.go) — 2026-09-07 까지는 PowerShell 감시기가 하던 일이다.
// 사용자: 「2021 은 그런 거 없어도 되잖아, 직접 프로세스 띄울 수 있지 않아」. 여기서 재는 것은 **띄울지 말지의 갈림**이다:
// 프로그램이 떠 있고, 이 설치본에 어댑터가 있고, 아직 안 떠 있을 때만 띄운다.
func TestTheHelperStartsTheEditingAdapterOnlyWhenItIsNeeded(t *testing.T) {
	running, known, alive, exe := true, true, false, `C:\magi\office\hand\magi-ppt-hand.exe`
	var spawned []string
	api := func(app *App) *API {
		return &API{App: app, Port: 3000,
			Running:      func() (bool, bool) { return running, known },
			AdapterExe:   func() string { return exe },
			AdapterAlive: func() bool { return alive },
			SpawnAdapter: func(at string) error { spawned = append(spawned, at); return nil },
		}
	}
	ppt := api(PPT)

	if !ppt.adapterTick() {
		t.Fatal("PowerPoint 가 떠 있고 어댑터가 없는데 안 띄웠다")
	}
	if len(spawned) != 1 || spawned[0] != exe {
		t.Fatalf("띄운 것: %v", spawned)
	}

	// 이미 떠 있으면 또 안 띄운다 — 같은 덱에 어댑터가 둘이면 호출을 서로 가로챈다(2026-09-07).
	alive = true
	if ppt.adapterTick() || len(spawned) != 1 {
		t.Fatalf("이미 떠 있는데 또 띄웠다: %v", spawned)
	}

	// PowerPoint 가 없으면 붙을 데가 없다. 못 재는 자리(known=false)에서도 안 띄운다 — 모르는 것을 「없다」로 안 읽는다.
	alive = false
	running = false
	if ppt.adapterTick() {
		t.Fatal("PowerPoint 가 없는데 띄웠다")
	}
	running, known = true, false
	if ppt.adapterTick() {
		t.Fatal("프로세스를 못 재는데 띄웠다")
	}

	// **이 설치본에 어댑터가 없으면 안 띄운다.** 설치기는 볼륨 판에서만 어댑터를 짓는다 — M365 는 작업창이 그대로
	// 손이고, 거기에 COM 어댑터가 붙으면 헬퍼가 덱 둘을 보고 호출이 엉뚱한 쪽으로 간다(2026-09-06 실측).
	known = true
	exe = ""
	if ppt.adapterTick() {
		t.Fatal("어댑터 실행 파일이 없는데 띄웠다")
	}

	// 엑셀·워드에는 어댑터가 아예 없다 — 작업창이 그대로 손이다.
	exe = `C:\magi\office\hand\magi-ppt-hand.exe`
	for _, app := range []*App{XL, Word} {
		if api(app).adapterTick() {
			t.Fatalf("%s 에 어댑터를 띄웠다", app.Key)
		}
	}
}
