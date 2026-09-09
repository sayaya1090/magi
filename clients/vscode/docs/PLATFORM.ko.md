# VS Code 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [설계](./DESIGN.ko.md) · [형제: 젯브레인 대조표](../../jetbrains/docs/PLATFORM.ko.md)

> **문서 목적.** 자체적인 설계 규칙과 **VS Code 공식 UX 가이드라인**의 요구사항을 대조하여 플랫폼 고유의 사용자 경험을 충실히 반영하기 위해 작성되었습니다.
>
> JetBrains 플러그인 개발 과정에서는 사전 규약 대조 문서의 부재로 인해 다국어 번들 누락, 잘못된 툴바 배치, 드롭다운 너비 산정 결함 등 3차례의 UI 수정이 발생한 이력이 있습니다. 이러한 결함을 사전에 방지하고자 구현 착수 전 플랫폼 규약을 명문화하여 준수합니다.
>
> 아래 표의 「구현 계획」은 확장의 설계 및 적용 원칙을 나타내며, 규약 항목은 공식 가이드라인(출처 §8)의 실제 문장을 인용한 것입니다.

---

## 0. VS Code 화면 구조 — 컨테이너와 아이템

VS Code는 화면 요소를 **컨테이너(Container)**와 **아이템(Item)**으로 구분합니다. JetBrains의 단일 도구 창 개념과 달리 컨테이너와 뷰가 분리되므로, 컴포넌트 배치 시 이 구조를 우선 고려합니다.

| 컨테이너 | 설명 | magi 확장 적용 위치 |
|---|---|---|
| 액티비티 바 | 좌측 세로 막대. 뷰 컨테이너 호출 | 미사용 (계획 패널은 보조 사이드바로 분리) |
| 프라이머리 사이드바 | 액티비티 바와 연동되는 기본 사이드바 | 미사용 |
| **보조 사이드바**(우측) | VS Code 1.106부터 `secondarySidebar`로 선언 가능. 에디터 챗이 위치하는 공간 | **계획 및 계기판 (`magi-side`)** |
| 세컨더리 사이드바 | 사용자가 임의로 뷰를 배치할 수 있는 영역 | 확장에서 기본 지정하지 않음 |
| 에디터 | 커스텀 에디터, 웹뷰 패널, 에디터 툴바 | 에디터 내부 액션 및 인레이 (§3) |
| 패널 | 하단 터미널, 문제, 출력 탭이 위치하는 공간 | **대화 패널 (`magi.chat`)** |
| 상태 표시줄 | 좌측: 워크스페이스 전역, 우측: 활성 파일 컨텍스트 | 실행 상태 및 승인 모드 표시 |

---

## 1. 상태 표시줄

| 규약 | 구현 계획 |
|---|---|
| ✔️ "Place primary (global) items on the left" | **좌측 배치.** 컴패니언은 개별 파일이 아닌 워크스페이스 단위로 동작합니다. |
| ✔️ "Place secondary (contextual) items on the right" | 해당 없음 (파일 컨텍스트 단위 항목 미제공). |
| ❌ "Add more than one item (unless necessary)" | **단일 항목 유지.** 상태와 승인 모드를 하나의 상태 표시줄 위젯에 통합합니다. |
| ✔️ "Use short text labels" · ✔️ "Use icons only when necessary" · ❌ "Add more than one icon" | 단일 아이콘(동작 시 회전 스피너)과 간결한 텍스트 레이블을 사용합니다. |
| ❌ "Add custom colors" — 경고/오류 배경은 "only as a last resort ... given their prominence" | **커스텀 배경색 미지정.** 사용자 승인 대기 상태 역시 일반 텍스트로 표현하여 의도치 않은 오류 오인을 방지합니다. |
| 배경 작업은 "a Status Bar item with the loading icon"; 더 주목이 필요하면 진행 알림으로 | 일반 턴 실행은 상태 표시줄에 표시하고, 바이너리 다운로드 등 주의가 필요한 작업은 진행 알림(§4)으로 전달합니다. |

