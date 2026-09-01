package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// 파워포인트가 **자기 컴패니언을 갖는다** — 남의 워크스페이스를 빌리지 않는다.
//
// # 왜 빌리면 안 되는가
//
// 앞 판본은 이 머신의 데몬을 훑어 명단으로 보여 주고 사람에게 고르게 했다(`attach.go`). 그 화면이
// 성립하려면 **이미 데몬이 떠 있어야** 하고, 데몬이 뜨려면 워크스페이스가 있어야 하고, 워크스페이스는
// 저장소에서 일하는 사람만 갖고 있다. 메일에서 받은 `.pptx` 를 더블클릭한 사람은 **빈 명단**을 보고
// 거기서 끝난다. 게다가 「컴패니언을 고르세요」는 그 사람 머릿속에 대응하는 개념이 없는 말이다.
// 이 도구의 목표가 PC 를 잘 다루지 못하는 사람이면, 첫 화면이 막다른 길인 설계다.
//
// # 왜 「덱이 든 폴더」가 아닌가
//
// 처음에 그렇게 하려 했다 — 발표 옆에는 늘 이미지·csv·이전 판이 있으니 그게 워크스페이스라고. 그런데
// 덱이 열리는 네 경우 중 셋에 폴더가 없다: **새 프레젠테이션은 저장 전까지 경로 자체가 없고**(그리고
// 그게 미숙련자의 기본 시나리오다), 메일 첨부는 `Content.Outlook\A1B2C3` 이고, OneDrive 는 `https://`
// 다. 없는 이득을 지키려던 셈이었다.
//
// # 그래서 magi 가 가진 폴더 하나
//
// 데몬에게는 진짜 디렉토리가 필요하다. 워크스페이스가 **세션이 아니라 데몬의 성질**인 것은 우연이
// 아니라 못 박은 결정이라(`daemon.go` — "a method that let the caller name a directory would be a way
// to run commands anywhere on that machine from a page"), 세션마다 다른 디렉토리를 주는 길로 뚫으면 안
// 된다. 대신 magi 가 소유한 폴더 하나를 워크스페이스로 준다. 불변식은 그대로고, 그 워크스페이스가
// 저장소가 아니라 **덱 작업용 마당**일 뿐이다.
//
// 덱이 저장돼 있어 경로를 아는 경우는, 그 경로를 **워크스페이스 뿌리로 삼는 대신 세션에 사실로
// 실어** 보내는 것이 맞다 — 셸의 cwd 로 만드는 것보다 사정거리가 훨씬 좁고, 바탕화면에서 연 덱이
// 바탕화면 전체를 워크스페이스로 만드는 일도 없다.

// deckSpaceName 은 그 폴더의 이름. 설정 디렉토리 **안**에 둔다 — 사람의 문서 트리에 폴더를 만들지
// 않고, 소켓 이름(`daemon-powerpoint-…`)이 `ls` 에서 읽히는 값이 된다.
const deckSpaceName = "powerpoint"

// DeckSpace 는 파워포인트 컴패니언이 도는 워크스페이스.
func DeckSpace(configDir string) string { return filepath.Join(configDir, deckSpaceName) }

// DeckSocket 은 그 컴패니언이 서는 자리.
//
// **유도식이 데몬의 것과 같아야 한다.** 한 글자만 달라도 「여기 데몬이 없다」로 읽히고, 그러면 우리는
// 이미 떠 있는 것 옆에 하나를 더 띄운다.
func DeckSocket(configDir string) string {
	return daemon.SocketPath(configDir, DeckSpace(configDir))
}

