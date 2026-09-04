package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/version"
)

// magi-ppt — PowerPoint 애드인의 헬퍼(DESIGN.md §5).
//
// 여기는 **조립만** 한다. 무엇이 무엇인지 아는 자리는 이 파일뿐이고, 안쪽은 서로를 인터페이스로만
// 안다 — 애드인의 `main.js` 가 같은 규율을 지키는 그 자리다.
//
//coverage:ignore — 프로세스 진입점. 아래 조각들은 각자 시험이 있다.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run 은 두 곳에 쓴다. **물어본 것에 답하는 것은 `out`(stdout), 진단은 `log`(stderr)** 다.
//
// 한 곳이었고, 그 한 곳이 stderr 였다. 그래서 매뉴얼 §7 이 시키는 대로 규칙을 받으려고
// `magi-ppt -allow-rules > config.toml` 을 하면 **빈 파일이 조용히 생겼다** — 화면에는 규칙이
// 보이므로 사람은 받은 줄 안다. 2026-09-04 에 실제로 두 번 겪고 고친다.
//
// 가르는 축은 「긴가 짧은가」가 아니라 **「사람이 물어본 것인가」**다. `-version`·`-allow-rules`
// ·`-cert-hint` 는 물음에 대한 답이라 파이프로 받을 수 있어야 하고, 기동 배너·실패 사유는
// 서버가 도는 동안 흘리는 말이라 stderr 가 맞다. 그래서 `fs.SetOutput` 도 `log` 그대로다 —
// 플래그 오류는 답이 아니다.
func run(args []string, out, log io.Writer) int {
	fs := flag.NewFlagSet("magi-ppt", flag.ContinueOnError)
	fs.SetOutput(log)
	var (
		port = fs.Int("port", DefaultPort,
			"애드인이 붙을 포트. **매니페스트의 <SourceLocation> 과 같은 값이어야 한다** — 못 잡으면 다른 번호로 안 흘러간다(§5.5.1)")
		cfgDir = fs.String("config-dir", "",
			"magi 설정 디렉토리(기본값: 플랫폼 것, MAGI_CONFIG_DIR 존중). 여기서 컴패니언 명단을 읽고 인증서를 둔다")
		addin = fs.String("addin", "",
			"애드인 소스 디렉토리(기본값: 이 바이너리 옆의 clients/powerpoint/addin)")
		showRules = fs.Bool("allow-rules", false,
			"덱을 고치지 않는 도구의 허용 규칙을 찍고 나간다(§6). config.toml 에 그대로 붙여 넣는다")
		showVer  = fs.Bool("version", false, "판본을 찍고 나간다")
		showCert = fs.Bool("cert-hint", false, "인증서를 신뢰 저장소에 넣는 법을 찍고 나간다")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Fprintf(out, "magi-ppt %s\n", version.String())
		return 0
	}
	if *showRules {
		// 산문으로 두면 안 자란다 — 도구를 하나 더할 때 규칙도 같이 자라야 하므로 코드가 만든다.
		fmt.Fprint(out, AllowRulesTOML())
		return 0
	}

	dir := *cfgDir
	if dir == "" {
		dir = platform.OS{}.ConfigDir()
	}
	if *showCert {
		fmt.Fprintln(out, CertInstallHint(dir))
		return 0
	}
	root := *addin
	if root == "" {
		root = defaultAddinRoot()
	}
	if _, err := os.Stat(filepath.Join(root, "taskpane.html")); err != nil {
		fmt.Fprintf(log, "애드인 소스를 못 찾았습니다(%s): %v\n-addin 으로 자리를 알려 주세요.\n", root, err)
		return 1
	}

	cert, err := LoadOrCreateCert(dir)
	if err != nil {
		fmt.Fprintf(log, "인증서를 못 마련했습니다: %v\n", err)
		return 1
	}
	token, err := newToken()
	if err != nil {
		fmt.Fprintf(log, "토큰을 못 만들었습니다: %v\n", err)
		return 1
	}

	ln, what, err := Acquire(BindAddr(*port), cert)
	switch {
	case err != nil:
		fmt.Fprintf(log, "%v\n", err)
		return 1
	case what == ClaimOurs:
		// 헬퍼는 사용자당 하나다(§5.2). 두 번째 기동은 **조용히 물러나되 왜인지는 말한다** —
		// 조용한 성공과 조용한 실패는 화면에서 같아 보인다.
		fmt.Fprintf(log, "이미 이 계정의 헬퍼가 %s 에 서 있습니다. 두 번째는 안 띄웁니다.\n", Origin(*port))
		return 0
	}

	hub := NewHandHub()
	bridge := NewBridge()
	attachments := NewAttachments()
	mux := http.NewServeMux()

	handHTTP := &HandHTTP{Hub: hub, Token: token, Feed: func(string) <-chan StreamFrame {
		ch, _ := bridge.Subscribe()
		return ch
	}}
	mux.Handle("/mcp", &MCPServer{Hand: hub, Token: token, Council: func() bool {
		// 데몬이 답한다(`daemon.Status.Council`). 못 닿으면 거짓 — 모르는 채로 없는 도구를
		// 가리키느니 안 적는 쪽이 낫다.
		st, err := bridge.Status()
		if err != nil {
			return false
		}
		on, _ := st["council"].(bool)
		return on
	}})
	mux.HandleFunc(handStreamPath, handHTTP.Stream)
	mux.HandleFunc(handReplyPath, handHTTP.Reply)

	api := &API{
		Bridge: bridge, Attachments: attachments, Hub: hub,
		Token: token, ConfigDir: dir, Port: *port,
		Own:  &OwnCompanion{ConfigDir: dir},
		Work: NewOwnWork(),
	}
	api.Route(mux)

	pages := &Pages{Root: root, Token: token, Boot: map[string]any{
		"version": version.Version,
		"origin":  Origin(*port),
	}}
	mux.Handle("/", pages.Handler())

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	// **컴패니언을 미리 마련해 둔다.**
	//
	// 실측(2026-09-02): 이 머신에서 데몬 냉시동이 **165초**다. 그걸 작업창을 연 뒤에 시작하면
	// 사람은 3분을 「준비 중」 화면 앞에서 보낸다 — PC 를 잘 다루지 못하는 사람에게 그건 고장과
	// 구별이 안 된다. 헬퍼는 애드인을 깐 사람의 머신에서 도는 것이므로, 여기서 시작하면 판을
	// 열 무렵에는 대개 이미 서 있다.
	//
	// **기동을 안 붙잡는다.** 뒤에서 돌고, 실패해도 헬퍼는 그대로 선다 — 명단으로 가는 길이
	// 남아 있고, 사유는 판이 `/api/own` 으로 물으면 그대로 나온다.
	if _, mine := api.Work.Begin(); mine {
		go api.makeOwn()
	}

	fmt.Fprintf(log, "magi-ppt %s\n애드인: %s\nMCP: %s\n애드인 소스: %s\n",
		version.Version, PageURL(*port), MCPURL(*port), root)
	fmt.Fprintln(log, CertInstallHint(dir))

	// 나갈 때 **우리 등록을 뗀다**(§5.4). 남겨 두면 다음에 뜬 헬퍼가 이름 충돌로 거절당하고,
	// 그 사이 모델에게는 손이 없는 도구가 광고된다.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	fmt.Fprintln(log, "나갑니다 — 붙여 둔 등록을 뗍니다.")
	attachments.DetachAll()
	bridge.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// A graceful shutdown that runs out of time is worth a line: it means a request was still in
	// flight when the deadline passed, and whoever restarts this helper is entitled to know that
	// rather than to guess from a port that is briefly still held.
	if err := srv.Shutdown(ctx); err != nil {
		fmt.Fprintln(log, "정리 중에 시한이 지났습니다 —", err)
	}
	return 0
}

