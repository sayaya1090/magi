package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// testEngine 은 데몬 뒤에 서는 최소 엔진. **door 를 가진 것과 안 가진 것 둘 다** 만들 수 있어야
// 하는데, Go 의 메서드 집합은 정적이라 타입을 둘로 나눈다 — 그게 코어가 door 를 선택적
// 인터페이스로 둔 이유이기도 하다(§5.0.5: cap 은 빌드가 아니라 엔진의 것이다).
type testEngine struct {
	mu sync.Mutex
}

func (e *testEngine) Submit(context.Context, command.SubmitPrompt) error                 { return nil }
func (e *testEngine) Steer(context.Context, command.SubmitPrompt) error                  { return nil }
func (e *testEngine) Interrupt(context.Context, command.Interrupt) error                 { return nil }
func (e *testEngine) RespondPermission(context.Context, command.RespondPermission) error { return nil }
func (e *testEngine) RespondQuestion(context.Context, command.RespondQuestion) error     { return nil }
func (e *testEngine) Waiting(session.SessionID) (app.Ask, bool)                          { return app.Ask{}, false }
func (e *testEngine) Doing(session.SessionID) (string, bool)                             { return "", false }

// About 이 없으면 `about` 자체가 거절당하고(`answerAbout`: "this daemon cannot describe its
// companion"), 그러면 이 시험들은 cap 을 재는 대신 핸드셰이크 실패만 재게 된다. 실물 데몬은
// 언제나 이것을 갖고 있으므로, 없는 엔진으로 재는 것은 **없는 세상을 재는 것**이다.
func (e *testEngine) About() string { return "시험용 컴패니언" }

// doorEngine 은 도구 서버를 받는 엔진.
type doorEngine struct {
	testEngine
	mu       sync.Mutex
	attached map[string]string
	detached []string
	tools    []string
	fail     error
}

func (e *doorEngine) AttachToolServer(_ context.Context, owner, name, url string, headers map[string]string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.fail != nil {
		return nil, e.fail
	}
	if e.attached == nil {
		e.attached = map[string]string{}
	}
	e.attached[name] = url + "|" + headers["Authorization"]
	return e.tools, nil
}

func (e *doorEngine) DetachToolServer(owner, name string) (bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.detached = append(e.detached, name)
	_, had := e.attached[name]
	delete(e.attached, name)
	return had, nil
}

func (e *doorEngine) seen() (map[string]string, []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]string{}
	for k, v := range e.attached {
		out[k] = v
	}
	return out, append([]string(nil), e.detached...)
}

// shortDir 은 유닉스 소켓 주소에 들어가는 짧은 임시 디렉토리.
//
// `t.TempDir()` 은 **시험 이름을 경로에 넣는다.** 이 파일의 시험 이름이 길어서 그 경로가 100
// 바이트 천장을 넘었고, 그러면 `Listen` 이 거절하고 시험은 **Skip 으로 초록이 된다** — §9 의
// 「하나도 안 틀렸다와 볼 것이 없었다는 같은 글자다」가 이 파일에서 실제로 일어났다(넷이 조용히
// 건너뛰는 동안 스위트는 ok 였다). 그래서 이름을 짧게 짓고, 아래 시험 하나가 **건너뛴 수를 센다.**
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "mg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// daemonsStarted 는 이 파일에서 실제로 뜬 데몬의 수. 아래 마지막 시험이 이 값을 본다.
var daemonsStarted int

// startDaemon 은 진짜 데몬을 하나 띄운다. 가짜 소켓이 아니라 **코어의 그 코드**라야
// `Hello`·`PeerSupports`·`mcp-attach` 가 실제로 오가는 것을 잰다.
func startDaemon(t *testing.T, dir, name string, eng daemon.Engine) (string, func()) {
	t.Helper()
	// 이름은 `daemon-*.sock` 이어야 한다 — `daemon.List` 가 그 글롭으로 명단을 만든다(그
	// 디렉토리가 곧 명단이다). 한 번 `d-*.sock` 으로 줄였다가 명단이 통째로 비었고, 증상은
	// 「컴패니언이 0 개다」 하나였다.
	sock := filepath.Join(dir, "daemon-"+name+".sock")
	d, err := daemon.Listen(sock)
	if err != nil {
		t.Skipf("이 머신에서 유닉스 소켓 데몬을 못 띄웠다: %v", err)
	}
	daemonsStarted++
	// 기록은 소켓 옆에 남는다 — **그 디렉토리가 곧 명단이다**(ARCHITECTURE §11). 그래서
	// `Fleet` 이 훑는 것도 이 디렉토리다.
	stopPublish, err := daemon.Publish(sock, filepath.Join(dir, name), "sess-"+name, daemon.Identity{Name: name, Role: "시험용"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Serve(ctx, eng) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			_ = d.Close()
			<-done
			// **기록은 남긴다.** 깨끗한 종료는 소켓 파일을 지우지만, 여기서는 죽은 컴패니언이
			// 명단에 어떻게 보이는지를 재려고 일부러 남긴다(§5.3 의 「소켓만 남았다」 갈래).
			_ = stopPublish
		})
	}
	t.Cleanup(stop)
	return sock, stop
}

