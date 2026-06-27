# magi

확장형 터미널 AI 코딩 에이전트 클라이언트. 여러 **에이전트 · 컨텍스트 프로바이더 · MCP · 스킬**을
플러그인으로 묶어 한데 엮어(magi) 굴린다.

📖 **사용법 전체는 [docs/MANUAL.md](docs/MANUAL.md)**.

## 빠른 시작

### 요구사항
- **Go 1.26+** (빌드 시)
- **LLM 백엔드** — OpenAI 호환 엔드포인트면 무엇이든. 로컬은 [Ollama](https://ollama.com) 권장.
  ```sh
  ollama pull qwen3-coder:30b     # 기본값 — 멀티스텝 코딩/툴콜 안정성 최고
  ollama pull gpt-oss:20b         # 더 가벼운 대안(균형 good)
  ```
  > 작은 모델(예: llama3.1:8b)은 툴이 활성화되면 인사에도 함수콜 JSON을 읊는 경향이 있어 기본값에서 제외.

### 설치
```sh
# 프리빌트 바이너리 (릴리스 후)
curl -fsSL https://raw.githubusercontent.com/sayaya1090/magi/main/scripts/install.sh | bash
# 또는 homebrew
brew install sayaya1090/tap/magi
```

### 빌드 (소스에서)
```sh
make build        # CGO_ENABLED=0, 버전 주입 → ./magi
# 또는
CGO_ENABLED=0 go build -o magi ./cmd/magi
```
순수 Go(단일 바이너리, CGo 없음) — 그대로 복사해서 어디서든 실행.

### 버전 / 자동 업데이트
```sh
./magi --version   # 버전 출력
./magi --update    # 최신 릴리스로 자가 업데이트(체크섬 검증)
```

### 대화형 TUI
```sh
./magi
```
- 입력 후 **Enter** 전송 · **Esc** 진행 중 작업 중단 · **Ctrl+C** 종료 · `/quit` 종료
- 위험 툴(write/edit/bash)은 실행 전 **권한 확인**(`y` 허용 / `a` 항상 / `n` 거부)
- 다크/라이트 터미널 자동 대응, 마크다운·코드 신택스 하이라이트

### 헤드리스 (스크립트/CI)
```sh
./magi -p "list the Go files and summarize the architecture"
./magi -p "create hello.txt containing: hi" --output json   # JSONL 이벤트 스트림
echo "explain main.go" | ./magi -p -                         # stdin
```

### 설정 (플래그 / 환경변수)
| 플래그 | 환경변수 | 기본값 | 설명 |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `qwen3-coder:30b` | 모델 id |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI 호환 base URL |
| `--permission` | `MAGI_PERMISSION` | TUI=`ask` / 헤드리스=`allow` | `allow`\|`deny`\|`ask` |
| `--output` | — | `text` | `text`\|`json` (헤드리스) |
| — | `MAGI_API_KEY` | (없음) | 원격 백엔드용 키 (Ollama는 불필요) |

> 첫 실행 시 주석 달린 **기본 `config.toml`이 `<config>`에 자동 생성**된다(있으면 안 건드림) — 사람이 보고 편집할 수
> 있는 설정 파일. 우선순위는 **플래그 > 환경변수 > config > 기본값**. `model`/`base_url`/`permission`도 config로 동작.

```sh
# 예: 로컬 qwen 사용
MAGI_MODEL=qwen2.5-coder:32b ./magi

# 예: vLLM / LiteLLM 등 다른 OpenAI 호환 백엔드
./magi --base-url http://localhost:4000/v1 --model my-model
```

### 도구 (빌트인)
`read`(줄번호) · `write` · `edit` · `multiedit`(원자적 멀티헝크) · `grep` · `glob` · `list` ·
`bash`(타임아웃·exit코드) · `webfetch` · `todowrite`(계획) · `remember`(공유 메모리) · `task`(서브에이전트).
편집 후 **진단 피드백**(gofmt/go vet/py_compile)으로 에이전트가 자가수정. 읽기전용 툴은 턴 내 병렬 실행.

### 슬래시 커맨드
`/help` `/route`(=`/model`=`/agents`) `/tools` `/sessions` `/resume` `/image` `/diff` `/init` `/permission` `/compact` `/clear` `/quit`(=`/exit`)
(`/` 입력 시 자동완성 팔레트 — 별칭도 접두사로 검색됨: `/m`→`/model`)
- **`/route`**(별칭 `/model`·`/agents`): **모델 & 라우팅 에디터** — 세션 기본 모델, 에이전트별 모델/백엔드
  (편집 중 ←/→로 프로파일 선택), 그리고 **LLM 프로파일 추가·편집**(엔드포인트/키/모델/헤더, `+ add profile`)을
  GUI식으로. 편집값은 `config.toml`에 **영구 저장**(주석 보존).

### 컨텍스트 & 메모리
- **프로젝트 메모리**: 작업 디렉터리의 `AGENTS.md`(+ `.magi/AGENTS.md`, 전역 `<config>/AGENTS.md`)가
  시스템 프롬프트에 주입되어 **압축돼도 사라지지 않는** 영속 컨텍스트가 된다(a reference agent의 CLAUDE.md 등가).
- **컨텍스트 인식 자동 압축**: 실제 토큰 수가 모델 윈도우의 80% 초과 시 오래된 턴을 요약, 최근 대화는 보존.
- **컨텍스트 미터**: TUI 헤더에 사용량(`ctx 42%`) 표시.
- **공유 두뇌(D13)**: `<config>/experience`(git repo면 팀 공유)의 메모리/스킬을 세션 시작 시 회수·주입,
  `remember` 툴로 학습을 리뷰 큐(pending)에 기여. `config.toml`의 `experience_dir`로 경로 지정.

### 플러그인 & MCP
- **Lua 플러그인**: `~/Library/Application Support/magi/plugins/` (또는 `.magi/plugins/`, `--plugins`)에
  `plugin.toml` + `init.lua`를 두면 자동 로드 + 핫리로드. 예제는 [plugins/examples/wordcount](plugins/examples/wordcount).
- **MCP 서버**: 설정 파일 `<config>/config.toml`에 선언하면 기동 시 spawn되어 툴이 등록됨.
  ```toml
  [mcp.filesystem]
  command = "npx"
  args = ["-y", "@modelcontextprotocol/server-filesystem", "."]

  [mcp.git]
  command = "uvx"
  args = ["mcp-server-git"]
  ```
  (`<config>`: macOS `~/Library/Application Support/magi`, Linux `~/.config/magi`)
- **모델/백엔드 라우팅**: 에이전트별로 **모델만**(같은 백엔드) 또는 **프로파일**(다른 엔드포인트/키)을 지정.
  `[routing]` 값이 프로파일 이름이면 그 백엔드로, 모델명이면 기본 백엔드에서 모델만 바뀐다. (`/route` 에디터로도 편집)
  ```toml
  [routing]
  explore = "fast"             # → [llm.profiles.fast] (다른 엔드포인트/키)
  planner = "fast"
  coder   = "qwen3-coder:30b"  # 기본 백엔드에서 모델만

  # 이름붙인 백엔드 (엔드포인트/키/모델/헤더; ${ENV} 확장)
  [llm.profiles.fast]
  base_url = "https://fast.gateway/v1"
  api_key  = "${FAST_KEY}"
  model    = "gpt-oss:20b"
  [llm.profiles.fast.headers]
  X-CLIENT-API-KEY = "${FAST_CLIENT_KEY}"
  ```
- **LLM 커스텀 헤더**: 사내 게이트웨이(LiteLLM 등)용 — `[llm.headers]`(정적, `${ENV}`) 또는 플러그인
  `magi.set_llm_headers`(요청마다 동적, SSO 토큰 등). [docs/EXTENDING.md](docs/EXTENDING.md) §3.3 참고.
- **선제 플래너**(`[orchestration] planner`, 기본 on): 턴 시작 전 작업이 독립 영역으로 쪼개지는지 판정해,
  그렇다면 읽기전용 explorer를 병렬로 띄워 조사 결과를 주입한 뒤 본 작업 진행 (`planner = false`로 끔).
  판정은 화면에 보인다 — 헤더 칩 `◈ plan: solo|parallel` + 대화에 `◈ planner: <mode> — <이유>` 줄.
  싼/좋은 모델로 태우려면 `[routing] planner = "fast"`.

### 테스트
```sh
go test ./...                                   # 단위 (모델 불필요)
MAGI_E2E_OLLAMA_MODEL=qwen2.5-coder:32b \
  go test -run E2E ./...                        # 실모델 E2E (Ollama)
./test/e2e/up.sh                                # LiteLLM(podman) 띄우고 E2E
```

## 핵심 설계 결정

| 영역 | 선택 | 이유 |
|---|---|---|
| 코어 언어 | **Go** | 단일 바이너리 크로스컴파일, 자동 업데이트 용이, goroutine 동시성 |
| TUI | **Bubble Tea (Charm)** | 미려한 TUI 표준, `glamour`로 마크다운/코드 렌더 턴키 |
| 플러그인 런타임 | **Lua (gopher-lua)** | 순수 Go 임베드(CGo 없음 → 크로스컴파일 유지), 핫리로드·샌드박스 자연스러움 |
| 아키텍처 | **포트 & 어댑터(헥사고날)** | 코어는 UI/LLM/플러그인을 모름 → UI 추가·교체가 쌈 |
| LLM | **공급자 무관** | Claude/GPT/Gemini/로컬을 어댑터로 |
| 확장 | 플러그인 = capability 번들 | `agent` / `context-provider` / `mcp-server` / `skill` / `ui-panel` |

자세한 설계와 로드맵은 [docs/PLAN.md](docs/PLAN.md).

## 디렉터리

```
cmd/magi            엔트리포인트(와이어링)
internal/core         도메인 — 어댑터에 의존하지 않음
  ├─ agent            에이전트 루프
  ├─ session          세션/대화 상태
  ├─ tool             툴 레지스트리
  └─ plugin           capability 레지스트리 / 플러그인 모델
internal/port         포트(인터페이스) — LLM, UI, PluginHost ...
internal/adapter      어댑터(구현)
  ├─ llm              anthropic / openai / gemini ...
  ├─ tui              bubbletea
  ├─ plugin/lua       gopher-lua 호스트
  └─ mcp              MCP 클라이언트
plugins/examples      예제 Lua 플러그인
```

## 라이선스

**Apache-2.0** ([LICENSE](LICENSE)). 서드파티 코드 재사용 시 고지 의무 준수 — `NOTICE` / `THIRD_PARTY_LICENSES` 유지.

## 상태

- ✅ **M1 — headless 코어**: 에이전트 루프, OpenAI 호환 LLM 어댑터(+프롬프트 폴백),
  빌트인 툴(read/write/edit/grep/glob/list), 이벤트소싱 영속, 권한, `magi -p`. 실모델 E2E 통과.
- ✅ **M2 — TUI**: Bubble Tea 대화 UI, 스트리밍, 권한 다이얼로그, 다크/라이트 대응.
- ✅ **M3 — Lua 플러그인**: 샌드박스, 권한, 핫리로드, 자동 로드.
- ✅ **M4 — MCP**: stdio JSON-RPC 클라이언트, config.toml로 서버 선언 → 툴 자동 등록.
- ✅ **M5 — 멀티에이전트**: 서브에이전트(explore/reviewer/coder), task 툴로 위임·병렬,
  bounded recursion(depth 3 / 동시 8 / 누적 50), artifact 보고.
- ✅ **M6 — 모델 레지스트리 + 라우팅**: 모델 메타(컨텍스트/툴/비전/가격),
  컨텍스트 인식 자동 압축, 비용 집계, 에이전트별 모델 라우팅.
- ✅ **M7 — 배포/자동업데이트**: goreleaser 멀티OS(darwin/linux/windows × amd64/arm64,
  CGO_ENABLED=0), `--version`, `--update`(체크섬 검증·크로스플랫폼 교체), install 스크립트, CI.

자세한 진행은 [docs/PLAN.md](docs/PLAN.md) · 기능 명세는 [docs/SPEC.md](docs/SPEC.md).
