# VS Code 플랫폼 규약 — 대조표

[↑ 클라이언트 개요](../README.md) · [설계](./DESIGN.ko.md) · [형제: 젯브레인 대조표](../../jetbrains/docs/PLATFORM.ko.md)

> **왜 있나.** 우리가 「집 규칙」으로 정한 것과 **VS Code 가 정해 둔 것**은 다른 글이다. 후자를
> 안 읽고 만들면, 되긴 되는데 그 편집기 답지 않은 물건이 된다.
>
> 젯브레인에서 이 문서가 없어서 **세 번** 났다: 액션 글자를 코드에 박아 영어 IDE 에서도 한글이
> 떴고, 단추를 「선택해야 뜨는」 막대에 뒀고, 드롭다운이 가장 긴 항목에 맞춰 판을 벌렸다. 그
> 문서는 그 뒤에 쓰였다. **이번에는 만들기 전에 쓴다** — 그게 이 문서가 여기 있는 이유다.
>
> 아래 표의 「우리 계획」 칸은 아직 **계획**이다. 코드가 서면 ✓/✗ 로 바뀌고, 어긋나면 코드가
> 이긴다. 규약 칸은 출처(§8)의 실제 문장을 옮긴 것이지 기억이 아니다.

---

## 0. 이 편집기의 뼈대 — 담는 것과 담기는 것

VS Code 는 화면을 **컨테이너**와 **아이템** 둘로 가른다. 젯브레인의 「도구창」 하나에 해당하는
것이 여기서는 둘로 갈리므로, 자리를 고를 때 먼저 이 갈림을 본다.

| 컨테이너 | 무엇 | 우리가 쓸 자리 |
|---|---|---|
| 액티비티 바 | 왼쪽 세로 띠. 뷰 컨테이너를 연다 | 계획·계기판 |
| 프라이머리 사이드바 | 액티비티 바와 **짝지어** 뷰를 그린다 | 계획·계기판 |
| 세컨더리 사이드바 | 사람이 뷰를 끌어다 놓는 자리 | 우리가 정하지 않는다 |
| 에디터 | 커스텀 에디터·웹뷰 패널·에디터 툴바 | 편집기 안의 것(§3) |
| 패널 | 아래. 터미널·문제·출력이 사는 자리 | **대화** |
| 상태 표시줄 | 왼쪽=워크스페이스, 오른쪽=활성 파일 | 도는 중·승인 모드 |

---

## 1. 상태 표시줄

| 규약 | 우리 계획 |
|---|---|
| ✔️ "Place primary (global) items on the left" | **왼쪽.** 컴패니언은 워크스페이스의 것이지 지금 연 파일의 것이 아니다 |
| ✔️ "Place secondary (contextual) items on the right" | 해당 없음 — 파일 단위로 말할 것이 없다 |
| ❌ "Add more than one item (unless necessary)" | **하나.** 젯브레인은 상태·승인 모드를 한 칸에 담는다. 같이 담는다 |
| ✔️ "Use short text labels" · ✔️ "Use icons only when necessary" · ❌ "Add more than one icon" | 아이콘 하나(도는 중이면 스피너), 짧은 글자 |
| ❌ "Add custom colors" — 경고/오류 배경은 "only as a last resort ... given their prominence" | **색을 안 정한다.** 승인 대기도 배경색을 안 쓴다 — 그건 그 사람이 지금 답해야 하는 오류가 아니다 |
| 배경 작업은 "a Status Bar item with the loading icon"; 더 주목이 필요하면 진행 알림으로 | 턴이 도는 것은 여기, 코어 내려받기는 진행 알림(§4) |

## 2. 패널 — 대화가 사는 자리

| 규약 | 우리 계획 |
|---|---|
| ✔️ 가로 폭에서 득 보는 뷰 | 전사는 넓을수록 읽기 낫다 |
| ✔️ "supporting functionality" — 주 작업 흐름이 아닌 것 | 맞다. 사람의 주 작업은 코드이고 대화는 옆이다 |
| ❌ 항상 보여야 하는 것은 두지 않는다("users often minimize the Panel") | **받아들인다.** 그래서 상태 표시줄이 따로 있다 — 패널이 접혀 있어도 컴패니언이 도는지는 보여야 한다 |
| ❌ 다른 컨테이너로 끌었을 때 "fails to resize/reflow properly" 한 웹뷰 | 사람은 이 뷰를 사이드바로 끌 수 있다. **좁은 폭에서 살아남는 것이 요구사항이다** |
| 패널 툴바는 "will only render if there is just a single View" | 대화 뷰 하나만 둔다. 둘을 두면 툴바가 뷰마다 갈린다 |
| ❌ 기본 아이콘(접기·닫기) 중복 금지 · ❌ 아이콘 단추 과다 | 툴바는 「지금 훑어보기」 하나 |

## 3. 웹뷰 — 가장 조심할 자리

VS Code 가 이 페이지에서 하는 말이 다른 어느 절보다 세다: **"webviews should only be used if you
absolutely need them."**

