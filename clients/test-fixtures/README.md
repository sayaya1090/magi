# 클라이언트 공통 계약 Fixture (§6.38)

이 디렉터리는 JetBrains 플러그인과 VS Code 확장이 공통으로 준수해야 하는 초안·답변·복구·수명 주기 핵심 계약 5종의 표준 JSON fixture를 단일 원본으로 보관합니다. 양쪽 실행기는 복사본 없이 이 디렉터리의 동일한 파일을 읽어 검증합니다.

## 1. 표준 시나리오 목록

| fixture | 파일 | 동작 순서 | 필수 관측 |
|---|---|---|---|
| 늦은 실패 | `late_failure.json` | A 제출 → B 사용자 편집 → A 실패 | B 유지, 실패한 A의 복구 정보 보존, 시도별 잠금 해제 |
| 다른 세션 결과 | `cross_session_result.json` | S1 제출 → S2 전환·제출 → S1 결과 | S2 입력·잠금 불변, S1 복귀 시 S1 결과 반영 |
| 복구 삭제 | `delete_recovery.json` | 복구 항목 생성 → 삭제 → 재그림·세션 왕복 | 삭제한 세대 재등장 없음, 새 편집은 복구 가능 |
| 같은 문자열 편집 | `same_string_edit.json` | A → B → A 사용자 편집, 별도 restore 대조 | 실제 편집 세대 증가, 프로그램 복원 시 세대 보존, 이후 편집 시 세대 증가 |
| 종료 후 콜백 | `disposed_callback.json` | 요청 시작 → 소유자 dispose → 결과 및 선택 콜백 | 입력 변경 0, 문서 열기 0, 신규 요청 0 (종료된 UI 부작용 없음) |

## 2. 검증 계층의 2단계 분리 (순수 모델 vs 실제 호스트)

| 계층 | 대상 컴포넌트 | 검증 범위 및 역할 |
|---|---|---|
| **순수 모델 계층** | JetBrains `AnswerDrafts`<br>VS Code `AnswerStateManager`, `RecoveryStateManager` | 세션 격리, 초안 보존, 질문 잠금/해제, 복구 저장소 수명주기, 단조 증가 편집 버전 관리 |
| **실제 호스트 계층** | JetBrains `MagiToolWindow.View` (`restoreAnswerText`, `closing` 가드)<br>VS Code `WebviewInputAdapter` (`chat_adapter.ts`), `WebviewActionAdapter` | 실제 UI 복원 시 `restoringAnswerDraft` 플래그 및 DOM 직접 바인딩으로 편집 세대 오염 차단, 소유자 `dispose()` 후 늦은 결과/선택 콜백 시 입력 변이·문서 열기·신규 전송 0건 실측 |

## 3. 플랫폼 간 의도적 차이 대응표

| 영역 | JetBrains (`AnswerDrafts` / `View`) | VS Code (`answer_state` / `chat_adapter`) | 의도적 차이 사유 및 별도 검증 |
|---|---|---|---|
| **컨텍스트 식별자** | `session` (단일 프로젝트 View 귀속) | `companionKey` + `sessionId` (멀티 워크스페이스·컴패니언 라우팅) | VS Code는 다중 작업공간 확장을 지원하므로 `companionKey`를 최상위 네임스페이스로 둡니다. 공통 fixture는 `session`을 공통 단위로 하며, VS Code 실행기에서 고정 fixture 컴패니언 키를 매핑합니다. |
| **복구 항목 ID** | `ans-rec-${++recoverySerial}` 형식 | `rec-${++recoverySeq}` 및 `seq` (역순 정렬) | 각 플랫폼 고유의 내부 식별자 체계를 존중합니다. fixture는 특정 ID 값 대신 `target: "last"` / `"first"` 논리 참조와 텍스트 일치(`hasRecoveryForText`)로 단언합니다. |
| **질문 이탈 시 복구 노출** | `bind()` 시 미제출 편집 텍스트가 있으면 즉시 `question_left` 복구 생성 | `questionDrafts[callId]`에 초안 보존 후 세션 전환 복원 | JetBrains는 질문 이탈 즉시 복구 패널에 노출하고, VS Code는 초안 상태 머신과 실패/충돌 이벤트 기반 복구를 중심으로 동작합니다. 실패 복구(`submission_failed` / `reply_failed`)는 양 플랫폼이 동일하게 복구 저장소를 생성합니다. |
| **실제 프로그램 복원 경로** | `restoreAnswerText(text)` (`restoringAnswerDraft = true`) | `applyAnswerModeUI` / DOM `say.value = text` 직접 설정 | 사용자 키보드 입력 이벤트(`DocumentListener`/`onInput`)와 프로그램 복원을 분리하여 복원 중에는 편집 세대(`version`)가 증가하지 않습니다. |
| **실제 수명 주기 (dispose) 가드** | `View.closing.get()` 플래그 및 `inputEpoch++` | `chat_adapter.dispose()` 및 `suggestCtrl.dispose()` | 소유자 해제 후 도달한 RPC 완료 콜백 및 제안/멘션 선택 콜백은 조기 반환되어 입력창 텍스트 변경(`inputChanges`), 에디터 문서 열기(`documentsOpened`), 신규 전송 요청(`newRequests`)을 일체 유발하지 않습니다. |

## 4. 실행기 실패 보고 및 엄격 유효성 검사 계약

양쪽 테스트 실행기는 단언 실패 시 반드시 다음 정보를 포함하여 보고해야 합니다:
- `scenarioId`: 시나리오 식별자 (예: `late_failure`)
- `step`: 실패가 발생한 실행 단계 번호 (1-indexed)
- `expected`: 기대값
- `actual`: 실제값

또한 다음 항목 발생 시 실행기는 조용히 넘기지 않고 즉시 오류로 보고합니다:
- 정의되지 않은 미등록 `action`
- 정의되지 않은 미등록 `assert` 키 (오타 방지)
- `attemptMap`에 등록되지 않은 `attemptKey` 참조
- 요청 실패(`submit` 불가)
- 삭제 대상 복구 항목 부재
