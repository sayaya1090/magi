# magi 사용자 매뉴얼

확장형 터미널 AI 코딩 에이전트 클라이언트. 공급자 무관(OpenAI 호환), 멀티에이전트,
Lua 플러그인, MCP, 공유 메모리를 지원한다.

---

## 1. 설치 & 요구사항

- **LLM 백엔드**: OpenAI 호환 엔드포인트(로컬은 [Ollama] 권장).
  ```sh
  ollama pull qwen3-coder:30b   # 기본값(툴 호출 강함)
  ollama pull gpt-oss:20b       # 가벼운 대안
  ```
- **빌드**: `make build` 또는 `CGO_ENABLED=0 go build -o magi ./cmd/magi` (순수 Go 단일 바이너리)
- **프리빌트**: `curl -fsSL .../scripts/install.sh | bash` 또는 `brew install sayaya1090/tap/magi`

## 2. 실행

### 대화형 TUI
```sh
./magi                 # 다크/라이트 자동 감지
./magi --theme light   # 테마 강제(auto|dark|light)
```

### 헤드리스 (스크립트/CI)
```sh
./magi -p "list the go files and summarize"
./magi -p "create hello.txt with: hi" --output json   # JSONL 이벤트
echo "explain main.go" | ./magi -p -                   # stdin
```

### 버전 / 자동 업데이트
```sh
./magi --version
./magi --update        # 최신 릴리스로 자가 업데이트(체크섬 검증)
```

## 3. 설정

플래그/환경변수 (우선순위: 플래그 > 환경변수 > 기본값):

| 플래그 | 환경변수 | 기본값 | 설명 |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `qwen3-coder:30b` | 모델 id |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI 호환 base URL |
| `--permission` | `MAGI_PERMISSION` | TUI=`ask`/헤드리스=`allow` | `ask`\|`auto`\|`allow`\|`deny` |
| `--theme` | `MAGI_THEME` | `auto` | `auto`\|`dark`\|`light` |
| `--plugins` | `MAGI_PLUGINS` | (없음) | 추가 플러그인 디렉터리 |
| `--no-harness` | — | (꺼짐=하네스 켜짐) | 내장 하네스(포맷/진단/Stop 훅) 비활성화 |
| `--output` | — | `text` | `text`\|`json` (헤드리스) |
| — | `MAGI_API_KEY` | (없음) | 원격 백엔드 키 (Ollama 불필요) |

권한 모드: `ask`=매번 확인 · `auto`=**편집은 자동 승인, 명령(bash)/네트워크만 확인** · `allow`=전부 자동 · `deny`=차단. TUI에서 `Shift+Tab`(또는 `/permission`)으로 순환.

