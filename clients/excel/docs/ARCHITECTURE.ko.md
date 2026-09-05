# magi Excel 클라이언트 — 아키텍처(지어진 대로)

[매뉴얼](MANUAL.ko.md) · [모델이 받는 것](TOOLS.ko.md) · [시험](TESTING.ko.md) · [설치](INSTALL.ko.md) · [파워포인트 판 아키텍처](../../powerpoint/docs/ARCHITECTURE.ko.md)

> 파워포인트 판을 **복사해 손만 바꾼** 구조다. 프로세스·얼굴·층·이름·사건·턴의 길은 저쪽 문서 그대로이고,
> 여기는 **엑셀 판이 다른 자리**만 적는다. 2026-09-06 기준 소스가 놓인 대로다 — 어긋나면 소스가 맞다.

## 0. 한 문장

Excel 작업창(애드인)이 사용자당 하나인 **헬퍼**(`magi-xl`, 3001)에 붙고, 헬퍼가 데몬(`magi --daemon`) 한 개에
통합 문서마다 **대화** 하나를 열어 그 대화에만 도구 61개(MCP 서버 이름 `xl`)를 단다. 모델이 도구를 부르면
데몬 → 헬퍼(MCP) → 작업창(SSE `/hand/stream`) → Office.js 순으로 내려가 통합 문서를 고치고, 결과가 같은 길을
되돌아온다(`/hand/reply`).

## 1. 프로세스가 넷이다 — 그리고 넷뿐이다

| 프로세스 | 몇 개 | 무엇 | 소스 |
|---|---|---|---|
| Excel + 작업창 | 창마다 하나 | Office.js 를 쥔 유일한 자리. 통합 문서를 읽고 고치는 코드는 전부 여기 | `addin/src` |
| 헬퍼 `magi-xl` | 사용자당 하나 | 페이지를 https 로 내주고, MCP 서버이며, 데몬을 마련해 붙인다 | `helper/*.go` |
| 데몬 `magi --daemon` | machine 에 하나(워크스페이스 `excel`) | 대화 N개, 모델 호출, 도구 디스패치, 권한 물음 | `internal/` (공용 코어) |
| 모델 심 | 모델당 하나 | OpenAI 호환 `/v1` | `plugins/{antigravity,codex,claudecode}` |

파워포인트 판의 다섯째·여섯째(COM 손과 손 감시기)는 **없다.** Excel 2019 부터 `ExcelApi 1.7` 이라 작업창이 손이다
(2021 LTSC 는 1.14). 바닥 아래 판(2016)에서는 작업창이 `role=viewer` 로 붙되 붙어 줄 손이 없으므로, 헬퍼의 404
를 보고 「이 Excel 판은 ExcelApi 1.7 이 없어 편집을 못 합니다 — Excel 2019 이상에서 여세요」라고 적고 물러선다
(`HelperStream.js`). 사람이 할 일이 「손을 띄워라」가 아니라 「다른 판에서 열어라」인 것이 파워포인트 판과 다르다.

## 2. 헬퍼 — 파워포인트 판과 다른 자리

| 자리 | 파워포인트 | 엑셀 |
|---|---|---|
| 이름(`names.go`) | `ppt`, 3000, `ppt-helper-cert`, `magi-ppt`, 워크스페이스 `powerpoint` | `xl`, **3001**, `xl-helper-cert`, `magi-xl`, 워크스페이스 `excel` |
| 문서 키(`hand.go`) | `pid-<id>` (프레젠테이션 id) | `wb-<id>` (통합 문서의 `MAGI.BOOK` 설정 — 없으면 허브가 짓는다) |
| 스트림 쿼리 | `?presentation=` | `?workbook=` |
| 도구(`tools.go`) | 48 | 61 — 시트 인자는 `withSheet`(별칭 `worksheet`), 범위는 `withRange`(별칭 `range`) |
| 열거형(`enums.go`) | 도형·글머리·전환… | 차트 21·정렬·테두리·표시/숨김·범례·조건부 서식·유효성·표 스타일 60 |
| 지침(`instructions.go`) | 덱 브리프 7단계 | 통합 문서 브리프 7단계 |
| 스킬 | 5벌 | 3벌(`excel/skills/`: `sheet-design`·`formulas`·`charts-and-pivots`) |
| 그림(`image.go`) | 슬라이드·도형 | 범위·차트(`render_range`·`render_chart`) — 같은 기전 |

