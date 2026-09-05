# magi PowerPoint 클라이언트 — 아키텍처(지어진 대로)

[매뉴얼](MANUAL.ko.md) · [다이어그램](DIAGRAMS.ko.md) · [모델이 받는 것](TOOLS.ko.md) · [시험](TESTING.ko.md) · [↑ 설계](../DESIGN.md)

> **현행 참고 문서.** 지어진 대로의 구조입니다. `../DESIGN.md` 는 설계 의도와 그 이유(§0–§12)를 적고, 이 문서는 2026-09-05 기준 소스가 실제로 어떻게 놓여 있는지를 적습니다. 둘이 어긋나면 이 문서와 소스가 맞습니다.

## 0. 한 문장

PowerPoint 작업창(애드인)이 사용자당 하나인 **헬퍼**(`magi-ppt`)에 붙고, 헬퍼가 machine 의 **데몬**(`magi --daemon`) 한 개에 덱마다 **대화(session)** 하나를 열어 그 대화에만 덱 도구 45개를 달아 줍니다. 모델이 도구를 부르면 데몬 → 헬퍼(MCP) → 작업창(SSE) → Office.js 순으로 내려가 덱을 고치고, 결과가 같은 길로 돌아옵니다.

## 1. 프로세스가 넷이다

| 프로세스 | 몇 개 | 무엇 | 소스 |
|---|---|---|---|
| PowerPoint + 작업창 | 창마다 하나 | Office.js 를 쥔 유일한 자리. 덱을 읽고 고치는 코드는 전부 여기 있습니다 | `addin/src` (JS, 12,799줄) |
| 헬퍼 `magi-ppt` | 사용자당 하나 | 애드인 페이지를 https 로 내주고(§5.5), MCP 서버이며(§4.5), 데몬을 마련해 붙입니다(§5.0·§5.9) | `helper/*.go` (Go, 10,395줄 · 19 파일) |
| 데몬 `magi --daemon` | machine 에 하나(워크스페이스 `powerpoint`) | 대화 N개, 모델 호출, 도구 디스패치, 권한 물음 | `internal/` (공용 코어) |
| 모델 심(shim) | 모델당 하나 | OpenAI 호환 `/v1` 을 흉내내어 실제 모델에 넘김 | `plugins-embedded/{antigravity,codex,claudecode}` |

실측(2026-09-05 09:30): 데몬 pid 23062 는 `--base-url http://127.0.0.1:58415/v1 --model "Gemini 3.8 Flash (High)"` 로 떠 있고, 헬퍼 pid 22624 는 3000 번 포트, 덱 둘이 각각 `s_98eb88…`·`s_b63eeb…` 대화를 쥐고 도구 45개씩 달았습니다.

**왜 헬퍼가 따로 있는가**(DESIGN §5.1). Office 는 애드인을 https 로만 받고, 애드인은 밖으로 소켓을 열 수 없습니다. 유닉스 소켓으로 데몬에 닿고 인증서를 쥐는 프로세스가 하나 있어야 하고, 그것이 창마다가 아니라 **사용자당 하나**여야 창 둘이 같은 데몬을 봅니다(§5.2).

## 2. 헬퍼 — 얼굴이 셋

`names.go` 첫 줄 그대로, 헬퍼는 얼굴이 셋입니다. 한 파일이 한 얼굴을 맡고, `main.go` 는 조립만 합니다(`main.go` 머리 주석: "무엇이 무엇인지 아는 자리는 이 파일뿐").

### 2.1 애드인 쪽 — `/api/*`, `/hand/*`, 페이지