## 2. 패널 — 대화 영역

| 규약 | 구현 계획 |
|---|---|
| ✔️ 가로 폭에서 득 보는 뷰 | 대화 전사는 폭이 넓을수록 가독성이 향상됩니다. |
| ✔️ "supporting functionality" — 주 작업 흐름이 아닌 것 | 사용자의 주 작업은 코드 편집이며 에이전트 대화는 보조 기능에 해당합니다. |
| ❌ 항상 보여야 하는 것은 두지 않는다("users often minimize the Panel") | 상태 표시줄을 별도로 제공하여 패널이 최소화된 상태에서도 컴패니언 실행 상태를 확인할 수 있도록 합니다. |
| ❌ 다른 컨테이너로 끌었을 때 "fails to resize/reflow properly" 한 웹뷰 | 사용자가 뷰를 사이드바로 이동할 수 있으므로, 좁은 폭에서도 정상 렌더링되도록 리플로우를 지원합니다. |
| 패널 툴바는 "will only render if there is just a single View" | 패널 컨테이너 내에 단일 뷰(`magi.chat`)만 등록하여 툴바 일관성을 유지합니다. |
| ❌ 기본 아이콘(접기·닫기) 중복 금지 · ❌ 아이콘 단추 과다 | 툴바에는 「지금 훑어보기(Look over)」 등 필수 액션만 배치합니다. |

## 3. 웹뷰(Webview) 사용 원칙

공식 가이드라인 명시 사항: **"webviews should only be used if you absolutely need them."**

| 규약 | 구현 계획 |
|---|---|
| ✔️ "Only use webviews when absolutely necessary" | 대화 패널과 계획 패널 **2곳에만 웹뷰를 적용**합니다. 나머지는 모두 VS Code 네이티브 API(§4, §5)를 사용합니다. |
| ✔️ "Ensure all elements in the view are themeable" | 하드코딩된 색상을 배제하고 VS Code 공식 테마 CSS 토큰 변수를 바인딩합니다. |
| ✔️ "Ensure your views follow accessibility guidance (color contrast, ARIA labels, keyboard navigation)" | 대화 말풍선 및 상호작용 요소에 시맨틱 롤과 ARIA 레이블을 명시합니다. |
| ✔️ "Use command actions in the toolbar and in the view" | 웹뷰 내부에 자체 툴바를 중복 구현하지 않고 뷰 툴바에 커맨드를 등록합니다. |
| ❌ "Use for wizards" | 설정 및 마법사 화면에 웹뷰를 사용하지 않습니다 (§5). |
| ❌ "Repeat existing functionality (Welcome page, Settings, configuration, etc.)" | 플랫폼 내장 기능과 중복되는 자체 설정 UI를 구현하지 않습니다 (`package.json` 선언 스키마 사용). |
| ❌ "Open on every window" · ❌ "Open on extension updates" | 창 오픈 시 무조건 열지 않으며 업데이트 시 팝업을 띄우지 않습니다. |
| ❌ "Use for promotions" | 홍보성 콘텐츠 노출 금지. |

> [!NOTE]
> 테마 변수명, `retainContextWhenHidden`, 포커스 정책, 메시지 패싱 등 구현 상세는 별도 공식 문서 및 예제를 참고하며 본 대조표에는 포함하지 않습니다.

## 4. 알림 (Notifications)