// OwnCompanion 은 파워포인트 몫의 컴패니언을 마련하는 일.
//
// 갈래를 **주입으로** 갈라 둔 것은 시험 때문만이 아니다. 「magi 를 못 찾았다」와 「띄웠는데 안 선다」와
// 「이미 서 있다」는 사람이 할 일이 각각 다른데, 실물에 붙여야만 재지는 코드로 두면 그 셋이 한 문장으로
// 뭉개진다.
type OwnCompanion struct {
	ConfigDir string
	// Binary 는 magi 실행 파일. 비어 있으면 찾는다.
	Binary string
	// Look 은 PATH 조회(기본 `exec.LookPath`).
	Look func(string) (string, error)
	// Exists 는 파일이 있는가(기본 `os.Stat` 성공).
	Exists func(string) bool
	// Spawn 은 데몬을 띄운다. 기본은 `magi --daemon --detach` 이고, **그 명령이 스스로 기다린다** —
	// 돌아오면 소켓이 답하는 상태다(`cmd/magi/detach.go`).
	Spawn func(bin, workdir string, env []string) error
	// Alive 는 그 소켓이 지금 답하는가.
	Alive func(socket string) bool
	// Self 는 이 바이너리의 경로(기본 `os.Executable`). magi 를 그 옆에서 먼저 찾는다.
	Self string
}

// OwnState 는 마련해 본 결과.
type OwnState struct {
	// Socket 은 그 컴패니언이 선 자리. 실패했으면 비어 있지 않고 **어디였어야 하는지**를 담는다 —
	// 사람이 로그를 찾아갈 주소다.
	Socket string
	// Workdir 는 워크스페이스.
	Workdir string
	// Started 는 이번에 우리가 띄웠는가. `false` 이고 오류가 없으면 이미 서 있었다는 뜻이다.
	Started bool
	// Log 는 데몬이 자기 말을 적는 자리. `--detach` 가 소켓 옆에 둔다.
	Log string
}

// magiNames 는 찾을 이름들. Windows 는 확장자가 붙는다.
func magiNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"magi.exe"}
	}
	return []string{"magi"}
}

