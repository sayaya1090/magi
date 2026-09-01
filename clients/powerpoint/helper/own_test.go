package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// 소켓 이름을 **데몬과 같은 식으로** 짓는가.
//
// 한 글자만 달라도 「여기 데몬이 없다」로 읽히고, 그러면 이미 서 있는 것 옆에 하나를 더 띄운다.
// 그 둘이 한 워크스페이스를 두고 다투는 것이 이 시험이 막는 화면이다.
func TestTheDeckSocketIsDerivedTheSameWayTheDaemonDoes(t *testing.T) {
	cfg := t.TempDir()
	if got, want := DeckSocket(cfg), daemon.SocketPath(cfg, DeckSpace(cfg)); got != want {
		t.Fatalf("소켓 유도가 갈렸다:\n  헬퍼: %s\n  데몬: %s", got, want)
	}
	if !strings.Contains(filepath.Base(DeckSocket(cfg)), "powerpoint") {
		t.Fatalf("소켓 이름에 워크스페이스 이름이 안 실렸다 — `ls` 로 자기 것을 못 찾는다: %s",
			DeckSocket(cfg))
	}
}

// 이미 서 있으면 **아무것도 안 한다.**
//
// 둘째 창이 열리는 것만으로 데몬이 하나 더 뜨면, 그 둘이 한 워크스페이스를 두고 다툰다.
func TestALiveCompanionIsNotStartedAgain(t *testing.T) {
	spawned := 0
	own := &OwnCompanion{
		ConfigDir: t.TempDir(),
		Binary:    "magi",
		Alive:     func(string) bool { return true },
		Spawn: func(string, string, []string) error {
			spawned++
			return nil
		},
	}
	st, err := own.Ensure()
	if err != nil {
		t.Fatalf("이미 서 있는데 실패했다: %v", err)
	}
	if spawned != 0 {
		t.Fatalf("이미 서 있는데 %d 번 띄웠다", spawned)
	}
	if st.Started {
		t.Fatal("우리가 안 띄웠는데 Started 가 참이다 — 「띄웠다」와 「이미 있었다」는 다른 말이다")
	}
	if st.Socket == "" || st.Workdir == "" {
		t.Fatalf("서 있는데 자리를 안 알려 준다: %+v", st)
	}
}

// 없으면 **띄우고, 띄웠다고 적는다.** 그리고 설정 디렉토리를 물려준다.
func TestAMissingCompanionIsStartedWithOurConfigDir(t *testing.T) {
	cfg := t.TempDir()
	var gotBin, gotDir string
	var gotEnv []string
	up := false
	own := &OwnCompanion{
		ConfigDir: cfg,
		Binary:    "/opt/magi",
		Alive:     func(string) bool { return up },
		Spawn: func(bin, dir string, env []string) error {
			gotBin, gotDir, gotEnv = bin, dir, env
			up = true
			return nil
		},
	}
	st, err := own.Ensure()
	if err != nil {
		t.Fatalf("띄우기가 실패했다: %v", err)
	}
	if !st.Started {
		t.Fatal("띄웠는데 Started 가 거짓이다")
	}
	if gotBin != "/opt/magi" {
		t.Fatalf("다른 바이너리를 불렀다: %s", gotBin)
	}
	if gotDir != DeckSpace(cfg) {
		t.Fatalf("워크스페이스가 아닌 곳에서 띄웠다: %s", gotDir)
	}
	// **이것이 이 시험의 요점이다.** 안 물려주면 데몬은 자기 기본값을 보고 우리는 여기를 보는데,
	// 그러면 데몬은 떴는데 우리 눈에는 안 보인다. 실물에서 정확히 그 화면을 봤다(2026-09-02).
	want := "MAGI_CONFIG_DIR=" + cfg
	found := false
	for _, e := range gotEnv {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("설정 디렉토리를 안 물려줬다 — 데몬과 우리가 다른 곳을 본다. 원한 것: %s", want)
	}
	// 워크스페이스는 **띄우기 전에** 있어야 한다.
	if st, err := os.Stat(DeckSpace(cfg)); err != nil || !st.IsDir() {
		t.Fatalf("워크스페이스를 안 만들었다: %v", err)
	}
}