| 길 | 파일 | 하는 일 |
|---|---|---|
| `/taskpane.html`, 정적 파일 | `page.go`, `certs.go`, `icon.go` | 페이지를 직접 내줍니다. 토큰은 페이지에 박혀 나오고(§5.5), 인증서는 헬퍼가 만들되 신뢰 저장소에는 사람이 넣습니다 |
| `/hand/stream` (SSE) · `/hand/reply` (POST) | `handhttp.go`, `hand.go` | 창이 **손**으로 등록하는 자리. 헬퍼가 조작을 `{kind, data}` 봉투로 내려보내고 창이 결과를 올립니다. WebSocket 이 아닌 이유는 `handhttp.go` 머리 주석에 있습니다(프록시·인증서·재접속) |
| `/api/own` · `/api/fresh` · `/api/choose` · `/api/companions` | `own.go`, `ownstate.go`, `attach.go`, `bridges.go` | 데몬을 마련하고 이 덱을 대화에 묶는 문. §4 참고 |
| `/api/submit` · `/api/steer` · `/api/interrupt` · `/api/status` · `/api/permission` · `/api/question` | `bridge.go` | 대화 한 개의 앞뒤. `submit` 은 202 만 답하고 답은 스트림으로 옵니다(§5.7) |
| `/api/documents` · `/api/caps` | `hand.go`, `main.go` | 지금 붙어 있는 덱 목록, 호스트가 말한 요구사항 집합 |
| `/api/guides` · `/api/guide` · `/api/instructions` | `guides.go`, `instructions.go` | 사람이 관리하는 가이드 문서 5벌과 지속 지시(AGENTS.md) |

모든 `/api/*` 와 `/mcp` 는 `guard` 를 거칩니다: 페이지에 박힌 토큰과 같은 Bearer 만 통과합니다.

### 2.2 데몬 쪽 — MCP 서버 `/mcp`

`mcp.go` 는 Streamable HTTP MCP 서버입니다. stdio 가 아닌 이유(머리 주석): 데몬이 서버를 자식으로 띄우면 워크스페이스마다 헬퍼가 생기는데, 헬퍼는 사용자당 하나여야 합니다.

- `tools/list` → `tools.go` 의 목록 45개(실측 2026-09-05, 저녁 판). 도구 하나가 Office.js 호출 하나에 대응하고, **실행은 창이** 합니다.
- `tools/call` → `args.go` 가 인자를 검사하고(모르는 키는 거절, 별칭은 광고된 것만), `hand.go` 가 **어느 창**에 보낼지 고릅니다. 문서 인자(`document`)가 첫째, 주소의 `?deck=` 이 둘째, 덱이 하나면 그것, 둘 이상이면 **거절**합니다(`hand.go pick`: "more than one deck is open … Name it").
- 그림 결과(`render_*`)는 `image.go` 가 파일을 읽어 MCP 이미지 블록으로 바꿉니다.

### 2.3 데몬 마련 — `own.go`, `attach.go`

`own.go`: PowerPoint 는 **자기 컴패니언**을 갖습니다. 남의 워크스페이스(IDE 가 연 저장소 등)를 빌리지 않고, 워크스페이스 `~/Library/Application Support/magi/powerpoint` 에 데몬을 띄우거나 이미 있는 것을 씁니다. 살아 있는지는 3초(`aliveTimeout`) 안에 문을 두드려 봅니다.

`attach.go`: 상태를 **들지 않습니다**(`type Attachments struct{}`). 붙이는 순서는 늘 같습니다 — `Hello` → `PeerSupports("tool-servers")` → `DetachMCP(owner, "ppt")` → `AttachMCP(owner, "ppt", url, headers)`. 떼고 붙이므로 두 번 불러도 결과가 하나입니다.

헬퍼가 쓰는 데몬 클라이언트 메서드는 열 개뿐입니다(실측 `grep`): `Hello`, `PeerSupports`, `AttachMCP`, `DetachMCP`, `NewSessionKeeping`, `Submit`, `Steer`, `Interrupt`, `Status`, `Transcript`. 찾기는 `daemon.List`/`daemon.SocketPath`/`daemon.DialWithin(2s)` 로 합니다.

## 3. 작업창 — 네 층

`addin/src` 는 클린 아키텍처 네 층입니다. 안쪽은 바깥을 모릅니다.

