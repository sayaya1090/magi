# 이웃 조사 — VS Code 코딩 어시스턴트들이 무엇을 어디에 다나 (2026-09-08)

[↑ 클라이언트 개요](../README.md) · [화면 설계](./UI.ko.md) · [플랫폼 규약 대조표](./PLATFORM.ko.md) · [형제: 젯브레인 이웃 조사](../../jetbrains/docs/SURVEY.ko.md)

> **왜 조사했나.** [화면 설계](./UI.ko.md)가 자리를 정했다 — 대화는 패널, 계획은 사이드바.
> 그 결정이 이 편집기에서 **혼자 이상한 것인지**를 재지 않고 정했다. 사람은 이웃들을 이미 쓰고
> 있고, 그들이 다 같은 자리를 쓰면 우리 자리는 배워야 하는 자리가 된다.
>
> **읽은 것은 기사가 아니라 매니페스트다.** 셋의 `package.json` `contributes` 를 그대로 읽었다
> — 무엇을 어디에 기여하는지가 거기 적혀 있고, 기사는 그것을 옮기면서 틀린다. Copilot 만 소스가
> 없어 문서를 읽었고, 그래서 그 줄은 다른 셋보다 약하다(§5 에 그렇게 적었다).

---

## 1. 자리 — 셋이 전부 액티비티 바다

| | 컨테이너 | 뷰 | 타입 |
|---|---|---|---|
| Cline | **activitybar** `claude-dev-ActivityBar` | `claude-dev.SidebarProvider` | webview |
| Roo Code | **activitybar** `roo-cline-ActivityBar` | `roo-cline.SidebarProvider` | webview |
| Continue | **activitybar** `continue` | `continue.continueGUIView` | webview |
| Continue(둘째) | **panel** `continueConsole` | `continue.continueConsoleView` | webview, `config.continue.enableConsole` 로 가려짐 |
| Copilot Chat | 문서가 "the editor sidebar" 라고만 한다 | | |
| **magi(계획)** | **panel** | 대화 | webview |
| **magi(계획)** | **activitybar** | 계획·계기판 | webview |

**셋이 다 대화를 액티비티 바에 단다. 우리만 패널이다.** 이 조사의 첫 사실이고, 그래서 §4 가 있다.

Continue 만 패널을 쓴다. 거기 놓은 것은 **채팅이 아니라 콘솔**이고, 기본은 꺼져 있다. 「패널은 옆에 두고 보는 것」이라는 규약의 말과 그들의 실물이 일치한다는 뜻이다.

## 2. 명령 — 접두와 개수

| | 명령 수 | 카테고리 | 비고 |
|---|---|---|---|
| Continue | 39(고유 38) | `Continue` | 「Continue: …」를 제목에 접두로 또 쓰는 것이 섞여 있다 |
| Cline | 19 | `Cline` (14개만; 나머지 5는 아이콘 전용이라 카테고리 없음) | |
| Roo Code | 18 | 설정 제목(로컬라이즈) 13 · `Terminal` 3 · 아이콘 전용 4 | |

**아이콘 전용 명령에는 카테고리를 안 단다.** 셋이 공통이다. 툴바 아이콘으로만 서는 것이 팔레트에 뜰 이유가 없기 때문이다. 우리 [화면 설계 §5](./UI.ko.md)의 「전부 `magi:` 접두」는 이 지점에서
**틀렸다**(§4-나).

## 3. 단축키 — 갈린다

| | 개수 | 무엇 |
|---|---|---|
| Continue | **21** | 채팅 `cmd+L`, 편집 `cmd+I`, diff 수락/거절, 코드 조각 수락/거절, 터미널 디버그, 화음 셋… |
| Roo Code | 2 | 컨텍스트에 넣기(`cmd+k cmd+a`), 자동승인 토글(`cmd+alt+a`) |
| Cline | 3 | 채팅에 넣기 / 입력으로 점프(둘 다 `cmd+'`, `editorHasSelection` 으로 갈림), 커밋 메시지(키 없음) |

**Cline 의 한 키 두 뜻**이 눈에 띈다. 같은 `cmd+'` 를, 선택이 있으면 「넣기」로 없으면 「입력으로 점프」로 쓴다. `when` 으로 가른 것이고, 키를 아낀다.

**Continue 의 21개는 이웃 중 유일하게 많다.** 규약은 ❌ "Overwrite existing keyboard shortcuts" 라고 한다. 21개를 두면서 그것을 지키기는 어렵다.

## 4. 우리 결정에 대한 판정 셋