// FindMagi 는 magi 실행 파일을 찾는다 — **우리 옆을 먼저** 본다.
//
// 설치본은 둘이 나란히 놓인다. PATH 를 먼저 보면 다른 판본의 magi 가 잡힐 수 있고, 그러면 헬퍼와
// 데몬의 판본이 어긋난 채로 도는데 **그 어긋남은 아무 데도 안 적힌다.**
func (o *OwnCompanion) FindMagi() (string, error) {
	if o.Binary != "" {
		return o.Binary, nil
	}
	exists := o.Exists
	if exists == nil {
		exists = func(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
	}
	self := o.Self
	if self == "" {
		if got, err := os.Executable(); err == nil {
			self = got
		}
	}
	var looked []string
	if self != "" {
		for _, name := range magiNames() {
			beside := filepath.Join(filepath.Dir(self), name)
			if exists(beside) {
				return beside, nil
			}
			looked = append(looked, beside)
		}
	}
	look := o.Look
	if look == nil {
		look = exec.LookPath
	}
	for _, name := range magiNames() {
		if got, err := look(name); err == nil {
			return got, nil
		}
		looked = append(looked, "PATH 의 "+name)
	}
	// **어디를 봤는지 적는다.** 「magi 를 못 찾았습니다」만으로는 사람이 할 일이 없다.
	return "", fmt.Errorf("magi 실행 파일을 못 찾았습니다 — 여기를 봤습니다: %s. "+
		"magi 를 이 헬퍼 옆에 두거나 PATH 에 넣어 주세요", strings.Join(looked, ", "))
}

// Ensure 는 파워포인트 몫의 컴패니언이 **서 있게** 한다.
//
// 이미 서 있으면 아무것도 안 한다 — 데몬은 워크스페이스마다 하나이고, 둘째 창이 열리는 것만으로 하나
// 더 뜨면 그 둘이 한 워크스페이스를 두고 다툰다.
func (o *OwnCompanion) Ensure() (OwnState, error) {
	if strings.TrimSpace(o.ConfigDir) == "" {
		return OwnState{}, errNoConfigDir
	}
	space := DeckSpace(o.ConfigDir)
	socket := DeckSocket(o.ConfigDir)
	st := OwnState{Socket: socket, Workdir: space, Log: socket + ".log"}

	alive := o.Alive
	if alive == nil {
		alive = func(s string) bool {
			cl, err := daemon.Dial(s)
			if err != nil {
				return false
			}
			_ = cl.Close()
			return true
		}
	}
	if alive(socket) {
		return st, nil
	}

	// 워크스페이스가 없으면 만든다. **데몬을 띄우기 전에** — 없는 디렉토리에서 시작한 데몬은
	// 「그 자리에 없다」로 죽는데, 그 사유가 로그에만 남는다.
	if err := os.MkdirAll(space, 0o755); err != nil {
		return st, fmt.Errorf("덱 작업 폴더 %s 를 못 만들었습니다: %w", space, err)
	}

	bin, err := o.FindMagi()
	if err != nil {
		return st, err
	}

	spawn := o.Spawn
	if spawn == nil {
		spawn = runDetached
	}
	// **설정 디렉토리를 물려준다.** 안 물려주면 데몬은 자기 기본값(Windows 는 `%APPDATA%\magi`)을
	// 보고, 우리는 여기를 본다 — 데몬은 떴는데 우리 눈에는 안 보이는 상태가 된다. 실물에서 정확히
	// 그 화면을 봤다(2026-09-02): 소켓은 만들어졌는데 우리 쪽 소켓 경로에는 아무것도 없었고, 게다가
	// 이 머신에서는 `%APPDATA%` 아래 AF_UNIX 가 연결을 못 받는다(TESTING §5.1).
	env := append(os.Environ(), "MAGI_CONFIG_DIR="+o.ConfigDir)
	if err := spawn(bin, space, env); err != nil {
		return st, fmt.Errorf("파워포인트 몫의 컴패니언을 못 띄웠습니다: %w (데몬이 남긴 말: %s)", err, st.Log)
	}
	// **띄웠다고 서 있는 것이 아니다.** `--detach` 는 소켓이 답할 때까지 기다렸다가 돌아오지만,
	// 그 약속을 우리가 한 번 더 확인한다 — 여기서 안 보면 「띄웠습니다」 다음 호출이 연결 거부다.
	if !alive(socket) {
		return st, fmt.Errorf("컴패니언을 띄웠는데 %s 가 답하지 않습니다 — 데몬이 남긴 말: %s",
			socket, st.Log)
	}
	st.Started = true
	return st, nil
}

// runDetached 는 `magi --daemon --detach` 를 부른다.
//
// **`--detach` 여야 한다.** 보통 방법으로 띄운 자식은 unix 에서는 부모의 프로세스 그룹에, Windows
// 에서는 부모의 job 안에 남아 **부모와 같이 죽는다.** 헬퍼가 내려가면 컴패니언도 같이 내려간다는 뜻이고,
// IDE 플러그인이 실물에서 정확히 그 일을 겪고 이 플래그를 만들었다(`cmd/magi/detach.go`).
//
// `--no-update-check` 는 기동을 사람이 안 보는 자리에서 붙잡아 두지 않으려는 것이다.
func runDetached(bin, workdir string, env []string) error {
	cmd := exec.Command(bin, "--daemon", "--detach", "--no-update-check")
	cmd.Dir = workdir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	// 데몬이 자기 사유를 적었으면 그걸 싣는다 — exit status 만으로는 사람이 할 일이 없다.
	said := strings.TrimSpace(string(out))
	if said == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, said)
}

// errNoConfigDir 는 설정 디렉토리 없이 부른 경우. 있을 수 없지만, 있으면 조용히 빈 경로에 폴더를
// 만드는 것보다 낫다.
var errNoConfigDir = errors.New("설정 디렉토리를 모르는 채로는 컴패니언을 마련할 수 없습니다")