나머지 파일(`args.go`·`attach.go`·`bridge.go`·`bridges.go`·`certs.go`·`claim.go`·`council.go`·`guides.go`·
`handhttp.go`·`icon.go`·`mcp.go`·`own.go`·`ownstate.go`·`page.go`·`skills.go`)은 이름만 바꾼 복사다. **이것은
빚이다** — 두 헬퍼가 공용 패키지를 나눠 갖는 것이 맞고, 파워포인트 판이 고치는 결함이 여기에 자동으로 오지
않는다. 갚는 순서는 파워포인트 판 DESIGN §5.9 의 이행 뒤.

## 3. 작업창 — 네 층, 그리고 엑셀 고유 파일

층은 같다: `ui/` → `usecase/` → `domain/`·`port/`, `adapter/` 만 Office 를 안다.

| 층 | 파워포인트 | 엑셀 |
|---|---|---|
| `port/` | `DeckPort` | `WorkbookPort` — `selection()`, `point(sheet,address)`, `sheetNames()`, `capabilities()` |
| `adapter/` | `OfficeDeck`·`OfficeHand`·`FakeDeck` + chartxml·animxml·notesxml·eaxml·zip | `OfficeWorkbook`·`ExcelHand`·`FakeWorkbook`·`FakeHand`·`handCore`(두 손이 나눠 쓰는 뼈대)·`a1`(A1 산수)·`pickBook` |
| `domain/` | `Quote`(슬라이드·도형) | `Quote`(시트·주소·크기·값 표본), `Advice`(시트·범위), `Suggestion`(누를 수 있는 손 여섯) |
| `usecase/` | `QuoteSelection`·`PointAtAdvice`·`HandRole`(1.8) | 같은 셋 — `HandRole` 바닥이 `ExcelApi 1.7` |
| `ui/` | `deckFixture`·`fakeCanvas`(슬라이드) | `bookFixture`(시트 둘)·`fakeCanvas`(격자)·`screen.js` 라벨 61개 |

`ExcelHand` 는 op 하나를 `Excel.run` 한 묶음으로 옮긴다. 호출은 한 줄로 선다(`Excel.run` 은 겹치면 거부한다):
40초 넘게 줄에 선 호출은 돌리지 않고 거절하고(헬퍼가 45초에 포기한다), 50초 안에 Excel 이 답하지 않으면 그렇게
말한다. 요구 집합은 op 마다 이름을 대고 잰다(`#need('ExcelApi','1.9','find')`).

## 4. 통합 문서 하나에 대화 하나

파워포인트 판 §4 그대로 — 여섯 이름 중 「문서 키」만 다르다. 통합 문서는 안정된 id 를 스스로 안 주므로
(`workbook.name` 은 파일 이름이라 겹친다) 첫 부착 때 `MAGI.BOOK` 설정에 `book-…` 을 적고 그 뒤로 그 이름으로
붙는다(`stableBookId`). 못 적으면 빈 이름으로 가고 허브가 번호를 짓는다 — 그 문서는 창을 껐다 켜면 새 대화다.

## 5. 한 턴이 지나는 길 · 6. 일부러 하지 않는 것

파워포인트 판 §5·§6 과 같다. 엑셀에서 더한 「하지 않는 것」 하나: **셸로 `.xlsx` 를 쓰지 않는다** — 첫 도구
(`list_sheets`)의 설명이 모델에게 그렇게 못박는다(openpyxl·pandas·COM 으로 만든 파일은 아무도 안 보는 파일이다).

## 7. 알려진 틈

- 작업창의 단추(인용·제안 적용·검토 부탁·카운슬)는 아직 실물에서 사람이 안 눌러 봤다(도구 61개는 MCP 로, 보내기·편집은
  Windows 2021 창에서 됐다 — TESTING §5.1·§5.1.1).
- 헬퍼가 파워포인트 판의 복사다(§2). 공용 패키지로 갈라야 한다.
- 도형·슬라이서·스파크라인은 손에 없다.
- Excel 2021 은 신뢰 카탈로그 키를 하나만 받는다 — 설치기 둘이 폴더·키를 같이 쓴다(INSTALL §3.3). 관리 공유로도 키가
  하나면 받는지는 안 쟀다. macOS 설치는 아직 손이다.
