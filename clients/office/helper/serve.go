package office

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
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

// magi office — PowerPoint·Excel·Word 애드인을 위한 통합 헬퍼 서비스입니다.
//
// 3개 오피스 호스트(PowerPoint, Excel, Word)에 대한 로컬 웹 서버, SSE 조작 스트림,
// MCP 도구 서버, 컴패니언 데몬 관리 기능을 단일 프로세스로 통합하여 제공합니다.
//
//coverage:ignore — 프로세스 진입점. 하위 세부 모듈 단위로 독립 테스트를 수행합니다.

// Run은 CLI 명령줄 입력을 파싱하고 헬퍼 서비스를 기동합니다.
// 조회 결과는 out(stdout)으로 전달하고, 런타임 진단 및 오류 로그는 log(stderr)로 출력합니다.
func Run(args []string, out, log io.Writer) int {
	fs := flag.NewFlagSet("magi office", flag.ContinueOnError)
	fs.SetOutput(log)
	var (
		port = fs.Int("port", DefaultPort,
			"애드인이 연결할 HTTP 포트. 매니페스트의 <SourceLocation>과 일치해야 합니다.")
		cfgDir = fs.String("config-dir", "",
			"magi 전역 설정 디렉토리(기본값: 플랫폼 표준, MAGI_CONFIG_DIR 환경변수 준수). 컴패니언 명단 및 인증서 위치로 사용됩니다.")
		sockDir = fs.String("socket-dir", "",
			"소켓 및 명단 파일을 배치할 디렉토리(기본값: 설정 디렉토리, MAGI_SOCKET_DIR 환경변수 준수). Windows 환경의 AF_UNIX 108바이트 주소 제한 방지용으로 분리 지정 가능합니다.")
		clients = fs.String("clients", "",
			"애드인 소스 디렉토리(기본값: 실행 바이너리 인접 또는 저장소 clients). 하위 powerpoint/addin, excel/addin, word/addin 경로를 참조합니다.")
		showRules = fs.String("allow-rules", "",
			"해당 프로그램(ppt·xl·word)의 읽기 전용 도구 허용 규칙(TOML)을 출력하고 종료합니다.")
		showVer  = fs.Bool("version", false, "버전 정보를 출력하고 종료합니다.")
		showCert = fs.Bool("cert-hint", false, "인증서 신뢰 저장소 등록 안내를 출력하고 종료합니다.")
		// Office 프로세스가 실행 중이지 않아도 헬퍼 프로세스를 유지할 수 있도록 개발 전용 옵션을 제공합니다.
		keepRunning = fs.Bool("keep-running", false, "Office 프로세스가 실행 중이지 않아도 종료하지 않고 유지합니다(개발 전용).")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Fprintf(out, "magi office %s\n", version.String())
		return 0
	}
	if *showRules != "" {
		app := AppByKey(*showRules)
		if app == nil {
			fmt.Fprintf(log, "-allow-rules 는 ppt·xl·word 중 하나입니다 — %q\n", *showRules)
			return 2
		}
		// 산문으로 두면 안 자란다 — 도구를 하나 더할 때 규칙도 같이 자라야 하므로 코드가 만든다.
		fmt.Fprint(out, app.AllowRulesTOML())
		return 0
	}

	// **소켓 자리는 환경으로 물려준다.** 컴패니언 데몬은 이 프로세스의 환경을 받으므로(own.go deckEnv) 여기서 세우면
	// 데몬도 같은 자리에 서고, 명단을 읽는 모든 자리(daemon.SocketDir)가 같은 곳을 본다.
	if *sockDir != "" {
		if err := os.Setenv("MAGI_SOCKET_DIR", *sockDir); err != nil {
			fmt.Fprintf(log, "-socket-dir 를 환경에 못 세웠습니다: %v\n", err)
			return 2
		}
	}
	dir := *cfgDir
	if dir == "" {
		dir = platform.OS{}.ConfigDir()
	}
	if *showCert {
		fmt.Fprintln(out, CertInstallHint(dir))
		return 0
	}
	root := *clients
	if root == "" {
		root = defaultClientsRoot()
	}
	roots := map[string]string{}
	for _, app := range Apps {
		r := filepath.Join(root, app.AddinDir, "addin")
		if _, err := os.Stat(filepath.Join(r, "taskpane.html")); err != nil {
			fmt.Fprintf(log, "%s 애드인 소스를 못 찾았습니다(%s): %v\n-clients 로 자리를 알려 주세요.\n", app.Product, r, err)
			return 1
		}
		roots[app.Key] = r
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

	mux := http.NewServeMux()
	var served []*served
	for _, app := range Apps {
		sv := mount(mux, app, dir, roots[app.Key], token, *port)
		served = append(served, sv)
		fmt.Fprintf(log, "%s: 애드인 %s · MCP %s · 소스 %s\n", app.Product, app.PageURL(*port), app.MCPURL(*port, ""), roots[app.Key])
	}
	fmt.Fprintf(log, "magi office %s\n", version.Version)
	fmt.Fprintln(log, CertInstallHint(dir))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	// 나갈 때 **우리 등록을 뗀다**(§5.4). 남겨 두면 다음에 뜬 헬퍼가 이름 충돌로 거절당하고,
	// 그 사이 모델에게는 손이 없는 도구가 광고된다.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	// **Office 가 하나도 없으면 헬퍼도 끝낸다**(idle.go officeWatch). 다만 **다시 띄워 줄 것이 있을 때만** 그렇게 한다
	// (wakesUpAgain) — mac·리눅스에는 COM 추가 기능 같은 자리가 없어서, 거기서 끝내면 사람이 손으로 띄운 헬퍼를 끄고
	// 다음에 Office 를 켤 때 빈 창을 띄운다. `-keep-running` 은 그 자동 종료를 아예 끈다(개발용).
	if !*keepRunning && wakesUpAgain() {
		go func() {
			w := &officeWatch{}
			t := time.NewTicker(idleTickEvery)
			defer t.Stop()
			for now := range t.C {
				if w.tick(now) {
					fmt.Fprintln(log, "Office 가 하나도 안 떠 있습니다 — 헬퍼를 끝냅니다(다음에 Office 를 켜면 다시 뜹니다).")
					stop <- syscall.SIGTERM
					return
				}
			}
		}()
	}
	<-stop
	fmt.Fprintln(log, "나갑니다 — 붙여 둔 등록을 뗍니다.")
	for _, sv := range served {
		sv.attachments.DetachAll(sv.bridges.Bindings())
		sv.bridge.Stop()
	}
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

// served 는 앱 하나 몫으로 세운 것들 — 나갈 때 뗄 것이 여기 있다.
type served struct {
	app         *App
	api         *API
	bridge      *Bridge
	bridges     *Bridges
	attachments *Attachments
}

// mount 는 앱 하나를 `/{key}/` 아래에 세운다. 손 허브·MCP 서버·API·페이지가 앱마다 따로고,
// 인증서·토큰·포트만 셋이 나눈다.
func mount(mux *http.ServeMux, app *App, dir, root, token string, port int) *served {
	hub := NewHandHub(app)
	bridges := NewBridges()
	// 열쇠 없는 덱의 대화. 이름을 안 실어 보내는 길이 그리로 간다.
	bridge := bridges.For("")
	attachments := NewAttachments(app)
	sub := http.NewServeMux()

	// **창마다 자기 덱의 대화를 듣는다.** 이 자리는 열쇠를 받고도 버리고 있었고, 그래서
	// 파워포인트 판에서 창을 둘 띄우면 양쪽 작업창에 같은 말이 흘렀다(2026-09-04 사용자 제보).
	handHTTP := &HandHTTP{Hub: hub, Token: token, Feed: func(deck string) <-chan StreamFrame {
		ch, _ := bridges.For(deck).Subscribe()
		return ch
	}}
	sub.Handle("/mcp", &MCPServer{App: app, Hand: hub, Token: token, Council: func() bool {
		// 데몬이 답한다(`daemon.Status.Council`). 못 닿으면 거짓 — 모르는 채로 없는 도구를
		// 가리키느니 안 적는 쪽이 낫다.
		st, err := bridge.Status()
		if err != nil {
			return false
		}
		on, _ := st["council"].(bool)
		return on
	}})
	sub.HandleFunc(handStreamPath, handHTTP.Stream)
	sub.HandleFunc(handReplyPath, handHTTP.Reply)

	api := &API{
		App:    app,
		Bridge: bridge, Bridges: bridges, Attachments: attachments, Hub: hub,
		Token: token, ConfigDir: dir, Port: port,
		Own:  &OwnCompanion{App: app, ConfigDir: dir},
		Work: NewOwnWork(),
	}
	api.Route(sub)
	// 프로그램이 없으면 컴패니언도 없다(idle.go). 헬퍼가 사는 동안 돈다.
	go api.watchProgram(make(chan struct{}))

	pages := &Pages{Root: root, Token: token, Base: app.Base(), Boot: map[string]any{
		"version": version.Version,
		"origin":  Origin(port),
		"base":    app.Base(),
		"app":     app.Key,
	}}
	sub.Handle("/", pages.Handler())
	mux.Handle(app.Base()+"/", http.StripPrefix(app.Base(), sub))

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
		// 운영 지침은 도구 쪽 일이다 — 사람이 브리프에 적게 두지 않는다(instructions.go). 마련(goroutine)
		// 안이 아니라 여기서 동기로 심는다: 시험의 TempDir 정리와 부딪히지 않고, 못 심어도 마련은 계속한다.
		_, _ = SeedInstructions(app, dir)
		_, _ = SeedSkills(app, dir)
		_ = app.SeedLandingSocket(dir) // 착지 플러그인이 land 없이 끝난 턴을 되부를 때 쓰는 소켓(council.go)
		go api.provision()
	}
	return &served{app: app, api: api, bridge: bridge, bridges: bridges, attachments: attachments}
}

// defaultClientsRoot 는 바이너리 옆에서 clients 디렉토리를 찾는다. 저장소에서 바로 돌릴 때가
// 흔하므로 소스 트리 자리도 같이 본다 — 못 찾으면 **말하고 멈춘다**(위 Run 이 그렇게 한다).
func defaultClientsRoot() string {
	candidates := []string{"clients", filepath.Join("..", "..", "..", "clients"), filepath.Join("..", "..")}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "clients"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "word", "addin", "taskpane.html")); err == nil {
			return c
		}
	}
	return candidates[0]
}

