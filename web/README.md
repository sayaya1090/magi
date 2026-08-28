# web/ — 콘솔 신규 구현 (스트랭글러)

기존 콘솔(`cmd/magi-web`)은 **무변경으로 유지**하고, 여기서 동일 기능을 새로 구현한다.
대조표가 전부 ✓가 되고 프로브 게이트를 통과하면 기존을 삭제한다. 그 전까지 정본은 기존 콘솔이다.

UI 쪽 상세 — 모듈 계약·셸 흐름·화면 이식 절차·빌드 — 는 [`ui/README.md`](ui/README.md)에 있다.

```
web/
├── server/   # 씬 개발 서버(Go): 새 UI 정적 서빙 + 나머지 전부를 기존 magi-web으로 리버스 프록시
│             #   → 새 프론트가 기존 BFF의 실데이터로 렌더 = 대조가 곧 개발 루프
└── ui/       # handbook식 GWT 멀티모듈 (Gradle) — 화면 하나 = 모듈 하나
    ├── console-bridge/  # 셸↔화면 계약: Render/Uri/Label 공유 + SseSharing(스트림 단독 소유) + FetchApi(목킹 지점)
    ├── ui-components/   # 공용 위젯(트랜스크립트 렌더러·게이지·컴포저)
    ├── shell-ui/        # 레일·마스트헤드·테마·i18n·라우팅·모듈 주입(ModuleResolver)
    ├── fleet-ui/        # 컴패니언 목록
    └── companion-ui/    # 컴패니언 상세 — "기본" 타입 UI (아래 참조)
```

## 컴패니언 타입별 고유 UI (설계 반영, 구현은 나중)

컴패니언 상세 화면은 고정 모듈이 아니라 **타입으로 해석(resolve)** 된다:

```
셸 ModuleResolver:  (destination | companion.type) → 모듈 스크립트 경로
  companion 상세 진입 → 그 컴패니언의 type으로 조회 → 있으면 타입 전용 모듈 로드
                                                    → 없으면 companion-ui(기본) 로드
```

- **계약**: 타입 전용 UI 모듈도 console-bridge 계약(Render 등록 + CompanionContext 수신:
  socket·type·peer)만 지키면 된다 — companion-ui 자체가 이 계약의 레퍼런스 구현.
- **매니페스트**: 셸 상수 맵으로 시작(모듈이 적다). 타입 UI가 재컴파일 없이 등록되어야
  할 때 BFF 라우트(`/ui-manifest`)로 승격.
- **보안 규칙(불변)**: 타입 UI 모듈은 **오퍼레이터가 설치한 것만** 로드한다 — 바이너리에
  임베드됐거나 오퍼레이터 설정이 지목한 경로. 컴패니언(워크스페이스)이 실어 보낸 스크립트를
  감독자 콘솔에 로드하는 일은 없다. 클론과 함께 온 것은 신뢰 게이트 앞에서 멈춘다는
  `.magi/plugins`(SECURITY)와 같은 규칙이다.
- **type의 출처**: 컴패니언 레코드의 선언(나중에 `[companion] type=` 추가) — 셸은 /fleet
  행에 실려 온 값을 읽기만 한다.

## 새 프론트의 구조 (handbook 클린 아키텍처)

의존 방향은 `interfaces → usecase → domain` 한쪽이고, console-bridge는 화면 모듈들이
공유하는 커널(창 브리지·와이어 DTO·언어 팩·HTTP 관용구)이다.

- `client/domain` — 순수 규칙: 상태 순서/그룹(AgentStates), 버전 비교(Versions),
  시간 축약(Spans), 명단 읽기(Roster: 세기·거르기·정렬·팀묶기). DOM을 모른다.
- `client/usecase` — 포트와 스토어: FleetRepository(읽기), FleetCommander(인터럽트/답),
  FleetStore(첫 fetch + 스트림 구독 소유; rxjs 미배포라 순수 자바 옵저버 — 셸이 rx를
  들이면 이 클래스만 교체).