설정 파일 `<config>/config.toml` (macOS `~/Library/Application Support/magi`, Linux `~/.config/magi`):
```toml
model = "qwen3-coder:30b"
base_url = "http://localhost:11434/v1"
permission = "ask"
experience_dir = "/path/to/team/experience"   # 공유 두뇌(git repo면 팀 공유)

[routing]                  # 에이전트별 라우팅 (프로파일 이름 또는 모델명)
explore = "fast"           # → [llm.profiles.fast] (다른 엔드포인트/키)
planner = "fast"
coder   = "qwen3-coder:30b"  # 기본 백엔드에서 모델만

[llm.profiles.fast]        # 이름붙인 백엔드 (엔드포인트/키/모델/헤더, ${ENV} 확장)
base_url = "https://fast.gateway/v1"
api_key  = "${FAST_KEY}"
model    = "gpt-oss:20b"
[llm.profiles.fast.headers]
X-CLIENT-API-KEY = "${FAST_CLIENT_KEY}"

[orchestration]            # 선제 플래너(기본 on): 독립 작업이면 읽기전용 병렬 조사
planner = true

[mcp.filesystem]           # MCP 서버 (stdio 또는 url=로 HTTP)
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "."]

[[hooks]]                  # 라이프사이클 훅 (아래 §하네스 참고)
event = "Stop"             # 턴 종료 직전
command = "go test ./... >/dev/null || echo 'tests failing' >&2"

[council]                  # 합의 종료 게이트(D14): 끄면 모델이 멈출 때 그냥 종료(기본). 켜면 council이 done/continue 투표
enabled    = true
rule       = "majority"    # unanimous | majority | quorum:2 | weighted:0.6 | veto:Balthasar
max_rounds = 3             # 라운드 상한(무진전·취소 안전장치와 함께 무한루프 방지)
# [[council.member]]       # 생략 시 MAGI 기본 3인 사용
# name = "Melchior"; lens = "correctness"   # lens: correctness|verification|completeness

[theme.dark]               # 컬러 테마 오버라이드 (모드별). 미지정 role은 NERV/MAGI 기본값 유지
primary = "#FF7A1A"        # role: primary·accent·muted·outline·error·success·
accent  = "#5CD8E6"        #       surface·primaryContainer·outlineVariant·warn
[theme.light]
primary = "#B45309"
```
> 위 `[routing]`/`[llm.profiles.*]`/`model`은 **`/route` 에디터로도 편집**되며 이 파일에 저장된다.
> **합의 council(D14, 시그니처 · 기본 on)**: 모델이 턴을 끝내려 할 때 바로 종료하지 않고, **3인 council(Melchior·Balthasar·Casper)**이 과제(목표)·에이전트 보고·diff를 보고 done/continue를 투표한다. (이때 diff는 **새로 만든 untracked 파일 내용까지 포함**해 갓 생성한 파일도 증거로 보인다.) 위원은 자기 렌즈로 **구체적 결함**을 짚을 때만 continue, 만족하면 done, 판단할 증거가 없으면 **abstain**(불확실하다고 반사적으로 continue하지 않음). 툴을 전혀 안 쓴 **순수 대화 턴(인사·질문)은 council을 소집하지 않는다**. continue면 합쳐진 피드백을 주입하고 루프를 이어간다 = "종료 판정을 단일 모델에서 빼앗는다". `rule`로 합의 방식, `max_rounds`로 라운드 상한(무진전/취소 안전장치 포함). `criteria=true`면 과제에서 **완료기준을 턴당 1회 도출**(LLM 1회 추가)해 council 계약으로 삼아 판정이 더 날카로워진다. **기본 켜짐**이며 매 종료 시점에 LLM 라운드가 추가되므로 끄려면 `[council] enabled=false`. 워크플로 모드에선 비활성(파이프라인 자체 verify 게이트 사용). 각 `[[council.member]]`에 `provider`(=[llm.profiles.*] 백엔드)·`model`을 줘 **위원마다 다른 모델(싸구려+강한 혼합)**로 심의시킬 수 있다(미지정 시 세션 모델/기본 백엔드). TUI에선 심의가 **헤더 칩**(`⚖ council rN: <위원>`)과 **트랜스크립트 라인**(소집 / **위원별 판단근거**(`↳ ✓ Melchior done [correctness] — <근거>`, continue면 `· next: <피드백>`) / 판정·집계)으로 라이브 표시된다 — 집계 숫자뿐 아니라 각 위원이 왜 그렇게 투표했는지 확인할 수 있다. `verify = "<명령>"`(단축) 또는 `[[council.signal]]`(name/command, 예: test·lint·typecheck **여러 개**)를 주면(opt-in) 매 라운드 그 명령들을 돌려 **결과를 council 증거로** 넣는다(분위기가 아니라 증거로 판정 = 거짓성공 차단).
> 컬러 테마는 `[theme.dark]`/`[theme.light]`에서 role별로 외부 정의 가능(기본=NERV/MAGI). `--theme`로 모드(auto/dark/light) 선택.
> 첫 실행 시 주석 달린 기본 `config.toml`이 자동 생성된다(있으면 안 건드림).

### 하네스 (기본 켜짐)

설정 없이도 "이해→계획→구현→검증→요약" 절차가 자연히 적용된다. 두 층으로 구성:

1. **운영 가이드 프롬프트(항상)** — 다단계 작업이면 `todowrite`로 계획 후 하나씩, 편집 후 빌드/테스트로 검증, 깨진 상태로 끝내지 않고, 마지막에 변경 요약. 사용법을 몰라도 그냥 대화만 하면 적용됨.
2. **내장 훅(항상, `--no-harness`로 끔)** — 파일 편집 직후 자동 포맷(gofmt) + 언어 진단(gofmt -e / go vet / py_compile)을 돌려 에러를 모델에 되먹임 → 자가 수정.