// defaultAddinRoot 는 바이너리 옆에서 애드인을 찾는다. 저장소에서 바로 돌릴 때가 흔하므로
// 소스 트리 자리도 같이 본다 — 못 찾으면 **말하고 멈춘다**(위 run 이 그렇게 한다).
func defaultAddinRoot() string {
	candidates := []string{
		filepath.Join("clients", "powerpoint", "addin"),
		filepath.Join("..", "addin"),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "addin"),
			filepath.Join(filepath.Dir(exe), "clients", "powerpoint", "addin"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "taskpane.html")); err == nil {
			return c
		}
	}
	return candidates[0]
}

// API 는 애드인이 두드리는 자리들(§5.7). 전부 토큰이 필요하고, 전부 루프백이다.
type API struct {
	// mu·hostCaps 는 **창이 잰 요구 집합**을 든다. 창 안에서만 잴 수 있는 값이고, 창에만 두면
	// 사람이 화면을 읽어야만 아는 값이 된다(`caps` 핸들러의 주석).
	mu       sync.Mutex
	hostCaps map[string]any

	Bridge      *Bridge
	Attachments *Attachments
	Hub         *HandHub
	Token       string
	ConfigDir   string
	Port        int
	// Own 은 **파워포인트 몫의 컴패니언**. 명단에서 남의 워크스페이스를 골라 빌리는 대신 이것을
	// 마련한다(own.go) — 메일에서 받은 덱을 더블클릭한 사람에게는 명단이 늘 비어 있다.
	Own *OwnCompanion
	// ReadFleet·Bolt 는 명단을 읽고 도구를 붙이는 길. **시험만 이 자리를 채운다** — 기본값은 바로
	// 아래 둘이다. 주입 자리가 없으면 이 핸들러의 실패 갈래는 실물 소켓 없이는 못 재고, 못 재는
	// 갈래는 안 만든 것과 같다(TESTING §1).
	ReadFleet func(configDir string) ([]Companion, error)
	Bolt      func(socket, url, token string) ([]string, error)
	// Ours 는 「아까 붙여 둔 것이 지금도 그대로인가」. 기본값은 stillOurs 다.
	Ours func(socket string) bool
	// Fresh 는 새 대화를 여는 길. 기본값은 데몬에 묻는 것이다.
	Fresh func(socket string) (string, error)
	// Work 는 그 마련하는 일의 상태. **한 번에 하나만 돈다**(ownstate.go).
	Work *OwnWork
}

