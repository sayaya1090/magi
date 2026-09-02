package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 적어 두면 **magi 가 매 턴 읽는 그 자리**에 놓인다.
//
// 그 자리를 우리가 새로 정하면 안 된다 — magi 는 워크스페이스의 AGENTS.md 를 매 시스템 프롬프트에
// 넣고 압축에도 안 날린다(internal/app/memory.go). 다른 이름에 적으면 아무도 안 읽고, 화면에는
// 저장됐다고 적힌다.
func TestInstructionsLandWhereTheEngineReadsThem(t *testing.T) {
	cfg := t.TempDir()
	if _, err := WriteInstructions(cfg, "불릿은 한 줄로"); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(DeckSpace(cfg), "AGENTS.md")
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("엔진이 읽는 자리에 없다(%s): %v", want, err)
	}
	if strings.TrimSpace(string(body)) != "불릿은 한 줄로" {
		t.Fatalf("적은 것과 다르다: %q", string(body))
	}
}

// 아직 아무것도 안 적은 것은 **실패가 아니다** — 그게 기본 상태다.
func TestNoInstructionsIsNotAFailure(t *testing.T) {
	text, err := ReadInstructions(t.TempDir())
	if err != nil {
		t.Fatalf("없는 것을 실패로 답했다: %v", err)
	}
	if text != "" {
		t.Fatalf("없는데 무언가를 줬다: %q", text)
	}
}

// **비우는 것이 지우는 것이다.** 빈 파일을 남기면 「아무것도 안 적힘」과 「파일 없음」이 두
// 상태가 되는데, 사람에게는 같은 뜻이고 우리에게만 다르다.
func TestEmptyInstructionsRemoveTheFile(t *testing.T) {
	cfg := t.TempDir()
	if _, err := WriteInstructions(cfg, "무언가"); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteInstructions(cfg, "   \n  "); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(instructionsFile(cfg)); !os.IsNotExist(err) {
		t.Fatalf("비웠는데 파일이 남아 있다: %v", err)
	}
	if text, _ := ReadInstructions(cfg); text != "" {
		t.Fatalf("비웠는데 읽힌다: %q", text)
	}
}

// 너무 길면 **자르지 않고 거절한다.**
//
// 조용히 자르면 사람이 적어 둔 규칙의 뒷부분이 어느 날부터 안 지켜지는데, 화면에는 저장됐다고
// 적혀 있다 — 이 저장소가 최악이라고 적은 그 모양이다.
func TestTooLongInstructionsAreRefusedNotTrimmed(t *testing.T) {
	cfg := t.TempDir()
	long := strings.Repeat("가", maxInstructions+1)
	if _, err := WriteInstructions(cfg, long); err == nil {
		t.Fatal("너무 긴데 받아 줬다")
	} else if !strings.Contains(err.Error(), "매번") {
		t.Fatalf("왜 안 되는지 안 적는다: %v", err)
	}
	if _, err := os.Stat(instructionsFile(cfg)); !os.IsNotExist(err) {
		t.Fatal("거절했는데 파일을 만들었다")
	}
}

// 앞뒤 공백은 다듬되 **안쪽은 안 건드린다** — 사람이 적은 줄 나눔이 규칙의 일부일 수 있다.
func TestInstructionsKeepTheirShape(t *testing.T) {
	cfg := t.TempDir()
	got, err := WriteInstructions(cfg, "\n\n첫 줄\n  들여쓴 줄\n\n마지막 줄\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != "첫 줄\n  들여쓴 줄\n\n마지막 줄" {
		t.Fatalf("안쪽을 건드렸다: %q", got)
	}
}

// 이 자리도 **토큰과 루프백을 지나고**, 읽기와 쓰기가 한 주소에 있다.
func TestTheInstructionsEndpointReadsAndWrites(t *testing.T) {
	cfg := t.TempDir()
	api := &API{Bridge: NewBridge(), Attachments: NewAttachments(), ConfigDir: cfg, Work: NewOwnWork()}

	// 처음에는 비어 있다.
	w := httptest.NewRecorder()
	api.instructions(w, httptest.NewRequest(http.MethodGet, "/api/instructions", nil))
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["text"] != "" {
		t.Fatalf("처음부터 무언가 적혀 있다: %v", got)
	}
	if got["path"] == "" || got["path"] == nil {
		t.Fatalf("어느 파일인지 안 알려 준다: %v", got)
	}

	// 적는다.
	w = httptest.NewRecorder()
	api.instructions(w, httptest.NewRequest(http.MethodPost, "/api/instructions",
		strings.NewReader(`{"text":"표는 머리글을 굵게"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("적기가 실패했다(%d): %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	// **언제부터 듣는지 말한다.** 「저장했습니다」만 적으면 사람은 지금 도는 턴에도 걸리는 줄 안다.
	if note, _ := got["note"].(string); !strings.Contains(note, "다음 부탁부터") {
		t.Fatalf("언제부터 듣는지 안 적는다: %q", note)
	}

	// 다시 읽으면 그대로 있다.
	w = httptest.NewRecorder()
	api.instructions(w, httptest.NewRequest(http.MethodGet, "/api/instructions", nil))
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["text"] != "표는 머리글을 굵게" {
		t.Fatalf("적은 것이 안 남았다: %v", got)
	}
}

// **읽기와 쓰기가 같은 모양이어야 한다.**
//
// 저장할 때만 다듬으면 화면이 보여 주는 글과 저장될 글이 달라서, 사람이 고치지도 않았는데
// 「바뀜」이 뜨고 저장 단추가 켜진다.
func TestReadingGivesBackWhatWritingStored(t *testing.T) {
	cfg := t.TempDir()
	stored, err := WriteInstructions(cfg, "  첫 줄\n둘째 줄  \n\n")
	if err != nil {
		t.Fatal(err)
	}
	back, err := ReadInstructions(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if back != stored {
		t.Fatalf("읽은 것과 쓴 것이 다르다:\n  쓴 것: %q\n  읽은 것: %q", stored, back)
	}
}
func TestInstructionsAreBehindTheSameGuard(t *testing.T) {
	api := &API{
		Bridge: NewBridge(), Attachments: NewAttachments(), ConfigDir: t.TempDir(),
		Token: "s3cret", Own: &OwnCompanion{ConfigDir: t.TempDir()}, Work: NewOwnWork(),
	}
	mux := http.NewServeMux()
	api.Route(mux)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/instructions", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("토큰 없이 지나갔다: %d %s", w.Code, w.Body.String())
	}
}
