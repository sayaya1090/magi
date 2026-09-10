# magi — 상세 설계 (역사)

[English](DESIGN.md) · [한국어](DESIGN.ko.md) · [↑ Docs](README.ko.md)

> ⚠️ **이 문서는 M1 착수 시점의 초기 설계 사양입니다.** 구현이 진행되며 크게 확장되었고, 그 중 일부(절차 플래너, 서브에이전트 위임, 사전 억셉턴스 체크 등)는
> 실측 평가를 거쳐 간소화되었습니다. **현재 기준 정본은 [`ARCHITECTURE.md`](ARCHITECTURE.md)**이며,
> 충돌 시 해당 문서가 우선합니다. 본 문서는 초기 설계 근거(결정 D1~D13의 구체화 과정)를 보존하기 위해 유지합니다.
>
> 영문판: [`DESIGN.md`](DESIGN.md). 본문의 초기 설계 의도는 보존하되, 현행 구현과 배치되는 주석은 코드 실측에 맞추어 사실에 부합하도록 정비하였습니다.

> PLAN의 결정(D1~D13)을 구현 수준으로 구체화한 사양입니다(이벤트/커맨드 스키마, 포트 시그니처, 패키지 구조).
> 핵심 패턴: **CQRS-lite** — 내부 입력은 *Command*, 외부 출력은 *Event*로 단일화하여 프로세스 내 호출과 원격 통신의 동등성을 보장합니다(D5).

---

## 1. 패키지 구조

> 아래는 *as-built* 트리(2026-06 기준) 현황입니다. 원안 대비 변경 사항:
> `core/capability` 제거(미사용) · `core/{model,plugin}` 추가 · `port`는 단일 `port.go`로 통합 ·
> `app`은 `service.go` 대신 `app.go`로 재구성되었으며 가드레일/워크플로 로직이 추가되었습니다 ·
> 빌트인 툴이 6개에서 대폭 확장되었습니다. 세부 사항은 [`ARCHITECTURE.md`](ARCHITECTURE.md) §패키지 맵을 참고하십시오.

```
github.com/sayaya1090/magi

cmd/magi/                 # 엔트리포인트: 플래그 파싱(-p 헤드리스), DI 와이어링, 시스템 프롬프트

internal/
  core/                     # 도메인 — 바깥(어댑터) 의존 0
    session/                #   Session, Message, Part, SessionMeta, Todo
    event/                  #   Event (영속 로그 + 버스 단위)  ★스키마 §3
    command/                #   Command (actor 태깅 입력)       ★스키마 §4
    artifact/               #   Artifact (D11)
    tool/                   #   Tool, ToolResult, Registry 계약
    model/                  #   ModelRef 등 모델 식별 타입
    plugin/                 #   플러그인/capability 메타 타입
    agent/                  #   Agent 설정 + 순수 규칙(stop조건/컨텍스트 조립)
    bus/                    #   EventBus (인메모리 pub/sub, 다중 구독 fan-out)
  port/                     # 포트(인터페이스) — 코어가 정의       ★시그니처 §5
    port.go                 #   LLMProvider/Store/Tool/ToolEnv/Platform/PluginHost/ExperienceStore …
  app/                      # 애플리케이션 서비스(유스케이스)       ★ §4
    app.go                  #   Application 구현 + Config(Profile/Sandbox/Workflow…)
    loop.go                 #   에이전트 루프(포트 오케스트레이션) + 루프 가드 + 언어 지시
    loop_gates.go           #   종료 경로, 순서대로: Stop 훅 · 빈 결과 · 종료 선언 ·
                            #   미실행 산출물 · 미회수 인계 · 받은 답 평가
    workflow.go             #   결정적 워크플로 엔진(phase 게이트)
    policy.go               #   가드레일 정책 엔진(allow/deny/egress/secret-deny)
    context.go·compact.go   #   컨텍스트 조립 + compaction
    memory.go·skills.go·hooks.go·diagnose.go  #   AGENTS.md 메모리 / 스킬 / 훅 / 진단
  adapter/                  # 어댑터(포트 구현)
    llm/openai/             #   OpenAI 호환(Ollama/vLLM/LiteLLM): 캐싱·폴백·에러매핑
    store/jsonl/            #   append-only JSONL
    tool/builtin/           #   read/write/edit/multiedit/grep/glob/list/bash/bash_output/
                            #   bash_kill/bash_input/wait_for/port_owner/todowrite/label/council/
                            #   webfetch/websearch/remember/skill/recall_context/recall_memory/
                            #   search_sessions/schedule  (+ ask_user/route_interjection은
                            #   대화형 전용, port_owner는 답할 수 없는 환경에서 철회)
    platform/               #   OS별 exec/경로/터미널 능력
    experience/git/         #   공유 두뇌(git repo)                (M5~)
    plugin/lua/             #   gopher-lua 호스트                  (M3)
    mcp/                    #   MCP 클라이언트                     (M4)
    tui/                    #   bubbletea UI                       (M2)
  config/                   # TOML 설정 로더

plugins/examples/           # 예제 Lua 플러그인
```

