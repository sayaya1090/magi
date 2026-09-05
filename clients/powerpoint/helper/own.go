package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/quietconsole"
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

// aliveTimeout 은 「거기 서 있는가」 한 번에 드는 시간의 천장이다.
//
// 넉넉하되 유한하다. 이 물음은 **사람이 기다리는 동안** 도는 것이라, 답이 안 오는 것과 「없다」는
// 사람에게 같은 뜻이다 — 다만 우리 쪽에서 그것을 「없다」로 적어 줘야 다음 걸음을 뗀다.
const aliveTimeout = 3 * time.Second

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

	// **폴더를 먼저 만든다 — 소켓 이름을 짓기 전에.**
	//
	// `daemon.WorkspaceKey` 는 경로의 심볼릭 링크를 푼 뒤 해시하는데, `filepath.EvalSymlinks` 는
	// **없는 경로에서는 실패한다.** 실패하면 안 푼 철자를 그대로 해시하므로, 폴더가 생기기 전에
	// 지은 이름과 생긴 뒤에 지은 이름이 다를 수 있다 — 그리고 데몬은 자기 cwd(이미 있는 폴더)를
	// 기준으로 짓는다. 이 머신에서 재 봤다(2026-09-02): 없을 때는 `GetFileAttributesEx ...` 로
	// 실패하고, 생긴 뒤에는 대소문자까지 정규화된다.
	//
	// 어긋나면 증상이 고약하다. 데몬은 멀쩡히 뜨고 `--detach` 는 **자기** 소켓에 성공했다고 답하는데,
	// 우리는 「띄웠는데 답하지 않습니다」를 적는다. 그리고 다시 해 볼 때마다 또 하나를 띄우고
	// 30초 `detachWait` 를 태운다. 리뷰가 짚었다.
	if err := os.MkdirAll(space, 0o755); err != nil {
		return OwnState{Workdir: space}, fmt.Errorf("덱 작업 폴더 %s 를 못 만들었습니다: %w", space, err)
	}

	socket := DeckSocket(o.ConfigDir)
	st := OwnState{Socket: socket, Workdir: space, Log: socket + ".log"}

	alive := o.Alive
	if alive == nil {
		alive = probeAlive
	}
	if alive(socket) {
		return st, nil
	}

	bin, err := o.FindMagi()
	if err != nil {
		return st, err
	}

	spawn := o.Spawn
	if spawn == nil {
		spawn = runDetached
	}
	env := deckEnv(o.ConfigDir, os.Environ())
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

// scrubbed 는 **물려주면 안 되는** 환경 변수들.
//
// 헬퍼는 사람이 로그인할 때 뜨거나 개발자의 셸에서 뜬다. 그 셸에 무엇이 켜져 있든 **덱을 고치는
// 컴패니언의 성질이 될 이유는 없다** — 사람은 파워포인트를 열었을 뿐이고, 그 순간 자기 셸의
// 설정이 슬라이드에 손대는 에이전트의 권한이 된다는 것을 알 길이 없다. 리뷰가 짚었다(2026-09-02).
//
// `MAGI_FLEET_LISTEN` 은 등급이 더 나쁘다. 그 값이 있으면 `magi` 는 플릿 문 갈래로 빠져 **데몬이
// 되기 전에 돌아간다** — 소켓은 영영 안 생기고, `--detach` 는 30초를 태운 뒤 실패하고, 우리는
// 「못 띄웠습니다」를 적는데 그 사유는 아무 데도 안 적혀 있다.
var scrubbed = []string{
	"MAGI_FLEET_LISTEN",
	"MAGI_PROFILE",
	"MAGI_PERMISSION",
}