| 층 | 파일 | 줄 |
|---|---|---|
| `domain/` | `Transcript`(460) · `Pending`(188) · `Suggestion`(165) · `AdviceBoard`(123) · `Advice`(109) · `Composer`(94) · `Cursor`(89) · `Quote`(70) | 1,298 |
| `port/` | `ChatPort{submit}` · `DeckPort{selection, point, slideNumbers, capabilities}` · `HandPort{run, ops}` · `StatusPort{status, answerPermission, answerQuestion}` · `TranscriptPort{subscribe}` | 220 |
| `usecase/` | `WatchPrompt`(263) · `QuoteSelection`(148) · `SendTurn`(134) · `ReadTranscript`(124) · `ServeHand`(76) · `PointAtAdvice`(22) | 767 |
| `adapter/` | `OfficeHand`(4,065) · `OfficeDeck`(252) · `HelperStream`(165) · `HelperPorts`(98) · `helperApi`(100) · OOXML 다섯(`chartxml`·`animxml`·`notesxml`·`eaxml`·`zip`/`zipwrite`) · Fake 다섯 | 7,354 |
| `ui/` + `main.js` | `view`(1,215) · `screen`(786) · `pick`(96) · 픽스처 셋 · `main.js`(794) | 3,160 |

**`OfficeHand.js` 가 4,065줄인 이유.** 도구 45개의 실행이 전부 이 파일입니다 — 도구 하나가 Office.js 호출 한 벌이고, Office.js 가 못 하는 것(한글 폰트 `a:ea`, 차트, 애니메이션, 노트)은 OOXML 을 직접 써서 슬라이드를 다시 만듭니다(`eaxml.js` 등). 그래서 `ea_font` 를 바꾸면 슬라이드 id 가 바뀝니다.

Fake 어댑터 다섯(`FakeHand` 1,038줄 포함)은 브라우저만으로 창을 띄우는 데모·시험용입니다(`main.js` 의 `real` 분기).

### 3.1 창이 열릴 때 하는 일(순서)

`main.js` 실측 순서입니다.

1. `HelperStream` 이 `/hand/stream` 을 엽니다. 헬퍼가 `hello{document}` 를 내려보내면 그것이 이 창의 **덱 키**입니다(`api.useDeck`).
2. `hello` 를 최대 3초 기다린 뒤 `/api/own?deck=<키>` 를 부릅니다. 덱 없이 부르면 열쇠 없는 대화에 묶이므로 기다립니다(`main.js:688` 주석).
3. `WatchPrompt.poll` 이 `/api/status?deck=` 을 돌며 `session`·`reachable`·`stale` 을 봅니다. 대화가 생기면 `ReadTranscript.attach(session)` 으로 전사를 그 대화에 묶고, 브랜드 줄에 `대화 s_…` 를 **한 번** 적습니다.
4. `stale` 이 서면 컴패니언이 재기동된 것이므로 2 로 돌아갑니다.

## 4. 덱 하나에 대화 하나 — 여섯 이름과 네 사건

DESIGN §5.9 의 결론을 지은 것이 `bridges.go`·`ownstate.go`·`main.go settle` 입니다.

### 4.1 여섯 이름

| 이름 | 누가 주나 | 언제 바뀌나 |
|---|---|---|
| 덱 키 `pid-deck-…` | 창(`OfficeDeck.stableDeckId`, 프레젠테이션 태그 `MAGI.DECK`) | 파일이 바뀔 때만. 창을 닫았다 열어도 같습니다 |
| 대화 `s_…` | 데몬(`NewSessionKeeping`) | 데몬이 재기동되면 사라집니다 |
| 소켓 경로 | 데몬(`daemon.SocketPath`) | 워크스페이스가 같으면 같습니다 |
| 주인(owner) | 헬퍼: 덱이 있으면 = 대화, 없으면 빈 문자열 | 대화가 바뀌면 같이 바뀝니다 |
| 등록 키 `ppt` + 주인 | 데몬(`serverKey(name, owner)`) | 등록이 데몬 메모리에만 있어 데몬 재기동에 지워집니다 |
| 데몬 수명(life) | 데몬(`Hello` 가 알려 주는 기동 표식) | 데몬이 재기동될 때 |

### 4.2 네 사건, 그리고 각 칸이 하는 일

| 사건 | 덱 키 | 대화 | 등록 | 헬퍼가 하는 일 |
|---|---|---|---|---|
| 창 다시 열림 | 같음 | 같음 | 같음 | `settle` 이 `BoundTo()` 를 보고 소켓·수명이 같으면 **아무것도 안 함** |
| 헬퍼 재기동 | 같음 | 있지만 헬퍼는 모름 | 데몬에는 있음 | 덱이 이름을 대므로 새 대화를 열고(`freshOn`) 떼고-붙임 |
| 데몬 재기동 | 같음 | 없어짐 | 없어짐 | `life` 가 달라 새 대화 + 재등록. 창은 `stale` 로 알아챔 |
| PowerPoint 재기동 | 같음(태그) | 같음 | 같음 | 창 다시 열림과 같음 |