// API 는 애드인이 두드리는 자리들(§5.7). 전부 토큰이 필요하고, 전부 루프백이다.
type API struct {
	// App 은 이 API 가 섬기는 프로그램. 워크스페이스·도구 표·거절문의 낱말이 여기서 온다.
	App *App
	// mu·hostCaps 는 **창이 잰 요구 집합**을 든다. 창 안에서만 잴 수 있는 값이고, 창에만 두면
	// 사람이 화면을 읽어야만 아는 값이 된다(`caps` 핸들러의 주석).
	mu       sync.Mutex
	hostCaps map[string]any

	// Bridge 는 **열쇠 없는 덱**의 대화다. 창이 자기 이름을 아직 안 실어 보내는 길(옛 판본의
	// 작업창·시험)이 그리로 간다. 이름이 오면 `Bridges` 에서 그 덱의 것을 고른다.
	Bridge *Bridge
	// Bridges 는 **덱 하나에 대화 하나**(bridges.go). 두 창이 한 스트림을 듣던 결함을 여기서 막는다.
	Bridges     *Bridges
	Attachments *Attachments
	Hub         *HandHub
	Token       string
	ConfigDir   string
	// ImageRoot 는 데몬이 도구의 그림을 두는 자리(`<데이터 디렉토리>/images`). 작업창의 `/api/image` 가 그 아래 파일만
	// 내준다. 비면 이 플랫폼의 데이터 디렉토리로 — 시험만 딴 곳을 준다.
	ImageRoot string
	Port      int
	// Restart 는 그 소켓의 데몬에게 restart 문을 두드리는 것(council.go). 시험이 바꿔 끼운다.
	Restart func(socket string) error
	// Own 은 **파워포인트 몫의 컴패니언**. 명단에서 남의 워크스페이스를 골라 빌리는 대신 이것을
	// 마련한다(own.go) — 메일에서 받은 덱을 더블클릭한 사람에게는 명단이 늘 비어 있다.
	Own *OwnCompanion
	// ReadFleet·Bolt 는 명단을 읽고 도구를 붙이는 길. **시험만 이 자리를 채운다** — 기본값은 바로
	// 아래 둘이다. 주입 자리가 없으면 이 핸들러의 실패 갈래는 실물 소켓 없이는 못 재고, 못 재는
	// 갈래는 안 만든 것과 같다(TESTING §1).
	ReadFleet func(configDir string) ([]Companion, error)
	Bolt      func(socket, url, token string) ([]string, error)
	// settling 은 `settle` 을 **한 번에 하나만** 돌린다. 창 둘이 같은 순간에 폴하면 두 `settle` 이
	// 나란히 도는데, 둘 다 아직 안 묶인 채라 서로를 못 보고 **같은 대화·같은 주인**으로 붙는다 —
	// 데몬은 그것을 `"ppt" attached and then vanished` 로 거절했다(2026-09-05 실물, 두 덱 다).
	// 멱등은 순서를 보장하지 않는다; 직렬화가 한다.
	settling sync.Mutex
	// wide 는 **컴패니언 전체 등록**을 한 생애에 한 번만 하려고 적어 두는 것(소켓 → 생애). settle 이 잠근 채로 만진다.
	wide map[string]string
	// Running·Stop·IdleAfter 는 「프로그램이 없으면 컴패니언도 없다」(idle.go)의 주입 자리. Running 은 (돌고 있나, 알 수
	// 있나); 기본은 OS 에 묻는다. Stop 의 기본은 데몬의 shutdown 문. IdleAfter 의 기본은 60초.
	Running func() (bool, bool)
	Stop    func(socket string) error
	// AdapterExe·AdapterAlive·SpawnAdapter 는 「PowerPoint 2021 의 편집 어댑터를 헬퍼가 띄운다」(adapter.go)의 주입 자리.
	// 기본은 헬퍼 실행 파일 옆의 `hand\magi-ppt-hand.exe`, tasklist, 숨겨 띄우기다.
	AdapterExe   func() string
	AdapterAlive func() bool
	SpawnAdapter func(exe string) error
	IdleAfter    time.Duration
	idleSince    time.Time
	// LifeOf 는 그 소켓에 선 데몬의 생애(pid@시작시각). **시험만 이 자리를 채운다** — 기본은
	// `publishedLife`. 「아까 마련한 데몬이 지금도 그것인가」를 이 값 하나로 잰다.
	LifeOf func(socket string) string
	// Fresh 는 새 대화를 여는 길. 기본값은 데몬에 묻는 것이다. deck 은 그 대화가 **누구 것인지** —
	// 데몬이 대화에 적어 두고(`session-new` 의 `name`), `sessions` 가 `for` 로 되돌린다.
	Fresh func(socket, deck string) (string, error)
	// Resume 은 그 데몬에서 **이 문서의 대화를 되찾는** 길 — `sessions` 에서 `for` 가 이 문서인
	// 가장 최근 것. 없으면 false. 기본값은 데몬에 묻는 것이고, 시험만 이 자리를 채운다.
	Resume func(socket, deck string) (string, bool)
	// Work 는 그 마련하는 일의 상태. **한 번에 하나만 돈다**(ownstate.go).
	Work *OwnWork
}