// deckEnv 는 덱 컴패니언에게 줄 환경. 설정 디렉토리를 물려주고, 위 셋은 걷어 낸다.
//
// **설정 디렉토리는 반드시 물려준다.** 안 물려주면 데몬은 자기 기본값(Windows 는
// `%APPDATA%\magi`)을 보고 우리는 여기를 본다 — 데몬은 떴는데 우리 눈에는 안 보이는 상태가
// 된다. 실물에서 정확히 그 화면을 봤다(2026-09-02): 소켓은 만들어졌는데 우리 쪽 경로에는
// 아무것도 없었고, 게다가 이 머신에서는 `%APPDATA%` 아래 AF_UNIX 가 연결을 못 받는다.
func deckEnv(configDir string, from []string) []string {
	out := make([]string, 0, len(from)+1)
	for _, kv := range from {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		drop := false
		for _, bad := range scrubbed {
			if name == bad {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	// 뒤에 붙인 것이 이긴다(os/exec 는 마지막 것을 쓴다). 앞의 것도 안 남기고 지우고 싶지만,
	// 지우면 우리가 못 본 다른 철자(대소문자)가 살아남을 수 있어 **덮는 쪽이 확실하다.**
	return append(out, "MAGI_CONFIG_DIR="+configDir)
}

// runDetached 는 `magi --daemon --detach` 를 부른다.
//
// **`--detach` 여야 한다.** 보통 방법으로 띄운 자식은 unix 에서는 부모의 프로세스 그룹에, Windows
// 에서는 부모의 job 안에 남아 **부모와 같이 죽는다.** 헬퍼가 내려가면 컴패니언도 같이 내려간다는 뜻이고,
// IDE 플러그인이 실물에서 정확히 그 일을 겪고 이 플래그를 만들었다(`cmd/magi/detach.go`).
//
// `--no-update-check` 는 기동을 사람이 안 보는 자리에서 붙잡아 두지 않으려는 것이다.
func runDetached(bin, workdir string, env []string) error {
	cmd := exec.Command(bin, daemonArgs()...)
	cmd.Dir = workdir
	cmd.Env = env
	// 헬퍼는 콘솔 없이 떠 있다(설치기가 숨겨 띄운다). 그 자식인 이 콘솔 프로그램은 창을 새로 열었다 — 카운슬
	// 스위치를 누를 때마다 검은 창이 떴다(2026-09-06, 사용자). 우리에게 콘솔이 없을 때만 자식을 숨긴다.
	quietconsole.Apply(cmd)
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

// daemonArgs 는 우리가 띄우는 컴패니언의 명령줄이다. 함수로 뺀 것은 **재려고**다 —
// `Spawn` 이음매는 `(bin, workdir, env)` 만 나르므로 인자는 그 이음매로 안 보인다.
//
// **`--permission ask` 가 여기 있는 이유가 이 함수가 있는 이유다.** 플래그도 config 도 프로파일도
// 비면 모드를 **기동 형태로** 정하고(`cmd/magi/main.go` 의 `headless`), `--daemon` 은 거기서
// headless 쪽이라 기본값이 `allow` 다. 그리고 `internal/app/permission.go` 의 `requestPermission`
// 은 `allow` 에서 **맨 앞에서 참을 돌려준다** — 허용 규칙도 프롬프트도 전부 그 뒤에 있다.
// 즉 안 적으면 **우리가 띄운 컴패니언만 `delete_shape` 까지 안 묻고 돈다.**
//
// 그게 세 곳을 동시에 무너뜨린다. `DESIGN.md` §8 의 「매 호출이 권한 게이트를 지난다」가 하필
// 우리가 만드는 데몬에서 거짓이 되고, §6 이 「쓰기는 허용 규칙에 안 넣는다」로 지키려던 것이
// 지켜지지 않으며, §5.7 이 지은 권한 물음 창이 **한 번도 안 뜬다.** 답할 UI 가 있기 때문에
// `ask` 가 성립한다 — 작업창이 그 UI 다(§5.0).
//
// **`auto` 가 아닌 이유**도 §5.0 에 있다: 답이 없을 때 정책으로 흘려보내므로, 사람이 자리를 비운
// 사이 덱이 바뀐다. 이 설계가 제일 피하려는 일이다.
//
// ⚠ **§5.0 이 요구한 둘 중 하나만 여기 있다.** 나머지 하나(`workspace-write`)는 **명령줄로 줄 수가
// 없다** — `cfg.Sandbox` 는 config·프로파일에서만 오고 `magi --help` 에 `-sandbox` 가 없다
// (2026-09-04 확인). 그러니 §8 의 「덱 디렉토리가 쓰기 루트」는 지금 기본값에 대한 서술이 아니고,
// 그 절반을 여기서 지킬 방법이 없다는 사실을 적어 둔다 — 안 적으면 다음 사람이 이 줄을 보고
// 둘 다 지켜진 줄 안다.
// 권한 모드는 allow 다 — 사용자 결정(2026-09-05 밤, 「아니 allow 로 해」). 그 전 여섯 시간은 ask 였다:
// 「승인 누르기 귀찮아」로 덱 도구 48개와 읽기·기억·검색만 컴패니언 설정의 allow 규칙으로 열고 bash·write·edit
// 는 물었는데, allow 로 통째 열었던 40분 동안 헬퍼 재기동으로 덱 도구가 끊긴 틈에 모델이 bash 로 프로세스·
// 로그·파일을 뒤진 것(17:39)을 보고 그렇게 했었다. 사용자가 그 위험을 알고 allow 를 골랐다 — 묻는 창이
// 흐름을 끊는 품이 더 크다는 판단이다. 컴패니언 설정의 allow 규칙은 그대로 둔다(모드를 되돌릴 때 그것이 문이다).
func daemonArgs() []string {
	return []string{"--daemon", "--detach", "--no-update-check", "--permission", "allow"}
}

// errNoConfigDir 는 설정 디렉토리 없이 부른 경우. 있을 수 없지만, 있으면 조용히 빈 경로에 폴더를
// 만드는 것보다 낫다.
var errNoConfigDir = errors.New("설정 디렉토리를 모르는 채로는 컴패니언을 마련할 수 없습니다")

// probeAlive is the one liveness probe: connect, and if that works the daemon is there.
//
// **A bounded round trip.** `daemon.Dial` has no deadline on the connect or the read, so a connect
// that hangs on a socket file whose owner is gone never returns — and then `OwnWork`'s `doing`
// stays true forever and every task pane shows "getting ready" for as long as the helper lives.
// A review found it (2026-09-02); this machine's %APPDATA% AF_UNIX makes exactly that state.
//
// One copy, because there were two: this closure and `main.go`'s fallback were the same six lines,
// and two probes are two places for the timeout to be forgotten in.
//
// The close is best-effort on purpose and the only discarded return left here: the question was
// "can it be reached", connecting answered it, and what the close says afterwards cannot change
// that answer or reach anybody who would act on it.
func probeAlive(socket string) bool {
	cl, err := daemon.DialWithin(socket, aliveTimeout, aliveTimeout)
	if err != nil {
		return false
	}
	_ = cl.Close()
	return true
}