**팀 공유**: 프로젝트 루트의 `.magi/config.toml`을 커밋하면 워크플로가 레포와 함께 이동한다. 전역(`<config>/config.toml`)과 병합되며 `[[hooks]]`는 누적된다.

훅 이벤트:
| event | 시점 | 종료코드 ≠ 0 효과 |
|---|---|---|
| `PreToolUse` | 툴 실행 전 | 툴 **차단**(stderr가 모델에 전달; 경로 보호 등) |
| `PostToolUse` | 파일 편집 후 | 출력을 피드백으로 전달 |
| `Stop` | 턴 종료 직전 | 종료 막고 계속 작업 강제(예: 테스트 통과 요구) |

훅 명령은 셸로 실행되며 `MAGI_TOOL`/`MAGI_PATH` 환경변수 + JSON stdin을 받는다. `match`로 툴 이름 필터(`"*"`=전체).

## 4. TUI 사용법

### 슬래시 커맨드 (`/` 입력 시 자동완성 팔레트 — 접두사 필터, ↑/↓ 선택, Tab 완성)
| 커맨드 | 설명 |
|---|---|
| `/help` | 도움말 |
| `/route` (=`/model`=`/agents`) | **모델 & 라우팅 에디터** (한 화면): **(session)** 기본 모델, 에이전트별 모델/백엔드, **backends(프로파일) 추가·편집**. ↑/↓ 선택 · Enter 편집/열기 · 빈 값=기본값 리셋 · Esc 닫기. 에이전트 편집 중 **←/→로 프로파일 선택**(또는 모델명 타이핑). `+ add profile`로 프로파일(엔드포인트/키/모델/헤더) 정의, 폼에서 Enter 필드편집·**Tab 저장**. **모든 편집값은 `config.toml`에 영구 저장**(주석 보존) |
| `/tools` | 사용 가능 툴 |
| `/sessions` | 이 디렉터리의 세션 목록 |
| `/resume [n]` | 세션 재개 (인자 없으면 목록, `/resume 2`로 전환) |
| `/rewind [n]` | 최근 n개 user 턴 롤백(기본 1) |
| `/image <path>` | 이미지 인라인 표시 |
| `/diff` | 워킹트리 git diff |
| `/loop` | **Loop map** — 턴·스텝·툴 활동·council 라운드를 구조로 투영(루프의 *모양* 가시화) |
| `/context` | **컨텍스트 창** 가시화 — 사용량/창크기·메시지 수·compaction 이력(토큰 before→after) |
| `/fork` | 현재 세션을 **분기**해 다른 시도 탐색(원본 보존). 분기로 전환됨 |
| `/replay` | 직전 턴을 **분기에서 다시 실행**(같은 입력 재현). `/loopdiff`로 비교 |
| `/loopdiff` | 현재 분기를 **fork 원본과 구조 비교**(턴·스텝·툴·council·토큰) |
| `/init` | 프로젝트 분석 후 AGENTS.md 작성 |
| `/permission` | 권한 모드 순환(ask→auto→allow→deny) |
| `/compact` | 컨텍스트 요약·축소 |
| `/clear` | 화면 초기화 |
| `/quit` (=`/exit`) | 종료 |

### 단축키
| 키 | 동작 |
|---|---|
| Enter | 전송 (**작업 중이면 메시지를 진행 중 턴에 주입 = steering**) |
| ↑ / ↓ | 입력 히스토리 (이전 프롬프트 불러오기 — 세션 재개 시 이전 턴도 포함) |
| Tab | 입력 접두사를 히스토리에서 자동완성 (슬래시·서브에이전트 포커스와 공유) |
| PgUp/PgDn · Ctrl+U/Ctrl+D · Shift+↑/↓ | 스크롤(페이지/반페이지/한 줄) |
| Tab | 서브에이전트 패널 포커스 순환(패널 있을 때) |
| Ctrl+O | 포커스된 서브에이전트 패널 줌인/줌아웃 |
| Esc | 줌 해제 → 포커스 해제 → 진행 중 작업 중단 |
| 마우스 휠 | 트랜스크립트 스크롤(드래그 중에도) |
| 마우스 드래그 | 텍스트 선택 → 떼면 클립보드 복사 |
| 마우스 클릭 | 서브에이전트 패널 포커스 |
| Ctrl+L | 화면 초기화 |
| Shift+Tab | 권한 모드 전환 |
| Ctrl+C | 종료 |
| 마우스 휠 | 스크롤 · 패널 클릭 → 포커스(다시 클릭 → 줌) |
| 권한 모달: y/a/n | 허용/항상/거부 |

