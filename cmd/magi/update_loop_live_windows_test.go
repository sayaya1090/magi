//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/shortdir"
)

// **아무도 안 누르는데 일어나는 갱신** — 데몬 자신의 루프를, 그것이 사는 곳에서 잰다(#193).
//
// 이 나무의 갱신 시험들은 전부 **문을 두드려서** 시작한다: `-update-core`, 콘솔 버튼, `UpdateWhen`.
// 그 셋은 사람이 있는 경로이고, 제품이 무인으로 하는 일은 그것이 아니다 — 여섯 시간마다 혼자 확인하고,
// 혼자 설치하고, 조용해지기를 기다렸다가 혼자 올라선다. 그 사슬을 통째로 재는 것이 이 파일이고,
// **이 시험은 문을 한 번도 두드리지 않는다**. 데몬을 띄우고, 기다리고, 무엇이 되었는지 묻는다.
//
// 윈도우에 두는 이유: 이 사슬의 마지막 고리가 후계 기동이고, 그것이 이 플랫폼에서 실제로 깨져 있었다
// (#190 — 못 띄운 후계를 성공이라 말했다). 루프가 문 경로와 다른 점은 **지켜보는 사람이 없다**는 것
// 하나뿐이라, 여기서 깨지면 아무도 모른다.
//
// ⚠ **일정은 링크 시점에 심는다.** 첫 확인이 최대 1.5시간 뒤이므로 기본 일정으로는 잴 수 없고,
// `daemonAutoUpdateTTL` 은 같은 프로세스에서만 줄어드는 var 다 — 데몬은 별도 프로세스다. 그래서
// `-X main.daemonUpdateEvery` 로 지어진 바이너리를 쓴다(그 이음매가 왜 환경 변수가 아닌지는
// autoupdate.go 의 주석에 적혀 있다).

func TestTheDaemonsOwnLoopPicksUpAReleaseWithNobodyWatching(t *testing.T) {
	if testing.Short() {
		t.Skip("판을 두 번 짓는다")
	}
	cfg, err := shortdir.Make("mgl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(cfg) })
	// 설치 자리의 판: 깨끗한 태그(SelfUpdatable 이 그것만 자기갱신시킨다) + 잴 수 있는 일정.
	exe := buildMagi(t, cfg, "-ldflags=-X github.com/sayaya1090/magi/internal/version.Version="+baseTag+
		" -X main.daemonUpdateEvery=2s")
	// 규칙 1 의 나머지 절반: **물어볼 수 있다.** 환경 변수는 도는 프로세스의 환경에서 읽을 수 있지만
	// 이것은 바이너리에 구워져 있어서, 바이너리에게 묻는 것이 유일한 방법이다.
	if said, verr := exec.Command(exe, "--version").CombinedOutput(); verr != nil {
		t.Fatal(verr)
	} else if !strings.Contains(string(said), "every 2s") {
		t.Errorf("-version 이 이 빌드의 일정을 말하지 않는다:\n%s", said)
	}
	other, err := shortdir.Make("mgm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	newBin, err := os.ReadFile(buildMagi(t, other,
		"-ldflags=-X github.com/sayaya1090/magi/internal/version.Version="+liveTag))
	if err != nil {
		t.Fatal(err)
	}
	rs := serveRelease(t, liveTag, newBin, trueDigest)

	ws, err := shortdir.Make("mgo")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ws) })
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

	logPath := filepath.Join(cfg, "loop.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	// `--no-update-check` 가 **없다**. 그것이 이 시험의 전부다 — 루프를 켜는 스위치는 그 플래그의
	// 부재와 `[update] auto`(설정이 없으면 켜짐)이고, 나머지는 데몬이 알아서 한다.
	cmd := exec.Command(exe, "--daemon")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(),
		"MAGI_CONFIG_DIR="+cfg, "MAGI_SOCKET_DIR="+cfg, "MAGI_RELEASE_API_BASE="+rs.URL)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	sock := daemon.SocketPath(cfg, ws)
	waitServing(t, sock, cmd.Process.Pid, logPath)

	// 심은 일정이 정말 박혔는지 **먼저** 묻는다. 이음매 이름을 하나 틀리면 링커는 조용히 아무것도 하지
	// 않고(`-X` 는 없는 심볼에 침묵한다), 그러면 이 시험은 「루프가 안 돈다」로 1.5시간을 기다린 뒤
	// 시간 초과로 죽는다 — 원인을 말하지 않는 빨간색이다. 규칙 1 의 그 줄이 여기서 값을 한다.
	before := readLog(logPath)
	if !strings.Contains(before, updateEveryVar) || !strings.Contains(before, "every 2s") {
		t.Fatalf("심은 일정이 박히지 않았다 — 이 회차는 루프를 못 기다린다:\n%s", before)
	}
	// 이제 아무것도 하지 않는다. 확인 → 내려받기 → 검증 → 안전 시점 → 교체 → 후계 → 서비스가
	// 저절로 일어나야 한다.
	deadline := time.Now().Add(120 * time.Second)
	var now daemon.Info
	for time.Now().Before(deadline) {
		if rec, ok := serving(sock); ok && rec.Version == liveTag {
			now = rec
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if now.PID == 0 {
		at, _ := serving(sock)
		t.Fatalf("루프가 새 판으로 올라서지 못했다 (지금 서비스하는 것: %+v):\n%s", at, readLog(logPath))
	}
	// 「말했다」가 아니라 「되었다」를 잰다: 새 판이 서비스하고 있고, 그 프로세스는 **처음 것이 아니다**.
	// 재기동 없이 디스크만 바뀌었으면 판은 새것인데 도는 것은 옛것이고, 그것이 #190 의 모양이었다.
	if now.PID == cmd.Process.Pid {
		t.Errorf("판은 %s 인데 프로세스가 그대로다 (pid %d) — 재기동이 없었다", now.Version, now.PID)
	}
	if !daemon.SamePath(now.Workdir, ws) {
		t.Errorf("올라선 데몬이 다른 작업공간을 들고 있다: %q", now.Workdir)
	}
	// 트랜잭션은 열려 있다: 되돌릴 이전 빌드와 그것을 적은 저널이 자리에 있어야 한다(§9.3). 확정은
	// 안정 창(60초)을 버틴 다음 세대의 일이고, 그쪽은 따로 잰다.
	if _, serr := os.Stat(exe + ".prev"); serr != nil {
		t.Errorf("루프가 이전 빌드를 안 남겼다 (%v) — 새 판이 엎어지면 돌아갈 곳이 없다", serr)
	}
	if _, serr := os.Stat(exe + ".update.json"); serr != nil {
		t.Errorf("루프가 트랜잭션을 안 적었다 (%v) — 아무도 되돌릴 수 없다", serr)
	}
	// 그리고 출처를 말했다(환경 문의 규칙 1). 사람이 안 보는 경로일수록 그 줄이 유일한 기록이다.
	//
	// ⚠ **확인이 시작될 때 나오고, 루프가 시작될 때 나오지 않는다.** 위의 `before` 에서 이것을 물었다가
	// 배웠다 — 데몬은 떠 있고 일정 줄도 있는데 출처 줄은 아직 없었다. 맞는 자리다(확인마다, 조회 직전),
	// 그래서 이 단언도 갱신이 끝난 뒤로 온다.
	if said := readLog(logPath); !strings.Contains(said, rs.URL) {
		t.Errorf("루프가 기본이 아닌 출처를 말하지 않았다:\n%s", said)
	}
	t.Logf("무인 갱신: pid %d(%s) → pid %d(%s)", cmd.Process.Pid, baseTag, now.PID, now.Version)
}
