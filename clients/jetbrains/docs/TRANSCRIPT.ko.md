# 전사 셰이퍼 — 이벤트 스트림을 행으로

[↑ 화면 설계](./UI.ko.md)

> **무엇을 정하는 문서인가.** [화면 설계](./UI.ko.md) §2.1 이 「행은 이 클라이언트가 짓는다」로
> 정했다. 이 문서는 그 셰이퍼 하나를 구현 수준으로 정한다 — 입력 스트림의 실측, 행 모델의 어휘,
> 이벤트→행 매핑, 그리고 골든이 무엇을 붙드는가. 아래 매핑의 근거는 전부 두 원본의 실측이다:
> 터미널(`internal/adapter/tui/model_event.go` 의 `onPartAppended` 와 그 이웃)과 웹의 셰이퍼
> (`cmd/magi-web/main.go` 의 `renderMessages`).

---

## 0. 원칙

- **얕게 짓는다.** 셰이퍼는 이벤트를 행 데이터로 펼 뿐, 문장을 짓지 않는다. 문장은 화면의 붓이다.
- **어휘는 웹 `line` 의 것이다.** 필드로 나르고, 렌더된 문장에서 도로 파내지 않는다.
- **문은 스트림이다.** 로그 파일을 재유도하지 않는다. 스트림이 안 싣는 것은 이 문서의 §7에
  적히고, 데몬에 요청할 일이 생기면 그 항목으로 한다.

## 1. 입력 — `transcript` 스트림의 실측

봉투는 `Wire.kt` 의 `LogEvent` 다: `seq · type · actor(kind,name) · ts · data`. `data` 는
타입마다 모양이 다르고, 그것을 파는 것이 셰이퍼의 일이다(같은 층의 `Problems.of` 가 선례).

**사실만 재생된다.** 코어가 이벤트를 둘로 가른다(`internal/core/event/event.go` 의
`transientTypes`) — 사실은 저장되고 재생되며, 전이(`part.delta` · `tool.progress` ·
`permission.requested` · `question.requested` · `context.usage` · `workflow.phase` ·
`council.deliberating`)는 라이브로 붙어 있는 동안만 온다. 따라서:

- **같은 말이 두 번 온다.** `part.delta` 조각들 뒤에 같은 `messageId` 의 `part.appended`
  사실이 온다. 조각에 행을 주면 답이 두 벌 쌓이고, 재생분에는 조각이 없어 **다시 붙은 창과
  붙어 있던 창이 갈린다**. 그래서 조각은 행이 아니다 — `Transcript.kt` 의 `echoesFact` 가
  이 술어다. (조각으로 같은 행을 고쳐 쓰는 라이브 타자기는 §8.)
- **물음은 행이 아니라 신호다.** `permission.requested` 류 넷(`Transcript.kt` 의
  `movesPrompt`)은 이벤트 내용으로 그리지 않고 「프롬프트를 다시 물어라」로만 쓴다 — 재생이
  지나간 물음을 지금 것으로 그리는 사고를 막는 근거가 그 술어의 주석에 있다. 기존 그대로다.
- **커서.** `since` 없으면 전량(스토어 `internal/adapter/store/jsonl/jsonl.go` 의
  `filterFrom` 규칙). 세션이 바뀌면 커서를 버리고 전량을 다시 받는다 — 기존 그대로.

## 2. 행 모델

웹 `line` 의 어휘를 코틀린으로 옮긴다. usecase 층의 데이터 클래스 하나:

```kotlin
data class Row(
    val who: Who,            // User | Agent | Thinking | Tool | Council | Info
    val text: String,        // 본문 (tool 이면 이름, council 이면 판정 요지)
    val at: String? = null,  // 이벤트 ts — 웹처럼 행마다 싣는다
    // tool 행
    val tool: String? = null, val args: String? = null,
    val ok: Boolean? = null,  // null = 아직 결과 없음
    val note: Boolean = false, // 됐고 읽을 것이 있음(advisory) — 실패가 아니다
    val out: String? = null,   // 실패한 호출이 말한 것 — args 를 덮지 않는다
    // user 행 표시
    val pending: Boolean = false, val queued: Boolean = false, val abandoned: Boolean = false,
    // council 행
    val member: String? = null, val round: Int = 0, val decision: String? = null,
    val lens: String? = null, val why: String? = null, val keep: String? = null,
    val cite: String? = null,
    // 셰이퍼 내부 짝맞춤 열쇠 — 화면은 안 읽는다
    val msgId: String = "", val callId: String = "",
)
```

