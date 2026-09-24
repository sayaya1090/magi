# VS Code 확장 — 시스템 구조도

[매뉴얼](MANUAL.ko.md) · [화면 설계](UI.ko.md) · [설계](DESIGN.ko.md) · [테스트 안내](TESTING.ko.md) · [플랫폼 규약](PLATFORM.ko.md) · [↑ 루트 다이어그램](../../../docs/DIAGRAMS.ko.md)

> **현행 참조 문서.** [DESIGN.ko.md](DESIGN.ko.md) 와 [UI.ko.md](UI.ko.md) 의 시각적 짝입니다. 프로세스 경계(L0)부터 에디터/웹뷰 상호작용 및 상태 기계(L6)까지 mermaid 다이어그램으로 기술하며, 각 그림의 하단에 근거 소스 파일과 주요 심볼을 명시합니다.

| 층 | 보는 것 | 단위 |
|---|---|---|
| [L0](#l0--프로세스와-신뢰-경계) | 프로세스와 신뢰 경계 | 프로세스 및 격리 영역 |
| [L1](#l1--한-턴의-수명주기-입력에서-착지까지) | 한 턴의 수명주기 (입력에서 착지까지) | 단계별 이벤트 및 통신 흐름 |
| [L2](#l2--2계층-모듈-구조와-컴포넌트-맵) | 2계층 모듈 구조와 컴포넌트 맵 | `src/core`, `src/ide`, `src/web` |
| [L3](#l3--호스트웹뷰-통신-프로토콜-매트릭스) | 호스트↔웹뷰 통신 프로토콜 매트릭스 | 메시지 스키마 및 이벤트 흐름 |
| [L4](#l4--데몬-연결-수명주기-및-재기동-상태-기계) | 데몬 연결 수명주기 및 재기동 상태 기계 | Phase 및 재시도 상태 |
| [L5](#l5--초안-복구-및-dom-포커스선택-보존-흐름) | 초안 복구 및 DOM 포커스/선택 보존 흐름 | DraftBackup 및 Keyed Node 갱신 |
| [L6](#l6--에디터-네이티브-통합-및-mcp-도구-제어-브리지) | 에디터 네이티브 통합 및 MCP 도구 제어 브리지 | 가상 문서(Diff/Output) 및 MCP Hand |

---

## L0 — 프로세스와 신뢰 경계

VS Code 확장은 Node.js 기반 Extension Host 프로세스에서 실행되며, 보안상 격리된 브라우저 컨텍스트의 Webview 프로세스, 그리고 워크스페이스 단위로 상주하는 백그라운드 `magi --daemon` 프로세스와 통신합니다.

```mermaid
%%{init: {'theme':'neutral','flowchart':{'curve':'basis'}}}%%
flowchart LR
  user(["사용자"])

  subgraph vscode["VS Code 환경"]
    subgraph editor["에디터 표면"]
      buf["TextEditor 버퍼<br/>선택 영역 · 진단 · 눈금자"]
      inline["Inline Completions<br/>고스트 텍스트 (magi.complete)"]
      inlay["Inlay Hints · 상단 띠<br/>타이핑 훑어보기 (magi.lookWhileTyping)"]
      gutter["Gutter Decorations<br/>수정 라인 강조 (magi.showEditMarks)"]
    end

    subgraph exthost["Extension Host 프로세스 (Node.js)"]
      ext["extension.ts · activate()"]
      coreMod["src/core (순수 도메인 로직)<br/>daemon · workspace · protocol · diff · recovery"]
      ideMod["src/ide (VS Code API 바인딩)<br/>chat · plan · status · doors · hand"]
      mcpSrv["MCP HandServer (루프백 HTTP)<br/>mcp__vscode__show / apply_edit / problems"]
      virtDiff["DiffProvider (magi-diff:)<br/>vscode.diff 사이드 바이 사이드"]
      virtOut["OutputProvider (magi-output:)<br/>구문 강조 출력 에디터 탭"]
    end

    subgraph webviews["격리된 웹뷰 프로세스"]
      chatView["하단 대화 패널<br/>magi.chat WebviewView<br/>chat_adapter · dom_interaction · markdown_render"]
      planView["우측 보조 사이드바<br/>magi.plan WebviewView<br/>now · plan · context · jobs · scheduled · fleet · handed"]
    end
  end

  subgraph daemonProc["magi 백그라운드 프로세스 (워크스페이스 전용)"]
    daemon["magi --daemon<br/>도메인 소켓 / Named Pipe"]
    engineCore["app.App 엔진 코어<br/>세션 영속 · 카운슬 합의 게이트 · 턴 실행"]
  end

  llmBackend[("LLM 백엔드 / Ollama")]
  workspaceFs[("워크스페이스 파일시스템")]

  user -->|"키 입력 · 클릭"| editor
  user -->|"메시지 입력 · 선택지 클릭"| chatView
  user -->|"계획 및 계기판 확인"| planView

  chatView <-->|"postMessage (WebviewProtocol)"| ideMod
  planView <-->|"postMessage (WebviewProtocol)"| ideMod
  ideMod --- coreMod
  ext --> ideMod

  editor <-->|"VS Code API 바인딩"| ideMod

  ideMod <-->|"AF_UNIX / ide-bridge 중계<br/>JSON-RPC / NDJSON 스트림"| daemon
  daemon --> engineCore
  engineCore <-->|"도구 호출 (mcp__vscode__*)"| mcpSrv
  engineCore <-->|"/v1/chat/completions"| llmBackend
  engineCore <-->|"파일 I/O"| workspaceFs
```

출처: `src/ide/extension.ts` (`activate`), `src/ide/chat.ts` (`Chat`), `src/ide/plan.ts` (`Plan`), `src/ide/hand.ts` (`Hand`), `src/core/workspace.ts` (`canonicalWorkspaceKey`).

---

## L1 — 한 턴의 수명주기 (입력에서 착지까지)

사용자가 프롬프트를 제출한 시점부터 데몬 이벤트 스트리밍, 승인 절차, 카운슬 검토 및 턴 종료까지의 전체 흐름입니다.

```mermaid
sequenceDiagram
  autonumber
  actor User as 사용자
  participant WV as 대화 웹뷰 (Panel)
  participant Host as 확장의 Chat 제공자
  participant Daemon as magi 데몬 (대화 세션)
  participant Model as LLM 모델
  participant Council as 3인 카운슬

  User->>WV: 프롬프트 입력 및 Enter 전송
  WV->>WV: aria-busy 설정 및 composer 비활성화
  WV->>Host: postMessage({kind: 'say', text, refs})
  Host->>Daemon: call('submit', {session, text, refs})
  Daemon-->>Host: {ok: true} (작업 접수)

  loop 이벤트 스트리밍 (NDJSON)
    Daemon->>Host: event {type: 'part.delta', delta: '...'}
    Host->>WV: postMessage({kind: 'rows', rows: [...]})
    WV->>WV: markdown_render 점진 렌더링
  end

  opt 위험 도구 실행 승인 (Permission Request)
    Daemon->>Host: event {type: 'permission.asked', callId, what, args, diff}
    Host->>WV: postMessage({kind: 'rows', rows: [ApprovalRow]})
    WV-->>User: allow · deny · always 버튼 및 diff 링크 표출
    alt 사용자 diff 클릭
      User->>WV: diff 검토 클릭
      WV->>Host: postMessage({kind: 'diff', callId})
      Host->>Host: openApprovalDiff() -> vscode.diff(magi-diff:)
    end
    User->>WV: allow 클릭
    WV->>WV: in-flight 락 적용 (aria-busy="true")
    WV->>Host: postMessage({kind: 'run', callId, decision: 'allow'})
    Host->>Daemon: call('permission', {callId, decision: 'allow'})
  end

  Daemon->>Model: 도구 결과 주입 및 다음 토큰 요청
  Model-->>Daemon: 작업 완료 보고

  opt 카운슬 합의 판정 (Council Review)
    Daemon->>Council: 3인 위원(melchior, balthasar, casper) 독립 검토
    Council-->>Daemon: 표결(approve/reject) 및 판정 근거(on/keep)
    Daemon->>Host: event {type: 'council.decided', round, verdict, voters}
    Host->>WV: postMessage({kind: 'rows', rows: [CouncilRow]})
    WV->>WV: 고정폭 근거 유지 렌더링
  end

  Daemon->>Host: event {type: 'turn.finished', turn}
  Host->>WV: postMessage({kind: 'rows', rows: [FinishedRow]})
  WV->>WV: 진행 인디케이터 소거 및 composer 재활성화
```

출처: `src/ide/chat.ts` (`onDidReceiveMessage`, `watchSession`), `src/web/chat_adapter.ts` (`WebviewInputAdapter`), `src/core/transcript.ts` (`foldEventsToPaintedRows`).

---

## L2 — 2계층 모듈 구조와 컴포넌트 맵

JetBrains 클라이언트와 동일하게 순수 Node.js 도메인 코어(`src/core/`)와 IDE API 연동 계층(`src/ide/`), 그리고 웹뷰 번들(`src/web/`)로 명확히 분리됩니다. `src/core/`는 `vscode` 모듈을 절대 import하지 않아 `node:test`로 완전 격리 테스트가 가능합니다.

```mermaid
%%{init: {'theme':'neutral','flowchart':{'curve':'basis'}}}%%
classDiagram
  direction TB

  namespace CoreLayer {
    class DaemonClient {
      +call(door, args) Promise~Response~
      +requestStream(door, args) Observable
      +subscribeEvents(session) Observable~Event~
      +phase: Phase
    }
    class WorkspaceDiscovery {
      +canonicalWorkspaceKey(root) string
      +socketPathFor(root) string
      +discoverActiveDaemon(root) Promise
    }
    class TranscriptFold {
      +foldEventsToPaintedRows(events) PaintedRow[]
      +detectCouncilRound(events) RoundInfo
    }
    class RecoveryStateManager {
      +register(options) RecoveryItem
      +listItems(filter) RecoveryItem[]
      +deleteItem(recoveryId) boolean
      +getFailedDrafts(callId) string[]
    }
    class ApprovalSnapshots {
      +put(uri, content) boolean
      +get(uri) string
      +evictExcess() number
      +protectTemp(uris) Function
    }
    class OutputSnapshots {
      +put(uri, content) boolean
      +get(uri) string
      +evictExcess() number
    }
    class WebviewProtocol {
      +parseWebviewToHost(raw) WebviewToHostMessage
      +parseHostToWebview(raw) HostToWebviewMessage
    }
  }

  namespace IdeLayer {
    class ExtensionEntry {
      +activate(context)
      +deactivate()
    }
    class ChatViewProvider {
      +resolveWebviewView(webviewView)
      +postMessage(message)
      -handleWebviewMessage(msg)
    }
    class PlanViewProvider {
      +resolveWebviewView(webviewView)
      +pollPlanSections()
    }
    class StatusBarManager {
      +updateStatus(state, model)
      +showQuickPickMenu()
    }
    class InlineCompletionProvider {
      +provideInlineCompletionItems(doc, pos)
    }
    class LookProvider {
      +onBufferPause(doc)
      +applyInlaysAndBanner(critiques)
    }
    class EditMarkerManager {
      +highlightModifiedLines(editor, ranges)
      +clearMarkers()
    }
    class HandMcpServer {
      +startLoopbackServer()
      +dispatchTool(name, args)
    }
  }

  namespace WebviewLayer {
    class WebviewInputAdapter {
      +submit()
      +handleTab()
      +handleCompose()
      +invalidateSuggest()
    }
    class DomInteraction {
      +captureSelection(root)
      +restoreSelection(root, bookmark)
      +moveDomChild(parent, node, index)
    }
    class RecoveryController {
      +refreshRecoveryUI()
      +restoreDraft(recoveryId)
      +discardDraft(recoveryId)
    }
    class MarkdownRenderer {
      +render(markdownText) string
      +renderCouncil(onText, keepText)
    }
  }

  ExtensionEntry --> ChatViewProvider
  ExtensionEntry --> PlanViewProvider
  ExtensionEntry --> StatusBarManager
  ExtensionEntry --> InlineCompletionProvider
  ExtensionEntry --> LookProvider
  ExtensionEntry --> EditMarkerManager
  ExtensionEntry --> HandMcpServer

  ChatViewProvider --> DaemonClient
  ChatViewProvider --> TranscriptFold
  ChatViewProvider --> RecoveryStateManager
  ChatViewProvider --> ApprovalSnapshots
  ChatViewProvider --> OutputSnapshots
  ChatViewProvider --> WebviewProtocol

  ChatViewProvider ..> WebviewInputAdapter : postMessage 통신
  WebviewInputAdapter --> DomInteraction
  WebviewInputAdapter --> RecoveryController
  WebviewInputAdapter --> MarkdownRenderer
```

출처: `src/core/daemon.ts`, `src/core/workspace.ts`, `src/core/transcript.ts`, `src/core/recovery_state.ts`, `src/core/diff.ts`, `src/core/output.ts`, `src/ide/chat.ts`, `src/ide/plan.ts`.

---

## L3 — 호스트↔웹뷰 통신 프로토콜 매트릭스

웹뷰와 호스트는 Valibot으로 선언된 스키마에 따라 메시지를 주고받습니다. 타입 불일치 및 정의되지 않은 메시지는 조기에 거절됩니다.

```mermaid
flowchart TD
  subgraph WebviewToHost["웹뷰 → 호스트 (WebviewToHostMessage)"]
    direction TB
    m_ready["ready: 웹뷰 로드 완료 및 이벤트 재개 준비"]
    m_say["say: 일반 대화 전송 (text, refs)"]
    m_run["run: 도구 승인/거부 판정 (callId, decision)"]
    m_diff["diff: 네이티브 변경 검토기 호출 요청 (callId)"]
    m_open["open: 전사 내 링크 클릭 파일 열기 (path, line, col)"]
    m_output["output: 출력 자료 에디터 탭 열기 (outputId)"]
    m_answer["answer: 대화형 질의 답변 전송 (callId, answer)"]
    m_reply["reply: 질의 선택지 클릭 전송 (callId, option)"]
    m_mention["mention: @ 파일 자동완성 추천 요청 (query, reqId)"]
    m_suggest["suggest: 컴포저 입력 중 문구 추천 요청 (text, reqId)"]
  end

  subgraph HostToWebview["호스트 → 웹뷰 (HostToWebviewMessage)"]
    direction TB
    h_rows["rows: 전사 행 렌더링 갱신 (PaintedRow[])"]
    h_state["state: 컴패니언 연결 상태 및 복구 안내 (ActivityState, Note)"]
    h_info["info: 세션 및 런타임 환경 상세 카드 정보"]
    h_mentions["mentions: @ 파일 검색 후보 목록 반환 (items, reqId)"]
    h_suggestion["suggestion: 컴포저 추천 텍스트 반환 (hint, reqId)"]
    h_sessionCreated["sessionCreated: 세션 생성 결과 (id, conflict 여부)"]
    h_replyResult["replyResult: 질의 답변 전송 성공/실패 여부 (callId, ok)"]
    h_draftRecovery["draftRecovery: 미전송 초안 복구 데이터 목록"]
  end

  WebviewToHost <-->|"VS Code postMessage Bridge"| HostToWebview
```

출처: `src/core/webview_protocol.ts` (`WebviewToHostSchema`, `HostToWebviewSchema`), `src/web/chat_adapter.ts`.

---

## L4 — 데몬 연결 수명주기 및 재기동 상태 기계

소켓 탐색, 데몬 연결 수립, 비정상 종료 감지 및 자동 재시도 백오프 제어의 상태 전이 다이어그램입니다.

```mermaid
stateDiagram-v2
  [*] --> Absent: 확장 활성화 / 작업 폴더 열림

  Absent --> Starting: magi.startCompanion=true 또는 수동 start
  Absent --> Connected: 이미 실행 중인 소켓 발견

  Starting --> Connected: 30초 내 핸드셰이크 성공
  Starting --> Blocked: 기동 실패 3회 초과 (60초 윈도우)
  Starting --> Absent: 데몬 준비 시간 초과 / 바이너리 부재

  Connected --> Lost: 소켓 연결 단절 (EOF / ECONNRESET)
  Connected --> Closing: 창 닫힘 / restart 명령 수신

  Lost --> Backoff: 자신이 기동한 자식 프로세스인 경우
  Lost --> Absent: 외부에서 기동된 독립 프로세스인 경우 (자동 재기동 차단)

  Backoff --> Starting: 지수 백오프 대기 만료 (1s -> 2s -> 4s)
  Backoff --> Blocked: 재시도 상한 도달 (3회)

  Blocked --> Starting: 사용자가 수동으로 'Start one' 단추 클릭

  Closing --> Closed: SIGTERM 전송 및 정상 정리
  Closed --> [*]
```

출처: `src/core/lifecycle.ts` (`LifecycleManager`), `src/core/launches.ts` (`LaunchBudget`), `src/core/daemon.ts`.

---

## L5 — 초안 복구 및 DOM 포커스/선택 보존 흐름

네트워크 장애, 질문 만료 또는 세션 전환 시 입력 중이던 초안이 유실되지 않도록 보존하며, 전사 재렌더링 중에도 사용자의 텍스트 선택(Selection)과 포커스를 안전하게 유지합니다.

```mermaid
flowchart TD
  subgraph InputFailure["입력 및 전송 실패 감지"]
    sendFail["답변 전송 실패 (replyResult: ok=false)"]
    conflictFail["세션 생성 충돌 (sessionCreated: conflict=true)"]
    reloadTrigger["웹뷰 리로드 / 연결 단절 발생"]
  end

  subgraph RecoveryCore["src/core/recovery_state.ts"]
    reg["RecoveryStateManager.register()"]
    item[("RecoveryItem<br/>recoveryId · text · reason · kind")]
    broadcast["HostToWebview (draftRecovery) 발행"]
  end

  subgraph RecoveryUI["src/web/recovery_view.ts"]
    domKeyed["Keyed Node 재사용 (recoveryItemsEl)<br/>포커스 및 Selection 캡처"]
    drawBanner["복구 배너 노출 (카운트 뱃지)"]
    userAction{"사용자 액션"}
    restoreBtn["[복구] 클릭"]
    discardBtn["[폐기] 클릭"]
    applyText["입력창에 초안 복원 및 Focus 바인딩"]
    dropItem["RecoveryItem 제거 및 카운트 감소"]
  end

  sendFail --> reg
  conflictFail --> reg
  reloadTrigger --> reg
  reg --> item --> broadcast
  broadcast --> domKeyed --> drawBanner
  drawBanner --> userAction
  userAction -->|"복구"| restoreBtn --> applyText
  userAction -->|"폐기"| discardBtn --> dropItem
```

출처: `src/core/recovery_state.ts` (`createRecoveryState`), `src/web/recovery_controller.ts`, `src/web/dom_interaction.ts` (`captureSelection`, `restoreSelection`).

---

## L6 — 에디터 네이티브 통합 및 MCP 도구 제어 브리지

VS Code 에디터 버퍼, 가상 문서 제공자, 그리고 데몬이 역으로 호출하는 루프백 MCP Hand 도구 서버 간의 상호작용입니다.

```mermaid
flowchart LR
  subgraph Daemon["magi 데몬"]
    agent["에이전트 코어"]
  end

  subgraph Ext["VS Code 확장"]
    subgraph VirtDocs["가상 문서 제공자"]
      pDiff["DiffProvider (magi-diff:)<br/>ApprovalSnapshots (FIFO 100)"]
      pOut["OutputProvider (magi-output:)<br/>OutputSnapshots (FIFO 100)"]
    end

    subgraph NativeBridges["에디터 네이티브 브리지"]
      edits["EditMarkerManager<br/>수정 라인 데코레이션 하이라이트"]
      look["LookProvider<br/>타이핑 멈춤 시 인레이 힌트 표기"]
      comp["InlineCompletionProvider<br/>고스트 텍스트 자동완성"]
    end

    subgraph MCPServer["MCP HandServer (루프백 :port)"]
      mcpShow["mcp__vscode__show<br/>파일 열기 및 커서 이동"]
      mcpEdit["mcp__vscode__apply_edit<br/>에디터 버퍼 변경 적용"]
      mcpProb["mcp__vscode__problems<br/>언어 서버 진단 수집"]
    end
  end

  subgraph Surfaces["VS Code 에디터 화면"]
    diffTab["내장 diff 에디터 탭<br/>vscode.diff"]
    outTab["출력 에디터 탭<br/>setTextDocumentLanguage"]
    activeEditor["활성 텍스트 에디터"]
  end

  agent -->|"tools/call"| MCPServer
  mcpShow -->|"showTextDocument"| activeEditor
  mcpEdit -->|"applyEdit"| activeEditor
  mcpProb -->|"languages.getDiagnostics"| activeEditor

  pDiff -->|"provideTextDocumentContent"| diffTab
  pOut -->|"provideTextDocumentContent"| outTab

  edits --> activeEditor
  look --> activeEditor
  comp --> activeEditor
```

출처: `src/ide/diff.ts` (`DiffProvider`), `src/ide/output.ts` (`OutputProvider`), `src/ide/hand.ts` (`Hand`), `src/ide/edits.ts`, `src/ide/look.ts`, `src/ide/complete.ts`.