타이핑 키는 입력창으로만 가고 스크롤은 위 전용 키로만 — 그래서 본문 타이핑(띄어쓰기 포함)이 화면을 스크롤하지 않는다. 위로 스크롤해 읽는 중이면 스트리밍이 화면을 끌어내리지 않는다(바닥에 있을 때만 자동 추적).

### 마우스 / 텍스트 복사 (모드 없음)
휠 스크롤·드래그 선택·클릭 포커스가 **모드 전환 없이** 모두 된다 — 선택/복사를 앱이 직접 처리하기 때문이다. **드래그하면 그 범위가 하이라이트되고(문자/셀 단위, 줄 부분 선택 가능), 떼는 순간 클립보드로 복사**된다(OS 클립보드 `pbcopy`/`wl-copy`/`xclip` + OSC52 둘 다 시도). 드래그 중 휠 스크롤도 된다(선택은 콘텐츠 위치에 고정되어 스크롤해도 유지).

### 작업 중 끼어들기 (steering)
작업(턴)이 도는 동안에도 입력은 살아 있다 — 계속 타이핑할 수 있고(한글 IME 포함 인라인 조합), Enter를 누르면 메시지가 **진행 중인 턴에 즉시 주입**된다. 메인 에이전트는 **다음 스텝**에서 그 메시지를 보고 반영한다 — 큐에 쌓였다가 턴이 다 끝나야 나오는 게 아니라, 실행 중인 에이전트를 그 자리에서 조종(steer)하는 것. 메시지는 트랜스크립트에 바로 표시된다.
메인 에이전트는 `task`로 위임한 서브에이전트를 **백그라운드(사이드카)로 돌리고 즉시 반환**하므로 블록되지 않는다 — steer 메시지에 바로 반응한다.
작업 중 슬래시 커맨드: 읽기/UI 전용(`/help`·`/route`(=`/model`=`/agents`)·`/tools`·`/sessions`·`/diff`·`/permission`)은 실행되고, 세션을 바꾸는 것(`/resume`·`/rewind`·`/clear`·`/init`·`/ultra`·`/compact`)은 작업 중 거부된다.

### 멀티에이전트 라이브 뷰 (split-pane)
서브에이전트가 떠 있는 동안 메인 트랜스크립트 아래에 **각 서브에이전트의 라이브 패널**이 타일로 표시된다(각 child 세션을 실시간 구독). 서브에이전트마다 **고유 색상**(M3 톤 팔레트)이 배정돼 패널 보더·헤더 배지·트랜스크립트의 `⚙ task → <이름>` 하이라이트에 일관 적용된다.
- `Tab`(또는 패널 클릭)으로 포커스 이동 → 포커스 패널엔 그 색상의 **포커스 링**.
- `Ctrl+O`(또는 포커스 패널 재클릭)로 **줌인** → 그 서브에이전트의 전체 트랜스크립트를 상세 관찰. 줌 진입 시 맨 아래(최신/결론)로 이동. 상단 **breadcrumb `‹ back  ✦ magi › coder`** 를 **클릭**(또는 `Esc`)하면 복귀. 상세 본문의 어시스턴트 라인은 `magi`가 아니라 **그 에이전트 이름(색상)**으로 표시.
- **색상이 식별자**: 메인이 같은 종류 에이전트에게 여러 작업을 시키면 **작업(세션)마다 독립 패널**이 뜬다. 같은 역할은 **색(hue)은 동일**(트랜스크립트 `task → coder` 하이라이트와 일치), **2번째·3번째는 명도**로 구분된다.
- **패널은 역할 기준**으로 관리된다: 같은 역할(coder 등)이 재시작/재위임돼도 **새 창을 만들지 않고 기존 창을 재사용**한다.
- **수명**: 작업 중엔 타일로 크게, **턴이 끝나면 한 줄짜리 compact로 접힌다**(여전히 `Ctrl+O`로 열어볼 수 있음). 다음 메시지를 보내면 사라지고, 나중에 `/resume`로 복원된다.

