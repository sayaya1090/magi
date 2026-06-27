# magi — 상세 설계 (M1 착수용)

> ⚠️ **이 문서는 M1 착수 *시점*의 설계 의도다.** 구현이 그 뒤로 크게 확장·일부 변경됐다
> (멀티에이전트 오케스트레이션·가드레일·워크플로 엔진·OS 샌드박스·프롬프트 캐싱 등).
> **현재 *as-built* 기준은 [`ARCHITECTURE.md`](ARCHITECTURE.md)** 를 보라 — 충돌 시 그 문서가 우선.
> 이 문서는 설계 근거(결정 D1~D13의 구체화)로 보존한다. 아래 §들은 정확성 보정만 반영했다.

> PLAN의 결정(D1~D13)을 코드 직전 수준으로 구체화. 이벤트/커맨드 스키마 · 포트 시그니처 · 패키지 구조.
> 핵심 패턴: **CQRS-lite** — 안으로는 *Command*, 밖으로는 *Event*. 이게 인프로세스↔원격을 같게 만든다(D5).

---

## 1. 패키지 구조

> 아래는 *as-built* 트리(2026-06 기준)로 갱신했다. 원안 대비 변경점:
> `core/capability` 제거(미사용) · `core/{model,plugin}` 추가 · `port`는 단일 `port.go`로 통합 ·
> `app`은 `service.go` 대신 `app.go`이며 오케스트레이션/가드레일/워크플로 파일이 추가됐다 ·
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
    orchestrate.go          #   멀티에이전트: task/spawn/dispatch/supervisor (M5)
    workflow.go             #   결정적 워크플로 엔진(phase 게이트)
    policy.go               #   가드레일 정책 엔진(allow/deny/egress/secret-deny)
    context.go·compact.go   #   컨텍스트 조립 + compaction
    memory.go·skills.go·hooks.go·diagnose.go  #   AGENTS.md 메모리 / 스킬 / 훅 / 진단
  adapter/                  # 어댑터(포트 구현)
    llm/openai/             #   OpenAI 호환(Ollama/vLLM/LiteLLM): 캐싱·폴백·에러매핑
    store/jsonl/            #   append-only JSONL
    tool/builtin/           #   read/write/edit/multiedit/grep/glob/list/findcontext/
                            #   bash/webfetch/task/ask/report/todo/remember/skill + sandbox_*
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
    Type      EventType       `json:"type"`
    Actor     Actor           `json:"actor"`     // 누가 유발(D5)
    TS        time.Time       `json:"ts"`
    Stage     string          `json:"stage,omitempty"` // D15(출하): plan|execute|council|finalize — 단계 전환마다 스탬프, Loop map(/loop)·rewind·diff가 단계 단위로 그룹/타깃
    Data      json.RawMessage `json:"data"`      // 타입별 페이로드
}
type Actor struct {
    Kind string `json:"kind"` // user|agent|system
    ID   string `json:"id"`   // user id / agent name
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
| `council.convened` 🚧 | `{round, members[], rule}` (D14 — 종료 게이트 소집) |
| `council.verdict` 🚧 | `{round, member, decision(done\|continue\|abstain), confidence, rationale, feedback}` |
| `council.decided` 🚧 | `{round, decision, tally, injectedFeedback}` (continue면 feedback이 prompt.submitted로 주입됨) |
| `compaction` | `{summary, replacesUpToSeq, tokens:{before,after}}` |
| `turn.finished` | `{usage:{in,out,cost}}` |
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
| `council.deliberating` 🚧 | `{round, member, state}` (라이브 심의 패널 — 전이, D14) |

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
// ToolEnv — 원안은 5개 필드였으나 멀티에이전트/가드레일 도입으로 확장됐다.
// 아래는 as-built 요약(전체·주석은 internal/port/port.go 참조).
type ToolEnv struct {
    SessionID SessionID
    Workdir   string
    AskPermission func(callID, name string, args json.RawMessage) (bool, error) // 권한 게이트
    EmitArtifact  func(Artifact)                                                // D11 산출물
    // 멀티에이전트(M5): task 툴에만 주입, 미지원 시 nil
    Spawn    func(ctx context.Context, req SpawnRequest) SpawnResult // 블로킹 서브에이전트
    Dispatch func(req SpawnRequest) string                          // 백그라운드 사이드카(""=성공/note=거부)
    Ask      func(question string) (string, error)                  // 서브→오케스트레이터 에스컬레이션
    Report   func(summary, status, details string) error            // 서브에이전트 최종 보고(done|blocked|failed)
    // 계획/메모리/스킬
    SetTodos  func(todos []session.Todo)            // TodoWrite
    Propose   func(c Contribution) error            // 공유 경험(D13) 기여
    LoadSkill func(name string) (string, bool)      // 명명된 스킬 로드
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

// Council — 루프 종료 게이트(D14, 🚧 planned). 위원 팬아웃은 어댑터, 합의규칙은 순수 core.
// 기본 어댑터는 위원별 LLMProvider.StreamChat 병렬 호출 → 응답을 Verdict로 파싱(JSON폴백 재사용).
type Council interface {
    Deliberate(ctx context.Context, r DeliberationRequest) (Deliberation, error)
}
type DeliberationRequest struct {
    Round    int
    Phase    string         // ""|"terminate"=종료 게이트 / "plan"=계획 감사(D17, report/diff/signal 없음)
    Task     string         // 원 과제(목표)
    Plan     string         // 계약: acceptance criteria, 또는 Phase=plan일 때 제안된 절차
    Report   string         // 주장: 에이전트 자기보고 (있으면)
    Signals  []Signal       // 증거: test/lint/type 결과
    Diff     string         // 선택: 작업 diff
    Members  []CouncilMember
    Rule     string         // unanimous|majority|quorum:k|weighted:θ|veto
}
type CouncilMember struct { // 테마명 라벨 + 렌즈 속성
    Name   string  // "Melchior" | "Balthasar" | "Casper"
    Lens   string  // "correctness" | "verification" | "completeness"
    Model  string  // 빈값=세션 모델
    Weight float64
}
// Verdict/Deliberation/Tally 등 결과 타입과 합의규칙은 core/council(순수). Signal은 D16.
```

---

> **확장 안내**: 실제 `app.Application`은 위 골격 외에 멀티에이전트(task/spawn/dispatch/ask/report),
> 가드레일 정책, 결정적 워크플로, AGENTS.md 메모리, 훅을 포함한다. 동작 기준은 [`ARCHITECTURE.md`](ARCHITECTURE.md).

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
      // 🚧 D14/D15 council 게이트 (depth==0, 카운슬 활성, max_rounds 미초과 시):
      //   verify → Signal 수집;  report 단계 유도(주장);
      //   delib = council.Deliberate({task, plan, report, signals, diff, members, rule})
      //   store.Append(council.convened / council.verdict×N / council.decided)
      //   if delib.Decision == continue && 진전 있음:
      //       store.Append(prompt.submitted{feedback}, actor=council); continue  // Stop훅 주입과 동일 경로
      //   // else: done / 라운드 소진 / 무진전 → 아래로
      store.Append(turn.finished); return
    for call in toolcalls:
      if needsPermission(call): bus.Publish(permission.requested); wait RespondPermission
      store.Append(permission.decided)
      bus.Publish(tool.started)
      res = registry.Get(call.name).Execute(...)
      store.Append(part.appended{tool-result})
    if budget/depth exceeded (D7): graceful stop
```

> as-built 추가: maxSteps 인자화 + **루프 가드**(반복/blocked 예산 초과 시 loop_guard 정지),
> 서브에이전트 report 유도 넛지, 언어 지시(langDirective) 주입, 워크플로 모드 시 `runWorkflow`로 분기.

---

## 7. M1 구현 순서 (이 설계 기준)
1. `core/session`,`core/event`,`core/command`,`core/artifact` 타입.
2. `core/bus` 인메모리 pub/sub.
3. `port` 인터페이스 전부 선언(빈 채로).
4. `adapter/store/jsonl` — Append/Read/Subscribe 재생.
5. `adapter/llm/openai` — Ollama `/v1` 스트리밍 + tool_calls + **프롬프트 폴백**.
6. `adapter/tool/builtin` — read/write/edit/grep/glob/list (Go).
7. `adapter/platform` — exec/경로/터미널 능력(darwin/linux/windows).
8. `app/service.go`+`loop.go` — 위 루프.
9. `cmd/magi` — `-p` 헤드리스(stdin 프롬프트 → stdout 이벤트).
10. **Ollama 실모델 tool-calling 라이브 테스트**(네이티브+폴백) + core 단위테스트.