웹에 있는데 1판에서 빼는 것과 그 사유: `diff`(웹은 서버가 호출 인자에서 지어 보냈다 — 여기선
지을 자가 이 클라이언트뿐이고, IDE 는 diff 뷰어를 이미 가졌으니 §8 에서 그 뷰어로 연다),
`confidence`·`feedback`·`by`(그릴 자리를 아직 안 정했다 — 어휘에는 있고 행에는 나중에 얹는다).

## 3. 이벤트 → 행 매핑

| 이벤트 | 행 | 근거(원본 실측) |
|---|---|---|
| `prompt.submitted` (actor=user) | **User 행 추가.** `parts` 의 텍스트를 합친다 | 웹·TUI 공통 |
| `prompt.submitted` (actor=agent) | 없음 — 서브에이전트 보고 주입은 소음 | TUI 가 삼킨다 |
| `prompt.submitted` (actor=system) | **Info 행** — `⟳ <actor> note: <첫 줄>` | TUI: 플래너·카운슬 노트를 안 그리면 화면이 헤드리스보다 덜 보여 준다 |
| `prompt.submitted` + `resurfacedFrom` | **재배치** — 그 id 의 User 행을 끝으로 옮기고 본문 갱신, queued 해제 | TUI: 물음이 답 위에 서는 짝 |
| `part.delta` | 없음 (§1) | `echoesFact` |
| `part.appended` kind=`text` | **Agent 행 추가.** `inReplyTo` 가 있으면 그 User 행을 이 답 위로 재배치 | TUI `onPartAppended` |
| `part.appended` kind=`reasoning` | **Thinking 행 추가** — 화면이 접어 그린다 | 웹 `who:"thinking"` |
| `part.appended` kind=`tool-call` | **Tool 행 추가** — 이름·인자·callId | 호출과 결과는 한 행의 절반 |
| `part.appended` kind=`tool-result` | **새 행 없음** — callId 로 Tool 행을 찾아 `ok=!isError`, `note=advisory`, 실패면 `out` 채움 | 웹: 갈라 그리면 "됐나"를 찾아 열어야 안다 |
| `permission.*` · `question.*` | 행 없음 — 다시 묻는 신호(§1) | `movesPrompt` |
| `interjection.deferred` | 그 User 행에 **queued 표시** | TUI 의 대기 글리프 |
| `interjection.answered` | 그 User 행을 마지막 Agent 행 위로 **재배치**, queued 해제 | TUI: 이미 제자리여도 글리프는 거둔다 |
| `prompt.abandoned` | 그 User 행에 **abandoned 표시** — 행 추가가 아니라 표시 | 취소된 요청이 무시된 질문으로 읽히면 안 된다 |
| `compaction` | **Info 행** — `↯ context compacted ~전→후 tok`. **지우지 않는다** (§4) | TUI 의 한 줄 + `reconstructWhole` 의 교훈 |
| `turn.finished` | 행 없음 — 턴 닫힘(§5), usage 는 상태 표시줄 몫 | |
| `error` | **Info 행** — `recovered` 면 그렇게 말한다. 회복된 에러는 끝이 아니다 | 코어의 「recovered ≠ ending」 |
| `council.verdict` | **Council 행 추가** — member·decision·rationale·keep·cite. `silent` 는 "답 없음"으로 | 자리색 셋 밖은 색 없음(`Look.seat`) |
| `council.decided` | **Council 행 추가** — 라운드 결과·tally·note, continue 면 feedback 줄들 | `CouncilDecidedData` 의 `FeedbackLines` 가 양 화면 공용으로 산다 |
| `council.convened` | 행 없음 — 라운드 고유 정보가 없다 | TUI 가 같은 사유로 지웠다 |
| `todos.changed` `labels.changed` `model.changed` `session.moved` `context.usage` `tool.progress` `workflow.phase` `result.elided` `user.label.changed` `session.created` | 행 없음 — 전사가 아니라 다른 자리의 사실 | |

**카운슬에 splice 가 없다** (화면 설계 문서의 옛 §8-2 의 답): 웹이 끼워 맞춘 이유는 입력이 **메시지로
재구성된 로그**라 카운슬 마크를 따로 읽었기 때문이다(`spliceCouncil`). 이 스트림은 사실을
**일어난 차례대로** 싣고 카운슬 이벤트도 그 안에 있다 — 제자리에 온다.

