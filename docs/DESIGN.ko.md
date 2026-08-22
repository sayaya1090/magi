# magi — 상세 설계 (역사)

[English](DESIGN.md) · [한국어](DESIGN.ko.md) · [↑ Docs](README.ko.md)

> ⚠️ **이 문서는 M1 착수 *시점*의 설계 의도다.** 구현이 그 뒤로 크게 확장됐고, 그 확장 중
> 상당수는 다시 걷어냈다 — 절차 플래너, 서브에이전트 위임, 저술된 억셉턴스 체크, 종료를 투표로
> 결정하던 카운슬. **현재 *as-built* 기준은 [`ARCHITECTURE.md`](ARCHITECTURE.md)** 를 보라 —
> 충돌 시 그 문서가 우선. 이 문서는 설계 근거(결정 D1~D13의 구체화)로 보존한다.
>
> 영문판: [`DESIGN.md`](DESIGN.md). 본문의 설계 의도는 당시 그대로 두되, **"as-built"라고
> 주장하던 주석은 코드와 대조해 사실로 고쳤다** — 틀린 현황 주석은 보존된 역사가 아니라 그냥
> 오답이기 때문이다. 고친 자리는 그 취지를 밝혀 적었다.

> PLAN의 결정(D1~D13)을 코드 직전 수준으로 구체화. 이벤트/커맨드 스키마 · 포트 시그니처 · 패키지 구조.
> 핵심 패턴: **CQRS-lite** — 안으로는 *Command*, 밖으로는 *Event*. 이게 인프로세스↔원격을 같게 만든다(D5).

---

## 1. 패키지 구조

> 아래는 *as-built* 트리(2026-06 기준)로 갱신했다. 원안 대비 변경점:
> `core/capability` 제거(미사용) · `core/{model,plugin}` 추가 · `port`는 단일 `port.go`로 통합 ·
> `app`은 `service.go` 대신 `app.go`이며 가드레일/워크플로 파일이 추가됐다 ·
> 빌트인 툴이 6개에서 대폭 늘었다. 세부는 [`ARCHITECTURE.md`](ARCHITECTURE.md) §패키지 맵 참조.

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
    loop_gates.go           #   종료 경로: Stop 훅 · 빈 결과 · 미실행 산출물 · 종료 선언
    workflow.go             #   결정적 워크플로 엔진(phase 게이트)
    policy.go               #   가드레일 정책 엔진(allow/deny/egress/secret-deny)
    context.go·compact.go   #   컨텍스트 조립 + compaction
    memory.go·skills.go·hooks.go·diagnose.go  #   AGENTS.md 메모리 / 스킬 / 훅 / 진단
  adapter/                  # 어댑터(포트 구현)
    llm/openai/             #   OpenAI 호환(Ollama/vLLM/LiteLLM): 캐싱·폴백·에러매핑
    store/jsonl/            #   append-only JSONL
    tool/builtin/           #   read/write/edit/multiedit/grep/glob/list/bash/bash_output/
                            #   bash_kill/bash_input/wait_for/port_owner/todowrite/council/
                            #   webfetch/websearch/remember/skill/recall_* + sandbox_*
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
| `artifact.emitted` | `{artifact}` |
| `council.convened` | `{round, members[], rule}` (D14 출하 M9 — 종료 게이트 소집) |
| `council.verdict` | `{round, member, decision(done\|continue\|abstain), confidence, rationale, feedback}` |
| `council.decided` | `{round, decision, tally, injectedFeedback}` (continue면 feedback이 prompt.submitted로 주입됨) |
| `compaction` | `{summary, replacesUpToSeq, tokens:{before,after}}` |
| `turn.finished` | `{usage:{in,out,cost}}` |
| `todos.changed` | `{todos[]}` (계획 변경 1회마다 — 시드→단계 체크→완료/취소; 로그·재생·패널 리렌더) |
| `error` | `{message, code}` |

**B. 전이(transient, 버스에만 — 저장 안 함)** — 라이브 UX용:
| Type | Data |
|---|---|
| `part.delta` | `{messageId, partId, kind, text}` (스트리밍 텍스트 조각) |
| `tool.started` | `{callId, name}` |
| `tool.progress` | `{callId, ...}` |
| `permission.requested` | `{callId, name, args}` → UI 프롬프트(결정은 A로 저장) |
| `agent.spawned` / `agent.status` | `{agentId, parent, role, state}` (멀티에이전트 라이브) |
| `context.usage` | `{used, max, …}` (컨텍스트 미터 — 전이) |
| `workflow.phase` | `{phase, status, detail}` (워크플로 엔진 단계 진행 — 전이) |
| `council.deliberating` | `{round, member, state}` (라이브 심의 패널 — 전이, D14 출하 M9) |