### 우측 상태 패널
계획·진행 상황이 있을 때 화면 우측에 **상태 패널**이 뜬다(없으면 숨김). **좌측 경계를 마우스로 드래그**해 폭을 조절할 수 있다(기본 44칸). 섹션:
- **Plan** — 현재 todo 체크리스트 + 진행도(`done/total`), 진행 중 항목 강조(◐). 실시간 갱신.
- **Subagents** — 활성 서브에이전트 목록(색상·상태·토탈 `소요시간·↑↓토큰`). **항목 클릭 → 그 서브에이전트 상세(zoom) 진입**(pane 클릭과 동일).
- **Context** — 컨텍스트 토큰 사용 막대.
서브에이전트 줌(상세) 화면에서도 동일 레이아웃으로 뜨며, 이때 Plan은 **그 서브에이전트의 todo**를 보여준다.

### 붙여넣기 접기
여러 줄(또는 200자 초과) 붙여넣기는 **입력창에서만** `[#N pasted L lines]` 칩으로 접힌다(입력창이 좁으므로). 전송하면 **트랜스크립트(메인)에는 전체 내용이 그대로** 표시되고, 에이전트에도 전체가 전달된다. (↑ 히스토리 recall은 칩 형태로 불러와 입력창을 어지럽히지 않는다.)

### `@` 파일 멘션
메시지에 `@경로/파일`을 넣으면 해당 파일 내용이 에이전트 컨텍스트에 첨부된다.

### 헤더 표시
`model <id> · ctx <%>` + **퍼미션 칩(색상 구분)** + 서브에이전트 실행 중 `⛐ N: explore, coder×2` 배지(이름·색상).
- 퍼미션 칩 색: `ask`=amber(안전) · `auto`=cyan(편집 자동) · `allow`=yellow(주의) · `deny`=red(차단).

### 세션 재개 (`/resume`)
`/resume`(인자 없이) → **인터랙티브 피커**: 각 세션의 시간 + 첫 메시지 요약을 보여주고 `↑/↓`로 선택, `Enter`로 재개, `Esc`로 취소. `/resume N`으로 바로 전환도 가능.
- 목록에는 **사용자 세션만** 나온다(서브에이전트가 만든 child 세션은 숨김).
- 부모 세션을 재개하면 그 세션의 **서브에이전트들이 완료 패널로 복원**되어 `Tab`/`Ctrl+O`로 다시 들여다볼 수 있다(라이브 spawn 이벤트는 휘발이라 디스크의 child 세션에서 복원).

### TUI 동작 체크리스트 (스크린샷)
검증 중인 인터랙션 동작 모음 — 각 항목 아래에 스크린샷을 붙이기 좋게 정리.

1. **작업 중 입력 + 한글 IME** — 턴이 도는 동안에도 입력창에 인라인으로 타이핑/조합됨(커서가 입력 위치에).
2. **Steering(작업 중 끼어들기)** — 작업 중 Enter로 보낸 메시지가 진행 중 턴에 즉시 주입, 다음 스텝에서 메인이 반영. 서브에이전트가 돌아도 메인은 바로 반응.
3. **멀티에이전트 split-pane** — 서브에이전트별 라이브 패널이 타일로, **에이전트마다 고유 색상**. `⚙ task → <이름>`도 같은 색.
4. **포커스 / 줌** — `Tab`(또는 패널 클릭, 마우스 ON일 때)으로 포커스 이동(색상 포커스 링), `Ctrl+O`로 전체화면 상세(브레드크럼·구분선·좌측 바가 그 색), `Esc` 복귀.
5. **서브에이전트 개별 인터럽트** — 실행 중 서브에이전트에 포커스 후 `Esc` → 그 서브에이전트만 중단(나머지·메인은 계속).
6. **슈퍼바이저(사이드카)** — 무응답/스톨 시 자동 재시작(헤더에 `restarting…`), 타임아웃·재시작 소진 시 ERROR 결과 주입. 하나가 죽어도 시스템은 안 멈춤.
7. **붙여넣기 접기** — 멀티라인 붙여넣기 → 입력창엔 `[#N pasted L lines]` 칩, 전송 시 메인엔 전체 내용.
8. **스크롤 위치 유지** — 바닥에 있으면 스트리밍/패널 추가·제거·**터미널 리사이즈** 후에도 바닥 유지, 위로 스크롤해 읽는 중이면 안 끌려감.
9. **입력 히스토리** — `↑/↓`로 이전 프롬프트 불러오기(세션 재개 시 이전 턴도 포함), `Tab`으로 접두사 자동완성.
10. **마우스/복사 (모드 없음)** — 휠 스크롤·드래그 선택+복사·클릭 포커스가 모드 전환 없이 모두 됨(앱이 선택을 직접 처리). 드래그 중 휠 스크롤도 됨.
11. **시작 시 화면 정리** — 실행 시 터미널을 한 번 clear하고 깨끗한 화면에서 시작.

