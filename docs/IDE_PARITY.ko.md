# JetBrains와 VS Code 기능·검증 대조

2026-11-14 저장소 구현을 기능별로 대조했습니다. 명령 팔레트 항목 수는 비교 기준으로 쓰지 않았습니다. JetBrains는 도구 창과 설정 화면, VS Code는 명령과 웹뷰로 같은 작업을 제공합니다. 아래 경로는 각 클라이언트 디렉터리를 기준으로 합니다.

| 기능 | JetBrains 구현 / 테스트 | VS Code 구현 / 테스트 | 결과 |
|---|---|---|---|
| 설치·시작·업데이트·종료 | `Launches`, `Workspace` / `DaemonLifecycleTest`, `LaunchesTest` | `ide/start.ts`, `getcore.ts`, `workspace.ts` / `lifecycle`, `launches`, `owner` | 양쪽 제공 |
| 연결 복구·세션 재생 | `MagiToolWindow`, `TranscriptReconnect` / `TranscriptReconnectTest`, `TranscriptTest` | `ide/chat.ts` / `chat_host`, `stream`, `lockstep` | VS Code에 증분 커서·중복 제거·취소 가능한 단일 재시도 작업 추가 |
| 메시지·조종·취소·첨부·입력 기록 | `Companion`, `MagiToolWindow` / `CompanionTest`, `PromptRecallTest`, `SendDraftTest` | `ide/chat.ts`, `core/refs.ts` / `chat_host`, `prompt_recall`, `wire` | 양쪽 제공 |
| 질문·승인·차이 검토·실패 초안 복구 | `AnswerMode`, `ReviewSurface` / `AnswerModeTest`, `AnswerTransitionCoordinatorTest`, `AnswerRecoveryDialogTest` | `ide/approvals.ts`, `web/answer_state.ts` / `approvals`, `answer_state`, `recovery_state`, 브라우저 asks·diff | 양쪽 제공; 테스트 방식은 서로 다름 |
| 세션 생성·선택·되감기·압축 | `Companion`, `PlanToolWindow` / `CompanionTest`, `PlanPanelLayoutTest` | `ide/doors.ts`, `plan.ts` / `chat_host`, `plan_host` | 양쪽 제공 |
| 모델·프로필·권한·카운슬 | `Companion`, 정보 팝업, `CouncilControl` / `CompanionTest`, `CouncilControlTest` | `ide/choose.ts`, `plan.ts` / `chat_host`, `plan_host`, `setup` | JetBrains 카운슬 전환 추가; 저장 프로필을 `use-profile`로 선택하도록 수정 |
| 계획·컨텍스트·카운슬 결과 | `PlanToolWindow`, `Rows` / `PlanStripViewTest`, `ContextGaugeTest`, `RowsTest` | `ide/plan.ts`, `core/transcript.ts` / `plan_host`, `planstrip`, `stream` | 양쪽 제공 |
| 자식 작업·종료·스케줄·위임 승인 | `PlanToolWindow`, `ScheduleActions` / `SchedulesTest`, `CompanionTest`, `PlanPanelLayoutTest` | `ide/doors.ts`, `handoff.ts` / `background`, `handoff`, `approvals` | 양쪽 제공 |
| 지식·프로필 관리 | `KnowledgeDialog`, `ProfileMaintenanceDialog`, `CompanionProfileEditor` / `CompanionProfileEditorTest`, `KnowledgeDialogTest`, `ProfileMaintenancePanelTest` | `ide/knowledge.ts`, `profileMaintenance.ts`, `companionProfile.ts` / 동명 테스트 | 양쪽 제공; JetBrains 목록 재조회·충돌·연결 실패·닫기 및 프로필 새로고침 실행 테스트 추가 |
| 코드 검토·자동완성·작성자·커밋 메시지 | `Assist`, `Authorship`, IDE 액션 / `AssistTest`, `AuthorshipTest`, `LookNotesTest` | `ide/look.ts`, `complete.ts`, `edits.ts`, `doors.ts` / `look`, `touched`, `hand` | 양쪽 제공; IDE 고유 편집기 기능은 같은 테스트로 대체할 수 없음 |
| 편집기 MCP·소유권·출력 문서 | `IdeHand`, `OutputEditor` / `HandRegistrationTest`, `HandEdtCallTest`, `OutputEditorTest` | `ide/hand.ts`, `output.ts` / `hand_lifecycle`, `hand_provider`, `output_provider` | 양쪽 제공 |
| 스트리밍·생각 접기·Markdown·Mermaid | `MagiToolWindow`, `NativeMarkdownSurface` / `TailScrollTest`, `StreamingHeightTest`, `NativeMarkdownSurfaceTest` | `web/chat_view.ts`, 공유 diagram 번들 / 브라우저 layout·markdown·virtualization | 생각은 양쪽 기본 접힘. VS Code에 Mermaid 렌더링·확대·복사·후속 스트림 보존 검증 추가 |