- `client/interfaces` — 어댑터: api/FetchFleetRepository(/fleet + /events SSE),
  api/FetchFleetCommander(/interrupt·/answer), FleetElement(루트)·CardListElement(행,
  cardSig 재사용 캐시)·StopDialogElement. 마크업 클래스는 기존 page.js와 동일 — CSS 계약.
- Dagger가 포트에 구현을 묶는다(FleetModule). 테스트는 같은 자리에 가짜를 묶는다
  (FleetTestModule) — HTTP 목 없이 화면을 검증하는 이유.

테스트: `web/ui/gradlew :fleet-ui:test` — 도메인 JVM 단위(JUnit5; GWT source path 밖
`dev/sayaya/magi/domain/`에 둘 것 — client/ 안에 두면 GWT가 컴파일하려 든다) +
Playwright 브라우저 스펙(kotest GwtTestSpec, fleettest.html, webPort 18090).

### 셸 드로어 (2026-08-27)

기존 콘솔 #rail의 이식: 버거 5획 애니메이션, nav(폭)/nav-wide(모양) 2속성 상태 기계와
250ms 타이밍, 스크림 닫기, [selected]+aria-current, 화살표키 이동, 문별 진짜 앵커(?v=
주소·pushState/popstate). 문은 이식된 화면(플릿)에만 단다 — 빈 화면으로 가는 문 금지.
RenderStore가 목적지별 렌더를 캐시해 재방문은 스크립트 재주입 없이 다시 그린다.
테스트 8(Playwright): 마크업·선택·단일 로드·열기(2단)·닫기·마스트헤드·배지.

메뉴 레일+툴 레일(2026-08-27, handbook 번역 — 정정 포함): 메뉴 레일이 목적지 전부의
집이고 열리면 라벨도 메뉴 레일의 것(2단이 명단을 들고 있던 첫 판은 정정 — 툴 레일은
메뉴가 아니라 도구의 것이다). 툴 레일은 도구 ≥2인 문에서만: 접히면 기둥을 대신(아이콘,
피크 라벨, ← 복귀 — 선택 유지), 열리면 둘째 기둥. 규칙=RailModes(순수)+RailMode →
#rail[menu/tool] 속성 → shell.css. ToolList.provide 용례는 아직 없다 — 속이 비면
펼쳐지지 않는다. 드로어는 운영과 같은 2속성 기계다: nav=open(폭)+nav-wide(모양,
닫힘은 250ms 늦게) — console.css의 펼친 배치가 nav-wide에 키가 걸려 있어, 하나만
세팅하면 폭만 열리고 모양이 안 따라온다(실측으로 되밟고 복원).

### 마스트헤드 + 스트림 소유 이관 (2026-08-27)

