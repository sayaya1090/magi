package main

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
)

// 데몬 쪽 얼굴 — 붙을 곳을 고르고, door 로 붙는다(DESIGN.md §5.0).
//
// **애드인이 고른다.** 앞 판본은 "config 에 `[mcp.ppt]` 를 적은 magi 가 덱 도구를 갖는다"였고
// 붙는 쪽이 magi 였는데, PPT 파일에는 워크스페이스 개념이 없는 경우가 많다 — 바탕화면에 있거나
// 메일에서 방금 내려받았을 수 있다. 워크스페이스에서 데몬을 유도할 수 없으므로 반대로 유도한다.

// candidateTimeout 은 후보 하나에게 묻는 데 드는 시간의 천장이다. 죽어 가는 데몬 하나가 목록
// 전체를 잡으면 안 된다 — `daemon.List` 가 자기 프로브를 병렬로 도는 것과 같은 이유다(§5.0.5).
const candidateTimeout = 2 * time.Second

// Companion 은 고르는 화면의 한 줄.
type Companion struct {
	Socket  string `json:"socket"`
	Workdir string `json:"workdir"`
	Name    string `json:"name,omitempty"`
	Role    string `json:"role,omitempty"`
	Team    string `json:"team,omitempty"`
	Session string `json:"session,omitempty"`
	Live    bool   `json:"live"`
	Doing   string `json:"doing,omitempty"`
	Asking  bool   `json:"asking,omitempty"`
	// Permission·Backend·Model 은 **명단을 그리는 그 호출에 이미 실려 온다**(§5.0.5 ⚠) —
	// 왕복이 하나 더 드는 일이 아니다. 화면에 그대로 적는 이유는 §12 #2 다: 「이 덱 본문이 이
	// 머신을 떠나는가」를 우리가 주소 모양으로 단정하면, 틀렸을 때 대가를 치르는 쪽이 우리가 아니다.
	Permission string `json:"permission,omitempty"`
	Backend    string `json:"backend,omitempty"`
	Model      string `json:"model,omitempty"`

	// ToolServers 와 Transcript 는 **셋 값이다** — `true`·`false`·`nil`.
	//
	// `nil` 은 「아직 못 물어봤다」이고 `false` 는 「호스트가 아니라고 답했다」다. `PeerSupports`
	// 는 둘을 같은 거짓으로 접는데(주석이 "false before the first Hello" 라고 적는다), 보내는
	// 쪽 게이트로는 그게 맞아도 **화면은 사람에게 사실을 적는 자리**라 합치면 안 된다(§5.0.5).
	// 바빴거나 프로브가 타임아웃한 후보를 「이 빌드는 도구 서버를 못 받는다」로 그리면, 다시
	// 물으면 될 것을 빌드의 성질로 적는 셈이다.
	ToolServers *bool `json:"toolServers,omitempty"`
	Transcript  *bool `json:"transcript,omitempty"`
	// AskError 는 못 물어본 사유. 있으면 위 둘은 nil 이다.
	AskError string `json:"askError,omitempty"`
	// Attached 는 이 헬퍼가 이미 이 데몬에 붙여 뒀는가. 둘째 창이 같은 컴패니언을 고르면
	// **아무것도 안 붙인다**(§5.0.1) — 등록의 임자는 헬퍼이고, 데몬 하나에 붙는 일도 한 번이다.
	Attached bool `json:"attached"`
}

// Chooseable 은 이 컴패니언을 고를 수 있는가. **도구를 못 붙이는 데몬은 못 고른다** —
// 고르는 순간 「이 컴패니언 됩니다」라고 말한 것이 되므로, 거절이 그 뒤에 도착하면 늦다(§5.0.5).
func (c Companion) Chooseable() bool {
	return c.Live && c.ToolServers != nil && *c.ToolServers
}

// Why 는 못 고르는 이유를 사람 말로. **셋을 한 문장으로 뭉치지 않는다** — 할 일이 각각 다르다.
func (c Companion) Why() string {
	switch {
	case !c.Live:
		return "이 컴패니언은 지금 응답하지 않습니다 — 소켓만 남아 있거나 막 죽었습니다."
	case c.AskError != "":
		return "지금 물어보지 못했습니다(" + c.AskError + "). 다시 시도하면 답할 수 있습니다."
	case c.ToolServers == nil:
		return "아직 물어보지 못했습니다."
	case !*c.ToolServers:
		return "이 컴패니언은 도구 서버를 받지 못하는 빌드입니다 — 덱 도구를 붙일 문이 없습니다."
	case c.Transcript == nil || !*c.Transcript:
		// 등급이 다르다: 이쪽은 **고를 수는 있고 채팅창만 못 뜬다**(§5.0.5).
		return "고를 수 있습니다. 다만 이 빌드는 대화를 내주지 못해서 채팅창이 비어 있게 됩니다."
	}
	return ""
}