// 띄웠는데 안 서면 **「띄웠습니다」라고 하지 않는다.**
//
// 여기서 낙관하면 다음 호출이 연결 거부이고, 그 사유는 이 자리에서 이미 알 수 있었던 것이다.
func TestStartedButNotListeningIsNotSuccess(t *testing.T) {
	cfg := t.TempDir()
	own := &OwnCompanion{
		ConfigDir: cfg,
		Binary:    "magi",
		Alive:     func(string) bool { return false }, // 띄운 뒤에도 안 답한다
		Spawn:     func(string, string, []string) error { return nil },
	}
	st, err := own.Ensure()
	if err == nil {
		t.Fatal("안 서는데 성공으로 답했다")
	}
	if st.Started {
		t.Fatal("안 서는데 Started 가 참이다")
	}
	// **로그 자리를 알려 준다.** 사유가 로그에만 있으므로, 주소를 안 주면 사람이 할 일이 없다.
	if !strings.Contains(err.Error(), ".log") {
		t.Fatalf("데몬이 남긴 말이 어디 있는지 안 알려 준다: %v", err)
	}
}

// 띄우기가 실패하면 **데몬이 뭐라고 했는지** 싣는다.
func TestASpawnFailureCarriesWhatTheDaemonSaid(t *testing.T) {
	own := &OwnCompanion{
		ConfigDir: t.TempDir(),
		Binary:    "magi",
		Alive:     func(string) bool { return false },
		Spawn: func(string, string, []string) error {
			return errors.New("config.toml 을 못 읽었습니다")
		},
	}
	_, err := own.Ensure()
	if err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("데몬의 사유가 안 실렸다: %v", err)
	}
}

// magi 를 **우리 옆에서 먼저** 찾는다.
//
// PATH 를 먼저 보면 다른 판본이 잡히고, 헬퍼와 데몬의 판본이 어긋난 채로 도는데 그 어긋남은
// 아무 데도 안 적힌다.
func TestMagiIsLookedForBesideUsFirst(t *testing.T) {
	name := "magi"
	if runtime.GOOS == "windows" {
		name = "magi.exe"
	}
	beside := filepath.Join("/opt/app", name)
	own := &OwnCompanion{
		Self:   "/opt/app/magi-ppt",
		Exists: func(p string) bool { return p == beside },
		Look:   func(string) (string, error) { return "/usr/bin/" + name, nil },
	}
	got, err := own.FindMagi()
	if err != nil {
		t.Fatalf("옆에 있는데 못 찾았다: %v", err)
	}
	if got != beside {
		t.Fatalf("PATH 쪽을 골랐다: %s", got)
	}
}

// 옆에 없으면 PATH 를 본다.
func TestMagiFallsBackToPath(t *testing.T) {
	own := &OwnCompanion{
		Self:   "/opt/app/magi-ppt",
		Exists: func(string) bool { return false },
		Look:   func(n string) (string, error) { return "/usr/bin/" + n, nil },
	}
	got, err := own.FindMagi()
	if err != nil {
		t.Fatalf("PATH 에 있는데 못 찾았다: %v", err)
	}
	if !strings.HasPrefix(got, "/usr/bin/") {
		t.Fatalf("엉뚱한 것을 골랐다: %s", got)
	}
}

// 아무 데도 없으면 **어디를 봤는지 적는다.**
//
// 「magi 를 못 찾았습니다」만으로는 사람이 할 일이 없다. 이 도구를 쓰는 사람은 PATH 가 뭔지
// 모를 수 있으므로, 본 자리를 그대로 보여 주는 것이 유일하게 행동으로 옮길 수 있는 말이다.
func TestNotFindingMagiSaysWhereItLooked(t *testing.T) {
	own := &OwnCompanion{
		Self:   "/opt/app/magi-ppt",
		Exists: func(string) bool { return false },
		Look:   func(string) (string, error) { return "", errors.New("not found") },
	}
	_, err := own.FindMagi()
	if err == nil {
		t.Fatal("없는데 찾았다고 했다")
	}
	for _, want := range []string{filepath.Join("/opt/app", "magi"), "PATH"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("본 자리 %q 가 사유에 없다: %v", want, err)
		}
	}
}

// 설정 디렉토리를 모르면 **빈 경로에 폴더를 만들지 않는다.**
func TestEnsureRefusesWithoutAConfigDir(t *testing.T) {
	if _, err := (&OwnCompanion{}).Ensure(); !errors.Is(err, errNoConfigDir) {
		t.Fatalf("빈 설정 디렉토리를 그냥 받았다: %v", err)
	}
}
