---
name: web_ui_module_dev
description: "새 콘솔(web/ui, handbook식 GWT 멀티모듈)에서 화면·컴패니언 타입 UI 모듈을 만들고 검증할 때 — 창 브리지 계약(렌더·명단·전사·이동), 셸의 스트림 단독 소유, 타입 카탈로그, gwt-test 하네스와 밟아본 함정들"
---

# web_ui_module_dev

이 스킬은 **`web/ui`(스트랭글러 신규 콘솔)에 화면 모듈을 추가·수정하거나 컴패니언 타입
전용 UI를 만들 때** 적용합니다. 정본 문서는 `web/ui/README.md`, 좌표는 `web/README.md`.

## 구조 (2026-08-27 빌드·테스트 40개로 검증된 사실)

- 모듈 하나 = 화면 하나 = `/ui/<name>/<name>.nocache.js`. 의존은
  `화면 모듈 → console-bridge ← shell-ui` 한 방향, 화면끼리는 서로를 모른다.
- 셸↔화면의 만남은 **window 브리지뿐**(GWT 모듈은 각자 딴 이름공간으로 컴파일된다):
  RenderSharing(`__magi_render`) · RosterSharing(`__magi_roster_*`) ·
  TranscriptSharing(`__magi_transcript_subscribe`/`__magi_turn_subscribe`) ·
  CompanionSharing(`__magi_companion_subscribe`) · GoSharing(`__magi_go`).
  구독은 **현재값을 재생**하므로 화면이 셸보다 늦게 로드돼도 첫 값을 놓치지 않는다.
- **스트림은 셸이 단독 소유**(창당 1스트림). 화면 모듈은 EventSource를 열지 않는다 —
  `hosted()`가 거짓일 때(단독 테스트 페이지)만 제 회선 폴백.
- **컴패니언 타입**: shell-ui `domain/CompanionType`이 (타입 키 → 모듈, 라벨) 카탈로그.
  무선언·미지 타입은 1(코딩 에이전트, companion-ui)로 푼다 — 빈 화면 금지. 새 타입 =
  카탈로그 한 줄 + 오퍼레이터가 설치한 모듈 하나. 컴패니언이 실어 보낸 스크립트는
  절대 로드하지 않는다(.magi/plugins와 같은 신뢰 경계).

## 새 모듈 절차

1) `build.gradle.kts`는 fleet-ui 것을 복사(웹포트만 바꾼다: fleet 18090 · shell 18091 ·
   companion 18092 · 다음 18093), `settings.gradle.kts` include, `<Name>.gwt.xml`의
   inherits는 User/elemento Core/dagger.Dagger/dev.sayaya.Ui/Rx/ConsoleBridge/UiComponents.
2) `client/domain`(DOM 무지) → `usecase`(포트·스토어) → `interfaces`(DOM·HTTP) 순.
   마크업 id·클래스는 기존 콘솔 page.js와 동일하게 — console.css가 읽는 계약이다.
3) EntryPoint: `RenderSharing.next((Render) frame -> { Labels.load(() -> mount(frame)); return true; })`.
   mount는 재진입 가드(`wired`)를 둔다 — 캐시된 렌더의 재마운트가 구독을 겹으로 쌓는다.
4) 테스트: 도메인 JVM 단위는 GWT source path(client/) **밖**(`dev/sayaya/magi/domain/`),
   브라우저 스펙은 kotest `GwtTestSpec`+`@GwtHtml`, 가짜 포트는 Dagger TestModule로 물린다.
   `src/test/webapp`의 js/·css/·<module>/ 디렉토리는 전부 생성물(gitignore) — 소스는 html뿐.
5) `./gradlew build` 초록 → `assembleConsole`(모든 모듈의 src/main/webapp 자산 포함) →
   `go run ./web/server`(7778, BFF 7777 프록시)로 `/next`에서 라이브 확인.

## 밟아본 함정