| 규약 | 우리 계획 |
|---|---|
| ✔️ "Only use webviews when absolutely necessary" | 대화와 계획판 **둘만**. 나머지는 전부 네이티브 API(§4·§5) |
| ✔️ "Ensure all elements in the view are themeable" | 색을 안 박고 VS Code 색 토큰만 쓴다. 젯브레인에서 팔레트를 TUI 원본과 맞춘 것과 같은 규율이고, 여기서는 **플랫폼이 값을 준다** |
| ✔️ "Ensure your views follow accessibility guidance (color contrast, ARIA labels, keyboard navigation)" | 전사는 읽는 물건이다. 말풍선에 역할과 이름을 단다 |
| ✔️ "Use command actions in the toolbar and in the view" | 웹뷰 안에 자체 툴바를 그리지 않고 뷰 툴바에 명령을 단다 |
| ❌ "Use for wizards" | 설정은 웹뷰가 아니다(§5) |
| ❌ "Repeat existing functionality (Welcome page, Settings, configuration, etc.)" | **설정 화면을 안 짠다.** 젯브레인은 손으로 짜는데(`MagiConfigurable`) 여기서는 그것이 금지다 |
| ❌ "Open on every window" · ❌ "Open on extension updates" | 뷰는 사람이 열 때 산다. 갱신 때 아무것도 안 연다 |
| ❌ "Use for promotions" | 해당 없음 |

⚠ **이 페이지가 말하지 않는 것**: CSS 변수 이름, `retainContextWhenHidden`, 포커스, 메시지
전달. 색 토큰 문서와 샘플로 넘긴다 — **그래서 이 표에도 안 적는다.** 안 읽은 것을 규약처럼 적으면
이 문서가 하려는 일의 반대가 된다.

## 4. 알림

| 규약 | 우리 계획 |
|---|---|
| ✔️ "Respect the user's attention by only sending notifications when absolutely necessary" | 턴의 진행은 **알림이 아니라** 상태 표시줄과 패널이다 |
| ✔️ "Add a **Do not show again** option for every notification" | 코어 내려받기 물음에 붙인다. 젯브레인이 `DECLINED` 로 하는 그 일이고, 여기서는 **플랫폼이 요구한다** |
| ✔️ "Show one notification at a time" | 창이 여럿이면 하나만 묻는다 — 설계 §6 의 잠금 파일이 이 규약을 지키는 자리이기도 하다 |
| ❌ "Send repeated notifications" · ❌ "Ask for feedback on the first install" | |
| ❌ "Show actions if there aren't any" | |
| 진행 알림은 "a last resort as progress is best kept within context" | **코어 내려받기만** 진행 알림. 그건 맥락이 없는 일이다(아직 화면이 없다) |
| 진행: ✔️ 단계 보고 · ✔️ 취소 제공 · ✔️ "Add timers for timed out scenarios" · ❌ "Leave a notification running in progress" | 받기는 취소된다. **취소도 비행의 끝**이다(설계 §6) |
| 모달: ✔️ 즉시 입력이 필요할 때만 · ✔️ Always/Never 로 되풀이를 막는다 · ❌ "actions that are not explicitly initiated by the user" | 승인 대화를 **모달로 만들지 않는다** — 사람이 시작한 일이 아니라 컴패니언이 시작한 일이다. 웹뷰 안에서 답한다 |

## 5. 설정

| 규약 | 우리 계획 |
|---|---|
| ❌ **"Create your own settings page/webview"** | 안 짠다. `contributes.configuration` 만 |
| ✔️ "Add default values to each setting" | |
| ✔️ "Add clear descriptions" · ❌ "Create long descriptions" | 긴 사유는 문서로 링크 |
| ✔️ "Link to documentation for complicated settings" | 승인 모드는 짧게 못 쓴다 → SECURITY 로 링크 |
| 설정이 필요하면 "open the Settings UI and query your extension setting via the setting ID" | 명령에서 설정 화면을 그 열쇠로 연다 |

⚠ **데몬이 답해야 아는 값**(모델 목록·프로파일)은 정적 스키마에 못 넣는다. 설정이 아니라
**명령**으로 고른다(§6). 이것은 규약이 말한 것이 아니라 우리 사정이라 그렇게 표시한다.

## 6. 명령 팔레트

| 규약 | 우리 계획 |
|---|---|
| ✔️ "Use clear names for commands" | |
| ✔️ "Group commands together in the same category" — 예시가 `category` 접두(`GitHub Issues`) | **`magi:` 하나.** 젯브레인에서도 같은 접두를 쓴다 |
| ✔️ "Add keyboard shortcuts where appropriate" · ❌ "Overwrite existing keyboard shortcuts" | 기본 단축키를 **안 준다.** 젯브레인에서 남의 키를 못 보는 문제로 보류한 자리와 같은 판단이다 |
| ❌ "Use emojis in command names" | |

⚠ 이 페이지는 `when`/`enablement` 로 명령을 숨기는 규칙을 **말하지 않는다.** 기여점 레퍼런스
쪽 일이라 여기 안 적는다.

## 7. 컨텍스트 메뉴