// fleetOf·boltOf 는 주입이 없을 때의 진짜 길.
func (a *API) fleetOf(configDir string) ([]Companion, error) {
	if a.ReadFleet != nil {
		return a.ReadFleet(configDir)
	}
	return a.Attachments.Fleet(configDir, a.ours())
}

func (a *API) boltOf(socket, url, token, owner string) ([]string, error) {
	if a.Bolt != nil {
		return a.Bolt(socket, url, token)
	}
	return a.Attachments.Attach(socket, url, token, owner)
}

func (a *API) Route(mux *http.ServeMux) {
	mux.HandleFunc("/api/own", a.guard(a.own))
	mux.HandleFunc("/api/fresh", a.guard(a.fresh))
	mux.HandleFunc("/api/council", a.guard(a.council))
	mux.HandleFunc("/api/context", a.guard(a.contextState))
	mux.HandleFunc("/api/models", a.guard(a.models))
	mux.HandleFunc("/api/model", a.guard(a.setModel))
	mux.HandleFunc("/api/compact", a.guard(a.compact))
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
	mux.HandleFunc("/api/image", a.guard(a.image))
	mux.HandleFunc("/api/caps", a.guard(a.caps))
	// 가이드 관리 — 추가·삭제·활성화·비활성화(guides.go).
	mux.HandleFunc("/api/guides", a.guard(a.guides))
	mux.HandleFunc("/api/guide", a.guard(a.guide))
}

// deckOf 는 이 요청이 어느 덱의 것인가. 창이 `deck` 으로 실어 보낸다 — 손 스트림이 이미
// `presentation` 으로 갈라 놓은 그 이름이다.
// ours 는 「이 소켓·이 생애에 우리 묶음이 있는가」 — 명단과 상태가 같은 물음을 같은 자리에 한다.
func (a *API) ours() attached {
	if a.Bridges == nil {
		return nil
	}
	return a.Bridges.AttachedTo
}