| 규약 | 구현 계획 |
|---|---|
| ✔️ "Respect the user's attention by only sending notifications when absolutely necessary" | 일반 작업 진행 상황은 팝업 알림 대신 상태 표시줄과 대화 패널을 통해 전달합니다. |
| ✔️ "Add a **Do not show again** option for every notification" | 코어 다운로드 제안 등 알림 시 반복 방지 옵션을 제공합니다. |
| ✔️ "Show one notification at a time" | 다중 창 환경에서도 락 파일을 통해 알림이 중복 발생하지 않도록 제어합니다 (설계 §6). |
| ❌ "Send repeated notifications" · ❌ "Ask for feedback on the first install" | 불필요한 반복 알림 및 초기 피드백 요구 금지. |
| ❌ "Show actions if there aren't any" | 동작이 없는 무의미한 버튼 노출 금지. |
| 진행 알림은 "a last resort as progress is best kept within context" | **바이너리 다운로드 등 UI 맥락이 없는 초기화 단계에만** 진행 알림(`withProgress`)을 사용합니다. |
| 진행: ✔️ 단계 보고 · ✔️ 취소 제공 · ✔️ "Add timers for timed out scenarios" · ❌ "Leave a notification running in progress" | 다운로드 진행률 및 취소 액션을 제공하고, 타임아웃을 설정하여 멈춤 상태를 방지합니다. |
| 모달: ✔️ 즉시 입력이 필요할 때만 · ✔️ Always/Never 로 되풀이를 막는다 · ❌ "actions that are not explicitly initiated by the user" | 에이전트 승인 요청을 모달 대화상자로 띄우지 않고 웹뷰 패널 내 인라인 카드에서 처리합니다. |

## 5. 설정 (Settings)

| 규약 | 구현 계획 |
|---|---|
| ❌ **"Create your own settings page/webview"** | 자체 설정 웹뷰를 생성하지 않고 `contributes.configuration` 선언형 스키마만 사용합니다. |
| ✔️ "Add default values to each setting" | 모든 설정 항목에 명시적 기본값을 정의합니다. |
| ✔️ "Add clear descriptions" · ❌ "Create long descriptions" | 설정 설명은 간결하게 기술하며, 상세 설명은 문서 링크로 안내합니다. |
| ✔️ "Link to documentation for complicated settings" | 승인 모드 등 복잡한 보안 정책은 문서 링크를 제공합니다. |
| 설정이 필요하면 "open the Settings UI and query your extension setting via the setting ID" | 커맨드를 통해 플랫폼 표준 설정 UI(`workbench.action.openSettings`)를 해당 ID로 엽니다. |

> [!NOTE]
> 데몬 런타임에 동적으로 결정되는 값(모델 목록, 프로필)은 정적 매니페스트 스키마 대신 퀵픽(QuickPick) 커맨드를 통해 선택하도록 구성합니다 (§6).

## 6. 명령 팔레트 (Command Palette)

| 규약 | 구현 계획 |
|---|---|
| ✔️ "Use clear names for commands" | 명령 명칭을 직관적으로 명시합니다. |
| ✔️ "Group commands together in the same category" — 예시: `category` 접두 | 모든 커맨드에 **`magi` 카테고리**를 지정합니다. |
| ✔️ "Add keyboard shortcuts where appropriate" · ❌ "Overwrite existing keyboard shortcuts" | 플랫폼 기본 단축키 충돌을 방지하기 위해 확장 기본 단축키를 강제 바인딩하지 않습니다. |
| ❌ "Use emojis in command names" | 명령 제목에 이모지 사용을 배제합니다. |

## 7. 컨텍스트 메뉴 (Context Menus)

| 규약 | 구현 계획 |
|---|---|
| ✔️ "Show actions when contextually appropriate" · ❌ "Show actions for every file without context" | 상황에 맞는 조건(`when` 절)을 정의하여 무관한 컨텍스트에서의 노출을 차단합니다. |
| ✔️ "Group similar actions together" · ✔️ "Place large groups of actions into a submenu" | 관련 액션을 `magi` 서브메뉴로 묶어 컨텍스트 메뉴 과밀을 방지합니다. |
| 그룹은 일관되게 | 에디터 및 탐색기 메뉴에서 일관된 순서로 배치합니다. |

## 8. 액티비티 바 미사용 사유

VS Code 1.106부터 지원되는 보조 사이드바(`secondarySidebar`)로 계획 패널을 배치함으로써 액티비티 바에는 별도의 전용 아이콘을 등록하지 않습니다.

