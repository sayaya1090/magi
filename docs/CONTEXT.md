# magi — 프로젝트 컨텍스트 요약

[↑ Docs](README.md)

> **길잡이 (한국어).** 목표·아키텍처·턴 루프의 짧은 요약. 자세한 참조는 [ARCHITECTURE](ARCHITECTURE.md)·[MANUAL](MANUAL.md).

## 1. 프로젝트 목표
- **목표**: 스스로 "다 했다"고 선언하지 않는 터미널 AI 코딩 에이전트 `magi`를 구현하고, 작업 종료를 **합의 카운슬**(세 전문가)으로 검증하여 기록·재현 가능하게 만든다.

## 2. 전체 아키텍처 개요
- **헥사고날 / 포트‑어댑터** 구조: `adapter → app → core` 의 의존 규칙을 강제.
- `app` 은 루프·가드·워크플로를 담당하고, `core` 는 도메인 로직(세션, 메시지, 파트, 이벤트 등)만 보유.
- 기록은 **이벤트소싱(JSONL)** 으로 관리되어 재생·분기·재실행이 가능.

## 3. 주요 패키지·디렉터리 구조
```
cmd/magi/                 # 진입점: 플래그 파싱, DI, 헤드리스 옵션, -daemon/-attach/-agents
cmd/magi-web/             # 콘솔: 이 머신(과 피어)의 모든 데몬을 보는 읽기 위주 웹 화면
internal/
  core/                  # 도메인 모델 (session, event, command, artifact, tool, model, plugin, agent, bus)
  port/                  # 포트 인터페이스 (LLMProvider, Store, Tool, ToolEnv, Platform, PluginHost, ExperienceStore)
  app/                   # 애플리케이션 서비스 (app.go, loop.go, loop_gates.go, workflow.go, policy.go, 등)
  config/                # TOML 설정 로더
  adapter/
    llm/openai/          # OpenAI 호환 어댑터 (Ollama, LiteLLM 등)
    store/jsonl/         # append‑only JSONL 저장소
    tool/builtin/        # read, write, edit, grep, glob, list, bash, … 등 내장 툴
    platform/            # OS 별 exec, 경로, 터미널 캡ability
    experience/git/      # 공유 기억 (git repo 백엔드)
    plugin/lua/          # gopher‑lua 플러그인 호스트
    mcp/                 # MCP 클라이언트 (stdio 기반)
    council/llm/         # 카운슬 위원 어댑터 (패널 한 번 호출 · 훑기 · 닫는 호출)
    tui/                 # Bubble Tea UI
    daemon/              # 유닉스 소켓 위의 엔진 (Listen/Serve · flock 클레임 · Publish · Client)
    fleet/               # 모든 magi가 무엇을 하는지 — 로그에서 유도, 콘솔과 --agents가 공유
plugins/examples/        # Lua 플러그인 예시
```

## 4. 핵심 데이터 모델
- `Session`, `Message`, `Part`(텍스트·추론·툴‑콜·툴‑결과·이미지·오류), `Event`, `Artifact` 등.
- `Event` 은 영속 (로그)와 전이 (버스) 두 종류가 있으며, `Seq`, `SessionID`, `Type`, `Actor`, `TS`, `Data` 로 구성.

## 5. 주요 포트·툴 환경 (`ToolEnv`)
- `SessionID`, `Workdir`, `ScratchDir`, `ScratchTmp`
- `AskPermission`, `EmitArtifact`, `EmitProgress`
- `Council`, `AskUser`, `RouteInterjection`
- `SetTodos`, `NoteForTurn`, `Propose`, `LoadSkill`, `Recall`, `RecallMemory`
- `Platform`, `Sandbox`

## 6. 에이전트 루프 흐름 (`app/loop.go`)
1. `Submit` → `store.Append(prompt.submitted)` → 비동기 `run(sessionID)` 시작
2. `runLoop` 에서 히스토리·컴팩션·컨텍스트·경험을 조합하고 LLM 스트리밍 시작
3. 스트림에서 `text‑delta` → `bus.Publish(part.delta)` (전이)
4. `tool‑call` 수집 → 필요 시 `permission.requested` → `RespondPermission`
5. 툴 실행 → `store.Append(part.appended)` (영속)
6. 툴이 없으면 종료 경로를 순서대로 실행 (`app/loop_gates.go`, `finishTurn`):
   1. Stop 훅
   2. 빈 결과 넛지
   3. `council` 선언 요구 (`council{complete:true}`) — 무진전 구간당 3회까지 상기,
      넘기면 UNVERIFIED·미선언으로 착지
   4. 저술했으나 실행되지 않은 산출물
   5. 미회수 인계 (다른 컴패니언이 아직 답을 안 줌)
   6. 받은 답의 평가 (`rate_handoff`)
   그다음 선택적 증류(기본 꺼짐), 늦게 들어온 인터젝션 수거, 계획 정리(`finalizeTodos`)
7. `store.Append(turn.finished)` 후 루프 종료 — UNVERIFIED 사유가 있으면 함께 실림

   카운슬은 이 경로가 소집하는 게이트가 아니라 **에이전트가 부르는 툴**이고,
   3번은 불렀는지만 확인한다.

## 6.5 터미널 하나를 넘어서
- `magi -daemon`은 UI 없이 엔진만 돌리고 워크스페이스별 소켓에서 대기한다(이름은 실제 경로에서
  나오고 flock으로 유일). `-attach`가 TUI를 붙이고, `-agents`가 머신 전체를 나열한다.
- `cmd/magi-web`은 같은 스토어 위에 **LLM도 툴도 없는** App을 만들어 읽기만 하고, 실행을 바꾸는
  것은 전부 소켓으로 데몬에 보낸다. 상태는 기록이 아니라 로그에서 **유도**한다.
- `-peer name=url`로 다른 콘솔을 합친다. 새 프로토콜 없음 — 콘솔이 콘솔을 읽는다.
- 자세히: `ARCHITECTURE.ko.md` §11, `MANUAL.ko.md` §12,
  `proposals/companions-and-supervision-2026-08-07.md`.

## 7. 주요 확장 포인트
- **Lua 플러그인** (`adapter/plugin/lua`): 능력 번들·핫 리로드
- **MCP** (`adapter/mcp`): stdio 기반 외부 툴 서버
- **훅** (`config.toml [[hooks]]`): PreToolUse/PostToolUse/Stop 셸 커맨드
- **카운슬**: `port.Council` 로 구현, 합의 규칙(majority, unanimous 등) 커스터마이징 가능
- **인증**: magi가 만들지 않는다. 콘솔은 루프백에 바인딩하고, 조직이 이미 쓰는 수단(터널·SSO
  프록시)을 통해 접근한다 — 회사마다 답이 다르고, 그들 문 옆의 두 번째 문은 언제나 더 약하다.
  LLM 백엔드 쪽 커스텀 인증은 Go `http.RoundTripper` 이음매에 붙인다(Lua가 아니라)
- **워크플로 엔진** (`app/workflow.go`): 결정적 파이프라인 phase 게이트

이 요약은 `docs/ARCHITECTURE.ko.md` 와 `docs/DESIGN.ko.md` 에서 직접 추출한 내용에 기반했으며, 프로젝트 목표, 아키텍처, 패키지·디렉터리 구조, 핵심 모델·포트·툴 환경, 에이전트 루프 흐름, 확장 지점을 포괄적으로 정리하고 있습니다.
