# web/ui — 새 콘솔의 UI 개발문서

상위 [`web/README.md`](../README.md)가 무엇을 왜 하는지(스트랭글러 좌표·대조표·컷오버 조건)를
말한다면, 이 문서는 어떻게 하는지다: 모듈이 어떻게 나뉘고, 셸과 화면이 무슨 계약으로 만나고,
화면 하나를 이식할 때 어디를 만지는지. 정본은 아직 기존 콘솔(`cmd/magi-web`)이다 — 여기
적힌 것과 기존 콘솔이 다르게 굴면 기존 콘솔이 맞다.

## 지도

```
console.html                     ← web/server(7778)가 /next 에서 서빙
└── /ui/shell/shell.nocache.js   셸(shell-ui): 레일·마스트헤드·라우팅·모듈 주입
    ├── /ui/fleet/fleet.nocache.js       화면 모듈 — 필요할 때 셸이 주입
    ├── /ui/companion/… (예정)
    └── window 브리지(console-bridge)    셸↔화면의 유일한 만남
```

모듈 열둘. 의존은 `화면 모듈 → console-bridge ← shell-ui` 한 방향이고, 화면끼리는 서로를
모른다.

| 모듈 | 무엇 | 산출물 |
|---|---|---|
| `console-bridge` | 셸↔화면 계약: 창 브리지(Render·Roster)·와이어 DTO(FleetAgent)·언어 팩(Labels)·HTTP 관용구(Console) | 소스 실린 jar (GWT 라이브러리 규약) |
| `ui-components` | 공용 위젯 자리 — 지금은 모듈 선언(`UiComponents.gwt.xml`)만 | 소스 실린 jar |
| `shell-ui` | 셸: 드로어(1단+2단)·마스트헤드·Navigation(Place)·RenderStore·RosterStore·타입 카탈로그(CompanionType)·ScriptModuleLoader | `shell/shell.nocache.js` + console.html + shell.css |
| ~~`fleet-ui`~~ | 목록 화면이 **컴패니언 목적지의 것**이 되면서 companion-ui로 들어갔다(513e26c6). 디렉토리는 남아 있지만 빌드 표(settings.gradle.kts)에 없다 — 참고용이고, 새 화면의 본은 companion-ui/knowledge-ui다 | — |
| `companion-ui` | 컴패니언이라는 목적지의 주인 — **목록**과 **상세 레이아웃**(위 사실판·오른쪽 판), 가운데·왼쪽은 자식에게 내준다 | `companion/companion.nocache.js` + companion.css |
| `coding-agent-ui` | 타입 1(코딩 에이전트)의 자식 UI — 가운데(대화·컴포저)와 왼쪽(워크스페이스: 트리·git) | `coding/coding.nocache.js` + coding.css |
| `knowledge-ui` | 지식 화면(경험·위키·서버) — 주소 v=skills, 모듈 이름도 skills | `skills/skills.nocache.js` |
| `board-ui` | 보드 — 문 없는 주소(v=board): 레일은 컴패니언 문, 진입은 플릿의 .toview | `board/board.nocache.js` |
| `map-ui` | 맵 — 문 없는 주소(v=map): 머신·계정 상자와 오간 것의 와이어 | `map/map.nocache.js` |
| `access-ui` | 접근 제어(v=access) — 레일의 셋째 문, admin 게이트 | `access/access.nocache.js` |
| `meeting-ui` | 회의(v=meet) — 방 목록과 방 하나 | `meeting/meeting.nocache.js` |
| `settings-ui` | 환경설정(v=settings) — 이 브라우저의 것과 데몬이 읽는 것 | `prefs/prefs.nocache.js` |
| `demo-ui` | **화면이 아니다** — 정적 데모에서만 실리는 목. 경로로 답하고 회선의 이음매에 걸린다 | `demo/demo.nocache.js` (운영 자산에는 없다) |

## 셸과 화면의 계약 (console-bridge)

계약은 전부 window 전역이다. GWT 모듈은 각자 딴 이름공간으로 컴파일되므로, 만나는 자리는
창밖에 없다.

- **렌더** (`__magi_render`): 셸이 수신자를 걸고(RenderSharing.register), 화면 모듈이
  로드 끝에 렌더 — 프레임 엘리먼트를 받아 그리는 함수 — 를 민다(RenderSharing.next).
  렌더에는 주인이 안 실리므로 셸의 RenderStore가 "지금 로드 중인 목적지"(expect)를
  주인으로 적고 목적지별로 캐시한다. 재방문은 스크립트 재주입 없이 캐시로 다시 그린다.
