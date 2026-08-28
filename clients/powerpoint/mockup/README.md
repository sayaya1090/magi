# PowerPoint 애드인 목업

`DESIGN.md` 의 화면 셋 — **채팅창**, **선택 인용**(§5.8), **안내**(§6.1) — 을 손으로 만져 보는 목업이다.
모델에는 안 붙는다. 답과 안내는 `FakeChat` 이 타이머로 낸다.

## 먼저, 무엇이 검증됐고 무엇이 안 됐는지

**이 머신에 PowerPoint 가 설치돼 있지 않다.** `/Applications` 에도 애드인 컨테이너에도 없다.
그래서 이 목업에서 오늘 **실제로 돌려 본 것은 브라우저 경로뿐**이고, `OfficeDeck` 은
**한 줄도 실행해 본 적이 없다.** 파일 머리에도 그렇게 적어 뒀다.

| 경로 | 덱 어댑터 | 오늘 상태 |
|---|---|---|
| 브라우저 (`http://localhost:3000/taskpane.html`) | `FakeDeck` | **돈다** — 인용·전송·안내 클릭까지 확인 |
| PowerPoint 사이드로드 | `OfficeDeck` | **미검증** — 붙여 볼 PowerPoint 가 없다 |

두 번째 줄을 "되겠지"로 적지 않는 이유는, 이 목업이 답하려는 질문 자체가 **재 봐야 아는 것**이기
때문이다. 붙여 보면 `DESIGN.md` §9 의 두 스파이크가 그 자리에서 답을 낸다.

- **S13** — `setSelectedSlides` → `setSelectedShapes` 두 번 호출의 순서·동기화가 실제로 먹는가.
- **S14** — 「선택 인용」을 누르려고 **포커스가 작업창으로 갈 때 캔버스 선택이 남는가.**
  안 남으면 인용은 단축키로만 되고, 그 바닥은 Windows 2601 · Mac 16.105.2 라 사실상 최신 빌드
  전용 기능이 된다(부록 A).
- **요구 집합 천장** — 붙는 순간 화면 위쪽에 한 줄이 뜬다. `PowerPointApi` 1.5·1.6·1.7·1.8 과
  `SharedRuntime` 1.1 을 호스트에게 직접 물어 그대로 적는다. 여기서 1.5 에서 끊기면 그게 곧
  §12 #4(LTSC 를 버리는가)의 실물 답이다. 브라우저에서는 **"가짜 덱 — 호스트가 없어 잰 것이
  없다"** 라고 뜬다 — 안 잰 것을 잰 것처럼 보이게 두지 않는다.

## 돌려 보기

```
node tools/serve.mjs          # http://localhost:3000/taskpane.html
node tools/smoke.mjs          # PowerPoint 없이 도는 확인
```

브라우저로 열면 왼쪽에 **가짜 캔버스**가 붙는다. PowerPoint 자리를 대신할 뿐이고 애드인에는
안 들어간다 — 도형 선택을 만들 곳이 필요해서 있다. 도형을 클릭해 잡고(Shift 로 여러 개)
오른쪽에서 「선택 인용」을 누르면 인용부호로 담긴다.

## PowerPoint 에 붙이기 (미검증 경로)

Office 는 애드인 소스를 https 로만 받는다. 인증서는 **직접** 만들어야 한다 — 개발용 CA 를
키체인에 심는 일이라 `tools/serve.mjs` 가 대신 하지 않고 명령만 알려 준다.

```
npx office-addin-dev-certs install
node tools/serve.mjs                       # 인증서를 찾으면 https 로 뜬다
```

그다음 매니페스트를 사이드로드 폴더에 둔다.

- **macOS** — `~/Library/Containers/com.microsoft.Powerpoint/Data/Documents/wef/` 에 `manifest.xml`
  복사 후 PowerPoint 재시작. 홈 탭에 「magi 창」 단추가 생긴다.
- **Windows** — 공유 폴더를 하나 만들어 신뢰할 수 있는 카탈로그로 등록하고 거기에 매니페스트를 둔다.

매니페스트에서 세 가지를 일부러 그렇게 뒀다.