> 원칙: **사실(fact)은 영속, 진행상황(delta/progress)은 전이.** 재생 시 delta는 불필요(완성 part로 충분). → 로그가 깔끔하고 D6의 "버스=저장" 정신 유지.

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
// 렌즈에는 경로(core/council.Routes)가 딸린다 — 같은 증거를 그 위원이 어디부터 훑는가다.
// 리터럴 문구와 값 그 자체(correctness), 각 동작이 실제로 돌아간 순간(verification), 과제가 요구한
// 서로 다른 부분 전부(completeness). 경로는 관할이 아니라 탐색 순서다: 셋 다 여전히 과제 전체를
// 판정한다. 나누면 한 위원의 몫 안에 든 결함이 아무것도 모르는 done 둘에 continue 하나로 맞서기
// 때문이다. 경로를 넣은 이유는 렌즈만으로 위원이 구별되지 않아서다 — 렌즈 한 줄만 다르고 나머지
// 지시가 전부 같았을 때 21회 중 21회를 이견 없이 done으로 투표했다.
//
// 위원은 결정을 말하기 전에 훑기를 쓴다: 요구사항 하나에 한 줄, SATISFIED 또는 UNSATISFIED,
// 툴 결과에서 그대로 떼어 온 조각이나 NO-EVIDENCE로 결론. 이 필드는 스키마에서 decision보다 앞에
// 놓여, 이미 내려놓은 결론에서 읽기를 거꾸로 조립할 수 없다. 위원이 무엇을 읽었다고 했는지는
// Verdict.Cite에 남는다.
//
// 집계 뒤 닫는 호출 하나가 세 훑기를 한자리에서 읽는다 — 위원 간 모순, 어느 훑기도 덮지 않은
// 요구사항, 그 자체로 틀린 값이 보이는 유일한 자리다. 그 결론은 한 방향으로 클램프되고
// (done → continue, 반대는 없음), 무엇을 바꿨든 아니든 Deliberation.Close에 실린다. 클램프와
// 패널 배치는 어댑터의 일이고, core/council은 여전히 표만 센다.
// Verdict/Deliberation/Tally 등 결과 타입과 합의규칙은 core/council(순수). Signal은 D16.
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
      // ★정정: 원안의 "카운슬이 스스로 소집하는 종료 게이트"는 없다. 그 배치가 카운슬이 옳게
      // 정할 수 없는 두 가지를 정해버렸다 — 언제 묻는가(에이전트가 이미 마음을 정한 그 순간)와
      // 그 답이 읽히기는 하는가(헤드리스에선 자문 주입과 turn.finished가 같은 틱이라 안 읽혔다).
      // 지금은 종료 경로(loop_gates.go)가 순서대로 돈다:
      //   1) Stop 훅 — 실패하면 그 출력을 실어 작업으로 되돌림
      //   2) 빈 결과 넛지(텍스트 없는 답변) — 1회
      //   3) 저술했으나 이름으로 지목한 실행이 없음 — 결정적, 모델 호출 없이 턴당 1회
      //   4) 종료 선언 요구 — `council` 툴을 complete:true로 부르라고 알려준다.
      //      무진전 구간당 3회로 경계. 마지막 요청 이후 실제 뮤테이션이 있으면 예산 재시작.
      // 카운슬은 이제 에이전트가 부르는 **툴**이고, 선언을 받아들이면 루프에 신호가 간다.
      store.Append(turn.finished); return
    for call in toolcalls:
      if needsPermission(call): bus.Publish(permission.requested); wait RespondPermission
      store.Append(permission.decided)
      bus.Publish(tool.started)
      res = registry.Get(call.name).Execute(...)
      store.Append(part.appended{tool-result})
    if budget/depth exceeded (D7): graceful stop
```

> ★정정 (as-built): **페이스를 정하는 상한은 없고 240스텝 폭주 백스톱만 있다.** 그리고 **가드는
> 보고만 하고 정지시키지 않는다.** 강제 정지는 측정으로 걷어냈다 — 외부 데드라인에 닿은 런도
> 채점되어 396건 중 76건이 통과한 반면 magi가 스스로 멈춘 28건은 통과가 0이었고 그중 8건은
> 비정상 종료 코드 때문에 채점조차 되지 않았다. 백스톱을 소진한 최상위 턴은 그것을 사유로 적은
> UNVERIFIED turn.finished를 영속으로 남기고 착지한다. 가드가 모으던 신호(반복·정체·자기되돌림·
> 무변경 쓰기·실행 처닝)는 지금도 전부 모아 에이전트에게 넛지로 **말한다**. 언어 지시
> (langDirective) 주입과 워크플로 모드 분기(`runWorkflow`)는 그대로다. 워크플로 페이즈는 자기
> 예산을 선언한다.

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
