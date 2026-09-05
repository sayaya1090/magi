package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 사람이 적어 둔 주석은 그대로고, `[council]` 절의 `enabled` 한 줄만 바뀐다.
func TestCouncilSwitchTouchesOneLineAndKeepsTheRest(t *testing.T) {
	src := "# 왜 소넷인가 — 62초\nmodel = \"sonnet\"\n\n# 카운슬 사유\n[council]\nenabled = true   # 2026-09-05 켬\n\n[plugins.engram]\nlessons = false\n"
	got := councilSwitched(src, false)
	if !strings.Contains(got, "enabled = false   # 2026-09-05 켬") {
		t.Fatalf("enabled 줄이 안 바뀌었거나 주석이 날아갔다:\n%s", got)
	}
	if !strings.Contains(got, "# 왜 소넷인가 — 62초") || !strings.Contains(got, "lessons = false") {
		t.Fatalf("다른 줄이 바뀌었다:\n%s", got)
	}
	if strings.Count(got, "enabled =") != 1 {
		t.Fatalf("enabled 줄이 하나가 아니다:\n%s", got)
	}
	// 다른 절의 enabled 는 안 건드린다.
	src2 := "[plugins.claudecode]\nenabled = true\n\n[council]\nenabled = false\n"
	got2 := councilSwitched(src2, true)
	if !strings.Contains(got2, "[plugins.claudecode]\nenabled = true") || !strings.Contains(got2, "[council]\nenabled = true") {
		t.Fatalf("엉뚱한 절을 건드렸다:\n%s", got2)
	}
}

func TestCouncilSwitchAddsWhatIsMissing(t *testing.T) {
	if got := councilSwitched("model = \"sonnet\"\n", false); !strings.HasSuffix(got, "model = \"sonnet\"\n\n[council]\nenabled = false\n") {
		t.Fatalf("절이 없으면 끝에 붙어야 한다:\n%s", got)
	}
	if got := councilSwitched("[council]\n# 아직 값 없음\n\n[x]\ny = 1\n", true); !strings.HasPrefix(got, "[council]\nenabled = true\n# 아직 값 없음") {
		t.Fatalf("절은 있고 줄이 없으면 머리 아래에 넣어야 한다:\n%s", got)
	}
	if got := councilSwitched("", true); got != "[council]\nenabled = true\n" {
		t.Fatalf("빈 파일: %q", got)
	}
}

// 읽기는 파일이 말하는 값 — 없으면 코어 기본(켜짐).
func TestCouncilSwitchReadsTheFileOrTheDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if on, err := ReadCouncilSwitch(path); err != nil || !on {
		t.Fatalf("없는 파일은 기본값 켜짐이어야 한다: %v %v", on, err)
	}
	if err := WriteCouncilSwitch(path, false); err != nil {
		t.Fatal(err)
	}
	if on, err := ReadCouncilSwitch(path); err != nil || on {
		t.Fatalf("끈 뒤 읽으니 %v %v", on, err)
	}
	if err := WriteCouncilSwitch(path, true); err != nil {
		t.Fatal(err)
	}
	if on, _ := ReadCouncilSwitch(path); !on {
		t.Fatal("켠 뒤 읽으니 꺼져 있다")
	}
}

// 문: POST 는 설정을 고치고 붙어 있는 데몬을 다시 띄운다. 안 붙어 있으면 아무것도 안 고친다.
func TestTheCouncilDoorWritesThenRestarts(t *testing.T) {
	cfg := t.TempDir()
	restarted := []string{}
	api := &API{
		Bridge: NewBridge(), Bridges: NewBridges(), Attachments: NewAttachments(), ConfigDir: cfg,
		Restart: func(socket string) error { restarted = append(restarted, socket); return nil },
	}
	body, _ := json.Marshal(map[string]any{"enabled": false})
	w := httptest.NewRecorder()
	api.council(w, httptest.NewRequest("POST", "/api/council?deck=d1", strings.NewReader(string(body))))
	if w.Code != 409 {
		t.Fatalf("안 붙었는데 %d — 다시 띄울 데몬이 없다: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(CouncilConfigPath(cfg)); err == nil {
		t.Fatal("안 붙었는데 설정을 고쳤다")
	}
	if err := api.Bridges.For("d1").Bind("/sock-a", "s1"); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	api.council(w, httptest.NewRequest("POST", "/api/council?deck=d1", strings.NewReader(string(body))))
	if w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if on, _ := ReadCouncilSwitch(CouncilConfigPath(cfg)); on {
		t.Fatal("설정이 꺼지지 않았다")
	}
	if len(restarted) != 1 || restarted[0] != "/sock-a" {
		t.Fatalf("붙어 있는 데몬을 다시 안 띄웠다: %v", restarted)
	}
	if !strings.Contains(w.Body.String(), "새 대화로 붙습니다") {
		t.Fatalf("대화가 새로 시작된다는 말이 없다: %s", w.Body.String())
	}
	// GET 은 파일이 말하는 값.
	w = httptest.NewRecorder()
	api.council(w, httptest.NewRequest("GET", "/api/council?deck=d1", nil))
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["enabled"] != false {
		t.Fatalf("GET 이 파일과 다르다: %v", got)
	}
	// 재기동이 실패해도 파일은 바뀌었다 — 그 사실을 적는다.
	api.Restart = func(string) error { return os.ErrDeadlineExceeded }
	body, _ = json.Marshal(map[string]any{"enabled": true})
	w = httptest.NewRecorder()
	api.council(w, httptest.NewRequest("POST", "/api/council?deck=d1", strings.NewReader(string(body))))
	if w.Code != 502 || !strings.Contains(w.Body.String(), "다음에 뜰 때부터") {
		t.Fatalf("재기동 실패를 안 적었다: %d %s", w.Code, w.Body.String())
	}
	if on, _ := ReadCouncilSwitch(CouncilConfigPath(cfg)); !on {
		t.Fatal("재기동이 실패했어도 파일은 바뀌어 있어야 한다")
	}
}

// 착지 플러그인이 쓸 소켓을 설정에 심는다 — 같은 값이면 파일을 안 건드리고, 다른 절은 그대로다.
func TestTheLandingSocketIsSeededOnce(t *testing.T) {
	cfg := t.TempDir()
	path := CouncilConfigPath(cfg)
	if err := WriteCouncilSwitch(path, false); err != nil {
		t.Fatal(err)
	}
	if err := SeedLandingSocket(cfg); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	want := "[plugins.landing]\nsocket = " + strconv.Quote(DeckSocket(cfg))
	if !strings.Contains(string(body), want) || !strings.Contains(string(body), "[council]\nenabled = false") {
		t.Fatalf("심은 모양이 다르다:\n%s", body)
	}
	st1, _ := os.Stat(path)
	time.Sleep(15 * time.Millisecond)
	if err := SeedLandingSocket(cfg); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(path)
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Fatal("같은 값인데 파일을 다시 썼다")
	}
	if got := tomlLine(string(body), "plugins.landing", "socket"); got != strconv.Quote(DeckSocket(cfg)) {
		t.Fatalf("되읽은 값이 다르다: %s", got)
	}
}
