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
	"syscall"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/version"
)

// magi-ppt — PowerPoint 애드인의 헬퍼(DESIGN.md §5).
//
// 여기는 **조립만** 한다. 무엇이 무엇인지 아는 자리는 이 파일뿐이고, 안쪽은 서로를 인터페이스로만
// 안다 — 목업의 `main.js` 가 같은 규율을 지키는 그 자리다.
//
//coverage:ignore — 프로세스 진입점. 아래 조각들은 각자 시험이 있다.
func main() { os.Exit(run(os.Args[1:], os.Stderr)) }

func run(args []string, log io.Writer) int {
	fs := flag.NewFlagSet("magi-ppt", flag.ContinueOnError)
	fs.SetOutput(log)
	var (
		port = fs.Int("port", DefaultPort,
			"애드인이 붙을 포트. **매니페스트의 <SourceLocation> 과 같은 값이어야 한다** — 못 잡으면 다른 번호로 안 흘러간다(§5.5.1)")
		cfgDir = fs.String("config-dir", "",
			"magi 설정 디렉토리(기본값: 플랫폼 것, MAGI_CONFIG_DIR 존중). 여기서 컴패니언 명단을 읽고 인증서를 둔다")
		addin = fs.String("addin", "",
			"애드인 소스 디렉토리(기본값: 이 바이너리 옆의 clients/powerpoint/mockup)")
		showRules = fs.Bool("allow-rules", false,
			"덱을 고치지 않는 도구의 허용 규칙을 찍고 나간다(§6). config.toml 에 그대로 붙여 넣는다")
		showVer  = fs.Bool("version", false, "판본을 찍고 나간다")
		showCert = fs.Bool("cert-hint", false, "인증서를 신뢰 저장소에 넣는 법을 찍고 나간다")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVer {
		fmt.Fprintf(log, "magi-ppt %s\n", version.String())
		return 0
	}
	if *showRules {
		// 산문으로 두면 안 자란다 — 도구를 하나 더할 때 규칙도 같이 자라야 하므로 코드가 만든다.
		fmt.Fprint(log, AllowRulesTOML())
		return 0
	}

	dir := *cfgDir
	if dir == "" {
		dir = platform.OS{}.ConfigDir()
	}
	if *showCert {
		fmt.Fprintln(log, CertInstallHint(dir))
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
	mux.Handle("/mcp", &MCPServer{Hand: hub, Token: token})
	mux.HandleFunc(handStreamPath, handHTTP.Stream)
	mux.HandleFunc(handReplyPath, handHTTP.Reply)

	api := &API{Bridge: bridge, Attachments: attachments, Hub: hub, Token: token, ConfigDir: dir, Port: *port}
	api.Route(mux)

	pages := &Pages{Root: root, Token: token, Boot: map[string]any{
		"version": version.Version,
		"origin":  Origin(*port),
	}}
	mux.Handle("/", pages.Handler())

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

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
	_ = srv.Shutdown(ctx)
	return 0
}

// defaultAddinRoot 는 바이너리 옆에서 애드인을 찾는다. 저장소에서 바로 돌릴 때가 흔하므로
// 소스 트리 자리도 같이 본다 — 못 찾으면 **말하고 멈춘다**(위 run 이 그렇게 한다).
func defaultAddinRoot() string {
	candidates := []string{
		filepath.Join("clients", "powerpoint", "mockup"),
		filepath.Join("..", "mockup"),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates,
			filepath.Join(filepath.Dir(exe), "addin"),
			filepath.Join(filepath.Dir(exe), "clients", "powerpoint", "mockup"))
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
	Bridge      *Bridge
	Attachments *Attachments
	Hub         *HandHub
	Token       string
	ConfigDir   string
	Port        int
}

func (a *API) Route(mux *http.ServeMux) {
	mux.HandleFunc("/api/companions", a.guard(a.companions))
	mux.HandleFunc("/api/choose", a.guard(a.choose))
	mux.HandleFunc("/api/submit", a.guard(a.submit))
	mux.HandleFunc("/api/steer", a.guard(a.steer))
	mux.HandleFunc("/api/interrupt", a.guard(a.interrupt))
	mux.HandleFunc("/api/status", a.guard(a.status))
	mux.HandleFunc("/api/permission", a.guard(a.permission))
	mux.HandleFunc("/api/question", a.guard(a.question))
	mux.HandleFunc("/api/documents", a.guard(a.documents))
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

func (a *API) status(w http.ResponseWriter, _ *http.Request) {
	st, err := a.Bridge.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, st)
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
