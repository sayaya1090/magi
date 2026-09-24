# 클라이언트 공통 계약 Fixture (§6.38)

이 디렉터리는 JetBrains 플러그인과 VS Code 확장이 공통으로 준수해야 하는 초안·답변·복구·수명 주기 핵심 계약 5종의 표준 JSON fixture를 단일 원본으로 보관합니다. 양쪽 실행기는 복사본 없이 이 디렉터리의 동일한 파일을 읽어 검증합니다.

## 1. 표준 시나리오 목록

| fixture | 파일 | 동작 순서 | 필수 관측 |
|---|---|---|---|
| 늦은 실패 | `late_failure.json` | A 제출 → B 사용자 편집 → A 실패 | B 유지, 실패한 A의 복구 정보 보존, 시도별 잠금 해제 |
| 다른 세션 결과 | `cross_session_result.json` | S1 제출 → S2 전환·제출 → S1 결과 | S2 입력·잠금 불변, S1 복귀 시 S1 결과 반영 |
| 복구 삭제 | `delete_recovery.json` | 복구 항목 생성 → 삭제 → 재그림·세션 왕복 | 삭제한 세대 재등장 없음, 새 편집은 복구 가능 |
| 같은 문자열 편집 | `same_string_edit.json` | A → B → A 사용자 편집, 별도 restore 대조 | 실제 편집 세대와 프로그램 복원을 구분 |
| 종료 후 콜백 | `disposed_callback.json` | 요청 시작 → 소유자 dispose → 결과/선택 콜백 | 입력·문서 열기·새 요청 등 종료된 UI 부작용 없음 |

## 2. 플랫폼 간 의도적 차이 대응표

| 영역 | JetBrains (`AnswerDrafts`) | VS Code (`answer_state` / `recovery_state`) | 의도적 차이 사유 및 별도 검증 |
|---|---|---|---|
| **컨텍스트 식별자** | `session` (단일 프로젝트 View 귀속) | `companionKey` + `sessionId` (멀티 워크스페이스·컴패니언 라우팅) | VS Code는 다중 작업공간 확장을 지원하므로 `companionKey`를 최상위 네임스페이스로 둡니다. 공통 fixture는 `session`을 공통 단위로 하며, VS Code 실행기에서 고정 fixture 컴패니언 키를 매핑합니다. |
| **복구 항목 ID** | `ans-rec-${++recoverySerial}` 형식 | `rec-${++recoverySeq}` 및 `seq` (역순 정렬) | 각 플랫폼 고유의 내부 식별자 체계를 존중합니다. fixture는 특정 ID 값 대신 `target: "last"` / `"first"` 논리 참조와 텍스트 일치(`hasRecoveryForText`)로 단언합니다. |
| **질문 이탈 시 복구 노출** | `bind()` 시 미제출 편집 텍스트가 있으면 즉시 `question_left` 복구 생성 | `questionDrafts[callId]`에 초안 보존 후 세션 전환 복원 | JetBrains는 질문 이탈 즉시 복구 패널에 노출하고, VS Code는 초안 상태 머신과 실패/충돌 이벤트 기반 복구를 중심으로 동작합니다. 실패 복구(`submission_failed` / `reply_failed`)는 양 플랫폼이 동일하게 복구 저장소를 생성합니다. |
| **순수 모델 밖의 수명 (dispose)** | `AnswerDrafts.close()`로 모델 내부 자원 정리 및 상태 플래그(`closed = true`) 설정 | `chat_adapter.dispose()`로 RxJS 스트림 구독 해제 및 이벤트 리스너 제거 | 모델 경계 밖의 UI 호스트 파괴는 각 환경의 라이프사이클 관리자(IntelliJ Disposer vs VS Code Disposable)가 담당하므로, 실행기에서 dispose 플래그와 부작용(문서 열림, 신규 요청, 입력 변이) 0건을 검증합니다. |

## 3. 실행기 실패 보고 계약

양쪽 테스트 실행기는 단언 실패 시 반드시 다음 정보를 포함하여 보고해야 합니다:
- `scenarioId`: 시나리오 식별자 (예: `late_failure`)
- `step`: 실패가 발생한 실행 단계 번호 (1-indexed)
- `expected`: 기대값
- `actual`: 실제값

실행기는 미지원 동작을 조용히 건너뛰지 않으며, 구현의 실제 출력으로 예상값을 동적으로 위조하지 않습니다.