// fleetOf·boltOf 는 주입이 없을 때의 진짜 길.
func (a *API) fleetOf(configDir string) ([]Companion, error) {
	if a.ReadFleet != nil {
		return a.ReadFleet(configDir)
	}
	return a.Attachments.Fleet(configDir)
}

func (a *API) boltOf(socket, url, token string) ([]string, error) {
	if a.Bolt != nil {
		return a.Bolt(socket, url, token)
	}
	return a.Attachments.Attach(socket, url, token)
}

func (a *API) Route(mux *http.ServeMux) {
	mux.HandleFunc("/api/own", a.guard(a.own))
	mux.HandleFunc("/api/fresh", a.guard(a.fresh))
	mux.HandleFunc("/api/instructions", a.guard(a.instructions))
	mux.HandleFunc("/api/companions", a.guard(a.companions))
	mux.HandleFunc("/api/choose", a.guard(a.choose))
	mux.HandleFunc("/api/submit", a.guard(a.submit))
	mux.HandleFunc("/api/steer", a.guard(a.steer))
	mux.HandleFunc("/api/interrupt", a.guard(a.interrupt))
	mux.HandleFunc("/api/status", a.guard(a.status))
	mux.HandleFunc("/api/permission", a.guard(a.permission))
	mux.HandleFunc("/api/question", a.guard(a.question))
	mux.HandleFunc("/api/documents", a.guard(a.documents))
	mux.HandleFunc("/api/caps", a.guard(a.caps))
	// 가이드 관리 — 추가·삭제·활성화·비활성화(guides.go).
	mux.HandleFunc("/api/guides", a.guard(a.guides))
	mux.HandleFunc("/api/guide", a.guard(a.guide))
}

// guard 는 **한 자리에서** 루프백과 토큰을 본다. 핸들러마다 적으면 하나를 빠뜨리는 날이 오고,
// 그 하나가 무엇이었는지는 아무도 모른다 — 콘솔이 같은 이유로 라우트 표를 하나 두고 시험이
// 핸들러 목록을 훑는다(SECURITY.md §4).
func (a *API) guard(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !loopbackOnly(w, r) {
			return
		}
		if a.Token != "" {
			got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
			if !constantEquals(got, a.Token) {
				http.Error(w, "이 자리는 페이지가 들고 있는 토큰이 필요합니다", http.StatusUnauthorized)
				return
			}
		}
		h(w, r)
	}
}

func (a *API) companions(w http.ResponseWriter, _ *http.Request) {
	fleet, err := a.Attachments.Fleet(a.ConfigDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(fleet))
	for _, c := range fleet {
		rows = append(rows, map[string]any{
			"companion":  c,
			"chooseable": c.Chooseable(),
			"why":        c.Why(),
		})
	}
	socket, sid, live := a.Bridge.Bound()
	writeJSON(w, map[string]any{
		"companions": rows,
		"bound":      map[string]any{"socket": socket, "session": sid, "streamLive": live},
	})
}

