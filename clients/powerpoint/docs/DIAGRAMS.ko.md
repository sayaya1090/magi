# magi PowerPoint 클라이언트 — 다이어그램

[매뉴얼](MANUAL.ko.md) · [아키텍처](ARCHITECTURE.ko.md) · [모델이 받는 것](TOOLS.ko.md) · [시험](TESTING.ko.md) · [↑ 설계](../DESIGN.md)

> **현행 참고 문서.** [ARCHITECTURE.ko.md](ARCHITECTURE.ko.md) 의 그림판입니다. 축은 하나 — 프로세스 경계(L0)에서 파일 안의 상태 기계(L5)까지. 전부 mermaid 이고, 각 그림의 출처 파일을 밑에 적었습니다.

| 층 | 무엇을 보이나 | 단위 |
|---|---|---|
| [L0](#l0--프로세스-넷과-그-경계) | 프로세스 넷과 그 경계 | 프로세스 |
| [L1](#l1--한-턴이-지나는-길) | 한 턴이 지나는 길 | 요청 |
| [L2](#l2--덱-하나에-대화-하나--붙는-순서) | 덱 하나에 대화 하나 — 붙는 순서 | 문(door) 호출 |
| [L3](#l3--재기동-네-사건) | 재기동 네 사건 | 이름 여섯 |
| [L4](#l4--헬퍼-안) | 헬퍼 안 | Go 타입 |
| [L5](#l5--작업창-안) | 작업창 안 | JS 층 |
| [L6](#l6--마련의-상태-기계) | 마련(`/api/own`)의 상태 기계 | phase |

## L0 — 프로세스 넷과 그 경계

```mermaid
flowchart LR
  subgraph PPT["PowerPoint (창마다 하나)"]
    Deck[(덱 .pptx)]
    Pane["작업창<br/>addin/src · Office.js"]
    Pane --- Deck
  end
  subgraph Helper["magi office (사용자당 하나, :3000 의 /ppt)"]
    Page["/taskpane.html<br/>토큰이 박힌 페이지"]
    Hand["/hand/stream (SSE)<br/>/hand/reply (POST)"]
    API["/api/* 문(main.go 가 센다)"]
    MCP["/mcp<br/>Streamable HTTP"]
  end
  subgraph Daemon["magi --daemon (machine 에 하나)"]
    S1["대화 s_98eb…<br/>ppt 48 · 주인=s_98eb…"]
    S2["대화 s_b63e…<br/>ppt 48 · 주인=s_b63e…"]
    Core["코어 도구 26 · 선언 게이트(landing)"]
  end
  Shim["모델 심 :58415<br/>agy → Gemini"]
  Pane -- https --> Page
  Pane -- SSE --> Hand
  Pane -- https --> API
  API -- "unix socket<br/>Hello · AttachMCP · Submit …" --> Daemon
  Daemon -- "tools/call ?deck=" --> MCP
  Daemon -- "/v1/chat/completions" --> Shim
```

출처: `helper/main.go`(라우트), `helper/attach.go`(문 호출), 실측 2026-09-05 09:30(데몬 pid 23062, 대화 둘).

## L1 — 한 턴이 지나는 길

사용자가 창에 "IR 자료 만들어" 를 넣은 뒤. 굵은 화살표가 덱을 실제로 바꾸는 자리입니다.

```mermaid
sequenceDiagram
  participant U as 사람
  participant P as 작업창
  participant H as 헬퍼
  participant D as 데몬(대화 s_…)
  participant M as 모델
  U->>P: 글 입력
  P->>H: POST /api/submit?deck=K {text}
  H->>D: Submit{session}
  H-->>P: 202 (답은 스트림으로)
  D->>M: 메시지 + 도구 목록(코어 26 · ppt 48)
  M-->>D: tool-call mcp__ppt__add_slides{slides:[…]}
  D->>P: permission.asked (Transcript 스트림)
  U->>P: 허용
  P->>H: POST /api/permission
  H->>D: 답
  D->>H: POST /mcp?deck=K tools/call
  Note over H: args.go 검사 → hand.go pick(K)
  H->>P: SSE call{kind, data}
  P->>P: OfficeHand → Office.js → context.sync()
  P->>H: POST /hand/reply {result, now}
  H-->>D: MCP 응답
  D->>M: tool-result
  M-->>D: council{complete: true}
  D->>D: 선언 게이트(렌더 수 · 제목 ⚠) → 카운슬
  D-->>P: part.appended … (Transcript)
```

출처: `addin/src/usecase/SendTurn.js`, `helper/bridge.go`(Submit·permission), `helper/mcp.go`, `helper/hand.go pick`, `addin/src/usecase/ServeHand.js`. `now` 는 `OfficeHand.#dispatch` 가 변이 결과 안에 넣습니다.

## L2 — 덱 하나에 대화 하나 — 붙는 순서

창이 열린 직후. 헬퍼는 결정을 기억하지 않고 매번 셋에게 묻습니다.

```mermaid
sequenceDiagram
  participant P as 작업창
  participant H as 헬퍼 (settle)
  participant D as 데몬
  P->>H: GET /hand/stream (SSE 열기)
  H-->>P: hello{document: K}
  Note over P: K 를 안 뒤에야 own 을 부른다(≤3s 대기)
  P->>H: POST /api/own?deck=K
  alt 마련 중
    H-->>P: {phase: working}
    Note over H: provision(): Ensure → waitForFleet → Done(Ready)
  else 마련됨
    H->>H: settle(K, rep) — settling 뮤텍스
    H->>H: Bridges[K].BoundTo() → (socket, sid, life, tools)
    alt sid 있고 socket·life 같음
      Note over H: 아무것도 안 함 (창 다시 열림)
    else
      H->>D: NewSessionKeeping() → sid'
      H->>D: DetachMCP(sid', "ppt")
      H->>D: AttachMCP(sid', "ppt", /mcp?deck=K, Bearer) → 48
      H->>H: Bridges[K].BindWith(socket, sid', life, tools)
    end
    H-->>P: {phase: ready, session: sid, tools: 48}
  end
  loop WatchPrompt.poll
    P->>H: GET /api/status?deck=K
    H-->>P: {session, reachable, stale}
    Note over P: 대화가 생기면 ReadTranscript.attach(sid) · 브랜드에 「대화 s_…」 한 번
  end
```

출처: `addin/src/main.js:688–760`, `helper/main.go settle`, `helper/bridges.go`, `helper/attach.go Attach`. 멱등과 동시 호출은 `helper/tests/join_deck_test.go` 가 못박습니다.

## L3 — 재기동 네 사건

이름 여섯 중 무엇이 살아남고 무엇이 지워지는지. ✓ 같음 · ✗ 사라짐 · ? 헬퍼가 모름.

```mermaid
flowchart TB
  subgraph names["이름 여섯 (누가 주나)"]
    K["덱 키 pid-deck-…<br/>창 · MAGI.DECK 태그"]
    SID["대화 s_…<br/>데몬"]
    SOCK["소켓 경로<br/>데몬"]
    OWN["주인 = 대화<br/>헬퍼"]
    REG["등록 ppt+주인<br/>데몬 메모리"]
    LIFE["데몬 수명<br/>Hello"]
  end
  E1["창 다시 열림<br/>K✓ SID✓ REG✓ LIFE✓<br/>→ settle 이 아무것도 안 함"]
  E2["헬퍼 재기동<br/>K✓ SID? REG✓ LIFE✓<br/>→ 새 대화 + 떼고 붙임"]
  E3["데몬 재기동<br/>K✓ SID✗ REG✗ LIFE 바뀜<br/>→ 새 대화 + 재등록 · 창은 stale"]
  E4["PowerPoint 재기동<br/>K✓(태그) SID✓ REG✓ LIFE✓<br/>→ E1 과 같음"]
  names --> E1 & E2 & E3 & E4
```

출처: `DESIGN.md §5.9`(표), `helper/tests/restart_events_test.go`(네 열을 각각 시험).

## L4 — 헬퍼 안

```mermaid
classDiagram
  class API {
    Port int
    Token string
    Own *Own
    Work *Work
    Bridges *Bridges
    Attachments
    own(w, r)
    settle(deck, rep) OwnReport
    provision()
  }
  class Bridges {
    map deck → *Bridge
    Holder(session) deck
    Bindings() []Binding
    AttachedTo(socket, life) bool
  }
  class Bridge {
    socket, session, life string
    tools []string
    BoundTo()
    BindWith(socket, sid, life, tools)
    Submit(text) / Steer / Interrupt
    Status() / Transcript()
  }
  class Attachments {
    <<stateless>>
    Fleet(configDir, ours) []Candidate
    Attach(socket, url, token, owner) []string
    DetachAll(bound []Binding)
  }
  class Own {
    Ensure() socket
    Alive(socket) bool
  }
  class Work {
    Begin() (report, mine)
    Done(OwnReport)
    Forget()
    stuckAfter 3m
  }
  class Hub {
    conns map key → *conn
    pick(document, deck) *conn
    run(conn, op, args) result
  }
  class MCPServer {
    Hand *Hub
    tools/list → tools.go (48)
    tools/call → args.go → checkBullets → Hub.pick
  }
  API --> Bridges
  API --> Attachments
  API --> Own
  API --> Work
  Bridges --> Bridge
  MCPServer --> Hub
  API --> Hub : /hand/stream · /hand/reply
```

출처: `helper/main.go`(`API`), `helper/bridges.go`, `helper/bridge.go`, `helper/attach.go`, `helper/own.go`, `helper/ownstate.go`, `helper/hand.go`, `helper/mcp.go`, `helper/args.go`, `helper/bullets.go`.

## L5 — 작업창 안

안쪽 층은 바깥을 모릅니다. 화살표는 의존 방향입니다.

```mermaid
flowchart LR
  subgraph domain["domain (1,298줄)"]
    Transcript
    Pending
    Suggestion
    AdviceBoard
    Composer
    Cursor
    Quote
  end
  subgraph port["port (220줄)"]
    ChatPort["ChatPort{submit}"]
    DeckPort["DeckPort{selection, point, slideNumbers, capabilities}"]
    HandPort["HandPort{run, ops}"]
    StatusPort["StatusPort{status, answerPermission, answerQuestion}"]
    TranscriptPort["TranscriptPort{subscribe}"]
  end
  subgraph usecase["usecase (767줄)"]
    SendTurn
    WatchPrompt
    ReadTranscript
    ServeHand
    QuoteSelection
    PointAtAdvice
  end
  subgraph adapter["adapter (7,354줄)"]
    OfficeHand["OfficeHand (4,065)<br/>도구 48 의 실행"]
    OfficeDeck["OfficeDeck<br/>stableDeckId · MAGI.DECK"]
    HelperStream["HelperStream (SSE)"]
    HelperPorts["HelperChat · HelperStatus · HelperTranscript"]
    ooxml["chartxml · animxml · notesxml · eaxml · zip"]
    Fakes["Fake* 다섯 (데모·시험)"]
  end
  subgraph ui["ui + main.js (3,160줄)"]
    view
    screen
    main["main.js (조립)"]
  end
  usecase --> domain
  usecase --> port
  adapter -.구현.-> port
  OfficeHand --> ooxml
  main --> usecase
  main --> adapter
  main --> ui
```

출처: `addin/src` 파일별 `wc -l`(2026-09-05). `OfficeHand` 가 큰 이유는 [ARCHITECTURE §3](ARCHITECTURE.ko.md#3-작업창--네-층).

## L6 — 마련의 상태 기계

`/api/own` 이 답하는 `phase`. 마련은 요청 안에서 기다리지 않습니다(첫 판본이 120초에 끊겼습니다, `ownstate.go`).

```mermaid
stateDiagram-v2
  [*] --> idle
  idle --> working: Begin() — 첫 호출이 provision() 을 띄움
  working --> ready: Done(Ready) — Ensure → waitForFleet → chooseable
  working --> failed: Done(Failed) — 사유는 한 번 읽힐 때까지 보관(unread)
  working --> working: stuckAfter 3m 전까지 같은 답
  working --> idle: stuckAfter 지남 → 다음 Begin 이 다시 띄움
  failed --> working: 읽힌 뒤의 다음 Begin
  ready --> ready: settle(deck) — 소켓·수명 같으면 그대로
  ready --> idle: Forget() — 수명(life) 불일치
```

출처: `helper/ownstate.go`(`Work`, `stuckAfter`, `unread`), `helper/main.go own`. 실측: 2026-09-05 09:30 데몬 재기동 뒤 두 덱이 `working` → `ready` 로 가는 데 약 2분(마련 + 첫 settle).