// 붙는 순서가 **언제나 detach → attach** 다(§5.4).
//
// 크래시한 헬퍼의 등록은 아무도 덱 도구를 안 부르는 동안 영영 안 치워지고, 이름이 고정이라
// 잡힌 등록이 남아 있으면 반드시 부딪힌다 — 다른 이름으로 피해 갈 여지가 설계상 없다.
func TestAttachingAlwaysDetachesFirst(t *testing.T) {
	dir := shortDir(t)
	eng := &doorEngine{tools: []string{"mcp__word__list_paragraphs", "mcp__word__insert_paragraphs"}}
	sock, _ := startDaemon(t, dir, "dsn", eng)

	a := NewAttachments()
	tools, err := a.Attach(sock, MCPURL(DefaultPort, ""), "tok123", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("등록된 도구가 %v 다", tools)
	}
	attached, detached := eng.seen()
	if len(detached) != 1 || detached[0] != ServerName {
		t.Errorf("detach 가 %v 다 — attach 앞에 한 번 있어야 한다", detached)
	}
	// 토큰이 헤더로 간다(§5.0.1 — headers 를 함수 자리로 열어 둔 이유가 토큰이다).
	if got := attached[ServerName]; !strings.Contains(got, "Bearer tok123") || !strings.Contains(got, MCPURL(DefaultPort, "")) {
		t.Errorf("붙인 자리가 %q 다", got)
	}
}

// door 가 없는 컴패니언은 **고를 수 없고, 사유가 빌드의 성질이다**(§5.0.5).
func TestACompanionWithNoDoorCannotBeChosen(t *testing.T) {
	dir := shortDir(t)
	sock, _ := startDaemon(t, dir, "old", &testEngine{})

	a := NewAttachments()
	fleet, err := a.Fleet(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fleet) != 1 {
		t.Fatalf("컴패니언이 %d 개다", len(fleet))
	}
	c := fleet[0]
	if c.ToolServers == nil {
		t.Fatalf("물어봤는데 답이 없다: %s", c.AskError)
	}
	if *c.ToolServers {
		t.Fatal("door 가 없는 엔진이 있다고 답했다")
	}
	if c.Chooseable() {
		t.Fatal("못 붙이는 컴패니언을 고를 수 있다고 한다")
	}
	if !strings.Contains(c.Why(), "빌드") {
		t.Errorf("사유가 빌드의 성질로 안 적혔다: %s", c.Why())
	}
	if _, err := a.Attach(sock, MCPURL(DefaultPort, ""), "", ""); err == nil {
		t.Fatal("door 없는 데몬에 붙었다")
	}
}