| 규약 | 우리 계획 |
|---|---|
| ✔️ "Show actions when contextually appropriate" · ❌ "Show actions for every file without context" | 「대화에 첨부」는 어디서나 뜨고, 「이 줄 누가 썼나」는 **컴패니언이 만진 파일에서만** |
| ✔️ "Group similar actions together" · ✔️ "Place large groups of actions into a submenu" | 넷 이상이면 하위 메뉴. 젯브레인에서 넷을 평평하게 깔았다가 접은 그 자리와 같다 |
| 그룹은 일관되게 | 편집기 메뉴·탐색기 메뉴에서 같은 순서 |

⚠ 항목 수 상한과 이름 규칙은 이 페이지가 **말하지 않는다.**

## 8. 액티비티 바

| 규약 | 우리 계획 |
|---|---|
| ✔️ "Use an icon that matches the default Activity Bar item icon style" | |
| ✔️ "Use a clear, obvious name for the View Container" | |
| ❌ "Duplicate an existing icon" | |
| ❌ **"Use an Activity Bar item to open a Webview Panel"** | 우리 계획은 웹뷰 **뷰**(컨테이너 안)이지 웹뷰 **패널**(에디터 자리)이 아니다. 그 둘을 헷갈리면 이 규칙을 어긴다 |

⚠ 개수 상한·아이콘 치수·배지는 이 페이지가 **말하지 않는다.**

---

## 9. 젯브레인과 정반대인 것 넷

포팅에서 가장 비싼 것은 없는 API 가 아니라 **반대인 규약**이다. 젯브레인의 습관을 그대로 옮기면
여기서는 규약 위반이 된다.

| | 젯브레인 | VS Code |
|---|---|---|
| 설정 | 화면을 **손으로 짠다**(`MagiConfigurable`) | ❌ "Create your own settings page/webview" |
| 웹뷰 | 도구창 안에 브라우저를 흔히 쓴다 | "only be used if you absolutely need them" |
| 색 | 우리가 팔레트를 정해 TUI 와 맞춘다 | ❌ "Add custom colors"(상태 표시줄), 웹뷰는 테마 토큰 |
| 알림 다시 안 보기 | 우리가 알아서 기억한다(`DECLINED`) | ✔️ **모든** 알림에 붙이라고 규약이 요구한다 |

## 10. 대조 절차

새 화면·명령을 넣을 때 이 순서로 본다.

1. 이 자리는 **VS Code 가 이미 가진 자리**인가(§0 표) — 있으면 그걸 쓴다.
2. 웹뷰여야 하나 — **네이티브로 되면 네이티브로.** 되는데 웹뷰로 했다면 규약 위반이다.
3. 알림인가 — 사람의 주의를 살 값이 있나. 없으면 상태 표시줄이나 뷰 안.
4. 색을 우리가 정하고 있지 않나 — 테마 토큰에 맡길 것.
5. 명령 이름에 `magi:` 접두가 있나. 남의 단축키를 덮지 않나.
6. 이 항목이 **맥락 없이 모든 파일에** 뜨지 않나.
7. 좁은 폭(사이드바로 끌었을 때)에서 살아남나.

## 11. 이 문서가 안 읽은 것

정직하게 적는다. 아래는 출처 페이지를 **안 봤거나**, 봤는데 그 페이지가 말하지 않은 것이다.
규약처럼 적지 않는다.

- **뷰(Views)·에디터 액션·퀵픽·워크스루** 페이지 — 안 읽었다. §0 의 개요에서 이름만 알고 있다.
- 웹뷰의 **CSS 변수·`retainContextWhenHidden`·포커스·메시지 전달** — 가이드라인 페이지가 안 다룬다.
- 명령의 **`when`/`enablement`** 와 제목 대소문자 — 안 다룬다.
- 컨텍스트 메뉴 **항목 수 상한·이름 규칙** — 안 다룬다.
- 액티비티 바 **개수·아이콘 치수·배지** — 안 다룬다.
- **접근성 상세**(대비 값, ARIA 패턴) — 「따르라」는 말만 읽었고 그 문서는 안 읽었다.

## 출처

읽은 날: 2026-09-08. 아래 여덟 페이지의 문장을 옮겼다.

- [UX Guidelines · Overview](https://code.visualstudio.com/api/ux-guidelines/overview) (§0 의 컨테이너/아이템 갈림)
- [Status Bar](https://code.visualstudio.com/api/ux-guidelines/status-bar) · [Panel](https://code.visualstudio.com/api/ux-guidelines/panel)
- [Webviews](https://code.visualstudio.com/api/ux-guidelines/webviews) · [Notifications](https://code.visualstudio.com/api/ux-guidelines/notifications)
- [Settings](https://code.visualstudio.com/api/ux-guidelines/settings) · [Command Palette](https://code.visualstudio.com/api/ux-guidelines/command-palette)
- [Context Menus](https://code.visualstudio.com/api/ux-guidelines/context-menus) · [Activity Bar](https://code.visualstudio.com/api/ux-guidelines/activity-bar)