// own 은 **파워포인트 몫의 컴패니언에 붙는다** — 고르는 화면을 거치지 않는다.
//
// 앞 판본은 이 머신의 데몬을 명단으로 보여 주고 사람에게 고르게 했다. 그 화면이 성립하려면 이미
// 데몬이 떠 있어야 하는데, 메일에서 받은 `.pptx` 를 더블클릭한 사람에게는 **늘 비어 있다.**
// 명단은 안 없앤다 — 저장소에서 일하다 코드를 보는 에이전트에게 덱을 맡기고 싶은 경우가 실제로
// 있어서, `/api/companions` 는 「고급」으로 남는다.
//
// **답이 즉시 온다.** 데몬 냉시동은 오래 걸리고(실물에서 120초 요청이 끊겼다, 2026-09-02) 그동안
// 판이 멎어 있으면 사람은 그것을 고장으로 읽는다 — 무엇을 기다리는지도, 다시 눌러야 하는지도
// 모른다. 그래서 이 자리는 **지금 상태를 답하고** 일은 뒤로 보낸다. 판은 다시 물어 진행을 본다.
//
// 실패를 **한 문장으로 뭉치지 않는다.** 「magi 를 못 찾았다」와 「띄웠는데 안 선다」와 「떴는데
// 도구를 못 받는 빌드다」는 사람이 할 일이 각각 다르고, 답에 자리(소켓·워크스페이스·로그)를 실어
// 두는 것이 유일하게 행동으로 옮길 수 있는 말이다.
func (a *API) own(w http.ResponseWriter, _ *http.Request) {
	if a.Own == nil || a.Work == nil {
		http.Error(w, "이 헬퍼는 자기 컴패니언을 마련하도록 세워지지 않았습니다", http.StatusNotImplemented)
		return
	}
	// **`Ready` 를 든 채로 굳지 않는다.**
	//
	// `Begin` 은 이미 `Ready` 면 새 일을 안 시작한다 — 붙은 것을 다시 붙이면 첫 등록이 떨어지기
	// 때문이다(§5.0.1). 그런데 그 사이에 데몬이 죽으면 그 빗장이 **다시 마련하는 길까지** 막는다.
	// 그러면 작업창은 「대화 연결됨」인데 덱 도구는 하나도 없고, 돌아갈 길도 없다 — 이 저장소가
	// 최악이라고 적은 「멀쩡하다고 적힌 고장」이다. 리뷰가 짚었다(2026-09-02): `Forget` 은 그
	// 빗장을 푸는 유일한 손인데 **부르는 자리가 시험 말고는 없었다.**
	if held := a.Work.Now(); held.Phase == OwnReady && !a.oursOf(held.Socket) {
		a.Work.Forget()
	}
	// **뒤늦게 생긴 대화를 잡는다.** 도구는 붙었는데 대화만 비어 있는 상태로 굳으면, 사람이 말을
	// 걸었을 때 「아직 대화가 없습니다」가 돌아오고 되돌릴 길이 없다.
	a.rebindChat()
	now, mine := a.Work.Begin()
	if !mine {
		// 이미 돌고 있거나 이미 다 됐다. **새로 시작하지 않는다** — 덱을 둘 열면 이 자리가 둘에서
		// 두드려지는데, 각자 띄우면 둘이 한 워크스페이스를 두고 다툰다.
		writeJSON(w, now)
		return
	}
	go a.makeOwn()
	writeJSON(w, now)
}

// chatWait·chatTries 는 **대화가 생기기를 기다리는** 만큼이다.
//
// 데몬은 소켓에 선 다음에 자기 기록을 쓴다. 그 사이에 명단을 읽으면 세션 칸이 비어 있고,
// `Bridge.Bind(socket, "")` 는 「이 컴패니언은 아직 대화가 없습니다」로 거절한다.
var (
	chatWait  = 400 * time.Millisecond
	chatTries = 8
)

