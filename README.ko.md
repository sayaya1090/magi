<div align="center">

# magi

### 스스로 "다 했다"고 판정하지 않는 터미널 AI 코딩 에이전트.

대부분의 에이전트는 작업을 끝낸 모델이 스스로 종료를 판정한다 — 그래서 일찍 멈추거나, 끝없이 맴돈다.
**magi는 그 결정을 투표에 부친다.** 서로 다른 렌즈를 가진 세 전문가가 *실제로 무슨 일이 있었는지*를
읽고, 합의했을 때에만 턴을 끝낸다 — 그리고 **magi가 직접 돌리는** 검증 명령이, 테스트가 뒷받침하지
않는 "완료"를 거부(veto)할 수 있다.

[English](README.md) · [한국어](README.ko.md) · [매뉴얼](docs/MANUAL.ko.md) · [라이브 데모](https://sayaya1090.github.io/magi/)

[![CI](https://github.com/sayaya1090/magi/actions/workflows/ci.yml/badge.svg)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
[![coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/sayaya1090/magi/badges/coverage.json)](https://github.com/sayaya1090/magi/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/License-Apache--2.0-blue)
![Single binary](https://img.shields.io/badge/build-CGO__free%20single%20binary-success)

</div>

---

<div align="center">

### 터미널에서 하나를 돌리고, 브라우저에서 팀 전체를 지켜본다.

<a href="https://sayaya1090.github.io/magi/">
  <img src="docs/img/console-companions.png" alt="magi 웹 콘솔 — 두 팀에 걸친 컴패니언 목록, 하나는 명령 승인 대기, 하나는 질문 대기, 상태·스텝·호스트가 실시간으로 보인다" width="900">
</a>

*콘솔 — 내 여러 머신 위의 모든 magi, 각자가 지금 무엇을 하는지, 그리고 나를 기다리는 둘.
[라이브 데모 열기 →](https://sayaya1090.github.io/magi/) (진짜 페이지, 목 데이터, 서버 없음.)*

</div>

---

## 단 하나의 아이디어

에이전트 루프에는 어려운 질문이 하나 있다. **턴은 언제 진짜로 끝나는가?**

그걸 암묵에 맡기면 — 모델이 툴 호출을 멈추면 턴이 끝나는 방식 — 생각하다 만 턴과 정말로 끝난 턴이
구별되지 않는다. magi는 종료를 **행위**로 만든다. 에이전트가 끝났다고 *선언*하고, 카운슬이 무슨 일이
있었는지 기록을 읽은 다음에야 턴이 닫힌다.

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
존재 이유 전부이고, 나머지는 그 루프를 **관찰 가능·조종 가능·재현 가능**하게 만들고, 그런 에이전트
여럿을 한 번에 돌리고 감독하기 위해 존재한다.

---

## 무엇을 얻는가

|  | 기능 | 무슨 의미인가 |
|---|---|---|
| 🗳️ | **합의 종료** | 세 멤버가 *완료 / 거부 / 기권*을 투표하고, 순수·단위테스트된 규칙이 집계한다. *거부*는 종합된 피드백을 다음 지시로 루프에 되먹인다. |
| 🔒 | **magi가 직접 돌리는 검증 명령** | `[council] verify`에 테스트 명령을 걸어두면 magi가 종료 게이트에서 그걸 실행한다 — 0이 아닌 종료 코드는 멤버가 뭐라 투표했든 완료를 **거부**한다. `go test`라면 비활성화된 스위트나, 강제 `exit 0`으로 가려진 실패까지 잡아낸다. |
| 🧾 | **주장이 아니라 기록** | magi가 모든 툴 호출을 승인하므로, 어떤 명령이 돌았고 *진짜* 종료 코드가 뭐였는지(파이프의 어느 단계가 실패했는지 포함), 어떤 파일을 썼는지 안다 — 게다가 "완료"마다 작업공간을 방금 다시 읽는다. |
| 🖥️ | **여러 에이전트를 위한 웹 콘솔** | 내 머신들 위의 모든 컴패니언을 브라우저에서 감독한다: 중단, 질문 답하기, 명령 승인, 학습한 것 읽기, 하나가 막히면 휴대폰으로 알림. |
| 🤝 | **컴패니언과 hand-off** | 작업공간에 이름과 역할을 주면 *무엇을 위한 것인지*로 부를 수 있다. `hand_off`는 전문가에게 일 한 조각을 넘기고 계속 진행한다 — 답은 끝나면 내 대화로 돌아온다. |
| 🗣️ | **회의(Meetings)** | 여러 컴패니언이 하나의 질문을 두고, 읽기 전용으로, 각자가 무엇을 할지 알 때까지 논의한 뒤 일이 배분된다. |
| ⏮️ | **들여다볼 수 있는 루프** | 매 턴이 append-only JSONL로 이벤트 소싱되어 `/rewind`·`/fork`·`/replay`·`/loopdiff`가 가능하다 — 루프는 블랙박스가 아니라 실체 있는 객체다. |
| 📦 | **단일 정적 바이너리** | 순수 Go, CGO 없음. [Ollama](https://ollama.com) 무료 클라우드 티어로 로컬 우선 — GPU 불필요, `ollama signin` 한 번 — 또는 OpenAI 호환 엔드포인트 아무거나. |

---

## 카운슬 (The Council)

루프가 자연히 끝나려는 순간, 멤버들의 카운슬이 **완료**, **거부**, **기권**을 투표하고 순수 합의
규칙이 그 표를 하나의 결정으로 만든다. 기본 세 멤버 — **MAGI** — 는 각자 다른 렌즈로 판단한다:

| 멤버 | 렌즈 | 묻는 것 |
|---|---|---|
| **Melchior** | `correctness` | 작업이 정확한가? 엣지 케이스, 회귀? |
| **Balthasar** | `verification` | 작동한다는 *증거*가 있는가 — 빌드/테스트가 통과하는가? 말만으로는 안 된다. |
| **Casper** | `completeness` | 과제가 요구한 걸 다 했는가? 남긴 게 없는가? |

**단일 심판이 아니라 합의.** 집계 규칙은 설정 가능하다:

| 규칙 | 이럴 때 끝난다… |
|---|---|
| `majority` *(기본)* | 투표 멤버의 과반이 완료라 할 때 (동수는 계속) |
| `unanimous` | 모든 멤버가 완료라 할 때 |
| `quorum:k` | 최소 *k*명이 완료라 할 때 |
| `weighted:θ` | 완료 가중치 비율이 임계치 θ를 넘을 때 |
| `veto:Name` | 지정 멤버가 어떤 완료든 거부할 수 있다 |

**루프를 가두지도, 무조건 통과시키지도 않도록** 설계됐다: 동수, 미달 정족수, 투표 없음, 오류는 모두
*계속*으로 귀결된다 — 확정적 합의가 있을 때에만 끝나고, 모호함으로는 끝나지 않는다. 진전 없음
감지가 맴돌기를 막고, 라운드에 상한이 있으며, 오류를 내거나 쓰레기를 반환한 멤버는 게이트를 막는
대신 **기권**한다.

> 합의 로직은 `internal/core/council`에 **순수 도메인 코드**로 산다 — I/O도 LLM도 없다. 그
> 분리가 *"카운슬이 결정한다, 한 모델이 아니라"*를 희망 섞인 프롬프트가 아니라 테스트된 불변식으로
> 만든다.

---

## 기록 — 그리고 에이전트가 속일 수 없는 검증

멤버들은 분위기로 판단하지 않는다. **magi가 직접 기록한** 것 위에서 에이전트의 *보고*를 *과제*에
비추어 판단한다: 승인된 모든 명령과 그게 실제로 어떻게 끝났는지, 이번 턴의 편집을 파일별
before→after 디프로, 그리고 완료 선언 시 작업공간을 방금 다시 읽은 결과(과제 시작 후 수정된 파일,
아직 살아 있는 백그라운드 잡, 기록은 썼다는데 디스크에 없는 경로).

그 기록 위에 **에이전트가 뒤엎을 수 없는 고정 검증 하네스**가 얹힌다:

```toml
[council]
verify = "go test ./..."   # magi가 종료 게이트에서 이것을 직접 실행한다
```

그 종료 코드가 최종 권한이다. 0이 아닌 종료는 멤버가 뭐라 투표했든 완료를 **거부**하고, 그 출력은
에이전트의 자기 보고가 아니라 magi가 돌린 증거로 멤버들에게 보여진다. `go test`라면 magi가 한
발 더 나가 `-json`으로 재실행하므로, 초록 스위트를 위조하는 두 고전적 수법이 모두 잡힌다:

- **아무 것도 안 돌리는** `TestMain` (비었거나 비활성화된 스위트가 여전히 0으로 종료), 그리고
- 테스트를 돌려 실패를 보고도 실패 위로 **`os.Exit(0)`를 강제**하는 `TestMain`.

둘 중 어느 쪽이든 "완료" 표를 *계속*으로 바꾸며, 증거가 그 실패를 지목한다. 미리 써둔 체크는 작업에
대해 틀릴 수 있어도, 무엇이 승인됐는지의 기록은 무엇이 돌았는지에 대해 틀릴 수 없다.

묻는 것과 선언하는 것은 별개다: `council{question}`은 확신 없는 부분에 대한 멤버들의 읽기를 받아올
뿐 아무 것도 끝내지 않는다.

---

## 콘솔 — 그리고 컴패니언 팀

<div align="center">
<a href="https://sayaya1090.github.io/magi/">
  <img src="docs/img/console-companion-detail.png" alt="콘솔의 단일 컴패니언 페이지 — 상태·모델·작업공간, 실패한 go test의 진짜 종료 코드와 메시지가 보이는 라이브 트랜스크립트, 그리고 승인 대기 중인 위험 명령" width="820">
</a>

*한 컴패니언의 페이지: 진짜 종료 코드가 담긴 라이브 트랜스크립트, 그리고 승인을 위해 잡아둔 위험 명령.*
</div>

<table>
<tr>
<td width="50%" valign="top"><a href="https://sayaya1090.github.io/magi/?v=meet"><img src="docs/img/console-meeting.png" alt="회의 페이지 — 둘 이상의 컴패니언을 골라 하나의 질문을 던지고 방을 연다; 진행 중인 회의가 아래에 보인다" width="100%"></a><br><b>회의.</b> 여러 컴패니언을 하나의 질문에 붙여 각자가 무엇을 할지 알 때까지 논의시킨다.</td>
<td width="50%" valign="top"><a href="https://sayaya1090.github.io/magi/?v=board"><img src="docs/img/console-board.png" alt="보드 — 하루치 일이 카드로, 팀별 한 열씩, 에이전트가 붙인 라벨로 묶여 있다" width="100%"></a><br><b>보드.</b> 하루치 일을 카드로, 팀별 한 열씩, 라벨로 묶어서.</td>
</tr>
<tr>
<td width="50%" valign="top"><a href="https://sayaya1090.github.io/magi/?v=skills"><img src="docs/img/console-shared.png" alt="공유 페이지 — 팀이 학습한 규칙과 기억한 사실, 각각 도달 범위와 Read/Forget 컨트롤" width="100%"></a><br><b>공유 두뇌.</b> 팀이 학습한 규칙과 기억한 사실 — 각각 누구에게 도달하는지로 범위가 정해진다.</td>
<td width="50%" valign="top"><a href="https://sayaya1090.github.io/magi/"><img src="docs/img/console-phone.png" alt="휴대폰에서의 콘솔 — 하단 내비게이션 바와 카드 스택, 작은 화면에서 승인 프롬프트에 답한다" width="100%"></a><br><b>휴대폰에서.</b> 같은 콘솔 — 어디서든 승인하고 답할 수 있다.</td>
</tr>
</table>

한 작업공간에 묶인 하나의 magi가 **컴패니언**이다. 이름과 역할을 주면 *무엇을 위한 것인지*로
부를 수 있게 된다:

```toml
# .magi/config.toml — 저장소와 함께 이동
[companion]
name = "design"
role = "디자인 시스템: 컴포넌트 명세와 비주얼 리뷰"
team = "frontend"     # 선택
```

- **`companions`** 는 나머지를 나열한다 — 각 작업공간이 *학습한* 것 포함. 전문가가 눈에 보이게 되는 방식.
- **`companion_can`** 은 그 중 하나에게 실제로 무엇을 할 수 있는지 묻는다.
- **`hand_off`** 는 일 한 조각을 넘기고 계속 진행한다. 요청은 그 목적과 답이 돌아와야 할 **형식**을
  함께 실어 보내고, 답은 끝나면 내 대화로 온다. 컴패니언은 **한 번에 한 턴**만 하고 그 사이 도착한
  일은 **큐에 넣는다** — 얼마나 큐에 쌓였는지가 공개 기록에 실리므로, 고르는 사람은 누가 한가한지 볼 수 있다.
- **회의(Meetings)** 는 여러 컴패니언을 하나의 질문에 붙여 각자가 무엇을 할지 알 때까지 논의시킨다.

**레지스트리도, 게이트웨이도, 열린 포트도 없다.** 모든 데몬이 자기 소켓 옆에 기록을 발행하고, 그
디렉터리가 곧 명단이다. 머신을 넘어서는 같은 기록을 **ssh**로 주고받는다 —
`magi --join-cluster <host>` 한 번이면 데몬들이 서로를 갱신하고 한 시간 못 본 상대는 잊는다. 일도
같은 길로 건너가므로, magi는 자기 포트를 열지 않고 자기 자격증명을 갖지 않는다.

터미널에서, 또는 브라우저에서 지켜본다:

```sh
./magi --daemon                # UI 없는 엔진 — 아무도 안 볼 때도 계속 일한다
./magi --attach                # 이 작업공간의 데몬에 터미널 UI를 붙인다
./magi --agents                # 이 머신의 모든 magi와 각자가 하는 일
./magi-web                     # 같은 것을 브라우저로 (127.0.0.1:7777) — 중단·답변·학습한 것 읽기·막히면 폰 알림
./magi-web -exposed            # 인증 프록시 뒤에서: 셸 없음, MCP 쓰기 없음, 모든 변경 기록
```

---

## 루프는 일급 객체다

너와 모델 사이의 블랙박스가 아니라 — 들여다보고, 분기하고, 재생할 수 있는 것.

| 명령 | 주는 것 |
|---|---|
| `/loop` | 루프 맵 — 턴·스텝·카운슬 라운드를 한눈에 |
| `/context` | 컨텍스트 창을 채우는 게 정확히 무엇인지 (사용량·컴팩션) |
| `/rewind` | 마지막 사용자 턴(들)을 되감기 |
| `/fork` | 대안을 시도하려 세션을 분기, 원본 보존 |
| `/replay` | 마지막 턴을 분기에서 재실행 |
| `/loopdiff` | 분기를 그 포크 원본과 비교 |

매 턴이 append-only JSONL 로그로 **이벤트 소싱**된다 — 바로 그것이 되감기·분기·재생을 가능케 한다.
루프는 관찰 가능하고 재현 가능하며, 휘발되지 않는다.

---

## 빠른 시작

### 요구 사항

- **Go 1.26+** (빌드용).
- **OpenAI 호환 LLM 백엔드.** [Ollama](https://ollama.com) 권장. 기본 모델은
  **`gpt-oss:120b-cloud`** — Ollama **무료 클라우드 티어**의 강력한 모델로, GPU 없이 한 번만 로그인:
  ```sh
  ollama signin                   # 무료 티어; 기본 gpt-oss:120b-cloud는 Ollama 클라우드에서 돈다
  ```
  **완전 로컬**을 원한다면? 모델을 받아 magi를 가리키면 된다:
  ```sh
  ollama pull qwen3-coder:30b
  ./magi --model qwen3-coder:30b  # 가장 강한 로컬 코더 (~24 GB GPU); 또는 MAGI_MODEL=…
  ```
  > OpenAI 호환 엔드포인트면 무엇이든 된다(vLLM, LiteLLM, 호스팅 API) — Configuration 참조. 아주 작은
  > 모델(예: `llama3.1:8b`)은 인사할 때도 툴콜 JSON을 뱉는 경향이 있어 잘 맞지 않는다.

### 설치

```sh
# 사전 빌드 바이너리
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
./magi -p "main.go 설명해줘"     # 헤드리스 원샷 (--output json 이면 JSONL 이벤트 스트림)
./magi --version               # 버전 출력
./magi --update                # 바이너리와 관리형 플러그인 업데이트 (체크섬 검증)
```

**TUI에서:** **Enter** 전송 · **Esc** 실행 중인 턴 중단 · **Ctrl+Q** / `/quit` 종료.
위험한 툴(`write`/`edit`/`bash`)은 실행 전 묻는다(`y` 허용 · `a` 항상 · `n` 거부). 마크다운과 구문
강조는 다크/라이트 터미널에 자동으로 맞춘다. `/`를 치면 자동완성 명령 팔레트가 열린다.

---

## 설정

첫 실행 시 주석 달린 `config.toml`이 생성된다(이후 덮어쓰지 않음). 우선순위는
**플래그 > 환경변수 > 설정 > 기본값**.

| 플래그 | 환경변수 | 기본값 | 용도 |
|---|---|---|---|
| `--model` | `MAGI_MODEL` | `gpt-oss:120b-cloud` | 모델 id (Ollama 무료 클라우드; `ollama signin`) |
| `--base-url` | `MAGI_BASE_URL` | `http://localhost:11434/v1` | OpenAI 호환 base URL |
| `--permission` | `MAGI_PERMISSION` | TUI `ask` / 헤드리스 `allow` | `ask` \| `auto` \| `allow` \| `deny` |
| `--output` | — | `text` | `text` \| `json` (헤드리스) |
| — | `MAGI_API_KEY` | *(없음)* | 원격 백엔드 키 (Ollama는 불필요) |

**에이전트별 모델·백엔드 라우팅** — 잡일엔 값싼 모델, 중요한 곳엔 강한 모델:

```toml
[routing]
explore = "fast"             # → [llm.profiles.fast] (자체 엔드포인트/키)
coder   = "qwen3-coder:30b"  # 기본 백엔드의 다른 모델일 뿐

[llm.profiles.fast]          # 이름 붙인 백엔드; ${ENV} 확장됨
base_url = "https://fast.gateway/v1"
api_key  = "${FAST_KEY}"
model    = "gpt-oss:20b"
```

전체 레퍼런스는 [매뉴얼](docs/MANUAL.ko.md)에 있다.

---

## 툴과 확장

**내장 툴:** `read` · `write` · `edit` · `multiedit` · `grep` · `glob` · `list` · `bash`
(타임아웃 · 종료 코드 · `background`) · `bash_output` · `bash_input` · `bash_kill` · `wait_for` ·
`port_owner` · `recall_context` · `recall_memory` · `webfetch` · `websearch` · `todowrite` ·
`council`(읽기 요청, 또는 완료 선언) · `remember`(공유 메모리) · `skill` · `companions` ·
`companion_can` · `hand_off` · `ask_user` 와 `route_interjection`(대화형 전용).

편집 후엔 **진단 피드백**(gofmt / go vet / py_compile / LSP)이 되돌아와 에이전트가 스스로 고친다.
읽기 전용 툴은 한 턴 안에서 병렬로 돈다.

- **기본은 에이전트 하나.** magi는 자기 서브에이전트를 싣지 않는다; 원한다면 플러그인에서 온다
  (`/subagents`로 켠다). 하나가 꺼진 채 딸려 온다: **Seele**, 쓰기 툴이 전혀 없는 플래너.
- **프로젝트 메모리** — `AGENTS.md`(및 `.magi/AGENTS.md`, 전역 하나)는 *컴팩션을 견디는* 지속 컨텍스트.
- **컨텍스트 인지 자동 컴팩션** — 모델 창의 ~80%를 넘으면 최근 턴은 지키고 오래된 턴을 요약; 헤더에
  `ctx 42%` 미터.
- **공유 경험** — 팀이 공유하는 git 기반 메모리/스킬 저장소; `remember` 툴이 리뷰 큐에 기여.
- **Lua 플러그인** — `<config>/plugins/`에 `plugin.toml` + `init.lua`; 자동 로드·핫리로드·샌드박스.
  [plugins/examples/wordcount](plugins/examples/wordcount) 참조.
- **MCP 서버** — `config.toml`에 선언하면 시작 시 툴이 등록된다.
- **무인 작업** — `schedule` / `[cron]`이 아무도 안 볼 때 잡을 돌린다.

---

## 아키텍처

magi는 **포트 & 어댑터(헥사고날)**다: 코어 도메인은 UI·LLM·플러그인을 모르고, 어댑터가 코어에
꽂힌다. 의존 방향은 언제나 안쪽이다.

```
cmd/magi            엔트리포인트 (배선)
cmd/magi-web        콘솔 — 같은 데몬들 위의 읽기 위주 웹 뷰
internal/core       도메인 — 어떤 어댑터에도 의존하지 않음 (순수 카운슬 포함)
internal/port       포트(인터페이스) — LLM, Store, Council, PluginHost …
internal/adapter    어댑터 — llm/openai · tui/bubbletea · plugin/lua · mcp · council/llm ·
                    daemon(소켓 위의 엔진) · fleet(모든 magi가 뭘 하는지)
plugins/examples    예제 Lua 플러그인
docs                ARCHITECTURE · DESIGN · SPEC · MANUAL · UI · EXTENDING · DIAGRAMS
```

더 깊이: [ARCHITECTURE](docs/ARCHITECTURE.ko.md) · [UI](docs/UI.ko.md) · [DESIGN](docs/DESIGN.ko.md) ·
[EXTENDING](docs/EXTENDING.ko.md) · [SPEC](docs/SPEC.ko.md) · [DIAGRAMS](docs/DIAGRAMS.ko.md).

---

## 라이선스

**Apache-2.0** — [LICENSE](LICENSE) 참조. 서드파티 코드를 재사용할 때는 `NOTICE`와
`THIRD_PARTY_LICENSES` 파일을 그대로 유지할 것.
