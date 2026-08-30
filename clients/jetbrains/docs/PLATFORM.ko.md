# JetBrains 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [사용자 매뉴얼](./MANUAL.ko.md) · [화면 설계](./UI.ko.md)

> **왜 있나.** 우리가 「집 규칙」으로 정한 것과 **JetBrains 가 정해 둔 것**은 다른 글이다.
> 후자를 안 읽고 만들면, 되긴 되는데 그 IDE 답지 않은 물건이 된다 — 실제로 이 저장소에서
> 세 번 났다(액션 글자를 코드에 박아 영어 IDE 에서도 한글이 뜸, 단추를 「선택해야 뜨는」
> 막대에 둠, 드롭다운이 가장 긴 항목에 맞춰 판을 벌림). 그래서 개발할 때마다 대조할
> 목록을 여기 둔다. 출처는 절 끝에.

## 1. 액션과 배치

| 규약 | 우리 실물 |
|---|---|
| 액션 하나가 여러 자리에 설 수 있고, 자리마다 **고유 id** 와 별도 `Presentation` 을 가진다 | ✓ — 같은 클래스를 콘솔·터미널 두 자리에 다른 id 로 등록(`magi.askConsole` / `magi.askTerminal`) |
| 툴바에는 **자주 쓰거나 발견성을 높일** 액션만. 나머지는 컨텍스트 메뉴·메인 메뉴 | ✓ — 「지금 검토」만 툴바(메인 툴바 오른쪽), 첨부·훑어보기·이 줄을 쓴 작업은 우클릭 |
| 툴바 방향은 도구창의 기본 방향에서 정한다(가로 도구창 → 세로 툴바 등) | 해당 없음 — 우리 도구창은 툴바를 안 쓰고 제목줄 액션만 쓴다 |
| 액션 아이콘은 **회색 단색**(팔레트 색은 특정 범주만), 도구창 아이콘도 회색 | ✓ — `AllIcons.Actions.Preview`, 스피너는 `AnimatedIcon.Default` |
| 짧은 항목은 **헤드라인 대문자**, 긴 문장은 문장형 | ⚠ 한국어에는 대소문자가 없다 — 영어 번들에서만 지킨다 |

## 2. 국제화(i18n)

| 규약 | 우리 실물 |
|---|---|
| 번들 클래스는 **상속하지 말고 위임**(`DynamicBundle(Class, String)`) | ✓ `MagiBundle` — 처음엔 상속으로 썼다가 이 문서를 읽고 고쳤다 |
| 열쇠 인자에 `@PropertyKey`, 돌려주는 글자에 `@Nls` | ✓ |
| 액션 글자는 `action.<id>.text` / `.description` 규약으로 번들에서 | ✓ — `plugin.xml` 에 `text=` 를 안 적는다(두 벌이면 갈라진다) |
| 번역 파일은 원본과 **같은 경로**(`messages/…`), 기본(무접미) 파일이 최후 폴백 | ✓ — 기본은 영어, 한국어는 `_ko` |
| 로그·식별자는 번역하지 않는다(UI 문자열과 같은 값이라도 자리를 갈라 둔다) | ✓ — 로그는 한국어 그대로 두되 UI 는 번들로 |
| `Plugin.xml i18n verification` 인스펙션을 켜서 하드코딩을 잡는다 | ⚠ 아직 CI 에 안 걸었다 — 잔여 |

## 3. 컨트롤

| 규약 | 우리 실물 |
|---|---|
| 툴바 드롭다운은 **폭이 내용에 끌려다니면 안 된다** | ✓ `Look.narrow` — 견본 값으로 폭 고정, 긴 값은 자르고 툴팁에 원문(사용자 실측으로 들어옴) |
| 편집기 위 알림은 `EditorNotificationProvider`, 줄 옆 글자는 인레이 | ✓ — 줄에 걸리는 지적은 인레이, 걸 줄 없는 말만 띠 |
| 인레이 색은 테마의 힌트 롤에서(직접 고르지 않는다) | ✓ `INLINE_PARAMETER_HINT` → `INLAY_DEFAULT` 폴백 |

## 4. 스레딩·수명 (이 저장소가 실제로 데인 것들)

- 모델(Document·VFS) 읽기는 **read-action 안에서** — EDT 라도 예외가 아니다(2026.1 에서 SEVERE).
- `refreshAndFindFileByPath` 는 캐시 미스에서 **동기 IO** — EDT 금지.
- 리스너·브라우저·트래커는 **수명에 묶는다**(프로젝트 서비스나 창의 disposable). 고아를 남기면 창을 닫아도 산다.
- 되먹임을 조심한다: 크기를 재서 크기를 정하면 고리가 된다(우리가 두 번 만들었다 — `RichAnswer` 주석).

## 5. 대조 절차

새 화면·액션을 넣을 때 이 순서로 본다:

1. 이 자리는 **IDE 가 이미 가진 자리**인가(`clients/jetbrains/docs/UI.ko.md` §0-5) — 있으면 그걸 쓴다.
2. 글자는 번들에 있나(`action.<id>.text` 포함), 영어·한국어 **양쪽에** 있나.
3. 툴바에 둘 만큼 자주 쓰나 — 아니면 컨텍스트 메뉴.
4. 폭·색을 우리가 정하고 있지 않나(드롭다운 폭, 회색 값) — 테마·플랫폼에 맡길 것.
5. 무거운 일이 EDT 에 있나, 수명에 안 묶인 것이 있나.

## 출처

- [Action System](https://plugins.jetbrains.com/docs/intellij/action-system.html) · [Toolbar](https://plugins.jetbrains.com/docs/intellij/toolbar.html) · [Icons](https://plugins.jetbrains.com/docs/intellij/icons-style.html)
- [Internationalization](https://plugins.jetbrains.com/docs/intellij/internationalization.html) · [Providing Translations](https://plugins.jetbrains.com/docs/intellij/providing-translations.html)
- [IntelliJ Platform UI Guidelines](https://jetbrains.design/intellij/) (툴바 드롭다운·대문자 규칙)