**의존 규칙**: `adapter → app → core`, 그리고 `app/adapter → port`. `core`는 무엇도 import 안 함(표준+core 내부만). 컴파일 타임에 강제.

---

## 2. 코어 데이터 타입 (`core/session`, `core/artifact`)

```go
type SessionID string
type Role string // "user" | "assistant" | "tool" | "system"

type Session struct {
    ID       SessionID
    Workdir  string
    Agent    string        // 사용 에이전트 이름
    Model    ModelRef      // provider+model
    Created  time.Time
    Meta     map[string]string
}

type Message struct {
    ID    string
    Role  Role
    Parts []Part
}

// Part = 스트리밍/저장 최소 단위. kind로 구분(태그드 유니온).
type Part struct {
    ID   string   `json:"id"`
    Kind PartKind `json:"kind"`
    // kind별 필드(하나만 채움)
    Text     string          `json:"text,omitempty"`      // text|reasoning
    ToolCall *ToolCall        `json:"toolCall,omitempty"`  // tool-call
    ToolResult *ToolResult    `json:"toolResult,omitempty"`// tool-result
    Image    *ImageRef        `json:"image,omitempty"`     // image
    Err      string           `json:"error,omitempty"`     // error
}

type PartKind string // text | reasoning | tool-call | tool-result | image | error

type ToolCall struct {
    CallID string          `json:"callId"`
    Name   string          `json:"name"`
    Args   json.RawMessage `json:"args"`
}
type ToolResult struct {
    CallID  string          `json:"callId"`
    Content json.RawMessage `json:"content"` // text/json/이미지참조
    IsError bool            `json:"isError,omitempty"`
}
type ImageRef struct { // 원본은 별도 파일/blob, 로그엔 참조만
    Path string `json:"path"` // 또는 blob 해시
    MIME string `json:"mime"`
}

// Artifact (D11) — 에이전트가 emit하는 검토용 산출물
type Artifact struct {
    ID          string    `json:"id"`
    Kind        string    `json:"kind"`   // plan|walkthrough|screenshot|test-report|diff|...
    Title       string    `json:"title"`
    Content     json.RawMessage `json:"content"`
    SourceAgent string    `json:"sourceAgent"`
    Status      string    `json:"status"` // draft|proposed|approved|rejected
    Created     time.Time `json:"created"`
}
```

---

## 3. 이벤트 스키마 (`core/event`) — 영속 로그 + 버스