// waitForFleet 은 명단을 읽되, **우리 컴패니언의 대화 이름이 아직 없으면 잠깐 더 기다린다.**
//
// 실물에서 본 것이다(2026-09-02). 방금 띄운 데몬은 소켓에 선 뒤 자기 기록을 쓰므로, 그 틈에
// 읽으면 세션 칸이 비어 있다. 앞 판본은 그 순간의 답을 그대로 `Ready` 로 굳혔고 — 도구 28개는
// 멀쩡히 붙어 있는데 **사람이 말을 걸면 「아직 대화가 없습니다」**가 돌아왔다. 작업창은
// 「준비됐습니다」라고 적혀 있고, 되돌릴 길은 헬퍼를 죽이는 것뿐이었다.
//
// **일시적인 상태를 영구 등급으로 적지 않는다.** 도구는 붙었는데 대화만 못 여는 갈래는 진짜로
// 있고(그 빌드가 전사를 안 내주는 경우) 그때는 등급을 갈라 적는 것이 맞다(§5.0.5). 다만 「아직」과
// 「못」은 다른 말이고, 여기서 그 둘을 가르는 것은 **한 번 더 물어보는 일**뿐이다.
func (a *API) waitForFleet(socket string) ([]Companion, error) {
	var last []Companion
	for i := 0; i < chatTries; i++ {
		fleet, err := a.fleetOf(a.ConfigDir)
		if err != nil {
			return nil, err
		}
		last = fleet
		for _, c := range fleet {
			if c.Socket == socket && c.Session != "" {
				return fleet, nil
			}
		}
		if i < chatTries-1 {
			time.Sleep(chatWait)
		}
	}
	// 끝내 안 생겼으면 **마지막 답을 그대로 준다** — 지어내지 않는다. 부르는 쪽이 그 사실을
	// `Chat` 칸에 적고, 아래 `rebindChat` 이 나중에라도 생기면 잡는다.
	return last, nil
}

// rebindChat 은 **나중에 생긴 대화를 뒤늦게라도 잡는다.**
//
// `Ready` 인데 대화 이름이 비어 있으면, 그건 「이 컴패니언은 대화를 못 준다」가 아니라 「우리가
// 너무 일찍 물었다」일 수 있다. 작업창은 이 자리를 계속 두드리므로, 그 물음마다 한 번 더 본다.
//
// **도구를 다시 붙이지는 않는다.** 재부착은 첫 등록을 떨어뜨린다(§5.0.1) — 여기서 고치려는 것은
// 대화뿐이고, 도구는 이미 멀쩡히 붙어 있다.
func (a *API) rebindChat() {
	held := a.Work.Now()
	if held.Phase != OwnReady || held.Session != "" || held.Socket == "" {
		return
	}
	fleet, err := a.fleetOf(a.ConfigDir)
	if err != nil {
		return
	}
	for _, c := range fleet {
		if c.Socket != held.Socket || c.Session == "" {
			continue
		}
		if err := a.Bridge.Bind(c.Socket, c.Session); err != nil {
			return
		}
		held.Session = c.Session
		held.Chat = ""
		a.Work.Done(held)
		return
	}
}

// stillOurs 는 아까 붙여 둔 것이 **지금도 그대로인가.**
//
// 둘을 같이 본다. 소켓이 답해도 그건 **다른 생애의** 데몬일 수 있고(같은 경로에 다시 뜬다),
// 그 생애에는 우리 등록이 없다. 반대로 등록 기록만 보면 죽은 데몬을 살아 있다고 읽는다.
//
// 못 읽은 것은 「떨어졌다」로 안 적는다 — `publishedLife` 가 빈 글을 주면 `HasLive` 가 옛 답을
// 그대로 주고, 그 관대함을 여기서 뒤집으면 멀쩡한 등록을 다시 붙이러 가서 첫 등록을 떨어뜨린다.
func (a *API) oursOf(socket string) bool {
	if a.Ours != nil {
		return a.Ours(socket)
	}
	return a.stillOurs(socket)
}

func (a *API) stillOurs(socket string) bool {
	if socket == "" {
		return false
	}
	if !a.Attachments.HasLive(socket, publishedLife(socket)) {
		return false
	}
	if a.Own != nil && a.Own.Alive != nil {
		return a.Own.Alive(socket)
	}
	return probeAlive(socket)
}

// instructions 는 **한 번 적어 두면 매번 지켜지는 말**을 읽고 쓴다(instructions.go).
//
// GET 은 지금 적혀 있는 것, POST 는 새로 적는 것. 빈 글을 보내면 지운다.
//
// **덱마다가 아니라 파워포인트 전체에 걸린다.** 파일이 컴패니언의 워크스페이스에 있고 그
// 워크스페이스가 파워포인트 몫이기 때문이다 — 엑셀이나 저장소 컴패니언은 이 글을 안 본다.
func (a *API) instructions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		text, err := ReadInstructions(a.ConfigDir)
		if err != nil {
			writeStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"text": text, "path": instructionsFile(a.ConfigDir)})
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	text, err := WriteInstructions(a.ConfigDir, in.Text)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	// **언제부터 듣는지 말한다.** 「저장했습니다」만 적으면 사람은 방금 그 말이 지금 도는 턴에도
	// 걸리는 줄 안다. 다음 부탁부터다.
	note := "적어 뒀습니다 — 다음 부탁부터 이 말이 매번 함께 갑니다."
	if text == "" {
		note = "지웠습니다 — 다음 부탁부터는 이 말이 안 갑니다."
	}
	writeJSON(w, map[string]any{"text": text, "path": instructionsFile(a.ConfigDir), "note": note})
}