// Fleet 은 이 머신의 컴패니언을 훑어 고를 수 있는지까지 답한다.
//
// 왕복이 후보당 **둘**이다(§5.0.5): `List` 의 프로브가 하나, `about` 핸드셰이크가 하나.
// 명단과 cap 은 다른 왕복이다 — `List` 가 dial 까지 하지만 싣는 것은 `Status` 이지 핸드셰이크가
// 아니다.
func (a *Attachments) Fleet(configDir string) ([]Companion, error) {
	infos, err := daemon.List(configDir)
	if err != nil {
		return nil, err
	}
	out := make([]Companion, len(infos))
	var wg sync.WaitGroup
	for i, in := range infos {
		out[i] = Companion{
			Socket: in.Socket, Workdir: in.Workdir, Name: in.Name, Role: in.Role,
			Team: in.Team, Session: in.Session, Live: in.Live, Doing: in.Doing,
			Asking: in.Asking != nil, Permission: in.Permission, Backend: in.Backend,
			Model: in.Model, Attached: a.HasLive(in.Socket, lifeOf(in)),
		}
		if !in.Live {
			// 안 사는 것에 핸드셰이크를 걸면 후보 수만큼 타임아웃을 산다. 프로브가 이미 답했다.
			continue
		}
		wg.Add(1)
		go func(i int, socket string) {
			defer wg.Done()
			ts, tr, err := ask(socket)
			if err != nil {
				out[i].AskError = err.Error()
				return
			}
			out[i].ToolServers, out[i].Transcript = &ts, &tr
		}(i, in.Socket)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Socket < out[j].Socket })
	return out, nil
}

// ask 는 후보 하나에게 핸드셰이크를 걸어 cap 둘을 읽는다.
//
// **`Hello` 가 에러면 「못 물어봤다」이고, 성공했는데 cap 이 없으면 「못 받는 빌드」다**(§5.0.5).
// `PeerSupports` 하나로 물으면 그 둘이 같은 거짓이 된다.
func ask(socket string) (toolServers, transcript bool, err error) {
	// **묶인 왕복이어야 한다.** 위 `candidateTimeout` 은 「후보 하나에게 묻는 데 드는 시간의
	// 천장」이라고 적혀 있었는데, 그 상수는 선언만 되고 **아무 데서도 안 쓰이고 있었다**
	// (2026-09-02 리뷰). `daemon.Dial` 은 연결에도 읽기에도 시한이 없어서, 임자가 사라진 소켓
	// 파일 하나가 명단 전체를 영영 붙잡을 수 있었다 — 그리고 이 머신에서는 %APPDATA% 아래
	// AF_UNIX 가 정확히 그렇게 군다. 주석이 약속한 천장을 이제 진짜로 건다.
	cl, err := daemon.DialWithin(socket, candidateTimeout, candidateTimeout)
	if err != nil {
		return false, false, err
	}
	defer cl.Close()
	if _, err := cl.Hello(); err != nil {
		return false, false, err
	}
	return cl.PeerSupports("tool-servers"), cl.PeerSupports("transcript"), nil
}

// Attachments 는 이 헬퍼가 붙여 둔 데몬들.
//
// **등록의 임자는 헬퍼다, 데몬마다 하나씩**(§5.0.1). 헬퍼는 사용자당 하나이고 등록되는 URL 이
// 그 헬퍼의 것이므로, 데몬 하나에 붙는 일도 한 번이다. 이미 붙어 있는 데몬을 둘째 창이 고르면
// 그 창은 **아무것도 안 붙인다** — 붙일 것이 이미 거기 있고, 덱을 가르는 것은 이름이 아니라
// `document` 다. 이 규칙이 없으면 둘째 창이 열리는 것만으로 첫째 창이 쓰던 등록이 떨어진다.
type Attachments struct {
	mu   sync.Mutex
	held map[string]attachment // socket → 우리가 붙여 둔 것
}

// attachment 는 등록 하나. **소켓 경로만으로는 못 센다** — 데몬이 죽었다 같은 경로로 다시 뜨면
// 우리 등록은 그 프로세스와 같이 사라지는데 경로는 그대로다. 실물에서 그 화면을 봤다
// (2026-09-01): 데몬을 `--permission ask` 로 다시 띄웠더니 카드가 「이미 붙어 있음」이라고
// 적었고, 모델에게는 덱 도구가 하나도 없었다. 사람은 셸로 우회하려는 모델을 보고 있었다.
type attachment struct {
	tools []string
	// life 는 그 데몬 **프로세스**의 신원(pid@시작시각). 다르면 남의 생애이고, 우리 등록은
	// 거기 없다 — 「이미 붙어 있다」가 아니라 「다시 붙여야 한다」다.
	life string
}

func NewAttachments() *Attachments { return &Attachments{held: map[string]attachment{}} }

