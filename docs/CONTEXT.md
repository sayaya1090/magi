# 프로젝트 컨텍스트 요약

## 1. 프로젝트 목표
- **목표**: 스스로 "다 했다"고 선언하지 않는 터미널 AI 코딩 에이전트 `magi`를 구현하고, 작업 종료를 **합의 카운슬**(세 전문가)으로 검증하여 기록·재현 가능하게 만든다.

## 2. 전체 아키텍처 개요
- **헥사고날 / 포트‑어댑터** 구조: `adapter → app → core` 의 의존 규칙을 강제.
- `app` 은 루프·가드·워크플로를 담당하고, `core` 는 도메인 로직(세션, 메시지, 파트, 이벤트 등)만 보유.
- 기록은 **이벤트소싱(JSONL)** 으로 관리되어 재생·분기·재실행이 가능.

## 3. 주요 패키지·디렉터리 구조
```
cmd/magi/                 # 진입점: 플래그 파싱, DI, 헤드리스 옵션
internal/
  core/                  # 도메인 모델 (session, event, command, artifact, tool, model, plugin, agent, bus)
  port/                  # 포트 인터페이스 (LLMProvider, Store, Tool, ToolEnv, Platform, PluginHost, ExperienceStore)
  app/                   # 애플리케이션 서비스 (app.go, loop.go, loop_gates.go, workflow.go, policy.go, 등)
adapter/
  llm/openai/            # OpenAI 호환 어댑터 (Ollama, LiteLLM 등)
  store/jsonl/           # append‑only JSONL 저장소
  tool/builtin/          # read, write, edit, grep, glob, list, bash, … 등 내장 툴
  platform/              # OS 별 exec, 경로, 터미널 캡ability
  experience/git/        # 공유 기억 (git repo 백엔드)
  plugin/lua/            # gopher‑lua 플러그인 호스트
  mcp/                   # MCP 클라이언트 (stdio 기반)
  tui/                   # Bubble Tea UI
config/                  # TOML 설정 로더
plugins/examples/        # Lua 플러그인 예시
```

## 4. 핵심 데이터 모델
- `Session`, `Message`, `Part`(텍스트·추론·툴‑콜·툴‑결과·이미지·오류), `Event`, `Artifact` 등.
- `Event` 은 영속 (로그)와 전이 (버스) 두 종류가 있으며, `Seq`, `SessionID`, `Type`, `Actor`, `TS`, `Stage`, `Data` 로 구성.

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
6. 툴이 없으면 종료 경로 순서대로 실행:
   - Stop 훅
   - 빈 결과 넛지
   - 실행되지 않은 산출물
   - `council` 선언 (`council{complete:true}`)
7. `store.Append(turn.finished)` 후 루프 종료

## 7. 주요 확장 포인트
- **Lua 플러그인** (`adapter/plugin/lua`): 능력 번들·핫 리로드
- **MCP** (`adapter/mcp`): stdio 기반 외부 툴 서버
- **훅** (`config.toml [[hooks]]`): PreToolUse/PostToolUse/Stop 셸 커맨드
- **카운슬**: `port.Council` 로 구현, 합의 규칙(majority, unanimous 등) 커스터마이징 가능
- **인증**(예정): OIDC/mTLS 등 커스텀 인증 플러그인
- **워크플로 엔진** (`app/workflow.go`): 결정적 파이프라인 phase 게이트

이 요약은 `docs/ARCHITECTURE.ko.md` 와 `docs/DESIGN.ko.md` 에서 직접 추출한 내용에 기반했으며, 프로젝트 목표, 아키텍처, 패키지·디렉터리 구조, 핵심 모델·포트·툴 환경, 에이전트 루프 흐름, 확장 지점을 포괄적으로 정리하고 있습니다.