// fresh 는 **새 대화를 연다** — 「얘가 이상해요」의 탈출구.
//
// 파워포인트 컴패니언은 워크스페이스가 하나라 대화도 하나이고, 그 하나가 **영원히 쌓인다.**
// 실물에서 봤다(2026-09-02): 한 번 헤맨 대화가 그 다음 부탁까지 끌고 가서, 사람이 19번 장을
// 보고 있는데 모델이 8번 장에 정렬을 걸고 6~17번을 헤맸다. 앞 문맥이 뒤를 오염시킨 것이다.
//
// 채팅을 쓰는 사람은 누구나 「새 대화」를 안다. **PC 를 잘 다루지 못하는 사람에게는 그것이
// 유일하게 아는 복구 수단**이고, 그 단추가 없으면 이상해진 판 앞에서 할 수 있는 일이 없다.
//
// **덱은 안 건드린다.** 지우는 것은 대화뿐이고, 슬라이드는 그대로다 — 답이 그렇게 적는다.
func (a *API) fresh(w http.ResponseWriter, _ *http.Request) {
	socket, _, _ := a.Bridge.Bound()
	if socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{
			"error": "아직 아무 컴패니언에도 안 붙어 있어서 새 대화를 열 자리가 없습니다",
		})
		return
	}
	sid, err := a.freshOn(socket)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "socket": socket})
		return
	}
	// **대화를 바꾸면 창도 그 이름으로 옮겨 앉아야 한다.** 안 그러면 새 대화의 이벤트가 전부
	// 남의 것으로 걸러진다 — 실물에서 그 화면을 봤던 자리다(§5.7).
	out := map[string]any{"session": sid, "socket": socket,
		"note": "새 대화를 열었습니다. 슬라이드는 그대로입니다 — 지운 것은 대화뿐입니다."}
	if err := a.Bridge.Bind(socket, sid); err != nil {
		out["chat"] = err.Error()
	}
	// 마련해 둔 기록도 새 이름으로 고친다. 안 고치면 다음 `/api/own` 이 옛 이름을 도로 물린다.
	if held := a.Work.Now(); held.Phase == OwnReady && held.Socket == socket {
		held.Session = sid
		held.Chat = ""
		a.Work.Done(held)
	}
	writeJSON(w, out)
}

// freshOn 은 그 데몬에 새 대화를 청한다. **시험만 이 자리를 채운다.**
func (a *API) freshOn(socket string) (string, error) {
	if a.Fresh != nil {
		return a.Fresh(socket)
	}
	cl, err := daemon.DialWithin(socket, aliveTimeout, aliveTimeout)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	return cl.NewSession()
}