// lifeOf 는 데몬 프로세스 하나의 신원. **세션 id 가 아니다** — 세션은 `/new` 로도 바뀌는데
// MCP 등록은 그때 안 죽는다. 죽는 것은 프로세스가 바뀔 때다.
func lifeOf(in daemon.Info) string { return fmt.Sprintf("%d@%s", in.PID, in.Started) }

// HasLive 는 **이 생애의** 데몬에 우리가 이미 붙어 있는가.
func (a *Attachments) HasLive(socket, life string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.held[socket]
	// life 를 못 읽었으면(기록이 없는 소켓) 옛 답을 그대로 준다 — 모르는 것을 「떨어졌다」로
	// 적으면 멀쩡한 등록을 다시 붙이러 가고, 그 재부착이 첫 등록을 떨어뜨린다.
	return ok && (life == "" || h.life == life)
}

func (a *Attachments) Tools(socket string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.held[socket].tools...)
}

func (a *Attachments) Sockets() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.held))
	for s := range a.held {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Attach 는 데몬 하나에 우리 도구를 붙인다.
//
// 순서가 **언제나 detach → attach** 다(§5.4). 크래시한 헬퍼의 등록은 아무도 덱 도구를 안 부르는
// 동안에는 영영 안 치워지는데(그물이 세는 것은 데몬이 어차피 하던 호출뿐이다), 이름이
// 고정이라(§5.0.6) 잡힌 등록이 남아 있으면 **반드시** 부딪힌다 — 다른 이름으로 피해 갈 여지가
// 설계상 없다. 깨끗한 상태의 detach 는 실패가 아니라 `Removed=false` 다(§5.0.4).
func (a *Attachments) Attach(socket, url, token string) ([]string, error) {
	// **어느 생애의 데몬인가**를 먼저 읽는다. 기록을 못 읽으면 빈 글이고, 그때는 옛 규칙대로
	// 「소켓이 같으면 같은 데몬」으로 군다 — 모르는 것을 「죽었다」로 적으면 멀쩡한 등록을
	// 다시 붙이러 가고, 그 재부착이 첫 등록을 떨어뜨린다.
	life := ""
	if in, err := daemon.Published(socket); err == nil {
		in.Socket = socket
		life = lifeOf(in)
	}
	if a.HasLive(socket, life) {
		// 이미 우리가 붙여 뒀다. **다시 안 붙인다** — 같은 이름으로 다시 붙이면 첫 등록이
		// 떨어지고, 그 창 동안 그 컴패니언의 호출은 도구가 없어서 실패한다.
		return a.Tools(socket), nil
	}
	cl, err := daemon.Dial(socket)
	if err != nil {
		return nil, fmt.Errorf("이 컴패니언에 못 닿았습니다: %w", err)
	}
	defer cl.Close()

	if _, err := cl.Hello(); err != nil {
		return nil, fmt.Errorf("핸드셰이크를 못 했습니다: %w", err)
	}
	if !cl.PeerSupports("tool-servers") {
		// 판정이 attach 보다 앞이다(§5.0.5). 여기까지 온 것은 화면이 그 판정을 건너뛴 경우라,
		// 사유를 빌드의 성질로 적는다.
		return nil, fmt.Errorf("이 컴패니언은 도구 서버를 받지 못하는 빌드입니다")
	}
	if _, err := cl.DetachMCP(ServerName); err != nil {
		// 「이미 깨끗했다」는 실패가 아니라 `Removed=false` 다. 여기 오는 것은 진짜 실패다 —
		// 오퍼레이터가 config 로 선언한 같은 이름의 서버가 있는 경우가 그것이고, door 는
		// 그것을 안 뗀다.
		return nil, fmt.Errorf("옛 등록을 못 뗐습니다: %w", err)
	}
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	tools, err := cl.AttachMCP(ServerName, url, headers)
	if err != nil {
		return nil, fmt.Errorf("도구를 못 붙였습니다: %w", err)
	}
	a.mu.Lock()
	a.held[socket] = attachment{tools: append([]string(nil), tools...), life: life}
	a.mu.Unlock()
	return tools, nil
}

// Detach 는 우리 등록을 뗀다. 나갈 때 부른다 — 남겨 두면 다음에 뜬 헬퍼가 이름 충돌로 거절당하고,
// 그 사이 모델에게는 **손이 없는 도구가 광고된다**(§5.4).
func (a *Attachments) Detach(socket string) error {
	a.mu.Lock()
	_, held := a.held[socket]
	delete(a.held, socket)
	a.mu.Unlock()
	if !held {
		return nil
	}
	cl, err := daemon.Dial(socket)
	if err != nil {
		return err
	}
	defer cl.Close()
	_, err = cl.DetachMCP(ServerName)
	return err
}

// DetachAll 은 나가는 길에 전부 뗀다.
func (a *Attachments) DetachAll() {
	for _, s := range a.Sockets() {
		_ = a.Detach(s)
	}
}