**공통 봉투(envelope)** — 모든 이벤트:
```go
type Event struct {
    Seq       int64           `json:"seq"`       // 세션별 단조증가(Store가 부여). 버스전용은 0
    SessionID SessionID       `json:"sessionId"`
    Type      Type            `json:"type"`
    Actor     Actor           `json:"actor"`     // 누가 유발(D5)
    TS        time.Time       `json:"ts"`
    Data      json.RawMessage `json:"data"`      // 타입별 페이로드
}
type Actor struct {
    Kind ActorKind `json:"kind"` // user|agent|system
    ID   string    `json:"id"`   // user id / agent name
}
```

**A. 영속(로그에 append, JSONL 한 줄)** — 재생하면 대화 복원:
| Type | Data |
|---|---|
| `session.created` | `{workdir, agent, model}` |
| `prompt.submitted` | `{messageId, parts[]}` (role=user) |
| `part.appended` | `{messageId, role, part}` (완성된 part 1개) |
| `permission.decided` | `{callId, decision}` (감사용) |
| `council.convened` | `{round, members[], rule, task, plan, report, actions, changes, noChanges, keep, epoch}` (D14 출하 M9 — 종료 게이트 소집) — 라운드가 자기 증거를 스스로 공표; `epoch`는 소집 시점 가드의 변이 카운트로, 잘려 실리는 `changes`가 될 수 없는 판별자 |
| `council.verdict` | `{round, member, decision(done\|continue\|abstain), confidence, rationale, feedback, keep, silent}` — `silent`는 아무도 내지 않은 평결 표시(백엔드 다운·시한·해독 불가 응답); `abstain` 옆에 실려 실패가 숙고된 기권으로 보고되지 않게 함 |
| `council.decided` | `{round, decision, tally, injectedFeedback}` (continue면 feedback이 prompt.submitted로 주입됨) |
| `compaction` | `{summary, replacesUpToSeq, tokens:{before,after}}` |
| `turn.finished` | `{usage:{in,out,cost}}` |
| `todos.changed` | `{todos[]}` (계획 변경 1회마다 — 시드→단계 체크→완료/취소; 로그·재생·패널 리렌더) |
| `error` | `{message, code}` |

**B. 전이(transient, 버스에만 — 저장 안 함)** — 라이브 UX용:
| Type | Data |
|---|---|
| `part.delta` | `{messageId, partId, kind, text}` (스트리밍 텍스트 조각) |
| `tool.progress` | `{callId, ...}` |
| `permission.requested` | `{callId, name, args}` → UI 프롬프트(결정은 A로 저장) |
| `context.usage` | `{used, max, …}` (컨텍스트 미터 — 전이) |
| `workflow.phase` | `{phase, status, detail}` (워크플로 엔진 단계 진행 — 전이) |
| `council.deliberating` | `{round, member, state}` (라이브 심의 패널 — 전이, D14 출하 M9) |

> 원칙: **사실(fact)은 영속, 진행상황(delta/progress)은 전이.** 재생 시 delta는 불필요(완성 part로 충분). → 로그가 깔끔하고 D6의 "버스=저장" 정신 유지.

> ★교정(실제 구현): 상기 표들은 **전체 어휘 목록이 아닌 대표 표본**입니다. 과거에는 전체 목록으로 취급되었으나 양방향 모두에서 실제 구현과 차이가 있었습니다. `artifact.emitted`와 `tool.started`는 상수, 페이로드 구조체, 발신 로직 없이 문서상에만 존재하여 클라이언트 혼선을 유발했습니다(실제 도구 시작은 사실 이벤트인 `part.appended(tool-call)`를 통해 모든 화면에 정상 전달됩니다). 반대로 실제 로그에 기록되는 `result.elided`, `labels.changed`, `session.moved`, `model.changed`, `interjection.deferred`, `interjection.answered` 등은 기존 표에 누락되어 있었습니다. `agent.spawned`/`agent.status`는 에이전트 단일화에 따라 완전히 제거되었습니다. **완전한 정본 집합은 [`SPEC.ko.md`](SPEC.ko.md) F-EVENT-FACT-TRANSIENT**에 정의되어 있으며, 테스트 코드가 `transientTypes` 선언을 통해 정합성을 검증합니다. 실제 페이로드 구조는 `internal/core/event/payload.go`를 따릅니다.