- **명단** (`__magi_roster_subscribe` / `__magi_roster_refresh`): 창당 1스트림 규칙의
  기전. 셸의 RosterStore가 `/fleet`+`/events`(SSE)의 유일한 소유자로 두 문을 걸고
  (RosterSharing.host), 화면은 구독만 한다 — 자기 EventSource를 열지 않는다. 셸 없이
  단독으로 뜬 화면(테스트 페이지)은 hosted()가 거짓이라 제 회선으로 폴백한다.
- **전사** (`__magi_transcript_subscribe` / `__magi_turn_subscribe`): 컴패니언에 조준된
  같은 회선의 기본 프레임(전사 행 전체 배열)과 turn 프레임을 셸이 받아 이 문으로 흘린다.
  전사의 null은 "아직/못 읽음" — 새 컴패니언으로 조준이 옮겨질 때도 null이 먼저 흘러
  이전 대화가 새 화면에 비치지 않는다.
- **컨텍스트** (`__magi_companion_subscribe`): 지금 보는 컴패니언(CompanionContext:
  socket·peer·type). type은 셸이 타입 카탈로그로 이미 해석한 키다 — 화면 모듈은 읽기만.
- **이동** (`__magi_go` / `__magi_go_view` / `__magi_go_past` / `__magi_go_sub`): 화면이
  셸에 이동을 청하는 문들 — 컴패니언으로(플릿의 행), 카탈로그 화면으로(플릿의 .toview),
  지난 일 층위로(?past= — null=지금 대화, ""=목록, 값=그 세션), 그리고 자식 하나로
  (?sub=<id> — 그 컴패니언이 낳은 아이의 전사). 지난 일과 자식은 **같은 자리**를 대신하고
  함께 서지 않는다. 주소(pushState)는 셸의 것이라 화면이 만지지 않는다.
- **HTTP**: Console.fetchList는 거부(HTTP 에러)·불통·깨진 본문을 전부 null로 접되
  console.warn에 원문을 남기고, Console.post는 대상을 `?d=<socket>&p=<peer>`로 지목한다
  (성공=빈 문자열, 거부=사유). 기존 page.js의 fetchList/post 이식이다.
