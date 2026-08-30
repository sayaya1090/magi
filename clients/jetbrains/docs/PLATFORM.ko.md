# JetBrains 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [사용자 매뉴얼](./MANUAL.ko.md) · [화면 설계](./UI.ko.md)

> **왜 있나.** 우리가 「집 규칙」으로 정한 것과 **JetBrains 가 정해 둔 것**은 다른 글이다.
> 후자를 안 읽고 만들면, 되긴 되는데 그 IDE 답지 않은 물건이 된다 — 실제로 이 저장소에서
> 세 번 났다(액션 글자를 코드에 박아 영어 IDE 에서도 한글이 뜸, 단추를 「선택해야 뜨는」
> 막대에 둠, 드롭다운이 가장 긴 항목에 맞춰 판을 벌림). 그래서 개발할 때마다 대조할
> 목록을 여기 둔다. 출처는 절 끝에.
>
> **네 번째는 이 표 자신이었다.** 아래 §2 의 「`plugin.xml` 에 `text=` 를 안 적는다」 칸은
> ✓ 였는데, 선택 디스크립터(`magi-terminal.xml`) 하나가 글자를 박고 있었다 — 영어 IDE 에서
> 터미널 우클릭 항목만 한국어로 떴다. 표는 사람이 적는 글이라 **표만으로는 안 선다.**
> 그래서 이 칸들은 이제 `ManualTest` 가 디스크립터 전부를 훑어 잰다.

## 1. 액션과 배치

| 규약 | 우리 실물 |
|---|---|
| 액션 하나가 여러 자리에 설 수 있고, 자리마다 **고유 id** 와 별도 `Presentation` 을 가진다 | ✓ — 같은 클래스를 콘솔·터미널 두 자리에 다른 id 로 등록(`magi.askConsole` / `magi.askTerminal`) |
| 툴바에는 **자주 쓰거나 발견성을 높일** 액션만. 나머지는 컨텍스트 메뉴·메인 메뉴 | ✓ — 「지금 검토」만 툴바(메인 툴바 오른쪽), 첨부·훑어보기·이 줄을 쓴 작업은 우클릭 |
| 툴바 방향은 도구창의 기본 방향에서 정한다(가로 도구창 → 세로 툴바 등) | 해당 없음 — 우리 도구창은 툴바를 안 쓰고 제목줄 액션만 쓴다 |
| 액션 아이콘은 **회색 단색**(팔레트 색은 특정 범주만), 도구창 아이콘도 회색 | ✓ — `AllIcons.Actions.Preview`, 스피너는 `AnimatedIcon.Default` |
| 짧은 항목은 **헤드라인 대문자**, 긴 문장은 문장형 | ✓ 영어 번들에서만(한국어에는 대소문자가 없다). 메뉴·단추·다이얼로그 제목은 헤드라인(`Open Diff`), **링크는 문장형**(`Show all`), 콤보 항목·체크박스는 문장형 |
| 메인 툴바 단추는 **사라지지 않는다** — 못 쓸 때는 회색 | ⚠ 잔여 — 「지금 검토」가 아직 `isEnabledAndVisible` 로 숨는다(옆 아이콘이 밀린다) |
| 액션 하나를 두 자리에 등록할 때는 자리별 `Presentation` | ⚠ 잔여 — `magi.attach`(편집기+프로젝트 뷰), `magi.lookNow`(툴바+편집기)가 한 벌을 나눠 쓴다 |

## 2. 국제화(i18n)

| 규약 | 우리 실물 |
|---|---|
| 번들 클래스는 **상속하지 말고 위임**(`DynamicBundle(Class, String)`) | ✓ `MagiBundle` — 처음엔 상속으로 썼다가 이 문서를 읽고 고쳤다 |
| 열쇠 인자에 `@PropertyKey`, 돌려주는 글자에 `@Nls` | ✓ |
| 액션 글자는 `action.<id>.text` / `.description` 규약으로 번들에서 | ✓ — 어느 디스크립터에도 `text=` 를 안 적는다(두 벌이면 갈라진다). **`ManualTest` 가 `META-INF/*.xml` 전부를 훑어 잰다** — 선택 디스크립터 하나가 이 규칙 밖에 있었다 |
| 번역 파일은 원본과 **같은 경로**(`messages/…`), 기본(무접미) 파일이 최후 폴백 | ✓ — 기본은 영어, 한국어는 `_ko` |
| 로그·식별자는 번역하지 않는다(UI 문자열과 같은 값이라도 자리를 갈라 둔다) | ✓ — 로그는 한국어 그대로 두되 UI 는 번들로 |
| `Plugin.xml i18n verification` 인스펙션을 켜서 하드코딩을 잡는다 | ⚠ 아직 CI 에 안 걸었다 — 잔여 |

## 3. 도구창·상태 표시줄