**JSONL 로그 예시** (`~/<datadir>/projects/<cwd>/<sessionId>.jsonl`):
```json
{"seq":1,"sessionId":"s_01","type":"session.created","actor":{"kind":"user","id":"local"},"ts":"...","data":{"workdir":"/x","agent":"default","model":{"provider":"openai","model":"qwen2.5-coder"}}}
{"seq":2,"sessionId":"s_01","type":"prompt.submitted","actor":{"kind":"user","id":"local"},"ts":"...","data":{"messageId":"m1","parts":[{"id":"p1","kind":"text","text":"add a test"}]}}
{"seq":3,"sessionId":"s_01","type":"part.appended","actor":{"kind":"agent","id":"default"},"ts":"...","data":{"messageId":"m2","role":"assistant","part":{"id":"p2","kind":"tool-call","toolCall":{"callId":"c1","name":"read","args":{"path":"x_test.go"}}}}}
{"seq":4,"sessionId":"s_01","type":"part.appended","actor":{"kind":"agent","id":"default"},"ts":"...","data":{"messageId":"m2","role":"tool","part":{"id":"p3","kind":"tool-result","toolResult":{"callId":"c1","content":"...","isError":false}}}}
```

---

## 4. 커맨드 스키마 + Application (`core/command`, `app`)

**Command = 안으로 들어가는 입력. actor 태깅 + 직렬화 가능.** 결과는 Event로 흘러나온다(CQRS-lite).

```go
type CreateSession struct { Workdir, Agent string; Model ModelRef; Actor Actor }
type SubmitPrompt   struct { SessionID SessionID; Parts []Part; Actor Actor }
type Interrupt      struct { SessionID SessionID; Actor Actor }
type RespondPermission struct { SessionID SessionID; CallID string; Decision string; Actor Actor } // allow|deny|always
type Compact        struct { SessionID SessionID; Actor Actor }
type ReviewArtifact struct { SessionID SessionID; ArtifactID, Decision string; Actor Actor }      // approve|reject (→ D13 기여)
```

**Application 인터페이스** — 커맨드 in, 이벤트 stream out:
```go
type Application interface {
    CreateSession(ctx context.Context, c CreateSession) (SessionID, error)
    Submit(ctx context.Context, c SubmitPrompt) error          // 비동기: 루프는 goroutine, 결과는 이벤트로
    Interrupt(ctx context.Context, c Interrupt) error
    RespondPermission(ctx context.Context, c RespondPermission) error
    Compact(ctx context.Context, c Compact) error

    // 구독: fromSeq부터 과거 재생 + 이후 라이브(late-joiner/재접속 지원)
    Subscribe(ctx context.Context, s SessionID, fromSeq int64) (<-chan Event, func(), error)
    ListSessions(ctx context.Context, workdir string) ([]SessionMeta, error)
}
```
> 이 모양 때문에 TUI(인프로세스)는 직접 호출, 미래 server는 HTTP/SSE로 같은 메서드를 노출 = D5 "트랜스포트만 추가".

---

## 5. 포트 시그니처 (`internal/port`)