func deckOf(r *http.Request) string {
	if r == nil {
		return ""
	}
	return r.URL.Query().Get("deck")
}

// chat 은 이 요청이 말할 대화. **등록부가 없으면 옛 하나로 떨어진다** — 시험과 옛 창이 그 길로
// 돈다. 있으면 덱마다 갈린다.
func (a *API) chat(r *http.Request) *Bridge {
	if a.Bridges == nil {
		return a.Bridge
	}
	key := deckOf(r)
	if key == "" {
		return a.Bridge
	}
	return a.Bridges.For(key)
}

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
		if debugRequests {
			// MAGI_DEBUG 일 때만 — 어느 창이 무엇을 두드리는지. 「띠가 안 보인다」가 창이 안 부른 것인지 답이 안 온
			// 것인지를 이 줄 없이는 못 가른다(2026-09-06).
			rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
			h(rec, r)
			log.Printf("api %s %s → %d", r.Method, r.URL.RequestURI(), rec.code)
			return
		}
		h(w, r)
	}
}

var debugRequests = os.Getenv("MAGI_DEBUG") != ""

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(code int) { s.code = code; s.ResponseWriter.WriteHeader(code) }

// Flush 는 SSE 가 아닌 /api/* 에서는 안 쓰이지만, 감싼 writer 가 Flusher 를 잃지 않게 넘겨 준다.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *API) companions(w http.ResponseWriter, r *http.Request) {
	fleet, err := a.Attachments.Fleet(a.ConfigDir, a.ours())
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
	socket, sid, live := a.chat(r).Bound()
	writeJSON(w, map[string]any{
		"companions": rows,
		"bound":      map[string]any{"socket": socket, "session": sid, "streamLive": live, "streamEmpty": a.chat(r).Empty()},
	})
}

// own 은 「이 덱은 준비됐나」. **결정을 기억하지 않는다 — 매번 진실을 보고 차이를 메운다.**
//
// 앞 판본(`makeOwn`)은 마련·붙임·묶음을 한 고루틴에서 하고 결과를 `OwnWork` 에 기억했다. 그
// 기억이 재기동 사건 넷마다 다른 방식으로 거짓이 됐고(DESIGN §5.9.1 의 표), 고칠 때마다 무효화
// 조건이 하나씩 늘었다 — `stillOurs`·`rebindChat`·`joinDeck`·`Forget` 이 그 자국이다.
//
// 이제 둘로 가른다. **마련**(`provision`)은 컴패니언당 한 번이고 오래 걸리므로 뒤에서 돌며
// `OwnWork` 가 「도는 중」과 그 결과(소켓·생애)를 든다. **묶음**(`settle`)은 덱마다·폴마다이고
// 멱등이다 — 이미 맞으면 아무것도 안 하고, 생애가 바뀌었으면 다시 붙인다.
func (a *API) own(w http.ResponseWriter, r *http.Request) {
	if a.Own == nil || a.Work == nil {
		http.Error(w, "이 헬퍼는 자기 컴패니언을 마련하도록 세워지지 않았습니다", http.StatusNotImplemented)
		return
	}
	// **데몬이 다시 떴으면 마련도 다시다.** 소켓 경로는 그대로인데 생애가 다르다 — 등록도 스트림도
	// 그 데몬과 같이 죽었다. 이것이 「아까 그것이 지금도 그것인가」를 재는 유일한 자리다.
	//
	// **죽은 것도 다시다.** 생애만 재면 소켓 옆에 남은 기록이 옛 생애를 그대로 답해 죽은 데몬이 산 것으로
	// 읽힌다 — 헬퍼를 띄운 뒤 데몬을 죽이니 창은 「데몬이 죽었다」를 적고 헬퍼는 영영 다시 안 띄웠다
	// (2021 실물, 2026-09-06 밤). 문을 두드려 답이 없으면 그것도 「아까 그것이 아니다」다.
	if held := a.Work.Now(); held.Phase == OwnReady && held.Socket != "" &&
		(a.lifeOf(held.Socket) != held.Life || !a.alive(held.Socket)) {
		a.Work.Forget()
	}
	now, mine := a.Work.Begin()
	if mine {
		_, _ = SeedInstructions(a.App, a.ConfigDir)
		_, _ = SeedSkills(a.App, a.ConfigDir)
		_ = a.App.SeedLandingSocket(a.ConfigDir)
		go a.provision()
	}
	if now.Phase == OwnReady {
		now = a.settle(deckOf(r), now)
	}
	writeJSON(w, now)
}

// alive 는 그 소켓의 데몬이 지금 답하는가. 마련할 때 쓰는 것과 같은 탐침이다(`OwnCompanion.Alive`) —
// 시험이 그 자리를 채우면 여기도 같이 채워진다.
func (a *API) alive(socket string) bool {
	if a.Own != nil && a.Own.Alive != nil {
		return a.Own.Alive(socket)
	}
	return probeAlive(socket)
}

func (a *API) lifeOf(socket string) string {
	if a.LifeOf != nil {
		return a.LifeOf(socket)
	}
	return publishedLife(socket)
}

// provision 은 **컴패니언을 마련한다** — 띄우고, 명단에 서기를 기다리고, 고를 수 있는지 본다.
// 덱은 모른다: 여기서 하는 일은 어느 덱이 물어도 같다. 결과는 `OwnWork` 에 든다.
func (a *API) provision() {
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
	fleet, err := a.waitForFleet(st.Socket)
	if err != nil {
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
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started, Why: mine.Why(),
			Socket: st.Socket, Workdir: st.Workdir, Log: st.Log,
		})
		return
	}
	a.Work.Done(OwnReport{
		Phase: OwnReady, Started: st.Started,
		Socket: mine.Socket, Session: mine.Session, Workdir: st.Workdir, Log: st.Log,
		Life: a.lifeOf(mine.Socket),
	})
}

