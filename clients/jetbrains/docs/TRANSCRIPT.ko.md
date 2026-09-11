# 전사 셰이퍼 — 이벤트 스트림을 행으로 변환

[↑ 화면 설계](./UI.ko.md)

> **문서 목적.** [화면 설계](./UI.ko.md) §2.1에 따라 대화 행 데이터의 구성 책임을 클라이언트 계층에 둡니다. 본 문서는 이벤트 스트림을 UI 행 목록으로 가공하는 전사 셰이퍼(Transcript Shaper)의 상세 사양을 정의합니다. 터미널 TUI(`internal/adapter/tui/model_event.go`) 및 웹 콘솔(`clients/web/server/main.go`)의 실제 구현 실측을 바탕으로 작성되었습니다.

---

## 0. 설계 원칙

- **경량 변환 계층:** 셰이퍼는 이벤트를 구조화된 행 데이터로 투영하는 역할에 집중하며, 시각 서술 문장을 임의로 조합하지 않습니다. 표출 문장은 UI 렌더러가 담당합니다.
- **표준 어휘 공유:** 웹 콘솔의 `line` 필드 명명 규칙을 준용하여 정형화된 필드로 상태를 전달합니다.
- **스트리밍 단일 원천:** 파일시스템 로그를 우회 재해석하지 않고 소켓 이벤트 스트림만을 신뢰 원천으로 삼습니다.

## 1. 입력 사양 — `transcript` 스트림 실측

이벤트 봉투(Envelope)는 `Wire.kt`의 `LogEvent` 구조를 따릅니다: `seq · type · actor(kind,name) · ts · data`.

**사실(Fact) 이벤트 영속 및 재생 원칙:** 코어 엔진은 이벤트를 영속 사실과 전이(Transient) 상태로 구분합니다(`internal/core/event/event.go`의 `transientTypes`). 사실 이벤트는 영속화되어 재접속 시 재생되며, 전이 이벤트(`part.delta`, `tool.progress`, `permission.requested`, `question.requested`, `context.usage`, `workflow.phase`, `council.deliberating`, `question.answered`, `user.label.changed` 등 9종)는 실시간 세션 중에만 송출됩니다.

- **실시간 조각과 확정본의 정합성:** 실시간 스트리밍 중에는 `part.delta` 조각들이 수신된 후 동일한 `messageId`를 갖는 `part.appended` 확정본 이벤트가 도착합니다. 조각마다 독립된 행을 생성할 경우 메시지가 중복 누적되므로, `part.delta`는 초안 행을 실시간 갱신하고 확정본 수신 시 최종 내용으로 완전히 치환합니다.
- **질의성 이벤트의 성격:** `permission.requested` 등의 질의 이벤트는 전사 본문 행이 아닌 상태 알림 신호로 취급하여 입력창 상단에 바인딩합니다.
- **시퀀스 커서(`since`):** 마지막 처리한 사실 이벤트의 `seq`를 커서로 전달하여 증분 데이터만 수신합니다.

## 2. 행 모델 (Row Model)

웹 콘솔의 규약을 코틀린 데이터 클래스로 매핑합니다:

```kotlin
data class Row(
    val who: Who,            // User | Agent | Thinking | Tool | Council | Info
    val text: String,        // 본문 (tool인 경우 도구명, council인 경우 판정 요지)
    val at: String? = null,  // 이벤트 발생 시각 (ISO 8601)
    // tool 행 전용 속성
    val tool: String? = null, val args: String? = null,
    val ok: Boolean? = null,  // null = 실행 진행 중
    val note: Boolean = false, // 성공 완료 및 권고사항(advisory) 존재 여부
    val out: String? = null,   // 도구 실행 실패 상세 출력
    // user 행 상태 표식
    val pending: Boolean = false, val queued: Boolean = false, val abandoned: Boolean = false,
    // council 판정 속성
    val member: String? = null, val round: Int = 0, val decision: String? = null,
    val lens: String? = null, val why: String? = null, val keep: String? = null,
    val thought: String? = null,
    val cite: String? = null,
    // 셰이퍼 내부 식별자
    val msgId: String = "", val callId: String = "",
)
```

## 3. 이벤트 → 행 매핑 규칙