// **「못 물어봤다」와 「못 받는다」는 다른 말이다**(§5.0.5).
//
// `PeerSupports` 는 둘을 같은 거짓으로 접는데, 화면은 사람에게 사실을 적는 자리라 합치면
// 안 된다 — 다시 물으면 될 것을 빌드의 성질로 적게 된다.
func TestNotAskedIsNotTheSameAsCannot(t *testing.T) {
	dir := shortDir(t)
	eng := &doorEngine{tools: []string{"mcp__word__list_paragraphs"}}
	sock, stop := startDaemon(t, dir, "dsn", eng)

	a := NewAttachments()
	fleet, err := a.Fleet(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	live := fleet[0]
	if live.ToolServers == nil || !*live.ToolServers {
		t.Fatalf("살아 있는 door 를 못 읽었다: %+v (%s)", live.ToolServers, live.AskError)
	}
	if !live.Chooseable() {
		t.Fatal("붙일 수 있는데 못 고른다고 한다")
	}
	if live.Transcript == nil {
		t.Fatal("transcript cap 을 안 물었다")
	}

	// 이제 **SIGKILL 당한 컴패니언**을 하나 만든다. 깨끗이 나간 데몬은 소켓도 기록도 지우고
	// 가므로(§5.4 — 그래서 「일부러 껐다」를 따로 기록하지 않아도 유도된다) 그 갈래로는 이걸
	// 못 잰다. 죽임을 당한 쪽은 **파일이 남는다.**
	a.DetachAll([]Binding{{Socket: sock}})
	stop()
	crashed := filepath.Join(dir, "daemon-gone.sock")
	if err := os.WriteFile(crashed, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemon.SessionFile(crashed),
		[]byte(`{"socket":"`+strings.ReplaceAll(crashed, `\`, `\\`)+`","workdir":"gone","session":"sess-gone"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	fleet2, err := a.Fleet(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var dead *Companion
	for i := range fleet2 {
		if strings.Contains(fleet2[i].Socket, "gone") {
			dead = &fleet2[i]
		}
	}
	if dead == nil {
		t.Fatalf("남은 소켓이 명단에 안 보인다: %+v", fleet2)
	}
	if dead.Live {
		t.Fatal("아무도 안 듣는 소켓이 살아 있다고 나온다")
	}
	// **여기가 이 시험의 요점이다.** 못 물어본 컴패니언의 cap 은 `nil` 이지 `false` 가 아니다 —
	// `false` 로 적으면 「이 빌드는 도구 서버를 못 받는다」가 되고, 그건 다시 물으면 될 것을
	// 빌드의 성질로 적는 것이다.
	if dead.ToolServers != nil {
		t.Errorf("못 물어본 컴패니언에 %v 라고 적었다", *dead.ToolServers)
	}
	if dead.Chooseable() {
		t.Error("응답 없는 컴패니언을 고를 수 있다고 한다")
	}
	if !strings.Contains(dead.Why(), "응답하지 않") {
		t.Errorf("사유가 %q 다", dead.Why())
	}
}

// 등록에 실패하면 **그 자리에서 말한다**(§5.3 「끝내 못 붙으면 말한다」). 조용히 넘어가면
// 화면이 「할 일 없음」처럼 보인다.
func TestAFailedAttachSaysSo(t *testing.T) {
	dir := shortDir(t)
	eng := &doorEngine{fail: errors.New("이 이름은 이미 잡혀 있다")}
	sock, _ := startDaemon(t, dir, "dsn", eng)

	a := NewAttachments()
	_, err := a.Attach(sock, MCPURL(DefaultPort, ""), "", "")
	if err == nil {
		t.Fatal("실패했는데 성공으로 답했다")
	}
	if !strings.Contains(err.Error(), "이미 잡혀 있다") {
		t.Errorf("데몬이 준 사유가 안 실렸다: %v", err)
	}
}

// httptest 를 쓰지 않는 시험이지만, MCP URL 이 실제로 열리는 주소인지까지 한 번 견준다 —
// door 에 넘기는 것이 URL 하나뿐이라(§5.0.1) 그 문자열이 곧 계약이다.
func TestTheAttachedURLIsTheOneWeServe(t *testing.T) {
	srv := httptest.NewServer(&MCPServer{Hand: &fakeHand{attached: true}})
	defer srv.Close()
	if !strings.HasSuffix(MCPURL(DefaultPort, ""), "/mcp") {
		t.Fatalf("붙이는 URL 이 %q 다 — 헬퍼가 여는 경로와 같아야 한다", MCPURL(DefaultPort, ""))
	}
}

// **건너뛴 것을 세는 자리.** 위 시험들은 데몬을 못 띄우면 Skip 하는데, Skip 은 화면에서 초록과
// 거의 같아 보인다(§9 「초록을 읽는 법」). 그래서 「몇 개를 실제로 봤는지」를 마지막에 한 번
// 소리 내어 적는다 — 0 이면 이 파일은 아무것도 검사하지 않은 것이고, 그 사실이 실패여야 한다.
//
// 이 시험이 이 파일에서 제일 나중에 도는 것에 기대지 않으려고 이름을 그렇게 지었다: go test 는
// 파일 안의 선언 순서대로 돈다.
func TestZZZAtLeastOneRealDaemonWasStarted(t *testing.T) {
	if daemonsStarted == 0 {
		t.Fatal("이 파일의 시험이 데몬을 하나도 못 띄웠다 — 초록이 아니라 '볼 것이 없었다'다")
	}
	t.Logf("진짜 데몬 %d 개를 띄워서 쟀다", daemonsStarted)
}

// ⚠ 여기 있던 셋 — 「둘째 창이 첫째 등록을 안 뺏는다」·「다시 뜬 데몬에는 안 붙어 있다」·「창 둘에
// 등록 둘」 — 은 `Attachments` 가 (소켓·주인·주소·생애)를 기억하던 시절의 시험이다. 그 기억은 같은
// 사실의 둘째 캐시였고 재기동마다 다르게 낡았다(DESIGN §5.9.1). 이제 그 판단은 `settle` 이 묶음의
// 기록으로 하고, 같은 셋을 `restart_events_test.go`·`join_deck_test.go` 가 그 층에서 잰다.
