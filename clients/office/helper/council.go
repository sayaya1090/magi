package office

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
)

// 카운슬 스위치 — 이 컴패니언의 종료 게이트를 켜고 끈다.
//
// **재기동 스위치다, 런타임 스위치가 아니다.** 카운슬이 있는가는 데몬이 뜰 때 정해지고 그 위에 세 가지가
// 선다: `council` 도구의 등록(cmd/magi applyCouncilAvailability), 착지 플러그인이 「게이트」로 설지
// 「land 도구」로 설지(plugins/landing, 로드 때 `magi.council_enabled()`), 헬퍼 도구 설명의 마무리 문장.
// 셋을 도는 중에 뒤집는 문은 없고, 만들면 플러그인이 낡은 쪽에 남는다. 그래서 이 문은 컴패니언 설정의
// `[council] enabled` 를 고쳐 쓰고 데몬의 `restart` 문을 두드린다 — 데몬은 같은 인자로 다시 뜨고, 창은
// 재기동 사건(§4.2)을 보고 새 대화로 붙는다. **대화가 새로 시작된다**는 것이 이 스위치의 값이고, 창의
// 단추가 그 말을 적는다.
//
// 설정 파일은 **글로 고친다.** BurntSushi 로 풀었다 다시 묶으면 사람이 적어 둔 주석(그 파일에는 왜 ask 였는지,
// 왜 소넷인지가 적혀 있다)이 전부 사라진다. `[council]` 절 안의 `enabled = …` 한 줄만 바꾸고, 절이 없으면 끝에
// 붙인다.

// CouncilConfigPath 는 파워포인트 컴패니언의 설정 파일. 데몬이 읽는 그 자리다(cmd/magi loadConfigLayers —
// `companions/<워크스페이스 키>/config.toml`). 키는 소켓 이름과 같은 것이라 다른 문과 어긋나지 않는다.
func (a *App) CouncilConfigPath(configDir string) string {
	return filepath.Join(config.CompanionDir(configDir, daemon.WorkspaceKey(a.DeckSpace(configDir))), "config.toml")
}

// ReadCouncilSwitch 는 설정이 말하는 값. 절이나 줄이 없으면 코어 기본값(켜짐)이다.
func ReadCouncilSwitch(path string) (bool, error) {
	var doc struct {
		Council struct {
			Enabled *bool `toml:"enabled"`
		} `toml:"council"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return true, err
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return true, err
	}
	if doc.Council.Enabled == nil {
		return true, nil
	}
	return *doc.Council.Enabled, nil
}

// WriteCouncilSwitch 는 `[council] enabled` 한 줄만 바꾼다. 나머지 글자는 그대로다.
func WriteCouncilSwitch(path string, on bool) error {
	want := "false"
	if on {
		want = "true"
	}
	return writeTomlLine(path, "council", "enabled", want)
}

// SeedLandingSocket 은 착지 플러그인에게 **이 데몬의 소켓**을 알려 준다 — `[plugins.landing] socket`.
//
// 플러그인은 제 데몬의 소켓을 모른다(브리지가 주는 것은 작업 디렉토리와 OS 이름뿐이고 `os` 도 없다).
// 그런데 land 없이 끝난 턴에 모델을 다시 부르려면(`magi --relay <소켓>` 으로 submit) 그 경로가 있어야
// 한다. 아는 쪽은 여기다 — 데몬을 띄우는 쪽이니. 데몬이 뜨기 전에 심는다(설정은 기동 때 읽힌다).
// 값이 이미 같으면 파일을 안 건드린다 — 사람이 적어 둔 파일의 mtime 을 매 기동마다 바꾸지 않는다.
func (a *App) SeedLandingSocket(configDir string) error {
	path := a.CouncilConfigPath(configDir)
	want := strconv.Quote(a.DeckSocket(configDir))
	if data, err := os.ReadFile(path); err == nil && tomlLine(string(data), "plugins.landing", "socket") == want {
		return nil
	}
	return writeTomlLine(path, "plugins.landing", "socket", want)
}

// writeTomlLine 은 `[section]` 절의 `key = …` 한 줄만 바꾼다(없으면 만든다). 나머지 글자는 그대로다 —
// BurntSushi 로 풀었다 다시 묶으면 사람이 적어 둔 주석이 전부 사라진다.
func writeTomlLine(path, section, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body := tomlSetLine(string(data), section, key, value)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

// tomlLine 은 그 절의 그 키가 지금 무슨 값인지(원문 그대로). 없으면 "".
func tomlLine(text, section, key string) string {
	in := false
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=\s*(.*?)\s*(#.*)?$`)
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = t == "["+section+"]"
			continue
		}
		if in {
			if m := re.FindStringSubmatch(line); m != nil {
				return strings.TrimSpace(m[1])
			}
		}
	}
	return ""
}