- 스크립트·자산 경로는 `/ui/` **절대** — 상대경로는 프록시(BFF)로 새 나간다.
- dagger가 아니라 **dagger-gwt** 아티팩트(GWT 모듈 xml이 그쪽에 실려 있다).
- GWT 라이브러리 모듈은 jar에 소스 동봉(`tasks.jar { from(allSource) }`).
- md-* 컴포넌트의 리액티브 필드는 attribute가 아니라 **property**로 넣는다.
- 셸 없이 뜬 화면에서 브리지 호출은 조용한 무시다 — 폴백 분기(hosted())를 잊으면
  테스트 페이지가 통째로 침묵한다.
- i18n 키는 기존 콘솔과 같은 팩(`cmd/magi-web/i18n/language.{en,ko}.json`)에 **양쪽**
  추가 — 키 폴백 덕에 테스트 페이지는 키 문자열 자체를 검사한다.

## 검증
cd web/ui && ./gradlew build && ./gradlew assembleConsole   # 테스트 전부 + 서빙 루트

## 레일의 두 기둥 (2026-08-27 정정 반영)

- 목적지(메뉴)는 전부 **메뉴 레일**의 것 — 툴 레일에 메뉴성 내용을 넣지 말 것(한 번 밟은 오용).
- **툴 레일**은 도구 ≥2인 문에서만: 접히면 기둥을 대신(← 복귀, 선택 유지), 열리면 둘째 기둥.
  규칙은 `domain/RailModes`(순수), 등록은 `ToolList.provide(destId, tools)` — 용례 대기.
- Playwright 함정: 클릭이 포인터를 레일 위에 남겨 피크(expand)가 켜진다 — 접힘(collapse)을
  재기 전에 `page.mouse().move(...)`로 손끝을 치울 것.

## 스타일 원칙 (사용자 지시 2026-08-27)