| 규약 | 구현 계획 |
|---|---|
| ✔️ "Use an icon that matches the default Activity Bar item icon style" | 필요 시 플랫폼 기본 아이콘 스타일 준수. |
| ✔️ "Use a clear, obvious name for the View Container" | 명확한 컨테이너 명칭 사용. |
| ❌ "Duplicate an existing icon" | 기존 플랫폼 아이콘 중복 금지. |
| ❌ **"Use an Activity Bar item to open a Webview Panel"** | 뷰 컨테이너 내부 웹뷰 뷰(Webview View) 형식을 준수합니다. |

> [!TIP]
> 아이콘 식별자는 공식 문서뿐 아니라 실제 런타임 코디콘 정의(`extensions/*/media/codicon.css`)를 확인하여 지정합니다.

---

## 9. JetBrains 플랫폼과의 핵심 설계 차이점

플랫폼 간 설계 규약의 차이로 인해 JetBrains 방식과 상반되게 구현되는 4가지 원칙:

| 구분 | JetBrains | VS Code |
|---|---|---|
| **설정 UI** | 코드로 설정 패널 직접 구성 (`MagiConfigurable`) | 선언형 스키마 (`contributes.configuration`) 전용 |
| **웹뷰 사용** | JCEF 브라우저 컴포넌트 광범위 사용 | 최소한의 필수 영역(대화, 계획)으로 엄격 제한 |
| **색상 정책** | 플러그인 전용 팔레트 정의 가능 | 플랫폼 테마 토큰 바인딩 원칙 (커스텀 배경색 자제) |
| **알림 정책** | 내부 플래그(`DECLINED`) 기반 자체 기억 | "다시 보지 않음" 옵션 명시적 제공 요구 |

## 10. 신규 기능 반영 시 검증 절차

1. **플랫폼 네이티브 컨테이너 존재 여부 확인** (§0 참조): 기존 슬롯을 우선 활용합니다.
2. **웹뷰 필요성 검토**: 네이티브 API(트리 뷰, 퀵픽 등)로 대체 가능한 경우 웹뷰 사용을 배제합니다.
3. **알림 적합성 평가**: 단순 상태 전달은 상태 표시줄 또는 패널 내에 표시합니다.
4. **테마 호환성**: 플랫폼 CSS 토큰 변수를 사용합니다.
5. **명령 카테고리 명시**: `magi` 카테고리 지정 및 단축키 충돌 방지.
6. **컨텍스트 필터링**: 적절한 `when` 절을 구성합니다.
7. **반응형 리플로우 지원**: 사이드바 등 좁은 뷰포트에서도 정상 동작하는지 확인합니다.

## 11. 참조 범위 안내

다음 항목은 본 문서의 가이드라인 요약 범위에서 제외되며 플랫폼 공식 기술 문서를 따릅니다:

- 뷰(Views), 에디터 액션, 퀵픽(QuickPick), 워크스루(Walkthrough) 상세 구현
- 웹뷰 내부 CSS 변수 목록, 생명주기(`retainContextWhenHidden`), 포커스 및 메시지 통신
- 커맨드 `when`/`enablement` 절 세부 표현식 문법
- 접근성 상세 규격(대비율, ARIA 위젯 패턴)

## 출처

참조 일자: 2026-09-08 (공식 VS Code UX 가이드라인 문서군 기반)

- [UX Guidelines · Overview](https://code.visualstudio.com/api/ux-guidelines/overview)
- [Status Bar](https://code.visualstudio.com/api/ux-guidelines/status-bar) · [Panel](https://code.visualstudio.com/api/ux-guidelines/panel)
- [Webviews](https://code.visualstudio.com/api/ux-guidelines/webviews) · [Notifications](https://code.visualstudio.com/api/ux-guidelines/notifications)
- [Settings](https://code.visualstudio.com/api/ux-guidelines/settings) · [Command Palette](https://code.visualstudio.com/api/ux-guidelines/command-palette)
- [Context Menus](https://code.visualstudio.com/api/ux-guidelines/context-menus) · [Activity Bar](https://code.visualstudio.com/api/ux-guidelines/activity-bar)