// settle 은 **이 덱을 그 컴패니언에 묶는다** — 멱등이다.
//
// 묶였는가는 기억이 아니라 묶음 자체가 든 기록(소켓·세션·생애)으로 잰다. 셋이 다 맞으면 아무것도
// 안 한다 — 다시 붙이면 첫 등록이 떨어지고 다시 묶으면 스트림이 끊겼다 이어진다. 하나라도 다르면
// 붙이고 묶는다. 붙임과 묶음은 **한 짝**이다: 등록의 주인이 그 대화이므로 따로 하면 어긋난다.
//
// 스트림의 `live` 는 안 본다 — 스트림은 스스로 재접속하고(`Bridge.stream`), 막 묶은 직후는 늘
// `live=false` 라 그것으로 재면 매 폴마다 다시 묶는다.
func (a *API) settle(deck string, rep OwnReport) OwnReport {
	a.settling.Lock()
	defer a.settling.Unlock()
	b := a.Bridge
	if deck != "" && a.Bridges != nil {
		b = a.Bridges.For(deck)
	}
	socket0, sid0, life0, tools0 := b.BoundTo()
	if sid0 != "" && socket0 == rep.Socket && life0 == rep.Life {
		rep.Session, rep.Tools = sid0, tools0
		return rep
	}
	// 세션을 정한다. 이미 이 소켓에 묶였던 대화면 그대로 — 데몬이 다시 떠도 대화는 디스크에 있다.
	//
	// **이름 있는 덱은 늘 자기 대화를 새로 연다.** 데몬의 「지금」 대화(`rep.Session`)는 콘솔의
	// 편의이지 어느 덱의 것이 아니다(DESIGN §5.9.3). 앞 판본은 첫 덱이 그것을 받고 둘째부터 새로
	// 열었는데, 그 「첫」이 경주에서 둘이 됐다. 이름 없는 창(옛 길)만 「지금」을 받고, 그것이
	// 비었으면 명단을 다시 읽는다 — 갓 뜬 데몬은 소켓에 선 뒤에 자기 기록을 쓴다.
	// **데몬이 다시 떴으면 옛 대화 이름은 남의 생애의 것이다.** 앞 판본은 소켓이 같으면 sid 를
	// 그대로 물고 새 생애에 붙였고, 실물에서 그 화면을 봤다(2026-09-04): own 은 옛 대화로
	// ready 라 답했고, 보내기는 502 「no conversation」이었으며, 사람이 /api/fresh 를 손으로
	// 불러서야 풀렸다. 생애가 갈렸으면 소켓이 갈린 것과 같이 다룬다 — 덱 창은 새 대화를 연다.
	// **새로 열기 전에 되찾는다.** 헬퍼가 다시 뜨면 묶음은 비지만 대화는 데몬의 디스크에 있고, 데몬은
	// 대화마다 「누구 것으로 열렸나」(`for`)를 안다. 실물(2026-09-06, 엑셀): 헬퍼를 껐다 켜자 창은 15초
	// 안에 되붙었는데 대화가 새로 서서 세 턴의 전사가 사라졌다 — 사람에게는 「껐다 켜면 대화가 비는」
	// 고장이다. 기억을 파일에 남기는 대신 데몬에 묻는다(DESIGN §5.9.2: 결정을 기억하지 않는다 —
	// 진실을 본다). 데몬이 다시 뜬 뒤도 같은 길이다: 말이 오간 대화는 디스크에 있어 목록에 서고,
	// 아무도 말하지 않은 대화는 애초에 안 적혀 못 찾는다 — 그때는 새로 연다.
	sid := sid0
	if sid == "" || socket0 != rep.Socket || life0 != rep.Life {
		if deck != "" {
			if old, ok := a.resumeOn(rep.Socket, deck); ok {
				sid = old
			} else {
				fresh, err := a.freshOn(rep.Socket, deck)
				if err != nil {
					rep.Chat = "이 " + a.App.NounKo + "의 대화를 못 열었습니다: " + err.Error()
					return rep
				}
				sid = fresh
			}
		} else {
			sid = rep.Session
			if sid == "" {
				sid = a.sessionOn(rep.Socket)
			}
		}
	}
	// **등록이 바뀔 때만 붙인다.** 등록의 신원은 (소켓, 주인, 생애)다. 대화가 바뀌는 경우는 위에서
	// 이미 「처음이거나 소켓이 다르다」로 걸러졌으므로 여기서 다시 안 본다 — 돌연변이가 그 조건을
	// 빼도 아무것도 안 울어서 죽은 조건임을 알았다(2026-09-05). 이름 없는 창은 주인이 비어 있어
	// 대화가 바뀌어도 등록은 그대로고, 그때 다시 붙이면 첫 등록이 떨어진다(§5.0.1).
	owner := sid
	if deck == "" {
		owner = ""
	}
	tools := tools0
	if len(tools0) == 0 || socket0 != rep.Socket || life0 != rep.Life {
		got, err := a.boltOf(rep.Socket, a.App.MCPURL(a.Port, deck), a.Token, owner)
		if err != nil {
			rep.Phase, rep.Why = OwnFailed, err.Error()
			return rep
		}
		if len(got) == 0 {
			rep.Phase = OwnFailed
			rep.Why = "붙이기는 했는데 " + a.App.NounKo + " 도구가 하나도 안 실렸습니다 — 이 컴패니언은 도구 서버를 " +
				"받지 못하는 빌드일 수 있습니다. 데몬이 남긴 말: " + rep.Log
			return rep
		}
		tools = got
	}
	// **덱 등록 옆에 컴패니언 전체 등록 하나.** 덱 몫의 등록은 그 대화에만 보인다(port.Owned) — 그러면 이 컴패니언이
	// **남의 부탁으로 여는 대화**(hand_off 의 옆 대화, 회의)에는 도구가 하나도 없다: 엑셀이 「이 표로 덱을 만들어라」를
	// 넘겨도 파워포인트 컴패니언은 덱에 닿을 손이 없었다(2026-09-07). 주인 없는 등록은 모두에게 보이고, 자기 등록이
	// 없는 대화의 손이 된다(mcp/tool.go handFor). 주소에 덱이 없으니 `document` 없는 호출은 허브의 「하나뿐이면 그것,
	// 둘이면 이름을 대라」 규칙으로 간다 — list_documents 가 그 이름을 준다. 한 생애에 한 번이면 된다.
	if deck != "" && rep.Socket != "" {
		if a.wide == nil {
			a.wide = map[string]string{}
		}
		if a.wide[rep.Socket] != rep.Life {
			if _, err := a.boltOf(rep.Socket, a.App.MCPURL(a.Port, ""), a.Token, ""); err == nil {
				a.wide[rep.Socket] = rep.Life
			} else {
				rep.Chat = "컴패니언 전체 몫의 도구를 못 붙였습니다(남의 부탁으로 여는 대화가 도구를 못 봅니다): " + err.Error()
			}
		}
	}
	if err := b.BindWith(rep.Socket, sid, rep.Life, tools); err != nil {
		// 붙기는 했고 대화만 못 열었다. **등급이 다른 둘을 한 칸으로 합치지 않는다**(§5.0.5).
		rep.Chat = err.Error()
	}
	rep.Session, rep.Tools = sid, tools
	return rep
}