```go
// LLM — OpenAI 호환 어댑터가 첫 구현(D3)
type LLMProvider interface {
    StreamChat(ctx context.Context, r ChatRequest) (<-chan ProviderEvent, error)
}
type ChatRequest struct {
    Model    string
    System   string
    Messages []Message
    Tools    []ToolSpec     // name/description/jsonschema
    Params   map[string]any // temp, maxTokens...
}
type ProviderEvent struct { // 공급자 스트림을 공통화
    Type string // text-delta|reasoning-delta|tool-call|finish|usage|error
    Text string
    ToolCall *ToolCall
    Usage *Usage
    Err   error
}

// Store — 이벤트소싱 영속(D6). 1차 구현 = jsonl
type Store interface {
    Append(ctx context.Context, s SessionID, evs ...Event) ([]int64, error) // seq 부여 반환
    Read(ctx context.Context, s SessionID, fromSeq int64) ([]Event, error)
    ListSessions(ctx context.Context, workdir string) ([]SessionMeta, error)
    Compact(ctx context.Context, s SessionID, upToSeq int64, snapshot Event) error
}

// Tool — 빌트인은 Go 구현(POSIX 비의존). 플러그인/MCP 툴도 같은 인터페이스
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, args json.RawMessage, env ToolEnv) (ToolResult, error)
}
// ToolEnv — 원안은 5개 필드였으나 가드레일/카운슬 도입으로 확장됐다.
// 아래는 as-built 요약(전체·주석은 internal/port/port.go 참조).
//
// ★정정: 원안에 있던 멀티에이전트 4개 필드(Spawn/Dispatch/Ask/Report)는 **없다.**
// 에이전트 단일화로 서브에이전트가 사라진 뒤 아무도 세우지 않고 아무 툴도 읽지 않는 채로
// 남아 있었고, 애플리케이션이 이행하지 않는 계약을 광고하는 포트는 읽는 사람과 툴 표면을 읽는
// 모델 둘 다에게 거짓을 가르치므로 제거됐다.
type ToolEnv struct {
    SessionID SessionID
    Workdir   string
    ScratchDir, ScratchTmp string                                               // 턴 스크래치 / 자식 TMPDIR
    AskPermission func(callID, name string, args json.RawMessage) (bool, error) // 권한 게이트
    EmitArtifact  func(Artifact)                                                // D11 산출물
    EmitProgress  func(text string)                                             // 툴이 막혀 있는 동안 라이브 노트
    // 카운슬/사용자 — 각각 nil이면 그 능력이 이 런에 없다는 뜻이고, 모든 툴이 호출 전 nil을 본다
    Council func(ctx context.Context, question string, complete bool) (string, error) // complete=종료 선언
    AskUser func(question string, options []string) (string, error)                   // 대화형 전용
    RouteInterjection func(action, reason, requestID string) error                    // 최상위 전용
    // 계획/메모리/스킬
    SetTodos    func(todos []session.Todo)          // todowrite
    NoteForTurn func(text string) error             // remember{scope:"turn"}; err=보관 안 됨
    Propose     func(c Contribution) error          // 공유 경험(D13) 기여
    LoadSkill   func(name string) (string, bool)    // 명명된 스킬 로드
    Recall       func(query string) (string, error) // 이 세션의 압축된 상세
    RecallMemory func(query string) (string, error) // 세션 간 D13 저장소
    Platform  Platform
    Sandbox   SandboxSpec                           // OS 샌드박스(read-only|workspace-write…); zero값=비격리
}
type ToolRegistry interface { Register(Tool); Get(name string) (Tool, bool); List() []Tool }

// ExperienceStore — 공유 두뇌(D13), git repo 백엔드
type ExperienceStore interface {
    Retrieve(ctx context.Context, q string) ([]Memory, []Skill, error) // 세션시작 RAG
    Propose(ctx context.Context, c Contribution) error                  // 리뷰 큐로(자동반영X)
}

// PluginHost — 핫리로드(D10)
type PluginHost interface {
    Load(ctx context.Context, dir string) (PluginInfo, error)
    Unload(name string) error
    Reload(name string) error
    Capabilities() CapabilitySet
}

// 기타
type ContextProvider interface { Provide(ctx context.Context, q ContextQuery) ([]ContextChunk, error) }
type Scheduler interface { // D12: Tier1 ticker(M5), Tier2 OS(Later)
    Schedule(spec ScheduleSpec, target Trigger) (id string, err error)
    Cancel(id string) error
}
type Platform interface { // 크로스플랫폼 추상화(§9.5)
    Exec(ctx context.Context, cmd Cmd) (ExecResult, error)
    ConfigDir() string
    DataDir() string
    TerminalCaps() TermCaps // truecolor/이미지 프로토콜 탐지
}

// Council — 루프 종료 게이트(D14, 출하 M9). 위원 팬아웃은 어댑터, 합의규칙은 순수 core.
// 기본 어댑터는 응답을 Verdict로 파싱한다(JSON폴백 재사용). 위원별 StreamChat 병렬 호출은 위원들이
// 서로 다른 백엔드에 핀됐을 때만이고, provider와 model이 같으면 패널 1회 호출이 위원 전원의 훑기와
// 판정을 싣고, 두 번째 호출이 라운드를 닫는다(아래 CouncilMember 참조).
type Council interface {
    Deliberate(ctx context.Context, r DeliberationRequest) (Deliberation, error)
}
type DeliberationRequest struct {
    Round    int
    Phase    string         // 심의 종류를 구분하는 라벨(현재는 종료 선언 심의 하나)
    Task     string         // 원 과제(목표)
    Plan     string         // 계약: acceptance criteria, 또는 Phase=plan일 때 제안된 절차
    Report   string         // 주장: 에이전트 자기보고 (있으면)
    Actions  string         // 증거: 이번 턴의 툴 결과 요약(write 바이트수, cat 출력 등) — git 비의존
    Signals  []Signal       // 증거: test/lint/type 결과
    Changes  string         // 이번 턴의 파일 편집을 에이전트의 write/edit 툴에서 재구성 (선택)
    Members  []CouncilMember
    Rule     string         // unanimous|majority|quorum:k|weighted:θ|veto
    Debate   bool           // MAGI_COUNCIL_DEBATE: SPLIT would-be-done → 위원 반박 1라운드 재폴링
    Keep     bool           // MAGI_COUNCIL_KEEP: 위원이 "유지할 부분"을 명시 → continue 피드백에 자문으로 실림
    // 그 밖: DefaultModel·NoChanges·Changes. 전체는 port.go 참조.
    // ★정정: Devil(MAGI_COUNCIL_DEVIL)은 없다. Phase="plan"의 계획 감사와 그것이 낳던
    // Criteria/deliverable Checks도 없다 — 작업이 존재하기도 전에 판단을 확정하던 단계들이라
    // 플래너와 함께 걷어냈다.
}
// 증거 출처: git diff는 비-git 작업폴더(샌드박스 태스크 디렉터리)에선 비어 →
// 생성물 판단 불가로 종료 게이트가 무한 churn하던 문제를, 이번 턴의 "툴 결과"(Actions)와
// 툴에서 재구성한 Changes를 git-독립 증거로 넘겨 해결. 단, 모델 자기서술은 증거에서 제외(Report=주장)하여 "산출물
// 없이 말빨로 done" 회귀를 막음 — [ok]/exit-0 자체는 증거 아님, 산출물을 보여야 함.
type CouncilMember struct { // 테마명 라벨 + 렌즈 속성
    Name     string  // "Melchior" | "Balthasar" | "Casper"
    Lens     string  // "correctness" | "verification" | "completeness"
    Model    string  // 빈값=세션 모델
    Provider string  // 빈값=기본 백엔드. 다르면 위원별 호출 모양 유지
    Weight   float64
}
// 렌즈에는 고유한 탐색 경로(`core/council.Routes`)가 연계된다 — 동일한 증거를 각 위원이 어느 우선순위로 점검하는가의 차이다.
// 리터럴 요구 문구와 수치 데이터 자체(correctness), 각 동작이 실제로 실행된 시점과 결과(verification), 과제가 요구한
// 모든 구성 요소의 완전성(completeness). 경로는 관할의 분할이 아닌 탐색 우선순위다: 세 위원 모두 과제 전체에 대해
// 독립적으로 최종 판정을 내린다. 관할을 분할할 경우 한 위원의 영역에 국한된 결함이 다른 영역 위원 2명의 완료 투표에 의해
// 다수결로 은폐되기 때문이다. 탐색 경로를 명시한 근거: 렌즈 레이블만 다르고 지시문이 동일했던 3인 위원 구성 실험에서
// 21회 시행 전량(21/21)이 단 하나의 이견도 없이 만장일치 완료로 수렴하는 과다승인 결함이 확인되었다.
//
// 위원은 판정을 내리기 전에 요구사항 교차 점검을 기록한다: 각 요구사항당 한 줄씩 SATISFIED 또는 UNSATISFIED,
// 도구 실행 결과에서 직접 인용한 증거 스니펫이나 NO-EVIDENCE로 결론을 기재한다. 이 필드는 스키마상에서 decision보다
// 앞선 위치에 배치되어, 이미 정해둔 결론에 맞추어 검증 증거를 사후 왜곡하는 현상을 방지한다. 위원이 실제로 확인한 증거는
// `Verdict.Cite`에 영속 보존된다.
//
// 투표 집계 후 단일 종결 호출(Closing call)이 세 위원의 점검 결과를 종합 검토한다 — 위원 해석 간의 상호 모순,
// 어떤 위원도 다루지 않은 미점검 요구사항, 그 자체로 명백히 오류인 수치가 식별되는 최종 단계다. 종결 호출의 결론은
// 비대칭적으로 클램핑되며(기존 done → continue로의 반전만 허용, 반대 방향 불가), 판정 번복 여부와 무관하게
// `Deliberation.Close`에 기록된다. 클램프 처리와 패널 배치는 어댑터 계층의 책임이며, `core/council`은 순수 함수로 투표만 집계한다.
// Verdict, Deliberation, Tally 등 결과 모델 및 합의 규칙은 `core/council`(순수 패키지)에 속한다.
```