1. **최상위 `<Requirements>` 에 `PowerPointApi` 를 안 적는다.** 적으면 조건이 안 맞는 빌드에서
   애드인이 목록에서 아예 사라져 사용자가 *왜* 없는지 모른다(§3.3). 확인은 런타임에 하고 **말한다.**
2. **`<Runtime ... lifetime="long" />` 은 적는다.** 작업창이 닫혀도 코드가 살아 있어야 하고
   (§5.7), 이 요구(SharedRuntime 1.1)는 문서가 이미 거는 1.8 바닥보다 낮아 새로 걸러내는 것이
   없다(부록 A).

   ⚠ **적는 자리를 MS 절차와 다르게 뒀다.** 절차는 최상위 `<Requirements>` 에 넣으라고 하는데
   그러면 못 맞추는 빌드에서 애드인이 아예 안 뜬다(1 번이 금지한 그것). `<VersionOverrides>`
   안에 두면 사라지는 것이 애드인이 아니라 **리본 단추**뿐이다. 걸리는 빌드가 이미 1.8 미달이라
   사람이 새로 걸러지지는 않는다 — 부록 A.

3. **`<Action>` 에 `<TaskpaneId>` 를 안 적는다.** 긴 수명 런타임이 있는 `<Host>` 아래에서는
   작업창이 하나여야 하고, MS 가 그걸 마크업 규칙으로 적어 뒀다(부록 A).

## 구조

유스케이스가 Office.js 를 **모른다.** 그래서 덱 어댑터 하나만 갈아 끼우면 같은 흐름이 맨
브라우저에서도 돈다 — PowerPoint 가 없는 오늘 검증할 수 있는 유일한 길이고, 층을 나눈 값이
그것이다.

```mermaid
graph TD
  UI[ui: view, fakeCanvas] --> UC[usecase: QuoteSelection, PointAtAdvice, SendTurn]
  UC --> D[domain: Quote, Advice, Conversation]
  UC --> P[port: DeckPort, ChatPort]
  OD[adapter: OfficeDeck] --> P
  FD[adapter: FakeDeck] --> P
  FC[adapter: FakeChat] --> P
  M[main.js] --> UI
  M --> OD
  M --> FD
```

- `domain/` — 값만 있다. `Quote` 는 **스냅숏**이다. 담을 때의 글과 크기를 그대로 들고 가고
  나중에 다시 안 읽는다. 보낼 때 도형이 달라졌으면 **조용히 바꿔치기하지 않고 멈춘다**(§5.8).
- `port/` — 덱으로 난 유일한 구멍. `selection()` 은 **풀 전용**이다. 도형 선택이 바뀌었다고
  알려 주는 이벤트가 PowerPoint 몫으로 적힌 곳이 없고(부록 A — MS 문서끼리 어긋나는 자리다),
  돈다 해도 *무엇이* 선택됐는지는 안 주므로 어차피 당겨야 한다. 그래도 되는 이유는 **누름 자체가
  이벤트**라서다.
- `usecase/` — `QuoteSelection` 은 `{added, skipped, empty}` 를 돌려준다. "고른 게 없다"와
  "포커스가 선택을 가져갔다"를 뭉뚱그리면 S14 를 못 재기 때문에 사유를 가른다.
- `adapter/` — `OfficeDeck` 만 Office.js 를 안다. `FakeDeck`·`FakeChat` 은 목업 전용이다.
- `main.js` — 조립하는 유일한 자리. `Office.onReady()` 와 1.5초 타임아웃을 경주시킨다.
  Office 밖에서는 `onReady` 가 **영영 안 풀리기** 때문이다.

## 아직 가짜인 것

- 모델. `FakeChat` 이 정해진 문장을 타이머로 낸다.
- 데몬·MCP. 목업은 소켓을 안 연다.
- 실제 편집. 인용은 담기지만 슬라이드를 고치지 않는다.
- **문서 열 때 자동 기동.** `Office.addin.setStartupBehavior(load)` 를 **안 부른다.** 설계는
  부르기로 했지만(부록 A) 그건 데몬이 있어야 뜻이 있는 줄이고, 목업은 소켓을 안 연다. 지금
  부르면 사용자 덱에 설정만 남는다 — 하는 일 없는 부수효과라 뺐다. 빼먹은 게 아니라 뺀 것이다.