## 5. 도구 (빌트인)

| 툴 | 설명 | 권한 |
|---|---|---|
| `read` | 파일 읽기(줄번호, offset/limit) | — |
| `write` | 파일 생성/덮어쓰기 | ask |
| `edit` | 정확 문자열 치환(고유 매칭) | ask |
| `multiedit` | 여러 hunk 원자적 적용 | ask |
| `grep` | 정규식 내용 검색 | — |
| `glob` | 글롭(** 지원) | — |
| `list` | 디렉터리 목록 | — |
| `bash` | 셸 실행(타임아웃·exit코드) | ask |
| `webfetch` | URL→텍스트 | ask |
| `todowrite` | 계획(체크리스트) 기록 | — |
| `skill` | 명명 스킬 본문 로드 | — |
| `remember` | 공유 메모리에 학습 기여 | — |
| `task` | 서브에이전트 위임(단일/병렬) | — |

- 모든 파일 툴은 **워킹디렉터리 밖 접근 거부**(jail).
- 읽기전용 툴은 한 턴에 **병렬 실행**.
- 파일 수정 후 **진단 피드백**(Go: gofmt/go vet, Python: py_compile) → 에이전트 자가수정.

## 6. 멀티에이전트

`task` 툴로 서브에이전트에 위임한다. 기본 에이전트:
- **explore** — 읽기전용 코드 탐색
- **reviewer** — 코드 리뷰(읽기전용)
- **coder** — 구현(read/write/edit/multiedit/grep/glob/list/bash)

제한(D7): 깊이 3 · 동시 8 · 누적 50. `[routing]`으로 에이전트별 모델 지정.

### 사이드카 실행 모델 (메인 = UI 스레드)
최상위 오케스트레이터가 `task`로 위임하면, 서브에이전트는 **백그라운드 사이드카**로 돌고 `task`는 즉시 반환한다. 메인 에이전트는 블록되지 않고(=UI 스레드처럼 비어 있어) **사용자 입력에 바로 반응**하며, 각 서브에이전트 결과는 **끝나는 대로** 메인 세션에 주입돼 **증분 처리**된다. 주입된 결과에는 **아직 실행 중인 서브에이전트 수**가 함께 붙어, 오케스트레이터가 남은 것을 기다릴지(재위임 말고)·결과 기반으로 **새 후속 작업을 위임**할지·종합할지 스스로 판단한다. 무거운 작업은 위임하고 가벼운 건 인라인으로 처리한다.

각 사이드카에는 **슈퍼바이저**가 붙어 헬스체크한다:
- **하드 타임아웃**(`SubagentTimeout`, 기본 5분/시도) — 무응답 시 중단.
- **스톨 감지**(`SubagentStall`, 기본 4분 무활동) — 진짜 행만 잡도록 넉넉히(큰 프롬프트의 first-token 지연으로 인한 거짓 재시작 방지).
- **자동 재시작**(`SubagentMaxRestarts`, 기본 2회) — 스톨/타임아웃/일시 오류 시 재시도(같은 역할 패널 재사용), 소진 시 ERROR 결과 주입. 하나가 죽어도 나머지·메인은 계속.