---

> **확장 안내**: 실제 `app.Application`은 위 골격 외에 가드레일 정책, 결정적 워크플로,
> AGENTS.md 메모리, 훅, 턴 중간 개입(interjection), 툴로서의 카운슬을 포함한다.
> ★정정: 원안이 열거하던 멀티에이전트(task/spawn/dispatch/ask/report)는 없다.
> 동작 기준은 [`ARCHITECTURE.md`](ARCHITECTURE.md).

## 6. 에이전트 루프 (`app/loop.go`) — 의사코드

```
Submit(cmd):
  store.Append(prompt.submitted); bus.Publish(...)
  go run(sessionID)           // 비동기, ctx 취소로 Interrupt

run(sessionID):
  for step in 0..maxSteps:
    msgs   = assemble(history, latest compaction, contextProviders, experience.Retrieve)
    stream = llm.StreamChat(req{msgs, tools})
    for ev in stream:
      text-delta   -> bus.Publish(part.delta)                 // 전이
      tool-call    -> collect
      finish       -> store.Append(part.appended for text)    // 영속
    if no tool calls:
      // ★정정: 원안의 "카운슬이 스스로 소집하는 종료 게이트"는 철회되었다. 해당 배치는 카운슬이
      // 합리적으로 결정할 수 없는 두 가지 문제 — 질의 시점(에이전트가 이미 판단을 굳힌 순간의 강제 개입)과
      // 답변 수신 가능성(헤드리스 모드에서 자문 주입과 turn.finished가 동일 틱에 발생하여 미수신) — 를 유발했다.
      // 현재는 종료 경로(loop_gates.go)가 6개 게이트를 순차 검증한다:
      //   1) Stop 훅 — 실패 시 해당 출력을 피드백으로 전달하여 작업 복귀
      //   2) 빈 결과 넛지(텍스트 없는 답변) — 1회
      //   3) 종료 선언 요구 — `council` 툴을 complete:true로 호출하도록 유도.
      //      무진전 구간당 3회로 제한되며, 마지막 요청 이후 실제 파일 변경 시 예산이 리셋된다.
      //      경계를 초과하면 미선언 사유를 기록한 UNVERIFIED 상태로 종료된다.
      //   4) 선언 이후 호출된 도구는 실행되지 않고 폐기됨을 통지 — 턴당 1회
      //   5) 미회수 인계 — 타 컴패니언에 위임한 작업의 결과가 아직 회신되지 않음
      //   6) 회신된 결과 평가(rate_handoff, 종료 시점에 허용)
      // 카운슬은 이제 에이전트가 자발적으로 호출하는 **도구**이며, 선언이 승인되면 루프에 완료 신호가 전달된다.
      // 이후 절차: 선택적 증류 패스(기본 비활성), 지연된 사용자 개입 수거, finalizeTodos — 열려 있던
      // 작업 스텝을 실완료 또는 취소로 확정한다. 완료된 실행이 불완전한 상태의 체크리스트를 남기지 않도록 보장한다.
      store.Append(turn.finished{Unverified: reason != ""}); return
    for call in toolcalls:
      if needsPermission(call): bus.Publish(permission.requested); wait RespondPermission
      store.Append(permission.decided)
      // 도구 호출 part는 응답이 수신될 때 이미 append 완료된다 — 별도의 tool.started는 존재하지 않는다
      res = registry.Get(call.name).Execute(...)
      store.Append(part.appended{tool-result})
    if budget/depth exceeded (D7): graceful stop
```

