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

모듈 다섯. 의존은 `화면 모듈 → console-bridge ← shell-ui` 한 방향이고, 화면끼리는 서로를
모른다.

| 모듈 | 무엇 | 산출물 |
|---|---|---|
| `console-bridge` | 셸↔화면 계약: 창 브리지(Render·Roster)·와이어 DTO(FleetAgent)·언어 팩(Labels)·HTTP 관용구(Console) | 소스 실린 jar (GWT 라이브러리 규약) |
| `ui-components` | 공용 위젯 자리 — 지금은 모듈 선언(`UiComponents.gwt.xml`)만 | 소스 실린 jar |
| `shell-ui` | 셸: 드로어(1단+2단)·마스트헤드·Navigation(Place)·RenderStore·RosterStore·타입 카탈로그(CompanionType)·ScriptModuleLoader | `shell/shell.nocache.js` + console.html + shell.css |
| `fleet-ui` | 플릿 화면 — 이식 완료, 카탈로그 화면의 레퍼런스 | `fleet/fleet.nocache.js` |
| `companion-ui` | 컴패니언 화면, 타입 1 = 코딩 에이전트 — 타입 전용 UI 계약의 레퍼런스 | `companion/companion.nocache.js` + companion.css |
| `knowledge-ui` | 지식 화면(경험·위키·서버) — 주소 v=skills, 모듈 이름도 skills | `skills/skills.nocache.js` |
| `board-ui` | 보드 — 문 없는 주소(v=board): 레일은 컴패니언 문, 진입은 플릿의 .toview | `board/board.nocache.js` |
| `map-ui` | 맵 — 문 없는 주소(v=map): 머신·계정 상자와 오간 것의 와이어 | `map/map.nocache.js` |
| `access-ui` | 접근 제어(v=access) — 레일의 셋째 문, admin 게이트 | `access/access.nocache.js` |

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
- **이동** (`__magi_go` / `__magi_go_view` / `__magi_go_past`): 화면이 셸에 이동을 청하는
  문들 — 컴패니언으로(플릿의 행), 카탈로그 화면으로(플릿의 .toview), 그리고 지난 일
  층위로(?past= — null=지금 대화, ""=목록, 값=그 세션; 보드 카드·이력 행이 쓴다).
  주소(pushState)는 셸의 것이라 화면이 만지지 않는다.
- **HTTP**: Console.fetchList는 거부(HTTP 에러)·불통·깨진 본문을 전부 null로 접되
  console.warn에 원문을 남기고, Console.post는 대상을 `?d=<socket>&p=<peer>`로 지목한다
  (성공=빈 문자열, 거부=사유). 기존 page.js의 fetchList/post 이식이다.
- **말**: Labels — 기존 콘솔과 같은 `/i18n/language.{en,ko}.json`. tr()은 키 폴백("번역
  빠짐"이 보이게), stateWord()는 원어 상태어 폴백(행에 "state.gone"을 안 적으려고).
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

`interfaces → usecase → domain` 한 방향. fleet-ui가 레퍼런스 구현이다.

- `client/domain` — 순수 규칙, DOM 무지. JVM 단위 테스트가 여기 붙는다. 단, 테스트
  파일은 `client/` 밖(`dev/sayaya/magi/domain/`)에 둔다: client/ 안은 GWT source path라
  gwtCompile이 JUnit까지 컴파일하려 든다.
- `client/usecase` — 포트와 스토어. rx 미배포라 순수 자바 옵저버로 쓴다.
- `client/interfaces` — DOM과 HTTP 어댑터. 마크업의 id·클래스는 기존 page.js와 동일하게
  간다 — console.css가 읽는 계약이다.
- Dagger가 포트에 구현을 묶고(FleetModule), 테스트는 같은 자리에 페이크를 묶는다
  (FleetTestModule). HTTP 목 없이 화면을 검증할 수 있는 이유다.

## 화면 하나 이식하기

1. 모듈 디렉토리 + `build.gradle.kts`(fleet-ui 것 복사) + `<Name>.gwt.xml`을 만들고
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
| shell.css | `shell-ui/src/main/webapp/shell.css` (원천이 여기) | assembleConsole · 테스트 webapp `css/` |
| companion.css | `companion-ui/src/main/webapp/companion.css` (원천이 여기 — 모듈이 스스로 <link>를 단다) | assembleConsole · 테스트 webapp `css/` |

assembleConsole은 모든 모듈의 `src/main/webapp`을 함께 나른다 — 화면 모듈 자신의
자산(css)은 제 모듈에 둔다. 그래서 `src/test/webapp/` 아래 `js/`·`css/`와 GWT 컴파일
산출 디렉토리(`fleet/`·`fleettest/`·`shell/`·`shelltest/`·`companion/`…)는 전부
생성물이고 gitignore다. 거기서 소스는
테스트 페이지 html뿐이다.

## 정적 데모 (Pages의 next/)

`go run ./web/server -emit-demo <dir>` — 조립된 콘솔을 자답(自答) 정적 사이트로 쓴다:
fetch(/fleet·/i18n)와 EventSource를 픽스처 목으로 갈아끼우는 심 한 조각을 페이지 앞에
붙이고, 루트절대 자산 경로(/ui/·/vendor/)를 상대로 고쳐 하위 경로(Pages의 `next/`)에서도
산다 — 구콘솔 demo.go의 그 수법. CI에선 `.github/actions/pages-site`(복합 액션)가 구
데모(루트)+벤치 보고서+새 데모(next/)를 한 사이트로 짓고, pages.yml(코어 변경)과
test-web.yml(웹 변경)이 같은 조리법을 쓴다 — deploy-pages는 사이트를 통째로 갈아끼우므로
누가 내보내든 전부를 내보낸다.

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

## 컴패니언 타입별 UI (기전 가동 중)

컴패니언 화면은 고정 모듈이 아니라 타입으로 해석된다: 명단 행의 type 선언 →
CompanionType 카탈로그 → 모듈. 지금 카탈로그는 타입 1(코딩 에이전트) = companion-ui
하나이고, 무선언·미지 타입도 1로 푼다 — 오늘의 magi 컴패니언은 전부 코딩 에이전트다.
타입 전용 모듈도 계약은 같다 — 렌더 등록 + CompanionContext(socket·peer·type)·전사·턴
구독. companion-ui가 그 계약의 레퍼런스 구현이다.
불변 규칙 하나: 오퍼레이터가 설치한 모듈만 로드한다. 컴패니언이나 워크스페이스가 실어
보낸 스크립트를 감독자 콘솔에 로드하는 일은 없다 — `.magi/plugins`(SECURITY)와 같은
신뢰 경계다.