(중첩 서브에이전트는 동기 위임으로 동작 — 백그라운드는 최상위에서만.)

### 에스컬레이션 (서브에이전트 → 오케스트레이터 `ask`)
서브에이전트는 실행 중 막히면 **`ask` 툴로 오케스트레이터에게 물어보고 그 자리에서 답을 받는다**(블록 후 재개). 오케스트레이터는 **전체 맥락**(사용자 원 요청·전체 계획·다른 서브에이전트 결과)을 갖고 있어: 의도 명확화·결정, 경로/제약 제공, **사용자에게 재질문 relay**, **다른 서브에이전트와 조율**(peer 질문도 오케스트레이터 경유 — 그래야 맥락이 한곳에 모임)을 해줄 수 있다. `ask` 툴 설명에 "오케스트레이터가 해줄 수 있는 것"이 명시돼 서브에이전트가 무엇을 요청할지 안다. 한 번에 하나의 ask만 처리(직렬화), 2분 타임아웃.

## 7. 메모리 & 컨텍스트

- **AGENTS.md**: 작업 디렉터리(+`.magi/AGENTS.md`, 전역 `<config>/AGENTS.md`)의 내용이
  시스템 프롬프트에 주입되어 **압축돼도 보존**된다. `/init`로 자동 생성.
- **자동 압축**: 실제 토큰 수가 모델 윈도우 80% 초과 시 오래된 턴 요약(최근 보존).
- **공유 두뇌(D13)**: `<config>/experience`(또는 `experience_dir`)의 `memories/`·`skills/`를
  세션 시작 시 회수·주입. `remember` 툴은 `pending/`에 기여(리뷰 후 `memories/`로 이동).
  디렉터리를 git repo로 두고 commit/pull하면 팀 공유.
  단계별 부트스트랩/파일형식/리뷰 절차: [`EXTENDING.md`](EXTENDING.md) §2.

## 8. 스킬

`<config>/skills/*.md` 또는 `<workdir>/.magi/skills/*.md` (첫 줄=설명, 이하=본문).
시스템 프롬프트에 목록이 노출되고, 모델이 `skill` 툴로 본문을 로드해 따른다.

## 9. 플러그인 (Lua)

`<config>/plugins/<name>/` 또는 `<workdir>/.magi/plugins/<name>/`에
`plugin.toml` + `init.lua`. capability: tool 등. 파일 변경 시 **핫리로드**.
샌드박스(위험 stdlib 차단) + 매니페스트 권한(`fs:read`, `net`, `exec`) 집행.
예제: `plugins/examples/wordcount`.

```toml
# plugin.toml
name = "wordcount"
capabilities = ["tool"]
permissions = ["fs:read:."]
```

## 10. MCP

`config.toml`의 `[mcp.<name>]`에 선언하면 stdio JSON-RPC로 spawn되어 툴이
자동 등록된다. 서버 종료 시 해당 툴 제거.

> 단계별 추가/검증/트러블슈팅: [`EXTENDING.md`](EXTENDING.md) §1.

## 11. 모델 권장

- **qwen3-coder:30b** — 멀티스텝/툴 호출 안정(기본).
- **gpt-oss:20b** — 가볍고 추론(thinking) 표시.
- 작은 모델(llama3.1:8b 등)은 툴 활성 시 함수콜 누설 경향 → 비권장.
- 로컬 모델의 tool-call 변종(JSON/XML/네이티브) 모두 파싱한다.

## 12. 미지원 / 향후

OS 샌드박스, 실시간 LSP 서버(gopls), 자동 컨텍스트 랭킹, web search(검색 API 키),
프롬프트 캐싱(호스티드 전용), 웹 UI/원격 공유는 미구현(자세한 표는 [FEATURES.md](FEATURES.md)).

**루프 엔지니어링 트랙(시그니처, 계획 — D14~D16)**: 종료 판정을 단일 모델에서 빼앗는 **합의 council**(Melchior·Balthasar·Casper), 루프 macro 단계(Plan→Execute→Verify→Report→Council→Finalize), 라이브 심의 패널·Loop map, rewind/fork/세션 diff. 설계는 [PLAN.md §4.2](PLAN.md) 참조. 아직 미구현.

[Ollama]: https://ollama.com