기능은 확장하되 **모양은 운영 콘솔을 따른다**: 같은 일을 하는 요소는 운영의 id·클래스
계약을 입어 console.css가 입히게 하고(턴바=#turnwrap/#turnbar(md-linear-progress)/#turnfor,
컴포저=.composer+md-outlined-text-field#t), 새 뼈대만 모듈 css에 토큰으로 조립한다.
새 색·그림자·애니메이션 발명 금지. 토큰 이름은 실측으로 확인(카멜케이스 --magi-ref-outlineVariant,
warn이지 warning 아님 — 틀려도 조용히 무색이 된다).

## 더 밟은 함정 (2026-08-27, 지식 화면)

- **GWT 캐시 규약을 서버가 지켜야 한다**: `<name>.nocache.js`는 no-store, `<hash>.cache.*`는
  immutable. 헤더 없이 내보내면 브라우저 휴리스틱 캐싱이 재컴파일 뒤 낡은 선택자↔새 산출물
  짝을 만들어 **간헐 공백**이 된다(web/server가 처리).
- **tr() 폴백은 치환 없이 키를 돌려준다** — 팩 없는 테스트 페이지에서 `{n}` 카운트 단언은
  숫자가 아니라 "단수/복수 키가 골라졌다"는 키 계약으로 잰다.
- **운영 CSS엔 자식 결합자·직계 구조 의존이 있다** — 운영 판(#skills 등)을 새 래퍼로 감싸면
  안쪽 여백류가 조용히 죽는다. 정밀 대조는 rect 프로브(top/left/width/height + 자식 순서)로:
  두 콘솔에 같은 JS를 넣어 수치로 비교할 것. 겹침처럼 "이상해 보이는 것"이 운영과 동일하면
  그대로 두는 것이 원칙이다.
- 모바일은 스펙에서 `page.setViewportSize(390,844)`로 재라(라이브 창은 OS 최소폭에 막힘) —
  가로 오버플로 0 + 핵심 요소 가시성, 셸은 하단 바 전환(#railMenu 숨김·railNav row)과
  body[at=agent]의 물러남까지.
- **`body[view=…]`를 적지 마라**: 운영의 화면 전환 CSS 기계(판 숨김·오프스크린 배치)가 그
  속성에 걸려 있다 — 마운트로 가시성을 관리하는 새 셸에서 켜면 판이 사라진다(실측 w=0/-1000).
  폰 규칙용 `body[at=agent|list]`만 운영 계약으로 적는다.
- **ui-components의 GWT source path는 `component`다**(client 아님) — 공용 클래스는
  `dev.sayaya.magi.component.*`에 둘 것(Rank가 첫 입주자).
- **문 없는 주소**(보드류): Destination.doors()엔 빼고 all()엔 넣는다 + `section()`이 레일에
  켤 문을 답한다. 화면이 카탈로그 화면으로 가는 문은 `GoSharing.view(v)`.
- 날짜 도는 화면의 픽스처 타임스탬프는 **Z 없이(로컬)** 적어라 — dayOf류가 로컬로 접어,
  UTC 픽스처는 KST에서 하루를 넘는다(보드에서 실측).
- **모양 대조는 수치로**: `scratchpad/cssdiff*.mjs`처럼 두 콘솔을 같은 뷰포트로 열어 계산
  스타일·rect를 항목별로 비교하라. 눈으로 "비슷"한 것이 커서(href 소실), 배지 부모,
  `--dock` 기본값 160px, 칩 마크 부재로 갈렸다(전부 실측으로 잡음).
- **아이콘은 빌려 쓴다**: 구 콘솔이 구운 `#isprite`를 `Icons.borrow`로 한 번 심고
  `dress/of/orGlyph`로 그린다 — 없는 빌드는 자체 도형으로 산다(운영과 같은 계약).
- **모듈마다 static이 따로다**(페더레이션): 창에 하나면 족한 것(언어 팩·스트림·컨텍스트)은
  반드시 창 브리지로 공유하라. 모듈 안 static 캐시만으로는 모듈 수만큼 중복된다.
- **렌더 콜백은 마운트마다 불린다**(캐시된 렌더 재마운트 포함) — 그 안에서 fetch하면
  화면을 옮길 때마다 샌다. 한 번이면 되는 일은 렌더 밖에서.

## 컴패니언 화면의 두 겹 (2026-08-27)

- `companion-ui`가 목적지의 주인이다: 목록(?d= 없음)과 상세 레이아웃(위 사실판·오른쪽 판).
- 가운데·왼쪽은 **자식 타입 모듈**이 `PaneSharing.next("centre"|"left", render)`로 채운다.
  왼쪽은 여러 번 밀면 쌓인다. 자식 이름은 셸 카탈로그 → `ctx.ui` → `ModuleInject.ensure`.
- 새 타입 UI = `CompanionType`에 한 줄 + 자식 모듈 하나(오퍼레이터 설치분만).
- **빌드 스크립트는 모듈에 두지 않는다** — 루트 표에 한 줄, 포트는 그 순서로 자동.
- 모듈은 제 스타일시트를 `Stylesheet.ensure("<name>")`로 스스로 건다(셸의 html은 셸 것만 안다).
- 왼쪽 슬롯의 첫 실사용처는 워크스페이스(coding-agent-ui)다 — 트리는 **열린 부모 아래
  열린 가지만** 걷고(순수 규칙 `domain/Tree`), 카드마다 제 스크롤을 갖는다. 와이어는
  라이브로 확인할 것: `/files`는 `{dirs:{경로:[{name,isDir}]}}`, `/git`은 `repo:false`도 답이다.
- 쓰기 컨트롤은 `May.can("shell")`로 그릴지 말지만 정한다(게이트는 서버). 능력은 `bridge.May`가
  창당 1회 읽는다 — 모듈마다 /me를 받지 말 것.
- **hover 노출 컨트롤의 스펙**: 동작은 `evaluate("b => b.click()")`로 결정적으로 재고, 노출은
  `mouse().move(5,5)` + `activeElement.blur()`로 손끝·포커스를 치운 뒤 따로 잰다(둘 다 규칙을 켠다).
- **브라우저 라운드**(`scratchpad/uitest/*.mjs`)를 돌려라 — 단위 테스트가 초록이어도 폰의
  출구·판 전환·데모 404·좁은 열 뭉갬은 거기서만 드러난다. 데스크톱과 폰 두 폭 모두.

## 부모가 지는 것 (2026-08-27, 사용자 원칙)

"부모는 자식이 지켜야 할 규약 부담을 최대한 줄여줘야 한다." 그래서 자식 모듈에는 다음이
**없다**: `Labels.load` 감싸기, `Stylesheet.ensure`, body 속성, `--dock`, 운영 CSS의 뼈대 이름.
새 화면·새 타입 UI를 만들 때 이것들을 자식에 적고 있으면 잘못 짚은 것이다.

- 팩: 부르는 쪽이 기다리고(`FrameElement.mount`), `tr()`이 창에서 든다(모듈마다 static이 따로).
- 시트: 카탈로그가 선언(`Destination.styles` / `CompanionType.styles`)하고 들이는 쪽이 건다.
- 자리: 부모가 옷 입힌 상자를 슬롯으로 건넨다(`centre`·`left`·`dock`), 껍데기는 `display:contents`.

## 컴패니언 뼈대는 운영 이름 그대로 (2026-08-27 실측)

`#agentview/#filecol/#stream/#sidecol/#dock .bay`. 새 이름을 쓰면 창 높이 앵커·기둥 접기·
도크 여백이 전부 비켜간다. 기둥은 **기본 닫힘**(폭 0)이고 손잡이는 마스트헤드(`ChromeSharing`).
폰 판 전환의 기준은 **52.5em**(운영이 그 위에서 `#ptabs`를 `display:none !important`로 누른다)
— 더 넓은 기준을 쓰면 840~1023px에서 탭도 없이 판만 감춰진다(실측: 860px에서 전사 폭 0).

⚠ `--dock` 같은 값을 실측해 쓸 때는 **바뀐 값만** 쓸 것: 쓰는 순간 배치가 바뀌고 그 변화가
ResizeObserver를 다시 깨워 왕복이 멈추지 않는다(브라우저가 응답을 멈춰 스펙까지 타임아웃).

## 모듈 이름은 화면 판의 id와 겹치면 안 된다 (2026-08-27 실측)

GWT의 IFrameLinker는 컴파일된 모듈을 숨은 0×0 iframe 안에서 돌리고, **그 iframe의 id를 모듈
이름으로 단다**(`a=createElement('iframe'); a.id=R`). 그래서 rename-to가 `skills`면 화면 판
`#skills`와 id가 둘이 된다(구 콘솔은 모듈이 하나라 없던 문제). 판의 id는 운영 계약이니 바꿀
쪽은 모듈 이름이다 — 지금: knowledge / boardview / mapview / accessview, 주소(`Destination.id`)는
운영 그대로(`skills` 등). ⚠ 이름을 바꾸면 `*/build/gwt/war`와 `build/console`에 옛 이름의
산출물이 남아 함께 배포된다 — 지우고 다시 assemble할 것.

이 iframe이 "모듈마다 static이 따로"의 정체이기도 하다: 페더레이션이 은유가 아니라 문자 그대로
분리된 전역 스코프다(그래서 언어 팩·스트림은 창 브리지로 공유한다).

## 폭별 대조는 화면마다 (scratchpad/uitest/widths.mjs)

구/신 두 콘솔을 8폭(2560~390)×화면 5개로 열어 판의 rect를 항목별로 비교한다(2px 허용).
2026-08-27 이 표로 잡은 것: 지식 화면의 좁은 폭 고르개(#sharedTabs, 52.5em 아래에서 셋 중
하나만) 미이식, 지식 카드의 메타에서 tags·groups 누락(카드가 18px 낮았다), 접근 문의 짧은
이름 미사용(폰 하단 바가 12px 자람), 그리고 위 id 충돌. **구버전이 늘 옳은 것은 아니지만,
다를 때는 왜 다른지 먼저 볼 것** — 이 넷은 전부 구버전이 옳았다.

## 움직임도 계약이다 (2026-08-27 실측)

운영의 움직임은 대부분 **클래스가 시작시킨다** — CSS만 베껴도 아무 일도 일어나지 않는다:

| 클래스 | 언제 | 누가 붙이나 |
|---|---|---|
| `.enter`(fadeThrough 200ms) | 목적지가 그려질 때마다 | 부르는 쪽(FrameElement.mount) — 컴패니언은 제 대화 기둥만 |
| `.slideL`/`.slideR` | 폰에서 판을 옆으로 옮길 때, 움직인 방향으로 | 판을 가진 쪽(CompanionElement) |
| `.rise` | 도크의 질문 상자가 올라올 때 | ⚠ 아직 그 상자가 없다(미이식) |
| `.noticed` | 명단 행의 상태가 **바뀌었을 때만**(첫 등장 제외) | CardListElement |
| `.spin` | 도는 중인 아이콘 | 그리는 쪽 |

⚠ 다시 붙이기 전에 **떼고 레이아웃을 한 번 읽어야** 한다(`Motion`이 그 일을 한다): 돌고 있는
애니메이션은 같은 클래스를 다시 붙여도 재시작하지 않아, 두 번째 방문이 조용히 무동작이 된다.
자리를 지켜야 하는 줄(요약 칩 등)은 `data-still`로 빠진다 — 화면이 선언하고 부모가 읽는다.

`scratchpad/uitest/motion2.mjs`(등장), `motion3.mjs`(폰 방향), `trans.mjs`(트랜지션 대조),
`railanim.mjs`(레일 라벨/서브)로 잰다. round 라운드가 화면마다 `.enter`를 지킨다.

## 도크의 질문 상자 (2026-08-27 이식)

`#prompt`는 **부모(companion-ui)의 것**이다 — 무엇을 물었든 답하는 방식은 타입과 무관하고,
도크도 이미 부모의 것이라서. 세 갈래(운영 그대로): 퍼미션=결정 넷(한 무게 + 표), 보기 딸린
질문=보기 버튼 + "그밖에", **맨 질문=상자를 그리지 않고 컴포저가 답한다**(글 상자 둘을 위아래로
세우지 않는다는 규칙). 그 마지막 갈래만 자식에게 알린다: `AskSharing`(창 브리지, 현재값 재생) —
자식은 제 입력의 목적지를 바꾸고, 라벨·버튼 낱말·아래 한 줄을 **몫이 바뀔 때만** 갈아입으며,
쓰던 초고는 지우지 않고 맡아 둔다(몫마다 제 초고).

## 남은 화면 셋 이식 (2026-08-27)

- **회의실**(meeting-ui, v=meet&m=): 주소의 조각(?m=)은 셸이 싣고 되읽는다 — 화면이 직접
  pushState하면 뒤로가기가 셸이 모르는 자리에 선다. `GoSharing.viewWith` + `Destination.PIECES`.
  폴 두 초, 쓰는 중이면 그리지 않기, 내용이 같으면 다시 그리지 않기(운영의 그 세 규칙).
- **환경설정**(settings-ui, v=settings): 다이얼로그가 아니라 화면 — 같은 컨트롤이 서 있는
  자리로 다른 config를 고치기 때문이고, 그 사실을 맨 위에 적는다. 저장 버튼 없음.
- **⌘K 팔레트**(shell-ui): 창의 것이라 화면 밖에 선다. 화면이 제 항목을 더하는 문은
  `PaletteSharing` — 툴 레일(ToolList)과 같은 규칙(셸이 자식의 기능을 알지 않는다).
  ⚠ md-dialog는 `open` 속성이 아니라 제 메서드로 연다(`show()`), 안 그러면 아무 일도 없다.

⚠ **한 API를 두 모듈이 읽고 있으면 단일 원천이 아니다**(사용자 지적): 지금 `/fleet`+`/events`를
shell-ui(RosterSource)와 companion-ui(FleetRepository)가, `CompanionSource`를 companion-ui와
coding-agent-ui가 각각 갖고 있다. 다음 정리 대상.

⚠ **데모의 목은 회선의 이음매에 건다**(2026-08-28 재정리): 모듈마다 `Demo*Source`를 싣던 방식은
운영 번들에 데모를 함께 실었고, 화면의 진짜 회선 코드가 데모에서 한 번도 돌지 않았다. 지금은
`demo-ui` 하나가 경로로 답하고 `Console.raw`/`Console.stream`이 그것을 건넨다. `window.fetch`를
갈지 않는 것은 그대로 유효하다(모듈 프레임의 fetch에 닿지 않는다) — 창의 **속성**으로 건넨다.

## 목은 모듈이 싣고, API의 주인은 하나다 (2026-08-27, 사용자 지적 둘)

**① 데모의 목은 모듈 하나, 회선에 건다**(2026-08-28 정정). 뒤에 데몬이 있느냐는 화면의 성질이
아니라 회선의 성질이다: `demo-ui`가 경로별로 답하고, `Console.raw`/`Console.stream`이 그 답을
건넨다(프록시). 화면 모듈은 살아 있는 소스 하나만 갖고 데모를 모른다. 운영 자산에는 목이
없다(`assembleConsole`이 `demo/**`를 뺀다 · `assembleDemoMock`이 따로 싣는다). 페이지가
`window.fetch`를 갈아끼우는 방식이 안 되는 이유는 그대로다 — 모듈은 제 프레임의 fetch를 쓴다.
⚠ 새 경로를 부르면 목에도 한 줄: `TestTheMockAnswersEveryPathTheScreensAsk`가 화면이 부르는
모든 길을 목이 답하는지 본다(파일로 서빙되는 것은 `served` 표에 적어 면제).
밟은 함정 셋(전부 실측): elemental2의 `EventListener`는 @JsFunction이 아니라 native @JsType이라
자바 람다가 **객체로** 건너온다 — `handleEvent`를 떼어내 부르면 this가 사라진다(그 객체에게
물어야 한다). 흉내 낸 평범한 객체 대신 **진짜 MessageEvent**를 건네야 한다. 그리고 목은
회선보다 빨라선 안 된다 — 같은 틱에 알리면 아직 마운트되지 않은 화면에 말을 건다(50ms).

**② 한 API를 두 모듈이 읽으면 단일 원천이 아니다.** 정리한 것:
- `/fleet`+`/events` → 셸 하나. companion-ui·coding-agent-ui의 "브리지 없으면 제 회선" 폴백을
  삭제(그 폴백이 창당 스트림 하나라는 규칙을 안에서 깨고 있었다), board/map도 RosterSharing.
- `/plan`·`/context`·`/compact` → 컴패니언 패널(부모)만. 자식에서 삭제.
- `/answer` → 부모만. 자식 컴포저는 `AskSharing.answer`로 넘긴다(질문을 아는 쪽이 보낸다).
- `/console`·`/me` → 셸이 한 번 읽어 창에 올리고(`Facts`), 화면은 든다. `May.load`는 이제
  읽지 않는다(셸이 올린 것을 읽기만).
같은 엔드포인트라도 **다른 질문**이면 각자 주인이다(예: `/history`는 자식의 "이 컴패니언의
지난 일"과 보드의 "모두의 지난 일").

## PWA·푸시·스프라이트 (2026-08-27)

- 설치에 필요한 것(manifest·icon·icon-maskable·sw.js)과 **아이콘 스프라이트**는
  `internal/webassets` 단일 원천. 옛 콘솔이 사라져도 새 콘솔이 그것을 잃지 않는다.
  console.html의 `<!--ICON-SPRITE-->` 자리에 서버가 심는다(라이선스 없는 빌드는 표식만 사라짐).
- 알림은 브라우저의 사실이라 `interfaces/Notifications`가 브라우저 API를 직접 만진다.
  ⚠ **JSNI의 파서는 옛 문법만 안다**: `async`/화살표 함수는 문법 오류, `.catch(` 는 예약어라
  `["catch"](…)`로 불러야 한다(둘 다 실측).
- 숨은 탭은 스트림을 반납한다(`visibilitychange`) — 돌아오면 열고 명단 한 번으로 따라잡는다.
