# JetBrains 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [사용자 매뉴얼](./MANUAL.ko.md) · [화면 설계](./UI.ko.md) · [무엇을 어디서 재나](./TESTING.ko.md)

> **문서 목적.** 저장소 내부 규칙과 **JetBrains 공식 플랫폼 UI/런타임 규약**을 명확히 대조하고 준수하기 위한 기준서입니다. 플랫폼 고유 규약을 준수하지 않을 경우 플랫폼 관례에 어긋나는 UX가 발생하기 쉽습니다. 따라서 기능 구현 시 사전 대조할 수 있도록 점검 목록을 구성하였습니다.
>
> 매니페스트 디스크립터의 다국어 번들 바인딩 등 핵심 정합성 항목은 `ManualTest`를 통해 전체 XML을 정적 검증합니다.

## 1. 액션과 배치

| 규약 | 우리 실물 |
|---|---|
| 액션 하나가 여러 자리에 설 수 있고, 자리마다 **고유 id** 와 별도 `Presentation` 을 가진다 | ✓ — 같은 클래스를 콘솔·터미널 두 자리에 다른 id 로 등록(`magi.askConsole` / `magi.askTerminal`) |
| 툴바에는 **자주 쓰거나 발견성을 높일** 액션만. 나머지는 컨텍스트 메뉴·메인 메뉴 | ✓ — 「지금 검토」만 툴바(메인 툴바 오른쪽), 나머지는 우클릭 |
| 남의 컨텍스트 메뉴에는 **하위 메뉴 하나**로(이웃·번들 플러그인의 표준) | ✓ `magi.editorMenu` — 4개 액션을 단일 하위 메뉴로 집약하였습니다. 메뉴 내부에서는 `magi: ` 접두사를 제거하여 시각적 중복을 방지합니다. Find Action 및 Keymap 검색 시에는 소속 식별을 위해 접두사를 유지합니다 |
| 툴바 방향은 도구창의 기본 방향에서 정한다(가로 도구창 → 세로 툴바 등) | 해당 없음 — 도구창 내부 툴바 대신 제목줄 헤더 액션을 활용합니다 |
| 액션 아이콘은 **회색 단색**(팔레트 색은 특정 범주만), 도구창 아이콘도 회색 | ✓ — `AllIcons.Actions.Preview`, 스피너는 `AnimatedIcon.Default` |
| 짧은 항목은 **헤드라인 대문자**, 긴 문장은 문장형 | ✓ 영어 번들 기준(메뉴·단추·다이얼로그 제목은 헤드라인 `Open Diff`, **링크는 문장형** `Show all`, 콤보 항목·체크박스는 문장형) |
| 메인 툴바 단추는 **사라지지 않는다** — 못 쓸 때는 회색 | ✓ — 툴바에서는 비활성 시 회색으로 유지하고(`isFromActionToolbar`), 우클릭 컨텍스트 메뉴에서는 비노출 처리하여 메뉴 오염을 방지합니다 |
| 액션 하나를 두 자리에 등록할 때는 자리별 `Presentation` | ✓ — 각 액션의 `update` 가 `e.place` 로 분기합니다 |

## 2. 국제화(i18n)

| 규약 | 우리 실물 |
|---|---|
| 번들 클래스는 **상속하지 말고 위임**(`DynamicBundle(Class, String)`) | ✓ `MagiBundle` 위임 패턴 적용 |
| 열쇠 인자에 `@PropertyKey`, 돌려주는 글자에 `@Nls` | ✓ 어노테이션 적용 |
| 액션 글자는 `action.<id>.text` / `.description` 규약으로 번들에서 | ✓ — 모든 디스크립터에서 하드코딩된 텍스트를 배제하고 번들 키를 바인딩합니다. `ManualTest`가 전체 `META-INF/*.xml`을 전수 검증합니다 |
| 번역 파일은 원본과 **같은 경로**(`messages/…`), 기본(무접미) 파일이 최후 폴백 | ✓ — 기본 영어, 한국어 `_ko` |
| 로그·식별자는 번역하지 않는다(UI 문자열과 같은 값이라도 자리를 갈라 둔다) | ✓ — 내부 로깅과 UI 표출 문자열을 분리 관리합니다 |
| `Plugin.xml i18n verification` 인스펙션을 켜서 하드코딩을 잡는다 | ✓ `ManualTest`를 통해 빌드 타임에 정적 검증합니다 |
| 확장점의 표시 이름도 번들에서 | ✓ — 설정 화면(`bundle`+`key`), 알림 그룹, 인텐션 분류(`categoryKey`) 전원 번들화 |

## 3. 도구창·상태 표시줄