| 규약 | 우리 실물 |
|---|---|
| 탭을 닫게 하려면 `canCloseContents="true"` — **기본이 false** 이고, 그때 `isCloseable = true` 는 조용히 무시된다 | ✓ — 안 적어 둔 동안 세션 탭이 안 닫혔고, 탭에 건 disposer 가 안 돌아 그 탭의 스트림이 창이 죽을 때까지 살았다 |
| 그 스위치는 **창 전체**에 걸린다 — 안 닫힐 탭은 `isCloseable = false` 로 못박는다(content 의 기본값은 true) | ✓ — 「채팅」·「문제」는 못 닫는다. 안 막아 뒀더니 닫으면 되돌릴 길이 없는데(창당 `createToolWindowContent` 한 번) 뷰는 창 disposable 에 걸려 있어 **화면만 사라지고 스트림은 사는** 새 누수가 났다 |
| 스트라이프 버튼 글자는 `toolwindow.stripe.<id>` — 없으면 **id 가 그대로 뜬다** | ✓ — 안 적어 둔 동안 오른쪽 독에 `magi.plan` 이라는 식별자가 떴다. ⚠ 이 통로는 플랫폼 로케일로 읽히므로 액션과 같은 누수를 탄다 — 프로젝트가 열릴 때 `stripeTitle` 을 `MagiBundle` 로 덮는다 |
| 도구창 아이콘은 필수, **회색 단색**, 16×16 과 20×20 | ✓ `icons/magiToolWindow.svg` + `@20x20` |
| 도구창 이름은 짧게(두 낱말 이내), 헤드라인 대문자 | ✓ `magi` / `magi Plan` |
| 상태 표시줄은 한 줄 폭을 옆 위젯과 나눠 쓴다 — 길어질 조각은 툴팁으로 | ✓ — 워크스페이스 밖 폴더 수는 툴팁 |
| 위젯은 **눌리는 것**이 낫다(막다른 글자 금지) | ✓ — 누르면 `magi` 도구창이 선다 |
| 자리(`order`)는 잡지 않는다 — 플랫폼 위젯 앞은 서드파티 자리가 아니다 | ✓ — `order="first"` 를 뗐다 |
| 프로젝트에 안 맞는 도구창은 버튼도 안 세운다(`shouldBeAvailable`) | ⚠ 잔여 — 지금은 모든 프로젝트에 선다 |

## 4. 컨트롤

| 규약 | 우리 실물 |
|---|---|
| 툴바 드롭다운은 **폭이 내용에 끌려다니면 안 된다** | ✓ `Look.narrow` — 견본 값으로 폭 고정, 긴 값은 자르고 툴팁에 원문(사용자 실측으로 들어옴) |
| 편집기 위 알림은 `EditorNotificationProvider`, 줄 옆 글자는 인레이 | ✓ — 줄에 걸리는 지적은 인레이, 걸 줄 없는 말만 띠 |
| 인레이 색은 테마의 힌트 롤에서(직접 고르지 않는다) | ✓ `INLINE_PARAMETER_HINT` → `INLAY_DEFAULT` 폴백 |

## 5. 스레딩·수명 (이 저장소가 실제로 데인 것들)

- 모델(Document·VFS) 읽기는 **read-action 안에서** — EDT 라도 예외가 아니다(2026.1 에서 SEVERE).
- `refreshAndFindFileByPath` 는 캐시 미스에서 **동기 IO** — EDT 금지.
- 리스너·브라우저·트래커는 **수명에 묶는다**(프로젝트 서비스나 창의 disposable). 고아를 남기면 창을 닫아도 산다.
- 되먹임을 조심한다: 크기를 재서 크기를 정하면 고리가 된다(우리가 두 번 만들었다 — `RichAnswer` 주석).

## 6. 대조 절차

새 화면·액션을 넣을 때 이 순서로 본다:

1. 이 자리는 **IDE 가 이미 가진 자리**인가(`clients/jetbrains/docs/UI.ko.md` §0-5) — 있으면 그걸 쓴다.
2. 글자는 번들에 있나(`action.<id>.text` 포함), 영어·한국어 **양쪽에** 있나.
3. 툴바에 둘 만큼 자주 쓰나 — 아니면 컨텍스트 메뉴.
4. 폭·색을 우리가 정하고 있지 않나(드롭다운 폭, 회색 값) — 테마·플랫폼에 맡길 것.
5. 무거운 일이 EDT 에 있나, 수명에 안 묶인 것이 있나.
6. **새로 만든 번들 열쇠를 코드가 실제로 부르나.** 정의만 되고 아무도 안 부르는 열쇠 옆에는
   대개 박힌 글자가 서 있다 — 18개가 그랬다. 죽은 열쇠는 번역이 된 척하는 자리다.

## 출처

- [Action System](https://plugins.jetbrains.com/docs/intellij/action-system.html) · [Toolbar](https://plugins.jetbrains.com/docs/intellij/toolbar.html) · [Icons](https://plugins.jetbrains.com/docs/intellij/icons-style.html)
- [Internationalization](https://plugins.jetbrains.com/docs/intellij/internationalization.html) · [Providing Translations](https://plugins.jetbrains.com/docs/intellij/providing-translations.html)
- [Tool Windows](https://plugins.jetbrains.com/docs/intellij/tool-windows.html) · [UI Guidelines · Tool window](https://plugins.jetbrains.com/docs/intellij/tool-window.html)
- [Capitalization](https://plugins.jetbrains.com/docs/intellij/capitalization.html) · [Checkbox](https://plugins.jetbrains.com/docs/intellij/checkbox.html) · [Layout](https://plugins.jetbrains.com/docs/intellij/layout.html)
- [IntelliJ Platform UI Guidelines](https://jetbrains.design/intellij/) (툴바 드롭다운·대문자 규칙)