## 4. 지우는 사건은 없다 (옛 §8-1 의 답)

실측: **컴팩션은 이벤트를 지우지 않는다.** 로그는 전부 남고, 모델에게 보낼 창만 접힌다 —
`internal/app/reconstruct.go` 의 `reconstructWhole` 주석이 그 결함의 기록이다: 사람 뷰를 모델
뷰(`reconstruct`)에서 읽으면 **읽는 중이던 스크롤백이 컴팩션 순간 제 요약으로 바뀐다**(라이브
콘솔 보고). 사람 뷰는 접지 않는다. 이 셰이퍼는 사람 뷰다.

그래서 증분 셰이퍼가 안전하다. 스트림이 시키는 변이는 다섯뿐이고 전부 목록 안 제자리 수정이다:
재배치 둘(재부상, 인라인 답), 표시 둘(queued·abandoned), 접붙임 하나(tool-result). 통짜 재생성이
필요한 것은 **스트림 자체가 다시 시작할 때**(재접속 전량 재생, 세션 이동)뿐이다 — 그때는 행
목록을 비우고 처음부터 짓는다. `Sink.began` 이 그 신호다.

## 5. 턴과 pending

- **턴 열림** = 마지막 `prompt.submitted`(user) 뒤에 `turn.finished` 가 아직 없다. 경과는 그
  이벤트의 `ts` 부터 IDE 시계로 센다 — 두 기계의 시계 비교는 하지 않는다.
- **pending** = 웹 `markPending` 의 규칙: 턴이 열려 있을 때, 답 없는 마지막 User 행과 결과
  없는 Tool 행. 행의 사실이므로 행에 싣는다. 턴 경과는 세션의 사실이므로 행에 안 싣는다
  (마지막 행이 무엇이냐에 따라 뜻이 달라진다 — 웹이 프레임을 가른 사유).

## 6. 어디 살고 누가 부르나

셰이퍼는 `plugin/core` usecase 층의 순수 클래스다 — `LogEvent` 를 받아 `List<Row>` 를 유지하고,
바뀌면 화면에 통짜 목록을 준다(행 수가 수백이고 Swing 재그리기가 밀리초라 diff 통지는 §8 전까지
필요 없다). 스트림 소유권은 지금 그대로 — `Transcript` 가 단독 소유, 화면은 `Sink` 로 구독.
셰이퍼는 그 `Sink` 와 화면 사이에 선다: `frame` 을 먹고, `began` 에 비우고, 목록을 내놓는다.

`MagiToolWindow.entry` 는 이 목록을 그리는 코드로 바뀐다 — `#seq type` 을 적는 지금 몸은
없어진다. 문제 판(`Problems.of`)은 같은 스트림을 계속 따로 읽는다 — 출처가 다른 두 글이라는
구분(전부 /  사람이 손댈 것)은 그대로다.

## 7. 골든이 붙드는 것

이벤트 JSONL 픽스처(실제 세션 저장분 `~/.magi/sessions` 에서 추려 익명화)를 넣고 행 목록을
견준다. 최소 다섯 벌:

1. **몸통.** user 프롬프트와 agent 답이 행에 있다 — 지금 화면이 `#seq type` 만 적어도 초록인
   그 구멍을 막는 시험이 첫째다.
2. **호출+결과 한 행.** 병렬 호출이 순서 밖으로 완료돼도 callId 짝이 맞고, advisory 는
   `ok=true·note=true` 다.
3. **delta 무행.** 조각 여럿 + 사실 하나 = 행 하나.
4. **재부상 재배치.** 재부상·인라인 답·abandoned 가 행을 늘리지 않고 옮기거나 표시한다.
5. **컴팩션 비삭제.** compaction 이벤트 앞의 행들이 그대로 있고 Info 행 하나가 는다.

## 8. 나중

- **라이브 타자기** — `part.delta` 로 같은 행을 고쳐 쓰기. `echoesFact` 가 "새 말 아님"을
  보증하므로 안전하게 얹을 수 있다.
- **diff** — 편집 호출의 인자에서 IDE diff 뷰어를 열기.
- **`confidence`·`feedback`·`by`** 를 행에 얹기, `result.elided` 표시.
- **diff 통지** — 목록이 커져 통짜 재그리기가 보이는 지연이 되면 그때.