// makeOwn 은 실제로 마련하는 일. **뒤에서 돈다.**
func (a *API) makeOwn() {
	// **깃발은 반드시 내려간다.** `doing` 을 내리는 것은 `Done` 하나뿐이라, 이 아래에서 패닉이
	// 나면 그 깃발이 영영 참으로 남고 헬퍼가 사는 내내 모든 작업창이 「준비하는 중」을 본다.
	// 헬퍼는 **판이 아니라 사람의 파워포인트 옆에서** 도는 프로세스이므로 조용히 죽어도 안 된다.
	defer func() {
		if r := recover(); r != nil {
			a.Work.Done(OwnReport{
				Phase: OwnFailed,
				Why: fmt.Sprintf("컴패니언을 마련하다 내부 오류가 났습니다: %v — "+
					"아래에서 컴패니언을 골라 주세요", r),
			})
		}
	}()
	st, err := a.Own.Ensure()
	if err != nil {
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Why: err.Error(),
			Socket: st.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	// 세션 id 와 「도구를 받을 수 있는가」는 **명단이 이미 답하는 것**이다. 여기서 따로 물으면
	// 같은 것을 두 식으로 재게 되고, 두 식은 언젠가 갈린다.
	fleet, err := a.waitForFleet(st.Socket)
	if err != nil {
		// **방금 띄운 것을 안 띄운 것으로 적지 않는다.** 다른 실패 갈래는 전부 `Started` 와
		// 워크스페이스를 싣는데 여기만 빠뜨리고 있었다 — 그러면 사람이 유일하게 할 수 있는 일
		// (「지금 <워크스페이스> 에 데몬이 하나 돌고 있다」)이 답에서 사라진다.
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started, Why: err.Error(),
			Socket: st.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	var mine *Companion
	for i := range fleet {
		if fleet[i].Socket == st.Socket {
			mine = &fleet[i]
			break
		}
	}
	if mine == nil {
		// 방금 섰다고 확인한 것이 명단에 없다. **사유를 하나로 단정하지 않는다** — 소켓 유도식이
		// 갈렸을 수도 있고, 그 사이 데몬이 죽어 소켓을 치웠을 수도 있다. 둘은 할 일이 다르고,
		// 우리는 여기서 어느 쪽인지 모른다.
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started,
			Why: "컴패니언이 " + st.Socket + " 에 섰는데 명단에 그 자리가 없습니다 — " +
				"그 사이 데몬이 내려갔거나, 헬퍼와 데몬이 서로 다른 자리를 보고 있습니다. " +
				"데몬이 남긴 말: " + st.Log,
			Socket: st.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	if !mine.Chooseable() {
		// **고를 수 없는 이유를 그대로 전한다**(§5.0.5). 「안 됩니다」만으로는 할 일이 없다.
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started, Why: mine.Why(),
			Socket: st.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	tools, err := a.boltOf(mine.Socket, MCPURL(a.Port), a.Token)
	if err != nil {
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started, Why: err.Error(),
			Socket: st.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	if len(tools) == 0 {
		// **붙었다는 증거는 ack 가 아니라 도구 이름이다**(§5.0.1). 이름이 하나도 없으면 붙은 것이
		// 아니고, 그때 `ready` 로 답하면 작업창은 「준비됐습니다 — 도구 0 개」를 적는다. 그
		// 문장이 이 저장소가 최악이라고 적은 그 모양이다.
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started,
			Why: "붙이기는 했는데 덱 도구가 하나도 안 실렸습니다 — 이 컴패니언은 도구 서버를 " +
				"받지 못하는 빌드일 수 있습니다. 데몬이 남긴 말: " + st.Log,
			Socket: mine.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	out := OwnReport{
		Phase: OwnReady, Started: st.Started, Tools: tools,
		Socket: mine.Socket, Session: mine.Session, Workdir: st.Workdir, Log: st.Log,
	}
	if err := a.Bridge.Bind(mine.Socket, mine.Session); err != nil {
		// 붙기는 했고 대화만 못 열었다. **등급이 다른 둘을 한 칸으로 합치지 않는다**(§5.0.5).
		out.Chat = err.Error()
	}
	a.Work.Done(out)
}
func (a *API) choose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Socket  string `json:"socket"`
		Session string `json:"session"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tools, err := a.Attachments.Attach(in.Socket, MCPURL(a.Port), a.Token)
	if err != nil {
		// **끝내 못 붙으면 말한다**(§5.3). 조용히 넘어가면 화면이 「할 일 없음」처럼 보인다.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	bindErr := a.Bridge.Bind(in.Socket, in.Session)
	out := map[string]any{"tools": tools}
	if bindErr != nil {
		// 붙기는 했고 대화만 못 열었다. **등급이 다른 둘을 한 칸으로 합치지 않는다**(§5.0.5).
		out["chat"] = bindErr.Error()
	}
	writeJSON(w, out)
}

// guides 는 목록. **꺼 둔 것도 같이 준다** — 빼면 다시 켤 길이 없다.
func (a *API) guides(w http.ResponseWriter, _ *http.Request) {
	list, err := ListGuides(a.ConfigDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []Guide{}
	}
	writeJSON(w, map[string]any{"guides": list})
}

// guide 는 한 벌을 읽고·쓰고·켜고·끄고·지운다. `op` 로 가른다.
//
// **한 문에 다섯을 넣은 이유**: 다섯이 같은 것(가이드 하나)에 대한 일이고, 화면이 그 다섯을 한
// 자리에서 부른다. 나누면 라우트가 다섯이 되고 토큰 검사도 다섯 벌이 된다.
func (a *API) guide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Op   string `json:"op"` // read | save | enable | disable | delete
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	switch in.Op {
	case "read":
		body, enabled, err := ReadGuide(a.ConfigDir, in.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"name": in.Name, "body": body, "enabled": enabled})
	case "save":
		g, err := WriteGuide(a.ConfigDir, in.Name, in.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, g)
	case "enable", "disable":
		g, err := EnableGuide(a.ConfigDir, in.Name, in.Op == "enable")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, g)
	case "delete":
		if err := DeleteGuide(a.ConfigDir, in.Name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"deleted": in.Name})
	default:
		// **모르는 것을 짐작하지 않는다** — 아는 것을 이름으로 대어 준다.
		http.Error(w, "모르는 op 입니다: "+in.Op+" — read|save|enable|disable|delete", http.StatusBadRequest)
	}
}

func (a *API) submit(w http.ResponseWriter, r *http.Request) { a.say(w, r, a.Bridge.Submit) }
func (a *API) steer(w http.ResponseWriter, r *http.Request)  { a.say(w, r, a.Bridge.Steer) }

func (a *API) say(w http.ResponseWriter, r *http.Request, fn func(string) error) {
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		http.Error(w, "빈 말은 안 보냅니다", http.StatusBadRequest)
		return
	}
	if err := fn(in.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// **받았다는 것만 답한다**(§5.7). 답은 이 왕복이 아니라 대화 스트림으로 온다.
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) interrupt(w http.ResponseWriter, _ *http.Request) {
	if err := a.Bridge.Interrupt(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// caps 는 **호스트가 무엇을 지원한다고 말했는지**를 창에서 받아 둔다.
//
// 이 값은 창 안에서만 잴 수 있다(`Office.context.requirements` 는 거기 있다). 그런데 그것을
// 창에만 두면 **사람이 화면을 읽어야만 아는 값**이 된다 — 「그건 1.10 이라 못 한다」가 문서에
// 적히고 아무도 다시 재지 않는 일이 실제로 있었다(2026-09-04). 재는 자리는 창이지만 **아는
// 자리는 여기여야** 도구도 시험도 그 답을 쓸 수 있다.
//
// 그대로 나른다. 요약하지 않는다 — 여섯이 여덟이 되던 날 요약이 있었으면 그 둘이 사라졌다.
func (a *API) caps(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Measured bool             `json:"measured"`
		Note     string           `json:"note"`
		Sets     []map[string]any `json:"sets"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	a.mu.Lock()
	a.hostCaps = map[string]any{"measured": in.Measured, "note": in.Note, "sets": in.Sets}
	a.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) status(w http.ResponseWriter, _ *http.Request) {
	st, err := a.Bridge.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// **붙어 있던 컴패니언이 다시 뜬 경우가 「닿는다」와 같아 보이면 안 된다.** 소켓 경로는
	// 워크스페이스에서 유도되므로 데몬이 죽었다 다시 떠도 그대로고, dial 도 성공한다 — 그런데
	// 우리 MCP 등록은 죽은 프로세스와 같이 사라졌고, 이 창이 붙들고 있는 대화 이름도 남의
	// 생애의 것이다. 실물에서 그 화면을 봤다(2026-09-01): 창은 「대화 연결됨」이라고 적었고,
	// 모델에게는 덱 도구가 하나도 없었다.
	if socket, _, _ := a.Bridge.Bound(); socket != "" {
		st["stale"] = !a.Attachments.HasLive(socket, publishedLife(socket))
	}
	// **안 잰 것과 「못 한다」를 가른다.** 창이 아직 안 보냈으면 이 칸은 아예 없다 — 빈 목록을
	// 실으면 「전부 미지원」으로 읽힌다.
	a.mu.Lock()
	if a.hostCaps != nil {
		st["caps"] = a.hostCaps
	}
	a.mu.Unlock()
	writeJSON(w, st)
}

// publishedLife 는 그 소켓에 지금 서 있는 데몬 프로세스의 신원. 기록을 못 읽으면 빈 글이고,
// `HasLive` 는 그때 옛 답을 그대로 준다 — 모르는 것을 「떨어졌다」로 적지 않는다.
func publishedLife(socket string) string {
	in, err := daemon.Published(socket)
	if err != nil {
		return ""
	}
	in.Socket = socket
	return lifeOf(in)
}

func (a *API) permission(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CallID   string `json:"callId"`
		Decision string `json:"decision"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.Bridge.AnswerPermission(in.CallID, in.Decision); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) question(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CallID string `json:"callId"`
		Text   string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := a.Bridge.AnswerQuestion(in.CallID, in.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) documents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"documents": a.Hub.Documents(), "attached": a.Hub.Attached()})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "이 자리는 POST 입니다", http.StatusMethodNotAllowed)
		return false
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "본문을 못 읽었습니다: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}