## 이번에 바꾼 동작

VS Code는 자동 재연결 때 기존 전사와 화면 세대를 유지하고 마지막 영속 이벤트 번호를 `since`로 보냅니다. 이전 연결의 늦은 콜백과 중복 번호는 버립니다. 세션 전환과 명시적 다시 읽기는 커서를 초기화하며, 데몬이 커서를 거절한 경우에도 전체 재생을 받습니다. 재시도는 연결 시도의 완료를 기다립니다. 창을 닫으면 대기 타이머를 취소합니다.

두 IDE의 생각 내용은 기본으로 접힙니다. 사용자가 펼친 생각은 스트리밍 중에도 펼침을 유지합니다. JetBrains에서는 닫힌 생각 행의 컴포넌트를 재사용하고 열린 초안에는 늘어난 부분만 반영합니다. VS Code도 초안 DOM을 유지합니다.

JetBrains 계획 창에 카운슬 전환을 추가했습니다. 확인을 취소하거나 데몬이 거절하면 기존 상태를 유지합니다. 정보 팝업의 저장 프로필 선택은 이제 `use-profile`을 호출하며, 현재 프로필 이름도 상태 응답에서 읽습니다.

## 검증 범위와 한계

VS Code Node 테스트는 660개 중 653개 통과, 플랫폼 조건으로 7개 제외입니다. Playwright는 9개 묶음이 통과했습니다. 그 안의 등록 시나리오는 56개이며, 긴 전사의 가상화 검증 두 건은 별도입니다. Mermaid는 실제 번들로 SVG 생성·확대·원본 복사·후속 스트림에서 SVG 유지까지 검사합니다. 번들이 없으면 테스트 시작 전에 실패하도록 보완했습니다.

JetBrains 전체 core 실행은 458개 중 451개 통과, 5개 제외, 아래 기존 파일로 인한 2개 실패입니다. IntelliJ 225개 테스트는 모두 통과했습니다. 작업 트리에 있던 미추적 `ZzReplayProbeTest.kt`는 다른 작업이므로 수정하지 않았습니다. 이 파일 때문에 전체 core 검증의 소스 추적·문서 목록 검사와 기본 테스트 컴파일은 별도 제약을 받습니다. 변경 관련 core 테스트는 별도로 통과했고, 플러그인 빌드도 검증합니다.

테스트 개수가 같다는 뜻은 아닙니다. VS Code에는 속성 기반 초안 복구와 브라우저 접근성 검증이 있고, JetBrains에는 실제 Swing·JCEF 컴포넌트 검증이 있습니다. 소스 문자열 검사만 있는 경로도 남아 있습니다. 이번 작업에서 모든 관리 대화상자나 Windows·Linux의 설치된 IDE를 실제 조작한 것은 아닙니다. VS Code `test:live`도 확장 활성화·명령 등록 검증이며 실제 장애 복구 시험과는 범위가 다릅니다.

## 관리 화면 후속 보완

JetBrains 지식 관리의 플러그인 액션은 성공 응답에 목록이 없어도 기존 항목을 지우지 않도록 액션 뒤에 전체 목록을 다시 읽습니다. 연결 실패 시 마지막 목록을 유지하며, 최신 목록을 받기 전까지 변경 버튼을 비활성화합니다. 조회 중 도착한 슬롯 알림은 한 번의 후속 조회로 합칩니다. 수정 충돌 뒤에는 새 리비전을 사용하고, 창을 닫으면 대기 중 요청을 취소합니다.

`KnowledgeDialogTest`와 `ProfileMaintenancePanelTest`는 실제 Swing 컴포넌트에서 버튼을 눌러 응답·실패·재시도·닫기를 검증합니다. 전송 응답은 주입하므로 실제 서버와 연결된 종단 간 시험은 아닙니다. 프로필 상태 본문 누락은 JetBrains에서 오류로 안내하며, VS Code는 잘못된 JSON 구조도 오류로 처리합니다. VS Code 후속 실행은 668개 중 661개 통과, 7개 제외입니다. JetBrains IntelliJ 후속 실행은 231개 모두 통과했습니다.