| 규약 | 우리 실물 |
|---|---|
| 탭을 닫게 하려면 `canCloseContents="true"` — **기본이 false** 이고, 그때 `isCloseable = true` 는 조용히 무시된다 | ✓ — 세션 탭 정상 종료 및 리소스 정리를 위해 명시 설정 적용 |
| 그 스위치는 **창 전체**에 걸린다 — 안 닫힐 탭은 `isCloseable = false` 로 못박는다(content 의 기본값은 true) | ✓ — 기본 고정 탭(「채팅」·「지적 사항」)은 닫기를 차단하여 뷰 소실을 방지합니다 |
| 스트라이프 버튼 글자는 `toolwindow.stripe.<id>` — 없으면 **id 가 그대로 뜬다** | ✓ — 번들 키 바인딩 및 런타임 `stripeTitle` 동적 오버라이드 적용 |
| 도구창 아이콘은 필수, **회색 단색**, 16×16 과 20×20 | ✓ `icons/magiToolWindow.svg` + `@20x20` |
| 도구창 이름은 짧게(두 낱말 이내), 헤드라인 대문자 | ✓ `magi` / `magi Plan` |
| 상태 표시줄은 한 줄 폭을 옆 위젯과 나눠 쓴다 — 길어질 조각은 툴팁으로 | ✓ — 워크스페이스 외부 폴더 개수 등 부가 정보는 툴팁으로 처리합니다 |
| 위젯은 **눌리는 것**이 낫다(막다른 글자 금지) | ✓ — 클릭 시 `magi` 도구창 활성화 |
| 자리(`order`)는 잡지 않는다 — 플랫폼 위젯 앞은 서드파티 자리가 아니다 | ✓ — 플랫폼 기본 배치 순서를 준수합니다 |
| 프로젝트에 안 맞는 도구창은 버튼도 안 세운다(`shouldBeAvailable`) | △ 기본 프로젝트가 아닌 유효 프로젝트에 대해 활성화 |
| 도구창 탭 이름이 IDE 자신의 창과 겹치지 않게 | ✓ — 플랫폼 내장 Problems 창과의 혼선을 방지하기 위해 「지적 사항」(Findings)으로 명명 |

## 4. 컨트롤

| 규약 | 우리 실물 |
|---|---|
| 툴바 드롭다운은 **폭이 내용에 끌려다니면 안 된다** | ✓ `Look.narrow` — 견본 값 기반 폭 고정, 긴 텍스트 축약 및 툴팁 제공 |
| **설명문 라벨도 같은 기전이다** — 한 줄로 펴는 라벨은 제 글자 길이만큼 폭을 요구한다 | ✓ `Look.note` — 자동 줄바꿈 처리 및 유연한 레이아웃 확장 보장 |
| 콤보 항목은 문장형 대문자 — 프로토콜 토큰을 화면에 세우지 않는다 | ✓ `Perms` — 프로토콜 내부 토큰을 인간 친화적 레이블로 변환 표출 |
| 편집기 위 알림은 `EditorNotificationProvider`, 줄 옆 글자는 인레이 | ✓ — 라인 검토 의견은 인레이 힌트, 파일 전반 의견은 상단 배너 띠로 표출 |
| 인레이 색은 테마의 힌트 롤에서(직접 고르지 않는다) | ✓ `INLINE_PARAMETER_HINT` → `INLAY_DEFAULT` 테마 롤 폴백 |
| 힌트는 Settings \| Editor \| Inlay Hints 에 항목이 선다 | ✗ 에디터 타이핑 중단 시 비동기 모델 질의로 생성되므로 전용 설정 스위치로 제어합니다 |

## 5. 스레딩·수명 주기

- 모델(Document·VFS) 조회는 반드시 **read-action 내부에서** 수행합니다(EDT 환경 포함).
- `refreshAndFindFileByPath`는 캐시 미스 시 동기 I/O가 발생할 수 있으므로 EDT에서 직접 호출하지 않습니다.
- 리스너, 브라우저, 트래커 등의 백그라운드 객체는 반드시 **부모 수명 주기(`Disposable`)에 바인딩**합니다.
- **`dispose` 단계에서는 신규 클래스 로딩을 유발하지 않습니다.** IDE 종료 시 클래스로더가 이미 정리되어 있을 수 있으므로 예외 발생으로 인한 후속 정리 누락을 방지합니다.
- 재귀적 레이아웃 루프를 방지하기 위해 크기 측정과 렌더링 배치를 분리합니다.

## 6. 대조 절차

신규 화면 컴포넌트 및 액션 추가 시 다음 절차를 준수합니다:

1. 해당 UI가 **IDE 플랫폼이 이미 제공하는 표준 슬롯**인가를 검토하고 우선 활용합니다.
2. 노출 문자열이 다국어 번들(`MagiBundle`, `MagiBundle_ko`)에 누락 없이 등록되어 있는지 확인합니다.
3. 툴바 배치 빈도와 컨텍스트 메뉴 배치 기준을 명확히 구분합니다.
4. 고정 폭 및 색상 하드코딩을 배제하고 플랫폼 테마 토큰을 준수합니다.
5. 무거운 I/O 작업의 EDT 진입 여부 및 Disposable 해제 등록을 확인합니다.
6. 선언된 번들 키가 실제 코드에서 올바르게 참조되고 있는지 대조합니다.

## 출처

- [Action System](https://plugins.jetbrains.com/docs/intellij/action-system.html) · [Toolbar](https://plugins.jetbrains.com/docs/intellij/toolbar.html) · [Icons](https://plugins.jetbrains.com/docs/intellij/icons-style.html)
- [Internationalization](https://plugins.jetbrains.com/docs/intellij/internationalization.html) · [Providing Translations](https://plugins.jetbrains.com/docs/intellij/providing-translations.html)
- [Tool Windows](https://plugins.jetbrains.com/docs/intellij/tool-windows.html) · [UI Guidelines · Tool window](https://plugins.jetbrains.com/docs/intellij/tool-window.html)
- [Capitalization](https://plugins.jetbrains.com/docs/intellij/capitalization.html) · [Checkbox](https://plugins.jetbrains.com/docs/intellij/checkbox.html) · [Layout](https://plugins.jetbrains.com/docs/intellij/layout.html)
- [IntelliJ Platform UI Guidelines](https://jetbrains.design/intellij/)