> ★정정 (as-built): **진행 속도를 임의 제한하는 상한은 없으며, 240스텝 폭주 백스톱만 유지한다.** 또한 **루프 가드는
> 관찰 신호를 보고할 뿐 실행을 강제 정지시키지 않는다.** 자체 휴리스틱 강제 정지는 벤치마크 실측을 통해 전면 제거되었다 —
> 외부 데드라인에 도달한 실행도 정규 채점되어 396건 중 76건이 통과한 반면, magi가 자체 판단으로 중단시킨 28건은 통과율이 0%였고
> 그중 8건은 비정상 종료 코드로 인해 채점 기회조차 박탈되었기 때문이다. 백스톱 상한을 소진한 최상위 턴은 해당 사유를 명시한
> `UNVERIFIED` turn.finished 이벤트를 로그에 남기고 안전하게 종료된다. 가드가 수집하는 이상 신호(반복, 정체, 자체 롤백,
> 무변경 파일 쓰기, 실행 처닝)는 현재도 모두 취합되어 에이전트에게 넛지로 전달된다. 언어 지침
> (langDirective) 주입 및 워크플로 모드 분기(`runWorkflow`)는 정상 유지된다. 워크플로 페이즈는 자체 실행 예산을 독립 선언한다.

---

## 7. M1 구현 순서 (이 설계 기준)
1. `core/session`,`core/event`,`core/command`,`core/artifact` 타입.
2. `core/bus` 인메모리 pub/sub.
3. `port` 인터페이스 전부 선언(빈 채로).
4. `adapter/store/jsonl` — Append/Read/Subscribe 재생.
5. `adapter/llm/openai` — Ollama `/v1` 스트리밍 + tool_calls + **프롬프트 폴백**.
6. `adapter/tool/builtin` — read/write/edit/grep/glob/list (Go).
7. `adapter/platform` — exec/경로/터미널 능력(darwin/linux/windows).
8. `app/app.go`+`loop.go` — 위 루프. (원안은 `service.go`였다.)
9. `cmd/magi` — `-p` 헤드리스(stdin 프롬프트 → stdout 이벤트).
10. **Ollama 실모델 tool-calling 라이브 테스트**(네이티브+폴백) + core 단위테스트.