`settle(deck, report)` 는 **결정을 기억하지 않고** 매번 셋에게 묻습니다: 창(덱 키), 데몬(수명·대화), 자기 대장(`Bridges`). 뮤텍스 하나(`settling`)로 직렬화하고, 같은 입력이면 같은 답을 냅니다(`tests/join_deck_test.go` 가 멱등과 동시 호출을 못박습니다).

### 4.3 코어가 해 주는 것

데몬은 원래 대화 N개입니다(`App.states`, `Submit` 이 요청의 세션으로 갈래). 이번에 더한 것은 **도구 서버의 주인**뿐입니다 — `Attach(ctx, owner, name, url, headers)`: 주인이 비면 데몬 전체, 주인이 있으면 그 대화에만 광고(`port.Owned.VisibleTo`)하고 남의 대화가 부르면 거절합니다. 자세한 선택과 남은 일은 `internal/adapter/mcp/SESSION_SCOPE.md`.

## 5. 한 턴이 지나는 길

사용자가 창에 "IR 자료 만들어" 를 넣으면:

1. 창 `SendTurn` → `POST /api/submit?deck=` → 헬퍼 `Bridge.Submit` → 데몬 `Submit{session}` → 202.
2. 데몬이 모델(심 `:58415`)을 부릅니다. 도구 목록에는 코어 내장 도구와 이 대화의 `ppt` 45개, 플러그인 `land` 가 있습니다([TOOLS.ko.md](TOOLS.ko.md)).
3. 모델이 `mcp__ppt__add_slides` 를 부르면 데몬이 권한을 묻습니다(`--permission ask`). 창의 `WatchPrompt` 가 물음을 그리고 사람이 답합니다.
4. 허용되면 데몬 → `POST /mcp?deck=` → 헬퍼 `pick` 이 창을 고름 → `/hand/stream` 으로 `{kind:"call", data}` → 창 `ServeHand` → `OfficeHand` 가 Office.js 실행 → `/hand/reply` → 헬퍼 → MCP 응답 → 데몬 → 모델.
5. 변이 도구는 답에 `now`(바뀐 뒤의 객체)를 싣습니다. 모델이 다시 읽지 않아도 되게 하려는 것입니다.
6. 턴이 끝나면 전사가 `/hand/stream` 이 아니라 데몬 `Transcript` 로 창에 옵니다(`ReadTranscript`). `land` 를 불렀으면 그 선언(did/verified/left)이 전사 끝에 섭니다.

## 6. 일부러 하지 않는 것

- **헬퍼는 도구 목록을 기억하지 않으려 합니다.** 지금은 `Bridge.tools` 에 들고 있습니다 — 데몬에 `mcp-list` 문이 없어서입니다(DESIGN §5.9.6 5번, ⏳).
- **활성 문서를 추측하지 않습니다.** 덱이 둘이고 이름이 없으면 거절합니다. 추측이 틀리면 보고 있지 않은 덱이 고쳐집니다.
- **신뢰 저장소를 안 건드립니다.** 인증서는 만들어 두고 명령만 보여 줍니다(`certs.go`).
- **`Ensure` 를 요청 안에서 기다리지 않습니다.** 첫 판본이 120초에 끊겼습니다(`ownstate.go`). 마련은 백그라운드, 문은 `phase` 만 답합니다.

## 7. 알려진 틈

- `-race` 가 잡는 둘: `hand.go` 허브의 맵 접근, `own` 픽스처의 `Alive` 교체(TESTING §8).
- 모델이 도구 없이 말만 하고 끝내는 턴은 `land` 플러그인이 세지만 막지는 못합니다(TOOLS §5).
- 헬퍼가 죽을 때 등록을 떼는데(`DetachAll`), 데몬이 느리면 "정리 중에 시한이 지났습니다" 를 남기고 나갑니다. 다음 `settle` 이 어차피 떼고 붙이므로 남은 등록은 해가 없습니다.
