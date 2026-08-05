<div align="center">

# magi

**스스로 "다 했다"고 선언하지 않는 터미널 AI 코딩 에이전트.**

대부분의 에이전트는 작업을 끝낸 모델이 스스로 종료를 판정한다 — 그래서 일찍 멈추거나, 끝없이 맴돈다.
**magi는 종료를 투표에 부친다.** 서로 다른 렌즈를 가진 세 전문가가 매 턴을 심의하고, 합의했을 때에만
루프를 끝낸다.

[![CI](https://github.com/sayaya1090/magi/actions/workflows/ci.yml/badge.svg)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
[![coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/sayaya1090/magi/badges/coverage.json)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)

[English](README.md) · [한국어](README.ko.md) · [매뉴얼](docs/MANUAL.ko.md)

![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-Apache--2.0-blue)
[![CI](https://github.com/sayaya1090/magi/actions/workflows/ci.yml/badge.svg)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
![Single binary](https://img.shields.io/badge/build-CGO__free%20single%20binary-success)

</div>

---

## 왜 magi인가?

에이전트 루프에는 어려운 질문이 하나 있다. **턴은 언제 진짜로 끝나는가?**

그걸 암묵에 맡기면 — 모델이 툴 호출을 멈추면 턴이 끝나는 방식 — 생각하다 만 턴과 정말로 끝난 턴이
구별되지 않는다. magi는 종료를 **행위**로 만든다. 에이전트가 끝났다고 선언하고, 카운슬이 *실제로
무슨 일이 있었는지*를 읽은 다음에야 턴이 닫힌다.

```text
you ▸ deploy 명령에 --dry-run 플래그 추가해줘

  … 에이전트가 cmd/deploy.go를 읽고 편집, go build 실행 …

  ⚙ council {complete: true}          에이전트가 끝났다고 선언

  ⚖ 카운슬이 기록을 읽는다
     ── WHAT MAGI OBSERVED
        changed: cmd/deploy.go
        ran clean: go build ./...
     ── THE WORKSPACE RIGHT NOW (기록이 아니라 방금 읽은 것)
        cmd/deploy.go — 4,102 bytes, 12초 전 수정

     ● Balthasar [verification]  새 플래그를 돌려본 게 없다 — `go test ./cmd`가 실행된 적 없음
     → 아직 수락 아님; 에이전트가 계속 작업

  … 에이전트가 테스트 추가, go test 실행 …

  ⚙ council {complete: true}   →   수락   ✓ 턴 종료
```

멈출지 말지의 결정을 단일 모델에서 빼앗아 **합의 카운슬**에게 넘긴다. 이 한 가지 변화가 프로젝트의
존재 이유 전부이고, 나머지는 그 루프를 **관찰 가능·조종 가능·재현 가능**하게 만들기 위해 존재한다.

---

## 카운슬 (The Council)

루프가 자연스럽게 끝나려는 그 지점에서, 멤버들이 **done · reject · abstain** 중 하나를 투표한다.
순수하게 단위 테스트된 합의 규칙이 표를 모아 하나의 결정으로 만든다. `reject`는 단순히 에이전트를
멈추는 게 아니라, 멤버들의 종합 피드백을 다음 지시로 루프에 되먹인다.

기본 세 멤버 — **MAGI** — 는 각자 다른 렌즈로 판단한다:

| 멤버 | 렌즈 | 묻는 것 |
|---|---|---|
| **Melchior** | `correctness` | 작업이 올바른가? 엣지 케이스·회귀는? |
| **Balthasar** | `verification` | *증거*가 있는가 — 빌드/테스트가 통과하나? 주장은 믿지 않는다. |
| **Casper** | `completeness` | 요청한 걸 다 했나? 미완으로 남은 건 없나? |

**단일 심판이 아닌 합의.** 집계 규칙은 설정 가능하다:

| 규칙 | 끝나는 조건 |
|---|---|
| `majority` *(기본)* | 투표 멤버의 과반이 done (동수면 계속) |
| `unanimous` | 전원이 done |
| `quorum:k` | 최소 *k*명이 done |
| `weighted:θ` | done 가중치 비중이 임계 θ 충족 |
| `veto:이름` | 지정 멤버가 어떤 종료든 거부 가능 |

**루프를 가두지도, 무조건 통과시키지도 않도록 설계:**

- **동수·정족수 미달·투표자 없음·오류**는 모두 *계속*으로 귀결 — 카운슬은 모호함이 아니라 명시적
  합의에서만 끝낸다.
- **무진전 감지**: 피드백이 비거나 반복되면 게이트를 멈춰 churn을 막는다.
- 라운드는 **상한**(`max_rounds`, 기본 3); 상한 도달 시 "미해결" 표기와 함께 종료.
- 오류·파싱 불가 응답을 낸 멤버는 **기권**(분모에서 제외)하여 게이트를 막지 않는다.

**감(感)이 아니라 증거.** 멤버는 에이전트의 *보고*를 *작업*·*계획*과 대조하고, 옵트인한 **결정적
시그널**(실제 `build`/`test`/`lint` 결과)을 함께 따질 수 있다. 아무것도 바꾸지 않은 읽기전용/조사 턴은
그렇게 인식되어(`NoChanges`), 존재하지도 않을 diff를 요구하는 대신 *답변 자체*의 타당성을 본다.

```toml
[council]
# enabled  = true         # 기본 on; false면 평범한 단일 모델 루프
rule       = "majority"   # unanimous | majority | quorum:k | weighted:θ | veto:이름
max_rounds = 3
verify     = "go test ./..."   # 매 라운드 카운슬이 따지는 결정적 시그널
# criteria = true              # 명시적 완료 기준을 계약으로 도출

# 벤치를 커스터마이즈 — 또는 MAGI 기본 유지
[[council.member]]
name = "Melchior"
lens = "correctness"
# model / provider로 멤버를 더 싸거나 강한 백엔드로 라우팅 가능
```

> 합의 로직은 `internal/core/council`에 **순수 도메인 코드**로 있다 — I/O도, LLM도 없이. 그 분리가
> *"단일 모델이 아니라 카운슬이 결정한다"*를 기대섞인 프롬프트가 아니라 테스트된 불변식으로 만든다.

---

## 턴은 어떻게 끝나는가

턴은 조용해져서 끝나지 않는다. 에이전트가 작업이 끝났다고 판단하면 그렇게 말하고 —
`council{complete: true}` — 세 멤버가 **기록을 읽은 뒤에야** 턴이 닫힌다. 무슨 명령이 실제로 돌았고
어떻게 끝났는지, 워크스페이스가 **지금** 무엇을 담고 있는지, 에이전트 자신이 뭐라고 했는지. 수락하면
턴이 끝나고, 아니면 무엇이 아직 안 됐는지를 짚어 돌려보내고 에이전트는 계속 일한다.

핵심은 그 기록이다. magi는 모든 툴 호출을 자기가 허가하므로 어떤 명령이 돌았는지, 진짜 exit이
무엇이었는지(보고된 exit이 감추는 **파이프라인 중간 단계의 실패**까지), 어떤 경로에 썼는지를 안다.
선언이 들어오면 워크스페이스를 **그 자리에서 새로 읽는다** — 작업 시작 이후 수정된 파일, 아직 살아있는
백그라운드 명령, 기록은 썼다는데 디스크에 없는 경로. 미리 작성된 체크는 작업에 대해 틀릴 수 있지만,
허가한 것의 기록은 허가한 것에 대해 틀릴 수 없다. 불완전한 부분은 불완전하다고 말한다.

같은 기록이 매 스텝 다시 렌더돼 실린다 — 에이전트가 기억에만 의존해 일하지 않도록.

묻는 것과 선언하는 것은 다르다. `council{question}`은 확신이 안 서는 것에 대한 읽기를 받아오고,
아무것도 끝내지 않는다.

---

## 에이전트는 하나다

magi는 한때 서브에이전트를 띄우고, 작업을 스텝으로 계획하고, 스텝마다 실행 가능한 체크를 저술하고,
카운슬이 그 체크가 충족됐다고 투표할 때까지 턴을 붙들었다. 전부 걷어냈고, 기본 상태에서는 여전히
에이전트가 하나다 — **magi는 자기 에이전트를 싣지 않는다.**

이유는 로그에 있다. 그 단계들은 **작업이 존재하기 전에** 무언가를 결정했고, 가장 비쌌던 결함은 정확히
한 종류였다 — magi가 실제로 벌어진 일의 기록보다 미리 내린 자기 판단을 믿은 것. 서버가 죽었을 때만
통과하는 프로브, 생성기는 언더스코어를 쓰는데 하이픈을 요구하는 grep, 채점 식별자가 사라질 때까지
패러프레이즈된 브리프. 남은 것은 루프, 툴, 기록, 그리고 에이전트가 원할 때 부르는 카운슬이다.

**서브에이전트가 필요하면 플러그인에서 온다.** magi는 심만 제공하고 정책은 두지 않는다: 플러그인이
선언하고, 사용자는 `/subagents`에서 켜고 모델을 고르고, 그런 플러그인이 없으면 닿을 방법이 없다.
들어가는 길에 요약되는 것은 없다 — 자식은 플러그인의 프롬프트와 툴 자신의 인자를 원문 그대로 받는다.
패러프레이즈된 브리프야말로 위 기록이 단죄한 그것이기 때문이다. 플러그인은 자기 자식이 실제로 무엇을
했는지 읽을 수 있고 파일 변경을 되돌릴 수 있으며, 둘 다 플러그인의 판단이지 magi의 것이 아니다.
예시 하나가 꺼진 채로 실린다 — **제레(Seele)**, 쓰기 툴이 아예 없는 플래너.

## 루프 엔지니어링 도구

루프는 너와 모델 사이의 블랙박스가 아니라, 들여다볼 수 있는 일급 객체다.

| 커맨드 | 주는 것 |
|---|---|
| `/loop` | 루프 맵 — 턴 · 스텝 · 카운슬 라운드를 한눈에 |
| `/context` | 컨텍스트 윈도우를 채우는 정확한 내역 (사용량 · 압축) |
| `/rewind` | 최근 유저 턴(들) 되감기 |
| `/fork` | 대안을 시도하려 세션 분기, 원본 보존 |
| `/replay` | 마지막 턴을 브랜치에서 재실행 |
| `/loopdiff` | 브랜치를 분기 원점과 비교 |

모든 턴은 append-only JSONL 로그로 **이벤트 소싱**된다 — 되감기·분기·재실행이 가능한 이유가 바로
이것이다. 루프는 휘발성이 아니라 관찰·재현 가능하다.

---

## 빠른 시작

### 요구사항

- **Go 1.26+** (빌드 시)
- **OpenAI 호환 LLM 백엔드.** [Ollama](https://ollama.com) 권장. 기본 모델은 **`gpt-oss:120b-cloud`**로,
  Ollama **무료 클라우드 티어**에서 도는 강력한 모델이다 — GPU 불필요, 한 번만 로그인:
  ```sh
  ollama signin                   # 무료 티어; 기본 gpt-oss:120b-cloud는 Ollama 클라우드에서 실행
  ```
  **완전 로컬**로 돌리고 싶다면 모델을 받고 지정:
  ```sh
  ollama pull qwen3-coder:30b
  ./magi --model qwen3-coder:30b  # 가장 강한 로컬 코더(24GB GPU); 또는 MAGI_MODEL=…
  ```
  > 어떤 OpenAI 호환 엔드포인트든 가능(vLLM, LiteLLM, 호스팅 API) — 설정 참고. 아주 작은 모델
  > (예: `llama3.1:8b`)은 툴이 켜지면 인사에도 툴콜 JSON을 읊어 적합하지 않다.

### 설치

```sh
# 프리빌트 바이너리
curl -fsSL https://raw.githubusercontent.com/sayaya1090/magi/main/scripts/install.sh | bash

# Homebrew
brew install sayaya1090/tap/magi
```

### 소스에서 빌드

```sh
make build        # CGO_ENABLED=0, 버전 주입 → ./magi
# 또는
CGO_ENABLED=0 go build -o magi ./cmd/magi
```

순수 Go — 단일 정적 바이너리, CGo 없음. 어디든 복사해서 실행.

### 실행

```sh
./magi                         # 대화형 TUI
./magi --version               # 버전 출력
./magi --update                # 바이너리 + 관리형 플러그인 업데이트(체크섬 검증)
./magi --update-core           # 바이너리만 업데이트
./magi --update-plugins        # 관리형(git) 플러그인만 업데이트
```

인터랙티브 터미널에서는 하루 한 번 새 릴리스를 확인함: 패치는 배너만, 마이너/메이저는 자동 설치(취소 가능)
후 재시작 안내. 비인터랙티브 실행(`-p`·파이프·CI)에선 확인 안 함. `--no-update-check`로 끔.

**TUI에서:** **Enter** 전송 · **Esc** 진행 중 턴 중단 · **Ctrl+Q** / `/quit` 종료 (Ctrl+C는 드래그 복사용으로 비워둠).
위험 툴(`write`/`edit`/`bash`)은 실행 전 확인(`y` 허용 · `a` 항상 · `n` 거부).
마크다운·신택스 하이라이트는 다크/라이트 터미널에 자동 대응.

### 헤드리스 (스크립트 & CI)

```sh
./magi -p "Go 파일을 나열하고 아키텍처를 요약해줘"
./magi -p "hello.txt 만들고 내용: hi" --output json   # JSONL 이벤트 스트림
echo "main.go 설명해줘" | ./magi -p -                  # stdin
```

---

## 설정

주석 달린 `config.toml`이 첫 실행 시 생성된다(이후 덮어쓰지 않음). 우선순위는 **플래그 > 환경변수 >
config > 기본값**.

| 플래그 | 환경변수 | 기본값 | 용도 |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `gpt-oss:120b-cloud` | 모델 id (Ollama 무료 클라우드 티어; `ollama signin`) |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI 호환 base URL |
| `--permission` | `MAGI_PERMISSION` | TUI `ask` / 헤드리스 `allow` | `ask` \| `auto` \| `allow` \| `deny` |
| `--output` | — | `text` | `text` \| `json` (헤드리스) |
| — | `MAGI_API_KEY` | *(없음)* | 원격 백엔드 키 (Ollama는 불필요) |

**에이전트별 모델 & 백엔드 라우팅** — 잡일엔 싼 모델, 중요한 데엔 강한 모델:

```toml
[routing]
explore = "fast"             # → [llm.profiles.fast] (자체 엔드포인트/키)
coder   = "qwen3-coder:30b"  # 기본 백엔드에서 모델만 변경

[llm.profiles.fast]          # 이름붙인 백엔드; ${ENV} 확장
base_url = "https://fast.gateway/v1"
api_key  = "${FAST_KEY}"
model    = "gpt-oss:20b"
```

---

## 도구

**빌트인 도구:**

`read` · `write` · `edit` · `multiedit`(원자적 멀티헝크) · `grep` · `glob` · `list` ·
`bash`(타임아웃 · exit코드 · 장시간 명령용 `background`) · `bash_output` · `bash_input` ·
`bash_kill` · `wait_for` · `port_owner` ·
`recall_context` · `recall_memory` ·
`webfetch` · `websearch`(DuckDuckGo, 또는 Brave/Tavily 키 사용) ·
`todowrite` · `council`(읽기를 청하거나, 작업 종료를 선언) · `remember`(공유 메모리) · `skill` ·
`ask_user`·`route_interjection`(대화형 실행에서만)

편집 후 **진단 피드백**(gofmt / go vet / py_compile / LSP)이 되돌아와 에이전트가 자가수정한다.
읽기전용 툴은 턴 안에서 병렬 실행.

**슬래시 커맨드** — `/` 입력 시 자동완성 팔레트(별칭도 접두사 검색):

`/help` `/route`(`/model`) `/tools` `/sessions` `/resume` `/rewind` `/image` `/diff`
`/loop` `/context` `/fork` `/replay` `/loopdiff` `/init` `/ultra` `/permission` `/compact` `/clear`
`/quit`

---

## 컨텍스트, 메모리 & 확장

- **프로젝트 메모리** — `AGENTS.md`(+ `.magi/AGENTS.md`, 전역 파일)가 시스템 프롬프트에 주입되어
  *압축돼도 사라지지 않는* 영속 컨텍스트가 된다(CLAUDE.md 등가).
- **컨텍스트 인식 자동 압축** — 실제 토큰 사용량이 모델 윈도우의 ~80%를 넘으면 오래된 턴을 요약하고
  최근 턴은 보존한다. 헤더에 `ctx 42%` 미터.
- **공유 경험** — 팀이 공유할 수 있는 git 기반 메모리/스킬 스토어(`<config>/experience`); `remember`
  툴이 리뷰 큐에 기여.
- **Lua 플러그인** — `.magi/plugins/`에 `plugin.toml` + `init.lua`를 두면 자동 로드·핫리로드·샌드박스.
  예제: [plugins/examples/wordcount](plugins/examples/wordcount).
- **MCP 서버** — `config.toml`에 선언하면 기동 시 툴이 등록된다:
  ```toml
  [mcp.filesystem]
  command = "npx"
  args = ["-y", "@modelcontextprotocol/server-filesystem", "."]
  ```

---

## 아키텍처

magi는 **포트 & 어댑터(헥사고날)** 구조다. 코어 도메인은 UI도, LLM도, 플러그인도 모른다 — 어댑터가
코어에 꽂힌다. 의존 방향은 항상 안쪽.

| 선택 | 이유 |
|---|---|
| **Go** | 단일 정적 바이너리, 손쉬운 크로스컴파일, 자가 업데이트, goroutine 동시성 |
| **Bubble Tea (Charm)** | 미려한 TUI의 표준; 마크다운/코드 렌더 턴키 |
| **Lua (gopher-lua)** | 순수 Go 임베드(CGo-free 빌드 유지), 자연스러운 핫리로드 + 샌드박스 |
| **이벤트 소싱 JSONL** | 관찰·재현·분기 가능한 루프 |
| **OpenAI 호환 LLM** | 프로토콜 어댑터 하나로 → 로컬(Ollama/vLLM) 또는 호스팅 엔드포인트(Claude/Gemini 호환 API 포함) |

```
cmd/magi            엔트리포인트(와이어링)
internal/core       도메인 — 어댑터에 의존 안 함 (순수 council 포함)
internal/port       포트(인터페이스) — LLM, Store, Council, PluginHost …
internal/adapter    어댑터 — llm/openai · tui/bubbletea · plugin/lua · mcp · council/llm
plugins/examples    예제 Lua 플러그인
docs                ARCHITECTURE · DESIGN · SPEC · MANUAL · PLAN · FEATURES
```

더 읽기(한국어판): [ARCHITECTURE](docs/ARCHITECTURE.ko.md) · [DESIGN](docs/DESIGN.ko.md) ·
[EXTENDING](docs/EXTENDING.ko.md) · [SPEC](docs/SPEC.ko.md) · [DIAGRAMS](docs/DIAGRAMS.ko.md).
영문판: [ARCHITECTURE](docs/ARCHITECTURE.md) · [DESIGN](docs/DESIGN.md) ·
[EXTENDING](docs/EXTENDING.md) · [SPEC](docs/SPEC.md) · [DIAGRAMS](docs/DIAGRAMS.md).

---

## 라이선스

**Apache-2.0** — [LICENSE](LICENSE) 참고. 서드파티 코드 재사용 시 `NOTICE`와 `THIRD_PARTY_LICENSES`
파일을 그대로 유지.