- **말** (`__magi_labels` / `__magi_labels_stream` / `__magi_labels_v`): Labels — 기존
  콘솔과 같은 `/i18n/language.{en,ko}.json`.
  팩도 창에 하나다: 먼저 읽은 모듈이 창에 올리고 뒤에 오는 모듈은 그것을 든다. 이게
  없으면 **모듈 수만큼** 받는다(static은 모듈마다다) — 게다가 렌더는 마운트마다 불려서
  이동할 때마다 한 번씩 더 샜다(실측: 부팅 2회 + 이동마다 1회 → 지금 창당 1회). tr()은 키 폴백("번역
  빠짐"이 보이게), stateWord()는 원어 상태어 폴백(행에 "state.gone"을 안 적으려고).
  팩은 **흐름**이다(운영 labels$과 같은 계약): 화면이 `Labels.onPack(this::render)` 한 줄로
  듣고, 사람이 언어를 갈면 그 자리에서 다시 칠한다. 모듈마다 제 사본을 들고 있어서
  갈렸다는 사실은 창이 센다(`__magi_labels_v`) — 이게 없으면 이미 한 번 읽은 모듈이 옛말을
  계속 든다(실측: 설정 화면만 한국어가 되고 마스트헤드·레일은 영어였다). 고른 언어는
  운영과 같은 자리(localStorage `lang`)에서 읽고, 팩이 아닌 것(배열·오류 페이지)은 앉히지
  않으며, 못 읽으면 영어로 물러선다.
- **와이어**: FleetAgent — `/fleet` 행의 JsType DTO. 필드명은 `internal/adapter/fleet`의
  json 태그와 일대일이고, omitempty 필드는 JS에서 undefined라 읽는 쪽이 가드를 진다.

## 셸의 흐름

주소가 원본이다. 카탈로그 화면은 `?v=<id>`, 컴패니언은 `?d=<socket>`(&p=) — 기존
콘솔의 그 주소라 옛 링크가 같은 곳에 닿는다. Navigation이 이를 Place(어느 문 + 어느
컴패니언)로 읽고, 문 클릭·행 클릭·뒤로가기가 같은 settle로 모인다. ShellInitializer는:
레일 select(컴패니언이어도 그 문이 켜져 있다) → 스트림 조준(RosterStore.aim: 전사·턴이
같은 회선에 실리고 컨텍스트가 흐른다) → 모듈 결정: 카탈로그 화면이면 목적지 id,
컴패니언이면 **타입 카탈로그**(CompanionType: 명단 행의 type 선언, 무선언·미지는 1 =
코딩 에이전트 = companion-ui — 빈 화면 금지) → RenderStore 캐시 조회 → 없으면 expect +
ModuleLoader.ensure(`/ui/<name>/<name>.nocache.js`, 모듈당 한 번) → 렌더를 프레임에
mount. 경로는 `/ui/` 절대다 — 상대경로는 프록시(BFF)로 새 나간다(관통 때 실측한 결함).
새 타입 = CompanionType 한 줄 + 오퍼레이터가 설치한 모듈 하나(디자인·인프라 관리·
리서처가 후보로 이름만 있다).

드로어는 메뉴 레일+툴 레일(handbook의 번역)이다. **메뉴 레일**이 목적지 전부(컴패니언
포함)의 집이고, 열리면 라벨·문장도 메뉴 레일이 말한다 — 개폐는 운영 콘솔과 같은
2속성(nav=open 폭 + nav-wide 모양, 닫힘은 모양이 250ms 늦게): console.css의 펼친
배치가 nav-wide를 읽는다. **툴 레일**은
도구가 2개 이상인 문에서만 선다 — 속이 비면 펼쳐지지 않는다(handbook 규칙): 접힌
드로어에선 메뉴 기둥을 **대신해** 기둥이 되고(아이콘, 손끝이 레일 위면 라벨 피크, 첫
항목 ←는 메뉴 레일로 복귀 — 선택 유지), 열린 드로어에선 1단 오른쪽의 둘째 기둥이다.
규칙은 domain/RailModes(순수 — JVM 테스트), 사실 수집은 usecase/RailMode, 결과는
#rail의 menu/tool 속성으로 적혀 shell.css가 읽는다. 도구는 usecase/ToolList.provide로
문별 등록 — **아직 부르는 곳이 없다**(용례 대기): 오늘의 화면은 메뉴 기둥뿐이다.
마스트헤드와 레일 배지는 RosterStore에서 읽는다 — 그리는 곳이 늘어도 요청은 늘지 않는다.

## 클린 아키텍처 규칙 (모든 화면 모듈 공통)

`interfaces → usecase → domain` 한 방향. knowledge-ui가 한 화면의 레퍼런스이고(포트 하나·
스토어 하나·판 셋), companion-ui는 목적지의 주인이 어떻게 자식에게 자리를 내주는지의 본이다.

- `client/domain` — 순수 규칙, DOM 무지. JVM 단위 테스트가 여기 붙는다. 단, 테스트
  파일은 `client/` 밖(`dev/sayaya/magi/domain/`)에 둔다: client/ 안은 GWT source path라
  gwtCompile이 JUnit까지 컴파일하려 든다.
- `client/usecase` — 포트와 스토어. 스토어는 **흐름 그 자체**다(handbook의 그 관용구:
  `@Delegate BehaviorSubject`): 구독하면 지금 값이 즉시 오고, 같은 값이 두 번 오는 일은
  스토어에서 끊는다. "무언가 달라졌다"만 나르던 여덟은 공용 밑감 하나로 모았다
  (`bridge/Told`). RxJS는 창의 `rxjs`를 쓰고(페이지가 셸보다 먼저 올린다), 그 바인딩은
  dev.sayaya.rx다.
- **조각을 내려보낸다.** 큰 스토어가 전부를 흘리면 받는 판마다 "내 것이 바뀌었나"를 제
  손으로 따져야 하고, 한 곳이라도 빠뜨리면 아무 소식 없는 판이 초당 한 번 다시 선다.
  그래서 스토어가 잘라서 내려보낸다: `RosterStore.of(socket, peer)`(그 컴패니언의 행),
  `CompanionStore.aimed()`·`alive()`, `WorkspaceStore.treeFacts()`·`gitFacts()`,
  `Told.when(그리는 것)`. 비교에서 **도는 숫자는 뺀다** — 매 초 달라지는 값을 넣으면
  "바뀌었다"가 늘 참이라 거른다는 말에 뜻이 없다(실측: 사실판이 12초에 1402번 →
  0번, 전사 49번 → 7번, 지도 70 → 8, 우측 판 30 → 0).
- `client/interfaces` — DOM과 HTTP 어댑터. 마크업의 id·클래스는 기존 page.js와 동일하게
  간다 — console.css가 읽는 계약이다.
- Dagger가 포트에 구현을 묶고(FleetModule), 테스트는 같은 자리에 페이크를 묶는다
  (FleetTestModule). HTTP 목 없이 화면을 검증할 수 있는 이유다.

## 화면 하나 이식하기

1. 모듈 디렉토리 + `<Name>.gwt.xml`을 만들고(빌드 스크립트는 모듈에 두지 않는다 — 루트 표에 한 줄)
   `settings.gradle.kts`에 include.
2. domain → usecase → interfaces 순으로 이식한다. 마크업 id·클래스는 기존 콘솔 그대로.
3. EntryPoint에서 RenderSharing.next로 렌더를 등록한다 — 프레임을 받아 Labels.load 뒤
   mount하는 함수(FleetApplication 참조).
4. 문을 단다: Destination.doors()/all()에 한 줄(문 없는 주소는 all()에만, `section()`이
   레일의 문을 답한다). 능력이 필요한 문은 `may`를 달면 셸의 MayStore가 접는다 —
   게이트는 늘 서버가 지고, 이건 눌러서 거절에 닿는 문을 없애는 것뿐이다 — 아이콘 패스는 기존 콘솔의 그 드로잉. 문은
   이식이 끝난 화면에만 단다. 빈 화면으로 가는 문은 없는 문보다 나쁘다.
5. 테스트: 도메인 JVM 단위 + Playwright 브라우저 스펙(kotest GwtTestSpec, 전용 테스트
   html). webPort는 모듈마다 하나씩 — fleet 18090, shell 18091, companion 18092, knowledge
   18093, board 18094, map 18095, access 18096, 다음은 18097.
6. 타입 전용 UI라면 문 대신 카탈로그다: Destination이 아니라 CompanionType에 한 줄 —
   화면 계약은 같다(렌더 등록 + CompanionContext·전사·턴 구독). companion-ui가 레퍼런스.
7. `../README.md` 대조표의 그 행을 갱신한다.

## 스타일 원칙: 기능은 늘려도 모양은 운영을 따른다

**대조는 눈이 아니라 수치로.** `scratchpad/cssdiff*.mjs`가 두 콘솔을 같은 뷰포트로 열어
계산 스타일과 rect를 항목별로 비교한다(커서·배지 부모/좌표·그리드·여백·아이콘 렌더 방식).
이 방식으로 잡은 실제 결함 넷: 행의 href 소실(커서가 default였다), 배지가 열린 드로어에서
재부모화되지 않음, `--dock` 미설정으로 main 하단 여백 160px(운영 32px), 요약 칩의 상태
마크 부재. 눈으로는 "비슷해 보였다".

새 구조물이라도 스타일은 운영 콘솔의 것부터 쓴다 — 같은 일을 하는 요소는 운영의
id·클래스 계약을 그대로 입어(console.css가 공짜로 입힌다: 전사 .row/.txt, 컴포저
.composer+#t, 턴바 #turnwrap/#turnbar/#turnfor, 레일 .raili), 정말 새로운 뼈대만 모듈
css에 적되 토큰(--magi-ref-*·--magi-sys-*·--md-sys-typescale-*)으로만 조립한다.
새 색·새 그림자·새 애니메이션을 지어내지 않는다 — 컷오버 때 두 콘솔이 같아 보여야
대조가 성립한다.

## 아이콘: 굽는 곳은 하나, 빌려 쓰는 곳은 둘

그림(Font Awesome Pro)은 라이선스상 파일로 재배포하지 않으므로 구 콘솔이 빌드 타임에
페이지 안에 굽는다(`icons.go`의 `#isprite`). 새 콘솔은 제 사본을 만들지 않고 부팅 때 그
페이지에서 한 번 가져와 문서에 심는다(`Icons.borrow` — 개발 서버에선 `/`, 정적 데모에선
`../`). 그 뒤 `Icons.dress`가 `data-i`를 단 도형을 스프라이트 그림으로 갈아입히고,
`Icons.of/orGlyph`가 새로 그리는 자리에서 같은 규칙을 쓴다. **스프라이트가 없는 빌드도
정상**이다 — 그때는 늘 그리던 제 도형이 남는다(운영 `icon()`의 그 계약).

## 단일 원천 복사 (스냅샷 드리프트 없음)

기존 콘솔과 새 콘솔이 다른 팔레트·다른 번들을 갖는 순간 대조가 무의미해지므로, 아래는
빌드마다 원천에서 복사한다:

| 무엇 | 원천 | 어디로 |
|---|---|---|
| console.css | `cmd/magi-web/page.css` | assembleConsole → `build/console/` · 각 테스트 webapp `css/` |
| material.js | `cmd/magi-web/vendor/material.js` | 테스트 webapp `js/` (프로덕션은 BFF `/vendor/` 프록시 — 두 콘솔이 한 번들) |
| rxjs.js | `cmd/magi-web/vendor/rxjs.js` | 테스트 webapp `js/` · 데모 `vendor/` (스토어가 그 위에 산다 — 페이지가 셸보다 먼저 `window.rxjs`에 올린다) |
| shell.css | `shell-ui/src/main/webapp/shell.css` (원천이 여기) | assembleConsole · 테스트 webapp `css/` |
| companion.css | `companion-ui/src/main/webapp/companion.css` (원천이 여기 — 모듈이 스스로 <link>를 단다) | assembleConsole · 테스트 webapp `css/` |

assembleConsole은 모든 모듈의 `src/main/webapp`을 함께 나른다 — 화면 모듈 자신의
자산(css)은 제 모듈에 둔다. 그래서 `src/test/webapp/` 아래 `js/`·`css/`와 GWT 컴파일
산출 디렉토리(`fleet/`·`fleettest/`·`shell/`·`shelltest/`·`companion/`…)는 전부
생성물이고 gitignore다. 거기서 소스는
테스트 페이지 html뿐이다.

## 정적 데모 (Pages의 next/)

`go run ./web/server -emit-demo <dir>` — 조립된 콘솔을 자답(自答) 정적 사이트로 쓴다.

목은 **화면이 아니라 회선의 이음매**에 걸린다. 모듈마다 `Demo*Source`를 싣던 방식(11개,
1193줄)은 운영 번들에 데모를 함께 실었고, 화면이 실제로 쓰는 회선 코드(경로 조립·스트림
열기·프레임 파싱·쓰기)는 데모에서 한 번도 돌지 않았다. 지금은 `demo-ui` 모듈 하나가
경로로 답하고 `Console.raw`/`Console.stream`이 그 답을 건넨다(프록시). 화면 모듈은 살아
있는 소스 하나만 갖고 데모를 모른다.

`window.fetch`를 갈아끼우지 않는 이유는 그대로다: GWT 모듈은 제 프레임에서 돌아 그
프레임의 fetch를 쓴다 — 페이지가 제 창의 것을 갈아도 닿지 않는다. 창을 건너는 것은
**속성**이라(DomGlobal.window는 호스트 창) 목을 그 자리에 걸어 둔다.

밟은 함정 셋(전부 실측): elemental2의 `EventListener`는 @JsFunction이 아니라 native
@JsType이라 자바 람다가 **객체로** 건너온다 — `handleEvent`를 떼어내 부르면 this가 사라진다.
흉내 낸 평범한 객체 대신 **진짜 MessageEvent**를 건네야 한다. 그리고 목은 회선보다 빨라선
안 된다 — 같은 틱에 알리면 아직 마운트되지 않은 화면에 말을 건다(50ms 뒤에 연다).

빌드가 갈라 놓는다: `assembleConsole`은 `demo/**`를 빼고(운영 자산에 데모는 없다),
`assembleDemoMock`이 목만 따로 싣고, `-emit-demo`가 그것을 페이지 옆에 놓고 **먼저**
로드한다(페이지가 `window.__magi_demo_mock`을 기다린다). 회귀 가드는
`TestTheMockAnswersEveryPathTheScreensAsk` — 화면이 부르는 모든 경로를 목이 답하는지 본다.

루트절대 자산 경로(/ui/·/vendor/)는 상대로 고쳐 하위 경로(Pages의 `next/`)에서도
산다 — 구콘솔 demo.go의 그 수법. CI에선 `.github/actions/pages-site`(복합 액션)가 구
데모(루트)+벤치 보고서+새 데모(next/)를 한 사이트로 짓고, pages.yml(코어 변경)과
test-web.yml(웹 변경)이 같은 조리법을 쓴다 — deploy-pages는 사이트를 통째로 갈아끼우므로
누가 내보내든 전부를 내보낸다.

## 부모가 진다 — 자식이 지켜야 할 계약을 줄이는 것이 이 층의 일

층이 셋이다(셸 → 컴패니언 패널 → 타입 UI). 아래층이 화면을 그리려고 **알아야 하는 것**은
위층이 하나씩 걷어 간다. 잊으면 조용히 어긋나는 것들이라서다:

| 무엇 | 누가 지는가 | 자식에게 시켰다면 |
|---|---|---|
| 언어 팩이 도착한 뒤에 그리기 | 부르는 쪽(`FrameElement.mount`) | 잊은 화면이 `field.facts`를 그대로 그린다(실측) |
| 그 팩을 제 모듈에 들이기 | `Labels.tr()`이 스스로 창에서 든다 | 모듈마다 static이 따로라 부모가 들여도 자식은 빈손 |
| 스타일시트 걸기 | 스크립트를 들이는 쪽(`ModuleLoader.ensure(module, styles)`) | 잊은 화면이 민얼굴로 뜬다 |
| 기둥 여닫이·도크·`--dock` 실측 | 컴패니언 패널(`Arrangement`) | 자식이 `body[files]`와 창 바닥 상자를 알아야 한다 |
| 자리의 옷(id·격자·높이) | 부모가 입혀서 건넨다 | 자식이 운영 CSS의 이름 계약을 외워야 한다 |
| 그 컴패니언의 행이 바뀌었나 | 스토어가 조각을 잘라 준다(`aimed()`·`drawn()`) | 판마다 제 서명을 손으로 쓰고, 하나 빠뜨리면 초당 한 번 다시 선다(실측 1402회) |
| 나이가 흐르나 | 창에 하나뿐인 시계(`component.Ages`) | 나이 칸이 프레임에 매달려, 아무 일도 없는 동안 — 볼 이유가 있는 바로 그동안 — 얼어붙는다(실측) |
| 낱말이 갈렸나 | 팩이 흐름이다(`Labels.onPack`) | 마운트 때 한 번 읽고 언어를 갈아도 옛말을 든다(실측: 설정만 바뀌었다) |
| 뒤에 데몬이 없을 때 답하기 | 회선의 이음매(`demo-ui`) | 화면마다 목을 싣고, 운영 번들이 데모를 함께 나른다(1193줄) |

선언은 **카탈로그**에 있다: `Destination.styles`(화면), `CompanionType.styles`(타입 UI).
자식 코드에는 그 이름이 하나도 없다.

## 나이는 창의 시계로 센다 (운영과 어긋나게 고친 첫 자리)

명단 프레임은 쉰 시간을 **초**로 싣는다. 그래서 그 바이트는 아무 일이 없어도 매초 달라지고,
서버는 그 프레임을 일부러 보내지 않는다 — 프레임을 가르는 열쇠에서 `Idle`만 빠져 있고
(`cmd/magi-web/main.go`의 `fleetKey`), 거기 적힌 계약이 그 절반을 화면에 맡긴다:

> The counter is drawn from the row when it lands and ticks on the page's own clock.

**그 시계가 어느 콘솔에도 없었다.** 이 콘솔에는 `setInterval`이 셋뿐이었고(데모 티커, 회의
스토어 폴, 턴 바) 그중 무엇도 나이 칸에 닿지 않았다. 운영도 마찬가지다 — `page.js`의 유일한
`setInterval`은 턴 바이고, `cardSig`는 `a.idle`을 넣어 두었지만 그 프레임이 오지 않는다.

값이 가장 필요한 자리에서 값이 얼었다는 것이 이 결함의 모양이다. 일을 마치고 쉬러 들어간 행은
그 순간 **상태가 바뀌어** 프레임을 한 번 받고, 그 프레임의 쉰 시간은 0에 가깝다. 세 시간을
놀아도 화면은 "방금"이라고 적는다. 나이를 보는 이유가 곧 아무 일도 안 일어났다는 것인데,
"다음 진짜 변화 때 다시 그린다"는 그 자리에서만 오지 않는다.

얼어 있던 자리 넷: 카드의 나이 칸, 상세판의 `field.last_activity`, 맵의 노드 나이, 그리고 맵의
"소식 없음" 문장. 문은 **영**이었다.

고친 자리는 프레임이 아니라 슬롯이다(`ui-components`의 `component.Ages`, `bridge.Tips`와 같은
모양 — 화면은 속성만 적고 그리는 것은 창이 한다):

- 낱말을 이고 있는 요소가 `data-since`에 **마지막 소식의 순간**을 진다. 프레임이 싣는 것이
  타임스탬프가 아니라 초라서 이 환산이 가능하다 — 데몬 시계와 브라우저 시계가 합의할 필요가
  없다(턴 바가 이미 같은 이유로 같은 일을 한다).
- 창에 하나뿐인 1초 시계가 `[data-since]`를 걸어 다니며 글자만 고쳐 쓴다. **낱말이 달라질
  때만** 쓴다(`textContent`는 글자 노드를 갈아치우므로, 같은 말을 다시 쓰는 것은 무동작이
  아니다 — 관찰자와 스크린리더에는 매번 새 일이다).
- 나이를 인 자리가 하나도 없으면 시계는 스스로 선다. 탭 수명만큼 도는 타이머는 아무도 보지
  않는 일을 위한 웨이크업이다.
- 문장 **안에** 든 나이는 감싸는 키까지 걸어 둔다(`Ages.in(el, sec, "map.unseen", "ago")`).

이래야 **서명에서 쉰 시간을 빼는 것이 옳아진다.** 카드 서명과 스토어의 `same()`은 매초 달라지는
값을 넣으면 거르는 뜻이 없어져서 뺐는데, 시계가 없는 동안 그 뺌은 거르기가 아니라 멈춤이었다.
지금은 서 있던 노드를 그대로 두는 것과 그 노드의 나이가 흐르는 것이 함께 참이다 — 스펙이 그
둘을 한 자리에서 잰다(행에 표를 꽂아 두고, 나이가 자란 뒤에도 그 표가 붙어 있는지 본다).

## 컴패니언 화면의 뼈대는 운영 콘솔의 이름이다

`#agentview` · `#filecol` · `#stream` · `#sidecol` · `#dock .bay` — 새 이름을 지었더니
console.css의 배치 기계가 통째로 비켜갔다(실측: 1024px 창에서 대화 224px, 전사는 4천 픽셀로
자라 잘림, 컴포저는 페이지와 함께 흘러감). 규칙 셋이 그 이름에 걸려 있다:

- `body[at=agent] main { height:calc(100dvh - shelltop) }` → 기둥이 창 높이에 물리고 전사만
  스크롤한다. 그래서 셸은 마스트헤드 높이를 실측해 `--magi-comp-shelltop`에 넣는다.
- `body[files|side]=shut` → 그 기둥의 폭이 **0**이다. 기본이 닫힘이라 처음 온 사람에게 이
  화면은 대화다. 손잡이는 마스트헤드에 선다(`ChromeSharing` — 셸이 자리를 내주고 화면이 민다).
- `#dock .bay` → 컴포저는 창 바닥의 고정 상자 안이고, 그 높이가 `--dock`으로 본문 바닥이 된다.

자식은 이 중 무엇도 모른다. 부모가 `display:contents` 껍데기(`.cfill`)로 자리를 건네므로,
자식이 넣은 것이 곧 기둥의 직계가 된다 — 사이에 상자가 하나라도 끼면 높이 사슬이 거기서 끊긴다.

## 브라우저로 재는 것 (scratchpad/uitest)

단위·모듈 테스트가 초록인데도 브라우저에서만 드러나는 것들이 있다 — 실제로 이렇게 잡았다:
폰에서 컴패니언 화면의 **유일한 출구가 사라진 것**(크럼의 .up/.leaf 미표시), 폰에서
**사실판·워크스페이스에 닿을 길이 없던 것**(판 탭 부재), 데모의 404 셋(글꼴·/console·
/context), 좁은 열에서 뭉개진 컨트롤.

다섯 라운드를 돌린다(`node scratchpad/uitest/<이름>.mjs`, 데스크톱 1500×950과 폰 390×844):

| 라운드 | 무엇을 재나 |
|---|---|
| `round` | 화면마다 한 바퀴 — 목록·지식·보드·맵·접근·상세·이력, 가로 오버플로 0, 폰 탭 |
| `deep` | 상호작용 — 필터·행→상세·크럼·뒤로가기·사실판 접기·워크스페이스 쓰기(실데몬) |
| `soak` | 오래 켜 둔 화면 — 화면을 돌고 와도 한 벌씩만 그려지는가, 스트림이 화면을 갉지 않는가 |
| `a11y` | 이름 없는 아이콘 컨트롤 0, 제목 존재, 흐린 글씨 3:1, 라이트·다크 |
| `demo` | 정적 데모에서 쓰기까지 — 다이얼로그·라벨 좁히기·능력 필터·스테이지 |

그리고 **두 콘솔을 나란히 놓고 수치로 견주는** 라운드가 하나 더 있다. 눈으로 "비슷"한
것들이 여기서 갈렸다: `widths`(7화면 × 8폭의 자리), `controls`(보이는 컨트롤의 이름),
`iconsweep`(버튼마다 어느 그림), `demodiff2`(데모 두 벌의 rect), `churnsweep`(초당 몇 번
다시 그리나 — 이 콘솔에서 가장 많이 잡아낸 검사다). 데모 비교는 `-emit-demo`로 낸 두
디렉토리를 각각 정적 서버로 띄워 쓴다.

⚠ 데모 두 벌은 **각자의 시계**로 돈다(픽스처가 같아도 상태가 같은 순간이 아니다) —
`screensweep`처럼 순간의 컨트롤 목록을 견주는 검사는 그래서 구조 차이와 시각 차이를
가리지 못한다. 자리(rect)를 재는 `demodiff2`가 그 자리를 대신한다.

⚠ 브라우저 스펙의 함정은 스킬(`web_ui_module_dev`)에 모아 두었다 — hover 노출 컨트롤,
폭 트랜지션, `matchMedia` change가 한 방향만 오는 것.

## 빌드 스크립트는 모듈마다 있지 않다

안쪽도 모듈로 나뉘므로 같은 스크립트를 아홉 번 베끼는 대신, 루트 `build.gradle.kts`가
표 하나로 전부 구성한다 — **모듈 디렉토리에 build.gradle.kts는 없다**. 모듈이 대는 것은
제 이름 두 개(GWT 모듈, 테스트 모듈)뿐이고 의존성·GWT 설정·테스트 포트(표의 순서로
18090부터)·테스트 자산 복사는 규약이다. 새 화면을 더할 때 고칠 곳은 그 표 한 줄과
`settings.gradle.kts`의 include 한 곳이다.

⚠ 루트는 GWT 플러그인을 적용하지 않으므로 확장의 타입이 없다 — `withGroovyBuilder`로
이름으로 설정한다. 오타를 컴파일이 잡아 주지 않으니, 검증은 `./gradlew build`가 전 모듈을
실제로 컴파일·테스트하는 것으로 한다.

## 빌드·실행

```sh
cd web/ui && ./gradlew build      # 컴파일 + 전체 테스트 (Gradle 9.3 / Java 25)
./gradlew assembleConsole         # build/console/ 에 서빙 루트 집결
cd ../.. && go run ./web/server   # 7778: /ui/* 정적 + 나머지는 7777(기존 magi-web)로 프록시
# → http://127.0.0.1:7778/next
```

- 의존성은 `gradle/libs.versions.toml` 한 곳 — handbook의 sayaya-web 번들 미러.
  dagger가 아니라 **dagger-gwt**다: GWT 모듈 xml이 그쪽 아티팩트에 실려 있고, 일반
  dagger를 넣으면 gwtCompile이 "Unable to find dagger/Dagger.gwt.xml"로 죽는다(실측).
- sayaya-ui 등은 GitHub Packages에서 온다. 자격증명은 `~/.gradle/gradle.properties`의
  `github_username`/`github_password` 또는 `GITHUB_USERNAME`/`GITHUB_TOKEN` 환경변수 —
  이 디렉토리의 `gradle.properties`는 gitignore라 커밋에 실리지 않는다.
- web/server는 요청의 Origin을 BFF 오리진으로 고쳐 보낸다 — 기존 콘솔의 same-site
  가드가 프록시 오리진(7778)을 교차 출처 POST로 읽고 거절하기 때문이다.

## 컴패니언 화면은 두 겹이다 (셸의 그 관계를 한 번 더)

셸이 화면에게 프레임을 내주듯, **컴패니언 패널은 자식에게 자리를 내준다**:

```
companion (범용)                     coding (타입 1의 자식)
├── #detail   위: 사실판             ├── centre 슬롯 → 대화(전사·컴포저)
├── #cstage
│   ├── #cleft   왼쪽 슬롯(들) ◀────┤ left 슬롯 → 워크스페이스(트리·git)
│   ├── #cframe  가운데       ◀─────┘
│   └── #side    오른쪽: 계획·건넨 일(잔여)·예약(잔여)
└── 목록(?d= 없을 때)  ← 같은 모듈의 다른 얼굴
```

- **왜 이렇게**: 위와 오른쪽은 타입이 무엇이든 같은 것을 답한다(무엇이고, 무엇을 하는
  중이고, 무엇을 하기로 했나). 가운데와 왼쪽은 타입의 것이다 — 코딩 에이전트에게 가운데는
  대화이고 왼쪽은 워크스페이스지만, 다른 타입에겐 다른 것이다.
- **문**: `PaneSharing`(`__magi_pane`) — 자식이 `next("centre"|"left", render)`로 민다.
  왼쪽은 여럿 밀면 순서대로 쌓인다. 부모는 무엇이 오는지 모른다.
- **자식을 들이는 것도 부모**: 셸이 컨텍스트에 실어 보낸 이름(`ctx.ui`)을 `ModuleInject`가
  한 창에 한 번 넣는다. 이름은 **카탈로그가 푼 것**이지 컴패니언이 댄 경로가 아니다.
- 목록도 이 모듈의 것이다(주소에 `?d=`가 없을 때) — 표에서는 어떤 타입이든 같은 것을
  답하기 때문이다.

## 컴패니언 타입별 UI (기전 가동 중)

컴패니언 화면은 고정 모듈이 아니라 타입으로 해석된다: 명단 행의 type 선언 →
CompanionType 카탈로그 → 모듈. 지금 카탈로그는 타입 1(코딩 에이전트) = companion-ui
하나이고, 무선언·미지 타입도 1로 푼다 — 오늘의 magi 컴패니언은 전부 코딩 에이전트다.
타입 전용 모듈도 계약은 같다 — 렌더 등록 + CompanionContext(socket·peer·type)·전사·턴
구독. companion-ui가 그 계약의 레퍼런스 구현이다.
불변 규칙 하나: 오퍼레이터가 설치한 모듈만 로드한다. 컴패니언이나 워크스페이스가 실어
보낸 스크립트를 감독자 콘솔에 로드하는 일은 없다 — `.magi/plugins`(SECURITY)와 같은
신뢰 경계다.