### (가) 대화를 패널에 두는 것 — **유지한다. 다만 사유를 바꾼다.**

셋이 다 액티비티 바인데 우리만 패널이면, 「이 편집기의 관습」은 저쪽이다. 그래도 유지하는 이유가
둘 있고, 둘 다 **우리가 저들과 다른 물건**이라는 데서 온다.

1. **우리 전사는 이 창의 것이 아니다.** Cline·Roo·Continue 의 대화는 그 확장이 그 창에서 시작한
   것이다. magi 의 전사는 **데몬이 원천**이라 터미널·웹 콘솔·젯브레인이 같은 대화를 본다. 그것은
   「이 편집기가 지금 하는 일」이 아니라 「이 워크스페이스에서 일어나는 일」이고, 터미널·문제·
   출력이 사는 자리가 그 성격이다.
2. **우리는 사이드바를 이미 쓴다.** 계획·컨텍스트·잡·플릿이 거기 간다(화면 설계 §3). 저들은
   그것들을 채팅 웹뷰 **안에** 그리므로 컨테이너가 하나면 된다. 우리는 둘이 필요하다.

⚠ **그래서 값을 치른다.** 규약이 "users often minimize the Panel" 이라 하고, 이웃을 쓰던 사람은
액티비티 바에서 찾을 것이다. 완화 둘: 상태 표시줄이 항상 서고([화면 설계 §4](./UI.ko.md)),
`magi: Open the conversation` 명령이 어디서나 연다. **그리고 사람이 끌어 옮길 수 있다** — 규약이
요구하는 「좁은 폭에서 살아남기」가 이 자리에서 값을 한다.

### (나) 「명령에 전부 `magi:` 접두」 — **고친다.**

셋 다 **아이콘 전용 명령에는 카테고리를 안 단다.** 뷰 툴바에만 서는 명령이 팔레트에 뜨면 소음이다.
화면 설계 §5 를 이렇게 고친다:

> 팔레트에 서는 명령은 `magi:` 접두. **뷰 툴바·컨텍스트 메뉴에만 서는 것은 카테고리를 안 달고,
> 필요하면 `commandPalette` 에서 `when: false` 로 감춘다.**

### (다) 「기본 단축키를 안 준다」 — **유지한다. 근거가 세졌다.**

Roo 2개, Cline 3개, Continue 21개. **적은 쪽이 둘이고 많은 쪽이 하나**이며, 규약은 남의 키를
덮지 말라고 한다. 우리는 0으로 시작하고, 사람이 원하는 것을 스스로 묶게 둔다. 다만 Cline 의
`when` 으로 한 키를 둘로 쓰는 수법은 나중에 키를 줄 때 **베낄 값이 있다.**

## 5. 재지 않은 것

- **Copilot Chat 의 실제 기여점.** 소스가 없어 문서만 읽었고, 그 문서는 프라이머리/세컨더리
  사이드바를 안 가른다. 「the editor sidebar」라고만 한다. 그래서 §1 표의 그 줄은 다른 셋과 같은
  근거가 아니다.
- **저들이 인라인 완성·인레이·코드 액션을 어떻게 등록하나.** 매니페스트에 안 적힌다 — 런타임에
  등록하는 것이 보통이라(Continue 는 `enableTabAutocomplete` 같은 설정으로 존재만 드러난다)
  소스를 읽어야 알 수 있고, 안 읽었다.
- **쓰는 사람이 실제로 어디에 두나.** 셋 다 사람이 끌어 옮길 수 있고, 기본값이 곧 사용 실태는
  아니다. 그것을 잴 방법이 없다.
- **설치 수·평판.** 검색 결과에 숫자가 있었지만 옮기지 않는다 — 이 문서가 답하려는 물음
  (「자리를 어디에 두나」)과 무관하고, 확인할 수 없는 숫자다.

## 출처

읽은 날: 2026-09-08.

- [Cline `apps/vscode/package.json`](https://raw.githubusercontent.com/cline/cline/main/apps/vscode/package.json) — `contributes` 실물
- [Roo Code `src/package.json`](https://raw.githubusercontent.com/RooCodeInc/Roo-Code/main/src/package.json) — `contributes` 실물
- [Continue `extensions/vscode/package.json`](https://raw.githubusercontent.com/continuedev/continue/main/extensions/vscode/package.json) — `contributes` 실물
- [GitHub Copilot Chat 문서](https://code.visualstudio.com/docs/copilot/chat/copilot-chat) — 문서만(§5)
- 규약 인용은 [플랫폼 대조표](./PLATFORM.ko.md)의 출처를 따른다
