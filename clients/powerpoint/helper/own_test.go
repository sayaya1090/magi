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

// 소켓 이름은 **폴더가 생긴 뒤에** 지어야 데몬의 것과 같다.
//
// `daemon.WorkspaceKey` 는 `filepath.EvalSymlinks` 로 경로를 푼 뒤 해시하는데, 그 함수는 **없는
// 경로에서 실패한다** — 실패하면 안 푼 철자를 그대로 해시한다. 그래서 첫 실행에서 폴더를 만들기
// 전에 이름을 지으면, 자기 cwd(이미 있는 폴더)를 기준으로 짓는 데몬과 다른 자리를 보게 된다.
//
// 증상이 고약하다: 데몬은 멀쩡히 뜨고 `--detach` 는 자기 소켓에 성공했다고 답하는데 우리는
// 「띄웠는데 답하지 않습니다」를 적고, 다시 해 볼 때마다 하나를 더 띄우며 30초씩 태운다.
//
// **앞 시험은 이걸 못 잡았다** — 같은 없는 경로에 같은 유도를 두 번 부르고 견줬을 뿐이라, 둘 다
// 똑같이 틀려도 통과한다. 여기서는 `Ensure` 가 실제로 고른 자리를, 폴더가 생긴 뒤의 정답과
// 견준다.
func TestTheSocketNameIsChosenAfterTheFolderExists(t *testing.T) {
	cfg := t.TempDir()
	own := &OwnCompanion{
		ConfigDir: cfg,
		Binary:    "magi",
		Alive:     func(string) bool { return true },
		Spawn:     func(string, string, []string) error { return nil },
	}
	st, err := own.Ensure()
	if err != nil {
		t.Fatalf("서 있는데 실패했다: %v", err)
	}
	// 폴더가 생긴 지금 다시 유도하면 그것이 **데몬이 볼 자리**다.
	want := daemon.SocketPath(cfg, DeckSpace(cfg))
	if st.Socket != want {
		t.Fatalf("헬퍼와 데몬이 다른 자리를 본다:\n  헬퍼가 고른 곳: %s\n  데몬이 볼 곳:  %s",
			st.Socket, want)
	}
	if _, err := os.Stat(DeckSpace(cfg)); err != nil {
		t.Fatalf("폴더를 안 만들었다: %v", err)
	}
}

// 폴더를 못 만들면 **그 자리를 이름 대어** 말한다. 소켓 이름은 아직 지을 수 없으므로 안 지어낸다.
func TestAnUnmakeableFolderIsNamed(t *testing.T) {
	// 파일을 폴더 자리에 놓아 MkdirAll 을 막는다.
	cfg := t.TempDir()
	if err := os.WriteFile(DeckSpace(cfg), []byte("나는 폴더가 아니다"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := (&OwnCompanion{ConfigDir: cfg, Binary: "magi"}).Ensure()
	if err == nil {
		t.Fatal("폴더를 못 만드는데 성공으로 답했다")
	}
	if !strings.Contains(err.Error(), DeckSpace(cfg)) {
		t.Fatalf("어느 자리인지 안 알려 준다: %v", err)
	}
	if st.Socket != "" {
		t.Fatalf("폴더도 없는데 소켓 이름을 지어냈다: %s", st.Socket)
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

// 개발자 셸의 설정이 **덱을 고치는 컴패니언의 성질이 되지 않는다.**
//
// 헬퍼는 로그인할 때 뜨거나 개발자의 셸에서 뜬다. 그 셸에 `MAGI_PROFILE=yolo` 가 켜져 있다고
// 해서 슬라이드에 손대는 에이전트가 그 자세를 물려받을 이유는 없다 — 사람은 파워포인트를
// 열었을 뿐이고, 자기 셸 설정이 그렇게 쓰인다는 것을 알 길이 없다.
func TestTheShellsPostureIsNotInheritedByTheDeckCompanion(t *testing.T) {
	from := []string{
		"PATH=/usr/bin",
		"MAGI_PROFILE=yolo",
		"MAGI_PERMISSION=allow",
		"MAGI_FLEET_LISTEN=0.0.0.0:9999",
		"MAGI_CONFIG_DIR=/somewhere/else",
	}
	got := deckEnv("/my/config", from)
	joined := strings.Join(got, "\n")
	for _, bad := range []string{"MAGI_PROFILE", "MAGI_PERMISSION", "MAGI_FLEET_LISTEN"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("%s 를 물려줬다 — 셸의 설정이 덱 에이전트의 권한이 된다:\n%s", bad, joined)
		}
	}
	// 상관없는 것은 그대로 둔다 — 걷어 내는 것이 목적이 아니라 이 셋이 문제다.
	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatalf("상관없는 환경까지 걷어 냈다:\n%s", joined)
	}
	// 설정 디렉토리는 **우리 것이 이긴다.** os/exec 는 마지막 것을 쓴다.
	if got[len(got)-1] != "MAGI_CONFIG_DIR=/my/config" {
		t.Fatalf("설정 디렉토리가 안 덮였다: %v", got)
	}
}

// `MAGI_FLEET_LISTEN` 은 등급이 다르다 — 그게 있으면 magi 는 **데몬이 되기 전에 돌아간다.**
// 소켓은 영영 안 생기고 --detach 는 30초를 태운 뒤 실패하는데, 그 사유는 아무 데도 안 적힌다.
func TestTheFleetDoorEnvIsAlwaysRemoved(t *testing.T) {
	got := deckEnv("/c", []string{"MAGI_FLEET_LISTEN=:9999"})
	for _, kv := range got {
		if strings.HasPrefix(kv, "MAGI_FLEET_LISTEN") {
			t.Fatalf("남아 있다: %v", got)
		}
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

// TestTheCompanionWeStartAsksBeforeItChangesTheDeck 는 우리가 띄우는 컴패니언이 **묻는가**를 잰다.
//
// 실물에서 나왔다(2026-09-04, Mac). 헬퍼가 마련한 컴패니언을 `/api/companions` 로 물으니
// `"permission": "allow"` 였다 — `DESIGN.md` §5.0 이 「우리가 띄우는 데몬은 모드를 명시한다 —
// `ask` 다」라고 못 박아 둔 자리인데 명령줄에 그 값이 없었다. 문서만 고쳐져 있었다.
//
// **자동 시험 넷이 다 초록인 채로 그랬다.** 인자는 `Spawn` 이음매로 안 나르므로 이 레인의
// 어느 층도 볼 수 없었고, 사람이 붙여 보고 명단을 읽어야 보였다.
func TestTheCompanionWeStartAsksAndTheRulesOpenTheDeck(t *testing.T) {
	args := daemonArgs()
	if len(args) == 0 {
		t.Fatal("명령줄이 비었다 — 이 시험은 아무것도 안 쟀다")
	}
	mode := ""
	for i, a := range args {
		if a == "--permission" && i+1 < len(args) {
			mode = args[i+1]
		}
	}
	// allow 다 — 사용자 결정(2026-09-05 밤). ask 였던 여섯 시간의 사유는 own.go 의 주석에 있다. 모드는
	// 명시적으로 적혀 있어야 한다(안 적으면 기동 형태의 기본값이 정한다).
	if mode != "allow" {
		t.Errorf("우리가 띄우는 컴패니언의 권한 모드가 %q 다 — %q 여야 한다. 명령줄: %v", mode, "allow", args)
	}
	for _, want := range []string{"--daemon", "--detach", "--no-update-check"} {
		found := false
		for _, a := range args {
			if a == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s 가 빠졌다 — %v", want, args)
		}
	}
}