// sessionOn 은 명단이 지금 그 소켓에 적어 둔 대화 이름. 없으면 빈 문자열이다 — 지어내지 않는다.
func (a *API) sessionOn(socket string) string {
	fleet, err := a.fleetOf(a.ConfigDir)
	if err != nil {
		return ""
	}
	for _, c := range fleet {
		if c.Socket == socket {
			return c.Session
		}
	}
	return ""
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

// instructions 는 **한 번 적어 두면 매번 지켜지는 말**을 읽고 쓴다(instructions.go).
//
// GET 은 지금 적혀 있는 것, POST 는 새로 적는 것. 빈 글을 보내면 지운다.
//
// **덱마다가 아니라 파워포인트 전체에 걸린다.** 파일이 컴패니언의 워크스페이스에 있고 그
// 워크스페이스가 파워포인트 몫이기 때문이다 — 엑셀이나 저장소 컴패니언은 이 글을 안 본다.
func (a *API) instructions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		text, err := ReadInstructions(a.App, a.ConfigDir)
		if err != nil {
			writeStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]any{"text": text, "path": instructionsFile(a.App, a.ConfigDir)})
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	text, err := WriteInstructions(a.App, a.ConfigDir, in.Text)
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
	writeJSON(w, map[string]any{"text": text, "path": instructionsFile(a.App, a.ConfigDir), "note": note})
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
func (a *API) fresh(w http.ResponseWriter, r *http.Request) {
	socket, _, _ := a.chat(r).Bound()
	if socket == "" {
		writeStatus(w, http.StatusConflict, map[string]any{
			"error": "아직 아무 컴패니언에도 안 붙어 있어서 새 대화를 열 자리가 없습니다",
		})
		return
	}
	sid, err := a.freshOn(socket, deckOf(r))
	if err != nil {
		writeStatus(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "socket": socket})
		return
	}
	// **대화를 바꾸면 도구도 그 이름으로 다시 붙어야 한다.**
	//
	// 등록은 주인(대화)별로 산다. 새 대화를 열고 등록을 그대로 두면 그 대화는 자기 이름으로 붙은
	// 등록이 없어 **주인 없는 옛 등록**으로 떨어지고, 그 등록의 주소에는 덱이 없다 — 그러면
	// `document` 를 생략한 호출이 「덱이 둘이라 못 고른다」로 죽고, 모델은 사람에게 「어느 덱에
	// 만들까요」를 묻는다. 실물에서 그 화면을 봤다(2026-09-05: 사람이 그 질문을 그대로 옮겨 물었다.
	// "플러그인 통해서 요청하면 저런거 안 떠?" — 안 떠야 맞고, 이 자리가 빠져 있었다).
	if _, err := a.boltOf(socket, a.App.MCPURL(a.Port, deckOf(r)), a.Token, sid); err != nil {
		// 못 붙였으면 대화는 열렸고 도구만 옛 것이다. **등급이 다른 둘을 한 칸으로 안 합친다.**
		defer func() {
			writeJSON(w, map[string]any{"session": sid, "socket": socket,
				"note":  "새 대화를 열었습니다. " + a.App.PartKo + " 그대로입니다 — 지운 것은 대화뿐입니다.",
				"tools": "이 대화 몫으로 도구를 다시 못 붙였습니다: " + err.Error()})
		}()
		return
	}
	// **대화를 바꾸면 창도 그 이름으로 옮겨 앉아야 한다.** 안 그러면 새 대화의 이벤트가 전부
	// 남의 것으로 걸러진다 — 실물에서 그 화면을 봤던 자리다(§5.7).
	out := map[string]any{"session": sid, "socket": socket,
		"note": "새 대화를 열었습니다. " + a.App.PartKo + " 그대로입니다 — 지운 것은 대화뿐입니다."}
	if err := a.chat(r).Bind(socket, sid); err != nil {
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

// freshOn 은 그 데몬에 새 대화를 청한다. **시험만 이 자리를 채운다.** deck 은 그 대화의 임자로
// 데몬에 적힌다 — 다음 헬퍼가 `resumeOn` 으로 되찾는 열쇠다.
func (a *API) freshOn(socket, deck string) (string, error) {
	if a.Fresh != nil {
		return a.Fresh(socket, deck)
	}
	cl, err := daemon.DialWithin(socket, aliveTimeout, aliveTimeout)
	if err != nil {
		return "", err
	}
	defer cl.Close()
	// **옮기지 않고 연다.** 이 헬퍼는 덱마다 대화를 하나씩 들고, 옮기면 방금까지 일하던 덱의
	// 대화에 「컴패니언이 떠났다」가 적힌다(2026-09-05 실측).
	return cl.NewSessionKeepingFor(deck)
}

// resumeOn 은 그 데몬에서 이 문서의 대화를 되찾는다 — `sessions` 의 `for` 가 이 문서인 것 중
// 가장 최근 활동. 목록은 최근 활동순이라 첫 짝이 답이다. 없거나 못 물으면 false — 그때는 새로 연다.
// 옛 데몬(`for` 를 모르는 판)은 빈 칸만 주므로 자연히 false 다.
func (a *API) resumeOn(socket, deck string) (string, bool) {
	if a.Resume != nil {
		return a.Resume(socket, deck)
	}
	if deck == "" {
		return "", false
	}
	cl, err := daemon.DialWithin(socket, aliveTimeout, aliveTimeout)
	if err != nil {
		return "", false
	}
	defer cl.Close()
	rows, err := cl.Sessions()
	if err != nil {
		return "", false
	}
	for _, r := range rows {
		if r.For == deck && r.ID != "" {
			return r.ID, true
		}
	}
	return "", false
}

func (a *API) choose(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Socket  string `json:"socket"`
		Session string `json:"session"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// **한 세션에 두 덱이 붙으면 갈라 놓은 뜻이 없어진다.** 다른 창이 이미 그 대화를 들고 있으면
	// 같은 데몬에 **새 대화**를 연다 — 사람이 명단에서 고른 것은 「저 컴패니언」이지 「저 대화」가
	// 아니다. 못 열면 그때는 사실대로 거절한다: 몰래 같은 대화에 밀어 넣으면 두 창이 다시 한
	// 줄이 되고, 그 증상이 바로 이 결함이다.
	session := in.Session
	// **덱마다 자기 대화다.** 컴패니언에 아직 대화가 없으면(갓 뜬 데몬) 여기서 하나 연다 —
	// 안 그러면 `Bind(socket, "")` 이 「아직 대화가 없습니다」로 거절하고, 그 창은 붙긴 했는데
	// 말을 걸 곳이 없다. 사람이 물었다(2026-09-05): "덱마다 새 세션 아니야?" — 맞는 말이고,
	// 그때까지는 내가 `/api/fresh` 를 손으로 불러 메우고 있었다.
	if session == "" && deckOf(r) != "" {
		if fresh, err := a.freshOn(in.Socket, deckOf(r)); err == nil {
			session = fresh
		}
		// 못 열었으면 그대로 간다 — 아래 `Bind` 가 사유를 적고, 그 사유가 화면에 뜬다.
	}
	if a.Bridges != nil && deckOf(r) != "" {
		// **붙어 있지 않은 덱은 임자가 아니다.**
		//
		// 저장 안 한 덱은 작업창이 다시 붙을 때마다 허브에서 새 번호를 받는다(`doc-…-1` → `-4`).
		// 그러면 옛 키는 아무 손도 없는 채로 그 대화를 붙잡고 있고, 새 키로 온 창은 **자기 대화를
		// 못 가져간다** — 사람은 아무것도 안 흐르는 빈 작업창을 본다. 실물에서 그 화면을 봤다
		// (2026-09-04: 사람이 "내용이 하나도 안 들어오노"라고 물었다).
		//
		// 붙어 있는 이름은 허브가 안다. 죽은 임자면 그 대화를 이 덱이 이어받는다.
		// 허브가 없는 판(시험)에서는 「모른다」이고, 그때는 임자를 그대로 존중한다 — 모르는 것을
		// 「죽었다」로 읽으면 멀쩡한 대화를 남이 가져간다.
		live := map[string]bool{}
		known := a.Hub != nil
		if known {
			for _, d := range a.Hub.Documents() {
				live[d["document"]] = true
			}
		}
		if who, taken := a.Bridges.Holder(session); taken && who != deckOf(r) && (!known || live[who]) {
			fresh, err := a.freshOn(in.Socket, deckOf(r))
			if err != nil {
				http.Error(w, "그 대화는 다른 "+a.App.NounKo+"가 쓰고 있고, 이 "+a.App.NounKo+"에 새 대화를 여는 데 "+
					"실패했습니다: "+err.Error(), http.StatusConflict)
				return
			}
			session = fresh
		}
	}
	// **주입 자리를 지나간다.** 이 한 줄만 `Attachments` 를 직접 불렀고, 그래서 이 핸들러의
	// 갈래는 실물 소켓 없이는 못 쟀다 — 못 재는 갈래는 안 만든 것과 같다(TESTING §1). 다른
	// 부착 자리(`makeOwn`)는 이미 `boltOf` 를 지난다.
	//
	// **이 통합 문서의 대화 것으로 붙인다.** 등록이 주인별로 살게 됐으므로(코어의 `Attach(owner, …)`),
	// 같은 컴패니언에 덱 둘이 붙어도 서로를 안 덮고 서로의 손을 안 본다. 그래서 이제 주소에
	// 덱을 적어도 참이다 — 그 등록으로 오는 호출은 이 덱의 것뿐이다.
	//
	// **세션이 먼저다.** 위에서 이 덱의 대화를 정하고 그 이름으로 붙인다 — 순서가 뒤집히면
	// 주인 없는 등록이 하나 생기고, 주인 없는 것은 모두에게 보인다.
	deck := deckOf(r)
	owner := session
	if deck == "" {
		// 이름 없는 창은 여태처럼 데몬 전체 등록을 쓴다.
		owner = ""
	}
	tools, err := a.boltOf(in.Socket, a.App.MCPURL(a.Port, deck), a.Token, owner)
	if err != nil {
		// **끝내 못 붙으면 말한다**(§5.3). 조용히 넘어가면 화면이 「할 일 없음」처럼 보인다.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	bindErr := a.chat(r).Bind(in.Socket, session)
	out := map[string]any{"tools": tools}
	if bindErr != nil {
		// 붙기는 했고 대화만 못 열었다. **등급이 다른 둘을 한 칸으로 합치지 않는다**(§5.0.5).
		out["chat"] = bindErr.Error()
	}
	writeJSON(w, out)
}

// guides 는 목록. **꺼 둔 것도 같이 준다** — 빼면 다시 켤 길이 없다.
func (a *API) guides(w http.ResponseWriter, _ *http.Request) {
	list, err := ListGuides(a.App, a.ConfigDir)
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
		body, enabled, err := ReadGuide(a.App, a.ConfigDir, in.Name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"name": in.Name, "body": body, "enabled": enabled})
	case "save":
		g, err := WriteGuide(a.App, a.ConfigDir, in.Name, in.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, g)
	case "enable", "disable":
		g, err := EnableGuide(a.App, a.ConfigDir, in.Name, in.Op == "enable")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, g)
	case "delete":
		if err := DeleteGuide(a.App, a.ConfigDir, in.Name); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"deleted": in.Name})
	default:
		// **모르는 것을 짐작하지 않는다** — 아는 것을 이름으로 대어 준다.
		http.Error(w, "모르는 op 입니다: "+in.Op+" — read|save|enable|disable|delete", http.StatusBadRequest)
	}
}

func (a *API) submit(w http.ResponseWriter, r *http.Request) { a.say(w, r, a.chat(r).Submit) }
func (a *API) steer(w http.ResponseWriter, r *http.Request)  { a.say(w, r, a.chat(r).Steer) }

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

func (a *API) interrupt(w http.ResponseWriter, r *http.Request) {
	if err := a.chat(r).Interrupt(); err != nil {
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

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	st, err := a.chat(r).Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// **붙어 있던 컴패니언이 다시 뜬 경우가 「닿는다」와 같아 보이면 안 된다.** 소켓 경로는
	// 워크스페이스에서 유도되므로 데몬이 죽었다 다시 떠도 그대로고, dial 도 성공한다 — 그런데
	// 우리 MCP 등록은 죽은 프로세스와 같이 사라졌고, 이 창이 붙들고 있는 대화 이름도 남의
	// 생애의 것이다. 실물에서 그 화면을 봤다(2026-09-01): 창은 「대화 연결됨」이라고 적었고,
	// 모델에게는 덱 도구가 하나도 없었다.
	if socket, _, _ := a.chat(r).Bound(); socket != "" {
		st["stale"] = a.Bridges != nil && !a.Bridges.AttachedTo(socket, publishedLife(socket))
	}
	// 마지막으로 이 헬퍼를 지나간 권한 답. 창은 이것으로 「다른 곳에서 답했다」에 결정을 붙인다.
	if id, d := a.chat(r).LastAnswer(); id != "" {
		st["answered"] = map[string]any{"callId": id, "decision": d}
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
	if err := a.chat(r).AnswerPermission(in.CallID, in.Decision); err != nil {
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
	if err := a.chat(r).AnswerQuestion(in.CallID, in.Text); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (a *API) documents(w http.ResponseWriter, _ *http.Request) {
	// viewers 는 화면(viewer)들이 청한 키와 본 손 — 2021 에서 두 창이 같은 손을 보는지 가르는 진단 창.
	writeJSON(w, map[string]any{"documents": a.Hub.Documents(), "attached": a.Hub.Attached(), "viewers": a.Hub.Peeks()})
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

// image 는 도구가 남긴 그림 한 장을 작업창에 내준다 — `/api/image?path=<절대 경로>`.
//
// 그림은 참조로만 온다: 코어의 MCP 어댑터가 도구의 이미지 블록을 `<데이터 디렉토리>/images/<세션>/…` 에 적고
// 결과에는 경로(ImageRef)만 싣는다(로그를 작게 두려고). 작업창은 그 경로를 받고도 열 문이 없어 「(그림 1장은 이
// 창이 아직 안 그립니다)」만 적었다(엑셀 실물 2026-09-07). 헬퍼는 데몬과 같은 기계라 그 파일을 읽을 수 있고,
// 여는 자리는 **그 디렉토리 아래, 그림 확장자**뿐이다 — 경로를 받아 파일을 내주는 문은 그 둘로 닫아야 문이지
// 구멍이 아니다.
func (a *API) image(w http.ResponseWriter, r *http.Request) {
	root := a.ImageRoot
	if root == "" {
		root = filepath.Join(platform.OS{}.DataDir(), "images")
	}
	want := strings.TrimSpace(r.URL.Query().Get("path"))
	if want == "" {
		http.Error(w, "path 가 없습니다", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(want)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rootAbs, _ := filepath.Abs(root)
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		http.Error(w, "그림은 데몬의 images 디렉토리 아래 것만 내줍니다", http.StatusForbidden)
		return
	}
	mime := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}[strings.ToLower(filepath.Ext(abs))]
	if mime == "" {
		http.Error(w, "그림 파일이 아닙니다", http.StatusForbidden)
		return
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		http.Error(w, "그 그림이 없습니다: "+err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	if _, err := w.Write(b); err != nil {
		// 받는 쪽이 끊은 것이다 — 창이 닫혔거나 다른 그림으로 넘어갔다. 우리 쪽 실패가 아니라 적어만 둔다.
		log.Printf("image: could not send %s: %v", filepath.Base(abs), err)
	}
}