창당 1스트림 규칙의 기전이 들어왔다: 셸의 RosterStore가 /events의 유일한 소유자가 되어
RosterSharing(window `__magi_roster_subscribe`/`__magi_roster_refresh`)으로 호스팅하고,
플릿 화면의 FetchFleetRepository는 브리지가 있으면 구독·없으면(단독/테스트) 제 회선 폴백.
마스트헤드(#masthead)는 기존 콘솔 이식: 브랜드·whereami(/console의 user@host)·크럼·
상태줄(점 3형태 live/asking/lost + 컴패니언 수 + "N명이 기다림" 점프→플릿 첫 대기 행)·
body[scrolled]. 레일 배지: 접히면 아이콘 위, 열리면 라벨 뒤(부모 재배치), 수는 문의
aria-label에 실린다.
잔여: 폰 탭바, #note/#say/#tip(쓸 사람이 생길 때), 팔레트·설정 문(그 화면들과 함께).

### 컴패니언 화면 + 타입별 UI 기전 (2026-08-27)

컴패니언별로 다른 화면이 나온다: 명단 행의 type 선언 → 셸의 CompanionType 카탈로그 →
모듈. 타입 1 = 코딩 에이전트 = companion-ui(오늘의 magi 컴패니언 전부; 무선언·미지도
1로 — 빈 화면 금지). 디자인·인프라 관리·리서처 등은 카탈로그 한 줄+설치된 모듈로
들어온다. 주소는 기존 콘솔의 `?d=<socket>`(&p=) 그대로. 스트림은 여전히 셸 하나 —
조준(aim)되면 같은 회선의 기본 프레임이 전사가 되어 TranscriptSharing으로 흐르고,
CompanionSharing이 해석된 컨텍스트를 나른다. 화면이 이동을 청하는 문은 GoSharing
(플릿 행·레일 2단 항목). 상세는 `ui/README.md`.

## 대조표 (100% = 전부 ✓ + 프로브 통과 → cmd/magi-web 삭제)

| 화면/기능 | 기존 | 신규 | 상태 |
|---|---|---|---|
| 플릿 목록 (타일·팀 그룹·행·인터럽트) | ✓ | 동등 이식 — 타일 필터(soft-disabled 규칙)·팀 그룹(시끄러운 순)·행 전 필드(플랜·짐·근거·behind)·인터럽트 confirm·질문/퍼미션 답(큐 점프)·SSE 스트림+재접속·빈/실패 상태; 테스트 18(도메인 7 + Playwright 11); 2026-08-27 실검증; 행→컴패니언 링크 가동(GoSharing) | ☐ |
| 컴패니언: 대화(SSE)+컴포저(스티어/답변 겸용) | ✓ | 첫 조각 이식(companion-ui = 타입 1 코딩 에이전트): 전사 스트림(행 클래스 계약 .row/.who/.txt)+턴 바+컴포저(/submit, 거부 시 복원)+사실 줄(이름·타입·상태·모델·세션). **타입별 화면 기전 가동**: 명단 type→CompanionType 카탈로그→모듈, 무선언=1; 들어오는 문 셋(플릿 행·레일 2단 GoSharing·?d= 주소). 테스트 10(도메인 3+Playwright 7); 2026-08-27 실검증(실데몬 턴 왕복). 툴 행 펼침 이식(2026-08-27): details.txt.fold 계약 — 요약=마크(✓/✗/⚠/⚙)+이름+인자+⟶답 첫 줄, 속=fold.asked/answered(디프면 경로+fold.changed, 줄마다 dadd/ddel/dhunk), 실패는 열려 도착, kind별 localStorage 선호(메아리 가드 포함), pending=runbar·pendtag. 셋 다 착지(2026-08-28): **마크다운 렌더**(운영과 같은 렉서 번들을 물고 노드로 조립 — href 검사·raw HTML은 글자), **jsonPairs 인자표**(.args/.argk/.argv 계약), **투표 화면**(운영 `drawVerdict`의 항목 그대로 — 결정·렌즈·확신 칩과 이유·다음·유지·근거를 카드로). 투표 화면은 전선을 고쳐야 서는 것이었다: 카운슬 행이 실어 온 것은 decision·member·round·text 넷뿐이라 **운영 화면도** 렌즈·확신·이유·다음·유지 없이, 모든 표 아래 "근거 없음"을 적고 있었다(실측). 여섯 필드를 `line`에 실어 둘이 함께 고쳐졌다. 착지 확인돼 뺀 것 여섯: 카운슬 **행**(자리 이름·라운드·증거 — ConversationElement), todo 플랜 판(SideElement), 행 재사용 윈도우잉(위치별 재사용 drawn/sigs), 접는 사실판, 컨텍스트 줄, 워크스페이스 판, 과거 세션 | ☐ |
| 컴패니언: 사실판(상태·모델·플랜·컨텍스트·핸드오프·이력·크론) | ✓ | 읽기 반쪽 이식(#detail 접는 카드 — 운영 마크업·접힘 기억 localStorage facts·기본은 창 폭): 상태·짐, 스텝, 마지막 활동, 역할, 팀(+대변), 호스트(주소·pid), 빌드, 워크스페이스, 세션, 결재(읽기), 모델, 캐시 줄, 컨텍스트 창(잰/셈 구분·바·지금 접기 /compact)·접혀 나간 것(횟수·양·마지막·시각). 이력 층위(?past= — 운영 주소 그대로: 빈 값=목록 /history, 값=그 세션 전사 /transcript 한 번 읽기; 층위에선 지금-대화 판·컴포저가 물러남, 이동은 GoSharing.past 문). 폰에서는 부모가 탭으로 한 번에 하나를 보인다(대화·정보·파일 — body[panel=…], 운영과 같은 계약). 테스트 30. **자식 층위 이식(2026-08-28)**: 우측 판의 자식 칩이 문이 된다(?sub=<id> — 그 아이가 무엇이었나·무엇을 하라고 보내졌나·제 전사, 지난 일 층위와 같은 자리·같은 행 렌더). 판이 다시 서는 규칙도 바뀌었다: 스토어가 **조각**을 내려보내고(`aimed()`·`alive()`) 판은 제 조각만 듣는다 — 사실판의 DOM 교체가 12초에 1402회에서 0회로(운영과 동수). **업데이트 컨트롤 이식(2026-08-28)**: 빌드 칸이 글자에서 컨트롤로 바뀐다 — 제 기계의 own 데몬이고, 명단이 본 최신보다 뒤처졌고, configure 능력이 있을 때만 버튼이 선다(뒤처졌다는 사실은 명단 전체가 아는 것이라 사실판이 명단을 통으로 읽는 유일한 자리이고, 그 답이 달라졌을 때만 다시 그린다). 누르면 `/update`로 몸 없는 POST를 보내고 **데몬이 답한 말을 그대로** 버튼 자리에 세운다 — 거부도 답이라 그 사유가 그대로 선다("그 기계에서 하라"). 아무 말도 오지 않았을 때만(회선 끊김) 대신할 말을 세우고 버튼을 남긴다: 다시 눌러 보라는 문장이 가리킬 컨트롤이 없으면 그 문장에 뜻이 없다. 상태가 버튼 곁이 아니라 순수 `domain/Updates`에 사는 이유는 운영이 모듈 스코프에 둔 그 이유다 — 명단이 몇 초마다 흘러 사실판이 다시 서고, 받는 중에 다시 서면 끝난 fetch가 사라진 버튼을 켜려다 잠긴 채로 굳는다. 실측 대조(두 정적 데모, 1512×950, `?d=/demo/ops.sock` v0.22.0·own): 누르기 전후 모두 `.v` 안쪽이 바이트 동일 — `<div class="vnum">v0.22.0</div><md-text-button>Update to latest</md-text-button><div class="updsay" hidden>`, 버튼 `ml -8px / mt 4px`, `.updsay mt 8px / fs 13.12px`, 누른 뒤 `updated v0.22.0 → v0.23.0, restarting` 한 줄에 버튼은 걷힘. 표는 운영이 청하는 것을 청하되(`#i-sl-cloud-arrow-down`) 스프라이트 없는 이 빌드에선 두 콘솔 다 말뿐이다. 테스트 10(도메인 7+Playwright 3). 신규 i18n 0. 잔여(실측): **past에서 move-and-send**(운영 1곳 / 신규 0), **멈춘 컴패니언의 사실판**(운영은 `live:false`여도 판을 그리고 연결 줄만 사라졌다고 적는다(`companionAlive`) / 신규는 `CompanionStore.rowOf`가 그 행을 null로 답해 `#detail`이 통째로 숨는다 — 데모의 ops(stopped·own·v0.22.0)로 실측. 이 항목을 재려고 그 픽스처를 잠시 살려 두었다가 되돌렸다) | ☐ |
| 컴패니언: 워크스페이스(트리·git·커밋·PR·파일편집·룩오버) | ✓ | 읽기 반쪽 이식(coding-agent-ui의 **왼쪽 슬롯** — 타입별 자식이 정의하는 첫 실사용처): 파일 트리(열린 가지만 걷는다 — 순수 규칙 domain/Tree, 펼침=그 디렉토리 하나만 더 읽기)+두 카드 각자 스크롤+왼쪽에서 연 파일 본문+git(브랜치·앞뒤·변경 두 무리). 쓰기·검색까지(2026-08-27): 찾기(이름/내용 — 찾는 동안 판은 결과를 지킨다)+새 파일/디렉토리+이름 바꾸기+지우기(2단 확인)+git 스테이지/언스테이지/되돌리기(2단)+커밋(실린 것 있을 때만)+브랜치 전환(확인)·새 브랜치·pull/push. 쓰는 컨트롤은 shell 능력이 있을 때만 그린다(bridge.May — 창당 1회, 게이트는 서버가 진다). 회선은 이 모듈이 직접 잡는다(셸 경유는 스트림뿐). 테스트 38(도메인 6+Playwright 32). **2026-08-28**: 이미 열어 둔 파일을 다시 누르면 그 탭이 선다(카드 표시 신호를 여럿이 듣게 고침), 트리·깃 카드는 제 조각이 흘렀을 때만 다시 선다(파일 클릭에 깃 카드가 깜빡이지 않는다), 두 카드의 높이 배분이 운영과 픽셀 일치(우리가 씌운 26rem 뚜껑 제거). 잔여(실측): **diff 보기**(운영에도 전용 뷰는 없다 — 전사 쪽 유니파이드 렌더만 있고 그건 이식됨(Rows.diffLineClass); 워크스페이스 판의 diff는 **양쪽 다 없음**이라 표에서 내린다), **룩오버**(운영 2곳 / 신규 0). 착지 확인돼 뺀 것 둘: PR(WorkspaceStore.pullRequest), 우클릭 메뉴(WorkspaceElement contextmenu) | ☐ |
| 지식(경험·위키·MCP·forget) | ✓ | 이식(knowledge-ui, 주소 v=skills 호환): 경험(규칙/기억 분리·IDF 랭킹 찾기·읽기 접기·잊기(2단 확인)·적어두기)+위키(정본·낡은 묘비 ⚠)+서버(목록·제거). 랭킹은 domain/Rank(운영 rankByIDF와 같은 식, JVM 테스트). 테스트 14(도메인 3+Playwright 11, 폰 뷰포트 포함); 2026-08-27 실검증(실데몬 engram 기억 표시). 추가로 이식: MCP 추가/편집 다이얼로그(추가=편집·kind별 필드·이름 readonly)+서버 머리 액션+빈 상태+임베딩 모델 줄+마스트헤드 크럼(지식/컴패니언 이름). 수용한 미시 편차: 운영 #skills만의 27px 인셋(운영 내부 비일관 — pad/mar 0인데 위키 판과 다름). 잔여(실측): **다이얼로그 닫기 ×**(운영 `.dlgclose`+`#i-sl-xmark` / 신규 0), **필드별 에러 매핑**. **낭독 요약은 내린다** — 운영에 그런 것이 없다(speechSynthesis·aloud 0건). 없는 것을 잔여로 적고 있었다 | ☐ |
| 보드·맵 | ✓ | 이식(board-ui, 주소 v=board 호환 — 문 없는 주소: 레일은 컴패니언 문이 켜진 채, 진입은 플릿 칩 곁 .toview 아이콘): 팀 레인(무팀=제 이름)·골칫거리 순·카드(시각/누가/제목→그 대화/소요/모델/라벨 칩 검색)·날짜 걸음(UTC 통날 산술)+오늘 잠금·걸친 밤일 양쪽 날 소속·세로 휠→가로 스트립. 순수 규칙은 domain/Lanes(JVM 테스트), 랭킹은 공용 component.Rank로 승격. 테스트 10(도메인 3+Playwright 7, 폰 포함). 보드 카드의 &past= 링크가 이제 그 세션에 닿는다. **맵**(map-ui, v=map — 역시 문 없는 주소, 진입은 플릿 .toview 둘째·둘 이상일 때): 두 경계 상자(머신>계정)+신뢰 어휘+팀 머리+노드(딴 머신 것은 링크 아님)+측정 기반 와이어(같은 상자=곡선, 사이=밑 레인, 팀은 선이 아님)+ResizeObserver 재그림+범례. 테스트 9(도메인 3+Playwright 6, 폰 포함) | ☐ |
| 회의실 (자기 스트림) | ✓ | 이식(meeting-ui, v=meet·방은 &m=): 여는 화면(주제·주인별 묶음+팀 색 칩·부를 수 없는 이 제외·못 여는 이유 한 줄)+방(주제/단계 한 줄·명단 칩으로 지명·바퀴별 오간 말·넘긴 차례·그 차례의 도구질 펼치기·바닥 쥐고 말하기·마무리)+결론(누구에게 무엇·건네기+그리로 가는 길)+다시 열기+사라진 방. 순수 규칙은 domain/Rooms(단계·차례 구간·열 수 있나). 테스트 24(도메인 6+Playwright 18) | ☐ |
| 접근 제어 | ✓ | 이식(access-ui, v=access — 레일의 셋째 문, admin 능력 게이트: 셸 MayStore가 /me를 한 번 물어 문을 접는다. 1인 콘솔은 "전부"라 답해 아무것도 안 바뀜): 누구의 콘솔(instance)·그룹 먼저(디렉토리의 것이라 읽기 전용)·사람(역할 메뉴·범위 칩 remove·이름 Enter 추가·삭제 2단 확인)·능력 범례 칩=명부 필터(한 화면 한 선택), 능력 낱말은 번역 안 함(auth.toml의 그 낱말). 테스트 8. 잔여(실측): **삭제 확인 다이얼로그**(신규는 2단 확인 arm() — 의도된 대체이지 미이식이 아니므로, 바꿀지 말지가 결정 사항). 착지 확인돼 뺀 것: 사람 추가 다이얼로그(AccessElement.addPerson — 첫 사람=admin 기본값까지) | ☐ |
| 환경설정 다이얼로그·테마·i18n(en/ko 패리티) | ✓ | 이식(settings-ui, v=settings — 다이얼로그가 아니라 화면: 어느 config를 고치는지가 서 있는 자리로 갈려서 그 사실을 맨 위에 적는다). 테마 3상태(시스템/라이트/다크, 적지 않는 것이 시스템)·언어(팩 재적재)·모델 보조 3스위치(브라우저)·완성 설정(데몬 config: ambient/cross/프로파일 둘, may=configure). 저장 버튼 없음(바뀌는 순간 저장). 테스트 13. 잔여 없음 — 웹푸시 스위치는 이식됐다(Notifications, 아래 웹푸시 줄) | ☐ |
| ⌘K 팔레트 | ✓ | 이식(shell-ui): ⌘K/Ctrl+K와 마스트헤드 버튼, 화면·컴패니언 후보, 화살표·Enter·Esc, role=listbox/option+aria-activedescendant. 점수는 domain/Match(앞>가운데>흩어짐). 화면이 제 것을 더하는 문 PaletteSharing — 셸이 자식의 기능을 알지 않게. 테스트 7. 잔여(실측): **파일 후보** — 문(PaletteSharing)은 서 있고 셸이 읽지만, 등록하는 화면이 아직 없다(용례 0) | ☐ |
| 웹푸시·PWA·sw.js | ✓ | 이식: 설정의 알림 줄(권한→워커 등록→구독→/push, 끌 때 구독 해지+서버에서 삭제)과 매니페스트·아이콘·sw.js. 켤 수 없으면 왜인지 적는다(https 아님/미지원/거부/키 없음/데모). 파일 셋은 internal/webassets 단일 원천 — 개발 서버·데모·옛 콘솔이 같은 바이트. 테스트 10 | ☐ |
| -emit-demo 정적 데모(목은 모듈 하나) | ✓ | 이식: 목은 화면이 아니라 **회선의 이음매**에 걸린다 — `demo-ui` 하나가 경로로 답하고(`Console.raw`/`Console.stream`가 그것을 건넨다), 화면 모듈은 살아 있는 소스 하나만 갖는다. `window.fetch`를 갈지 않는 이유는 그대로다(모듈은 GWT iframe에서 돌아 그 프레임의 fetch를 쓴다) — 창의 **속성**은 건너간다. 운영 자산에는 목이 없다(`assembleConsole`이 뺀다). 공개 데모와 카드 9개 동수·404 0. 회귀 가드: 화면이 부르는 모든 경로를 목이 답하는지 보는 Go 테스트 | ☐ |
| 규칙: 창당 1스트림·숨김 탭 반납 (bridge가 구조로 강제) | ✓ | 이식: 스트림 주인은 셸 하나(폴백 삭제 — 그 폴백이 규칙을 안에서 깨고 있었다), 숨은 탭은 회선을 반납하고 돌아오면 다시 열며 명단 한 번으로 따라잡는다 | ☐ |
| 팔레트 = TUI styles.go 원천 (theme.css 생성) | ✓ | 상속: 새 콘솔은 매 빌드 page.css를 단일 원천에서 복사한다(assembleConsole) — 팔레트가 갈리는 일이 구조적으로 없다 | ☐ |
| 자기완결(임베드, CDN 0) · FA 빌드타임 주입 | ✓ | 이식: 스프라이트도 internal/webassets로(go generate가 그리로 굽는다) — 새 콘솔이 제 페이지에 직접 심는다(옛 페이지에서 빌리지 않는다). 콘솔 html에 외부 URL 0 | ☐ |

프로브 게이트: scratchpad의 Chromium 프로브를 신규 프론트에 그대로 돌려 기존과 동수 이상 통과.

**2026-08-28 실측** (실데몬 + 두 데모를 그날 코드로 다시 구움):

- 라이브 173/173 — round 84, deep 26, a11y 21, soak 14, demo 28. 실패 0.
- 아이콘 쓸기: 7화면 동수·동일. 컴패니언 화면만 108개 중 94개가 갈리는데 방향은 운영 `없음` /
  신규 `svg`다 — 운영은 스프라이트가 없으면 글리프로 떨어지고 우리는 도형을 그린다.
- 컨트롤: 32조합 중 29 일치. 어긋난 셋은 한 원인 — 운영의 접기 버튼은 폴백 글리프 `▾`가 읽히는
  이름에 섞여 들어가고, 신규는 aria-hidden svg라 이름이 깨끗하다.
- 다시 그리기(10초, childList): skills·access·settings·side·files는 운영과 동수 0.
  board 2 / meet 4 / detail 8 / map 72. **운영 지도는 정지 사진이고**(6초 뒤 노드 상태·쉰 시간
  그대로) 우리 지도는 명단을 따라간다 — 확장이지 회귀가 아니다. 남은 비용은 다시 세우는 방식이
  통짜라는 것이고, 제자리 갱신(사실판이 받은 처치)이 다음 일이다.

## 빌드

- `web/ui`: `./gradlew build` (전용 wrapper, Gradle 9.3 / Java 25 — **컴파일 검증됨**).
  의존성은 `gradle/libs.versions.toml` 한 곳에서 관리 — handbook의 sayaya-web 번들을
  그대로 미러(핵심: dagger가 아니라 **dagger-gwt** — GWT 모듈 xml이 그쪽에 실려 있다).
  라이브러리 모듈(console-bridge·ui-components)은 jar에 소스를 싣는다(GWT 규약).
  `./gradlew assembleConsole` → `build/console/`에 서빙 루트(console.html+모듈 스크립트) 집결.
  sayaya-ui 등은 GitHub Packages — 로컬은 gradle 캐시로 통과, CI는
  `GITHUB_USERNAME`/`GITHUB_TOKEN`(또는 gradle.properties) 필요.
- `web/server`: `go build ./web/server` — 기존 magi-web(기본 127.0.0.1:7777)을 띄운 채
  `go run ./web/server`로 실행(기본 -ui web/ui/build/console), http://127.0.0.1:7778 에서 새 UI 개발.
- 컷오버 시점에: 산출물 go:embed·web-v* 릴리스 레인 전환·기존 삭제.
