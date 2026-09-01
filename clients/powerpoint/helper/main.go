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
	_ = srv.Shutdown(ctx)
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

// makeOwn 은 실제로 마련하는 일. **뒤에서 돈다.**
func (a *API) makeOwn() {
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
	fleet, err := a.fleetOf(a.ConfigDir)
	if err != nil {
		a.Work.Done(OwnReport{Phase: OwnFailed, Why: err.Error(), Socket: st.Socket, Log: st.Log})
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
		// 방금 섰다고 확인한 것이 명단에 없다 — 유도식이 갈렸다는 뜻이라 조용히 넘기면 안 된다.
		a.Work.Done(OwnReport{
			Phase: OwnFailed, Started: st.Started,
			Why:    "컴패니언이 " + st.Socket + " 에 섰는데 명단에서 그 자리를 못 찾았습니다",
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
	// **붙어 있던 컴패니언이 다시 뜬 경우가 「닿는다」와 같아 보이면 안 된다.** 소켓 경로는
	// 워크스페이스에서 유도되므로 데몬이 죽었다 다시 떠도 그대로고, dial 도 성공한다 — 그런데
	// 우리 MCP 등록은 죽은 프로세스와 같이 사라졌고, 이 창이 붙들고 있는 대화 이름도 남의
	// 생애의 것이다. 실물에서 그 화면을 봤다(2026-09-01): 창은 「대화 연결됨」이라고 적었고,
	// 모델에게는 덱 도구가 하나도 없었다.
	if socket, _, _ := a.Bridge.Bound(); socket != "" {
		st["stale"] = !a.Attachments.HasLive(socket, publishedLife(socket))
	}
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