// tomlSetLine 은 순수 함수 — 시험이 잰다. 값은 TOML 원문(따옴표 포함)이다.
func tomlSetLine(text, section, key, value string) string {
	re := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)(.*?)(\s*#.*)?$`)
	lines := strings.Split(text, "\n")
	in, header := false, -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			in = t == "["+section+"]"
			if in {
				header = i
			}
			continue
		}
		if in {
			if m := re.FindStringSubmatch(line); m != nil {
				lines[i] = m[1] + value + m[3]
				return strings.Join(lines, "\n")
			}
		}
	}
	if header >= 0 {
		// 절은 있는데 줄이 없다 — 머리 바로 아래에 넣는다.
		out := append([]string{}, lines[:header+1]...)
		out = append(out, key+" = "+value)
		out = append(out, lines[header+1:]...)
		return strings.Join(out, "\n")
	}
	body := strings.TrimRight(text, "\n")
	if body != "" {
		body += "\n\n"
	}
	return body + "[" + section + "]\n" + key + " = " + value + "\n"
}

// councilSwitched 는 옛 이름 — 시험이 부른다.
func councilSwitched(text string, on bool) string {
	if on {
		return tomlSetLine(text, "council", "enabled", "true")
	}
	return tomlSetLine(text, "council", "enabled", "false")
}

// restartOn 은 그 소켓의 데몬에게 제 restart 문을 두드린다. 데몬은 같은 인자로 다시 뜬다(graceful.Reexec).
func (a *API) restartOn(socket string) error {
	if a.Restart != nil {
		return a.Restart(socket)
	}
	cl, err := daemon.DialWithin(socket, aliveTimeout, aliveTimeout)
	if err != nil {
		return err
	}
	defer cl.Close()
	return cl.Restart()
}

// council — GET 은 설정이 말하는 값, POST {enabled} 는 바꾸고 다시 띄운다.
//
// 답의 `note` 는 창이 그대로 띄우는 한 줄이다. 「바꿨습니다」로 끝내지 않는다 — 바뀐 것은 파일이고, 화면이
// 달라지는 것은 데몬이 다시 뜬 뒤라 그 사이가 몇 초 있다. 그 몇 초를 사람이 알아야 「눌렀는데 그대로다」가
// 안 된다.
func (a *API) council(w http.ResponseWriter, r *http.Request) {
	path := a.App.CouncilConfigPath(a.ConfigDir)
	if r.Method == http.MethodGet {
		on, err := ReadCouncilSwitch(path)
		out := map[string]any{"enabled": on, "path": path}
		if err != nil {
			out["warning"] = err.Error()
		}
		writeJSON(w, out)
		return
	}
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		writeStatus(w, http.StatusBadRequest, map[string]any{"error": "enabled 가 없습니다 — true 나 false"})
		return
	}
	socket, _, _ := a.chat(r).Bound()
	if socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{
			"error": "아직 아무 컴패니언에도 안 붙어 있어서 다시 띄울 데몬이 없습니다",
		})
		return
	}
	if err := WriteCouncilSwitch(path, *in.Enabled); err != nil {
		writeStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "path": path})
		return
	}
	word := "껐습니다"
	if *in.Enabled {
		word = "켰습니다"
	}
	if err := a.restartOn(socket); err != nil {
		// 파일은 바뀌었다 — 다음 기동부터 그 값이다. 그 사실을 적는다.
		writeStatus(w, http.StatusBadGateway, map[string]any{
			"enabled": *in.Enabled, "path": path, "restarted": false,
			"error": "설정은 바꿨는데 컴패니언을 다시 못 띄웠습니다: " + err.Error() + " — 다음에 뜰 때부터 그 값입니다",
		})
		return
	}
	writeStatus(w, http.StatusAccepted, map[string]any{
		"enabled": *in.Enabled, "path": path, "restarted": true,
		"note": "카운슬을 " + word + " — 컴패니언을 다시 띄우는 중입니다. 몇 초 뒤 새 대화로 붙습니다(" + a.App.PartKo + " 그대로).",
	})
}