| 이벤트 타입 | 생성 행 | 매핑 상세 |
|---|---|---|
| `prompt.submitted` (actor=user) | **User 행 추가** | `parts`의 텍스트를 병합하여 본문 구성 |
| `prompt.submitted` (actor=agent) | 행 미생성 | 서브에이전트 중간 보고는 전사 오염 방지를 위해 생략 |
| `prompt.submitted` (actor=system) | **Info 행** | `⟳ <actor> note: <첫 줄>` 형태로 내부 노트 표출 |
| `prompt.submitted` + `resurfacedFrom` | **재배치** | 해당 ID의 User 행을 최하단으로 이전하고 본문 갱신 및 대기 해제 |
| `part.delta` | **초안 갱신** | 동일 `messageId`의 초안 행 본문을 스트리밍 갱신 |
| `part.appended` kind=`text` | **Agent 행 추가** | 확정 본문 반영 (필요 시 `inReplyTo` 대상 사용자 행 재배치) |
| `part.appended` kind=`reasoning` | **Thinking 행 추가** | 모델의 추론 블록으로 기본 접힘 상태 렌더링 |
| `part.appended` kind=`tool-call` | **Tool 행 추가** | 호출 도구명, 인자 요약, callId 기록 |
| `part.appended` kind=`tool-result` | **행 갱신** | callId로 선행 Tool 행을 탐색하여 성공 여부(`ok`), 권고사항(`note`), 에러 출력(`out`) 확정 |
| `permission.*` · `question.*` | 행 미생성 | 승인 및 질의 카드 표출 신호로 처리 |
| `interjection.deferred` | 상태 표식 | 대상 User 행에 대기(queued) 표식 설정 |
| `interjection.answered` | 상태 표식 | 대상 User 행을 응답 위치로 재배치하고 대기 표식 해제 |
| `prompt.abandoned` | 상태 표식 | 사용자 취소 요청에 대해 취소선 표식 반영 |
| `compaction` | **Info 행** | `↯ context compacted` 요약 정보 표출 (기존 로그 보존) |
| `turn.finished` | 행 미생성 | 턴 종료 신호로 수신하여 상태 표시줄에 지표 반영 |
| `error` | **Info 행** | 오류 안내 표출 (`recovered` 여부 구분 명시) |
| `council.verdict` | **Council 행 추가** | 위원별 판정 결과, 검토 렌즈, 근거(`cite`), 유지 요구사항(`keep`), 위원 추론(`thought`) 표출 |
| `council.decided` | **Council 행 추가** | 라운드 합의 의결 결과 및 후속 피드백 내역 표출 |

## 4. 이벤트 영속성 및 증분 갱신

컴팩션(Context Compaction)은 모델 컨텍스트 윈도우를 최적화할 뿐 과거 이벤트 로그를 삭제하지 않습니다. 전사 셰이퍼는 완전한 이벤트 기록을 기반으로 동작하므로 사용자의 스크롤백 내역이 임의로 소실되지 않습니다. 재접속 시에는 `Sink.began` 신호에 따라 목록을 초기화한 후 전량을 안전하게 복원합니다.

## 5. 턴 생명주기 및 대기 상태

- **턴 활성 상태:** `prompt.submitted`(user) 수신 후 해당 턴의 `turn.finished`가 도착하기 전까지를 활성 상태로 판정합니다. 경과 시간은 로컬 클라이언트 시계 기준으로 계측합니다.
- **Pending 표식:** 응답 대기 중인 마지막 User 행 및 실행 중인 Tool 행에 진행 인디케이터를 활성화합니다.

## 6. 계층 구조 및 모듈 책임

셰이퍼는 `plugin/core` usecase 계층의 순수한 상태 머신으로 동작합니다. `LogEvent` 프레임을 수신하여 불변 `List<Row>` 목록을 생성하고 변경 시 UI 컴포넌트에 통지합니다. 소켓 스트림은 `Transcript` 컴포넌트가 단독 관리하며, UI는 `Sink` 인터페이스를 통해 구독합니다.

## 7. 골든 테스트 검증 항목

실제 세션 로그 픽스처를 기반으로 다음 핵심 불변식을 상시 검증합니다:

1. **대화 본문 보존:** 사용자의 프롬프트와 에이전트 응답이 누락 없이 행으로 변환되는지 검증합니다.
2. **도구 호출-결과 결합:** 병렬 실행 및 비동기 수신 환경에서도 callId를 기준으로 단일 행에 정확히 매핑되는지 검증합니다.
3. **스트리밍 조각 단일화:** 다수의 delta 청크 수신 후 확정본이 도착했을 때 단일 행으로 정확히 치환되는지 검증합니다.
4. **인터젝션 재배치:** 사용자 인터젝션 및 취소 처리가 기존 대화 순서를 훼손하지 않고 올바르게 반영되는지 검증합니다.
5. **컴팩션 무결성:** 컨텍스트 압축 이벤트 수신 시 선행 대화 행이 삭제되지 않고 요약 안내 행이 안전하게 추가되는지 검증합니다.
