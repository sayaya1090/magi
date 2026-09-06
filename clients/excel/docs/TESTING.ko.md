# 무엇을 어디서 재나 — Excel 판

[↑ 매뉴얼](./MANUAL.ko.md) · [구조](./ARCHITECTURE.ko.md) · [파워포인트 판 TESTING](../../powerpoint/docs/TESTING.ko.md)

> 파워포인트 판의 층 다섯(계약·유도 가드·상호운용·화면 규칙·실물)을 그대로 쓴다. 여기는 **엑셀 판에서 무엇이
> 어느 층에 있고, 어디까지 잰 것인지**만 적는다. 원칙(「초록을 읽는 법」, 가드를 세울 때 묻는 셋)은 저쪽 §9.

## 0. 한 줄로

```bash
go test ./clients/office/helper/                     # 헬퍼(세 판 공용): 계약·유도 가드·문서 대조
node clients/excel/addin/tools/smoke.mjs             # 작업창: 화면 규칙·인용·안내·제안·가짜 손 72개
node clients/excel/addin/tools/smoke-hand.mjs        # 손 노릇: 스트림 → 손 → 답, 역할(손/화면), 헬퍼 어댑터
node clients/excel/addin/tools/excelhand.mjs         # 진짜 손(ExcelHand)을 가짜 Office.js 위에서 72개 전부
TOKEN=… node clients/excel/addin/tools/livehand.mjs   # 가짜 손을 살아 있는 헬퍼에 붙인다 — MCP 로 부르면 여기로 온다
```

2026-09-06 실측: 헬퍼 전부 통과, smoke 344 ok, smoke-hand 71 ok, excelhand 61/61 지나감(거절 0).

## 1. 계약 — 헬퍼 (`go test`)

파워포인트 판 헬퍼의 시험을 이름만 바꿔 옮겼다(`list_sheets`·`read_range`·`write_range`·`format_range`·
`set_number_format` 의 별칭 `number_format`·`move_sheet{to}`). 엑셀에서 새로 잰 것:

- 열거형 표(`enums.go`)가 도구 설명의 예시 값과 같은가 — 차트 21종, 표 스타일 60종, 정렬·테두리·표시/숨김·
  범례·조건부 서식 종류·유효성 종류. **예시 값은 계약이다**(파워포인트 판이 `InvalidArgument` 한 단어로 죽은
  자리).
- 시트를 받는 도구는 전부 `sheet` 와 별칭 `worksheet` 를 받는다.
- 허용 규칙은 「통합 문서를 고치는가」로 갈린다 — 읽기 18개만.

## 2. 유도 가드 — 두 벌이 갈리는 것을 막는다

| 두 벌 | 무는 시험 |
|---|---|
| 헬퍼가 광고하는 61개 ↔ 손(`handCore.ALL_OPS`)이 아는 61개 | `smoke.mjs` 「헬퍼와 손이 같은 이름을 안다」 — 헬퍼 소스를 읽어 대조 |
| 읽기 18개 ↔ `READ_OPS` | 「읽기 도구 집합도 같다」 |
| 제안으로 누를 수 있는 여섯: 헬퍼 `suggest` 설명 ↔ `FIX_TOOLS` ↔ 화면 `FIXABLE` | 「제안으로 누를 수 있는 손 목록이 … 같다」 |
| 61개 전부 사람 말 라벨 | 「도구 61개 전부 라벨이 있다」 |
| 컨텍스트 띠·드롭다운·카운슬 확인 판이 정하는 값 | `contextMeter`·`modelPicker`·`confirmAsk('council')` 블록(2026-09-06) — 다섯 조각 순서, 모름은 0이 아님, 지금 것은 목록에 없어도 섬, 덜 위험한 쪽이 그만두기 |
| 상태 포트가 대화 이름을 옮긴다 | `smoke-hand.mjs` 「대화 이름이 값으로 온다」 — 떨어뜨리면 띠가 영영 숨는다(실물에서 그랬다) |
| 매뉴얼의 허용 규칙 ↔ `AllowRulesTOML()` | `TestTheManualQuotesTheRulesWeGenerate` |
| 매뉴얼이 이름 대는 도구 ↔ 카탈로그 | `TestTheManualNamesEveryTool` |
| 「도구 61개」「읽는 것 18」 등 수 | `TestTheDocsCountTheToolsWeAdvertise` — 이 문서의 「준비됐습니다 — 도구 61 개.」 도 그 시험이 센다 |

## 3. 상호운용 — 가짜 상대는 상대를 검사하지 않는다

- `FakeHand` 는 메모리 통합 문서 위에서 61개를 **정말로** 돈다(값·수식·서식·표·차트·조건부 서식·유효성·이름·
  메모·피벗·스냅숏·제안). 그림 둘(`render_range`·`render_chart`)만 「진짜 Excel 에서만」이라고 **거절한다** —
  가짜는 픽셀을 지어내지 않는다.
- `excelhand.mjs` 의 stub 은 **호스트가 아니다.** 어떤 속성을 읽어도 그럴듯한 값을 주고 어떤 메서드도 받아
  준다 — 재는 것은 「없는 메서드·틀린 인자·load 없이 읽기」 같은 우리 쪽 TypeError 뿐이다. 범위 산수
  (`getRange`·`getResizedRange`·`getCell`)는 경로에 실어 A1 이 2×2 로 커지는 길까지 지나간다.

## 4. 화면 규칙 — 값으로 재는 자리

파워포인트 판 `smoke.mjs` 의 호스트 무관 구간(대화 스트림·권한·펜딩·커서·전사 접기·아이콘 단추·마크다운)을
옮겨 왔고, 엑셀 고유 구간은 새로 썼다: 인용(`Quote` — 시트·주소·크기·값 표본, 빈 범위와 못 읽음의 구분),
안내(`Advice` — 시트·범위 가리키기, 없어진 시트), 제안 카드(`fixLabel`·`fixBoard`), 호스트 고르기
(`pickBook`), 요구 집합(`OfficeWorkbook.capabilities` — 여덟+하나를 각각, 던진 것은 「모름」), 역할
(`handRole` — `ExcelApi 1.7` 바닥), 통합 문서의 안정된 이름(`stableBookId` — `MAGI.BOOK` 설정), A1 산수.

## 4b. 살아 있는 헬퍼에 가짜 손을 붙여 본 것 — 2026-09-06 새벽

Excel 없이 헬퍼·MCP·손 규약을 실물 헬퍼에 대고 돌렸다(`tools/livehand.mjs`, 그때의 `magi-xl` 3001 — 지금은 `magi office` 3000 의 `/xl`):

- `/hand/stream` → `hello`(문서 키 `doc-…`) → `/api/documents` 가 그 문서를 든다.
- MCP `tools/list` 61개, 첫 설명이 「A WORKBOOK IS ALREADY OPEN IN EXCEL…」.
- MCP `tools/call list_sheets` → SSE `call` → FakeHand → `/hand/reply` → 결과(시트 둘, 활성 표시)가 MCP 로 돌아온다.
- 거절이 MCP 오류로 그대로 온다: 열거형(`chart_type="bubble3d"` → 21종 목록), 모르는 인자(`value` → 받는 이름
  목록), `visibility="Nope"`, 가짜 손의 그림 거절. 별칭 `number_format` 은 `format` 으로 옮겨져 돈다.
- **막힌 자리 하나**: 데몬이 도구를 붙일 때 헬퍼의 자가 서명 인증서를 못 믿는다(`x509: certificate signed by
  unknown authority`). 파워포인트 판은 사람이 `magi-ppt helper` 를 키체인에 넣어 둔 상태였고, 엑셀 것
  (`xl-helper-cert.pem`, CN `magi-xl helper`)은 그 시각엔 아직이었다(§5.1 전에 사용자가 넣었다). 데몬의 MCP 클라이언트에는
  CA 를 따로 주는 길이 없다 —
  신뢰 저장소가 유일한 길이고, 그것은 사람이 한다(`./magi-xl -cert-hint`).

## 5. 실물 — Excel 과 사람의 손

### 5.1 2026-09-06 — 처음 붙인 날, 도구 61개 전수

Mac · Excel 16.112.3 · 사용자가 `xl-helper-cert` 를 키체인에 넣고 작업창을 열었다(`wb-book-985f…`). 사람이 작업창에서
「하이」를 친 것과 별개로, 같은 통합 문서에 **MCP 로 도구를 하나씩** 불렀다(스크래치 `xlreal.py`, 쓰기는 새 시트
`magi-test` 안에서만). 첫 판 75호출 중 17이 안 됐고 **전부 우리 쪽**이었다. 고친 뒤 둘째 판 77호출 실패 0, 61/61.

| 층 | 막힌 것 | 원인 | 고침 |
|---|---|---|---|
| 헬퍼 | `add_chart{chart_type:"막대"}` 거절 → 차트 넷 전멸 | 손의 한국어 별칭이 헬퍼 열거형 검사에 먼저 막혔다 | 별칭은 검사만 받고 스키마엔 정본 21개(`chartAliases`) |
| 헬퍼 | `suggest` 전부 거절 | `what` 이 `clear_range`·`autofit` 의 열거형 `what` 과 이름이 같다 | 도구별 예외(`enumExempt`) |
| 헬퍼 | `add_table_rows`·`add_pivot` 의 `rows` 가 「1부터」로 거절 | 파워포인트에서 온 수 검사가 목록에도 걸렸다 | 수가 아니면 검사 밖 |
| 손 | `read_table`·`set_table_cells` | `Table.getDataBodyRangeOrNullObject` 는 Office.js 에 없다 | `getDataBodyRange()` |
| 손 | `add_comment`·`resolve_comment` | `comments.getItemByCellOrNullObject` 도 없다 | `getItemByCell` + `ItemNotFound` 받기 |
| 손 | `clear_conditional_formats` 가 「undefined개」 | 컬렉션에 `count` 속성이 없다 | `getCount()` |
| 손 | `add_table{name}` 이 「표1」로 보고 | 이름은 sync 뒤에 다시 읽어야 지은 이름 | 재로드 |
| 손 | `read_validation.rule` 이 OData 덩어리 | 종류별 칸이 다 실린다 | 채워진 칸만, 재귀 |
| 손 | `add_image{address}` 없음 | 셀에 앵커할 길이 없었다 | `address` 로 그 셀의 left/top |

둘째 판에서 본 것: `render_range` 5,976B·`render_chart` 10,596B(480×300)가 MCP 이미지 블록으로 왔다. 메모 달기·답글·
해결·삭제, 피벗, 이름, 유효성, 조건부 서식 둘, 표(만들기·칸·행·정렬·필터·풀기), 시트 복사·이름·이동·숨김·고정·보호가
전부 사람 눈앞의 통합 문서에서 됐다. 호출당 5~230ms.

배운 것 하나 — **stub 은 없는 메서드를 못 잡는다.** `tools/excelhand.mjs` 의 stub 은 어떤 이름을 불러도 받아 주므로
`getDataBodyRangeOrNullObject` 같은 지어낸 이름이 초록으로 지나갔다. 그 층이 재는 것은 「우리가 부르는 모양」이지
「Office.js 에 그 이름이 있는가」가 아니다. 그 답은 실물뿐이고, 그래서 이 표가 있다.

작업창 쪽은 이날 헬퍼를 다시 띄우자 스스로 새 판(`/v/<id>/`)으로 되살아났다 — 파워포인트 판이 캐시와 싸워 얻은 길이
그대로 왔다. 통합 문서에는 `magi-test` 시트(표·꺾은선 차트·피벗·그림)가 남아 있다 — 사람이 보라고 남겼다.

### 5.1.1 2026-09-06 — Windows LTSC 2021, 설치기로

같은 날 이 저장소의 Windows 머신(Office LTSC 2021 16.0.14334, 볼륨 판)에 `install.ps1` 로 깔았다. 빌드·인증서·카탈로그
키·Run 키·헬퍼(3001)까지 돌았고, 삽입 → 내 추가 기능 → 공유 폴더에 Magi(AI Assistant)가 서고, 추가하니 홈 탭에
「AI Assistant › Magi」(마크 아이콘 포함)가 섰고, 창이 「준비됐습니다 — 도구 61 개」로 열렸다(지원 API 줄 숨음 = 전부 ✓,
ExcelApi 1.14). 창의 보내기 길(`/api/submit`)로 「A5 에 '설치 확인', B5 에 B2:B3 합계」를 시키니 A5 와 `=SUM(B2:B3)`→8 이
들어갔다 — COM 으로 읽어 확인, 다른 셀은 그대로.

그 전에 반나절 막혔던 것: **Excel 2021 은 `WEF\TrustedCatalogs` 아래에 키가 둘 이상이면 뜰 때 전부 지운다**(신뢰 센터의
「설정을 읽는 도중 문제가 발생」). 파워포인트 판이 이미 키 하나를 갖고 있어서 둘이 됐고, 둘 다 사라졌다 — 같은 머신의
PowerPoint 2021 은 둘을 받는다. 관리 공유(`\\localhost\C$`)·GUID 대소문자·매니페스트 내용을 하나씩 걷어내고(빈 폴더 키
하나만 더해도 지워졌다) 남은 것이 개수였다. 두 설치기가 `~/.magi/catalog` 한 폴더에 키 하나를 같이 쓴다(INSTALL §3.3).
개발자 키는 파워포인트 판처럼 무시된다. 이 판에서 안 잰 것: 관리 공유 UNC 로도 키가 하나면 Excel 이 받는지(진짜 공유를
쓴 채로 쟀다).

같은 날 저녁, 메인 `a58dbc52` 까지 받아 다시 깔았다: 브랜드 줄이 「대화 s_…」를 적고 ⋯ 를 펴면 컨텍스트 띠·접기 단추·
프로바이더/모델 고르기가 선다(그 전 판은 「대화 없음」에 띠가 숨어 있었다 — `3b32b1d1` 이 고친 것). 창에서 시킨 편집
(B6 에 `=B5*2`)도 새 빌드에서 들어갔다. `/api/context` 가 한 번 `used 160,858 / window 131,072`(parts 합 약 29,800)를
냈다 — 데몬 쪽 수라 여기서 안 건드렸고, 파워포인트 판 TESTING §5.5 에도 적었다.

통합 헬퍼(`magi office`, `clients/office/install.ps1`)로 다시 깔아도 같은 2021 에서 그대로 된다(2026-09-06 밤): 공유 폴더에서
다시 추가(주소가 `/xl/` 로 바뀌었다), 창 「도구 61 개」, 띠 ~28,732 / 131,072(시스템·도구 목록), 창의 보내기로 시킨
편집이 셀에 들어감. 헬퍼를 띄운 뒤 옛 데몬을 죽이면 창이 「데몬이 죽었다」를 적고 헬퍼는 다시 안 띄웠다(그날 밤 고침: 답 없는 소켓은 다시 마련한다) — 파워포인트 판
TESTING §5.5 에 적었다.

### 5.1.2 2026-09-06 — 모델·컨텍스트 문 넷

새 바이너리(데몬 `context` 문)로 두 데몬을 다시 띄운 뒤 엑셀 헬퍼에 대고: `/api/models` 가 지금 백엔드(클로드 심
58412: opus·sonnet·haiku·claude-sonnet-5)와 전역 Ollama 를 명단으로 답했고, `/api/model{model:haiku}` 뒤 `models` 가
haiku 를 지금 것으로 답했다(sonnet 으로 되돌림). 명단 밖 주소(`http://evil:1/v1`)는 400. `/api/compact` 202.
`/api/context` 는 새 대화라 `used 0 / window 200000, parts 없음` — 첫 턴 뒤에야 다섯 조각이 선다. 처음 판의 명단은
지금 백엔드를 못 세웠다(플러그인 기록 밖의 심) — 헬퍼가 지금 백엔드를 후보에 넣게 고쳤다.

창에서: 처음엔 띠가 안 섰다 — 창은 새 판을 받아 status·documents 는 두드리는데 `/api/context` 는 한 번도 안 왔다(헬퍼의
MAGI_DEBUG 요청 로그로 갈랐다). 상태 포트가 헬퍼 답의 `session` 을 떨어뜨려 「아직 안 붙었다」였다. 고친 뒤 사용자가
Excel 창에서 띠를 봤고(「보인다」), 그 자리에서 「상시 나올 필요는 없다」고 해 ⋯ 판 안으로 옮겼다.

### 5.2 점검표 — 판을 낼 때 손으로 돈다

파워포인트 판 §5.2 의 점검표를 엑셀 말로 옮긴 것. §5.1 은 MCP 로, §5.1.1 은 창의 보내기로 잰 것이라 1·2·3·5·6 은 지났고
4(인용)·7(제안 적용)·8(그림이 대화에)·9(창 둘)·10(헬퍼 재기동 뒤 되살아남 — Mac 에서는 이날 헬퍼를 다시 띄우자 창이 새 판으로
붙었다, §5.1)은 사람이 아직 안 눌렀다:

1. Excel 을 열고 리본 「홈」 오른쪽 끝 **AI Assistant › Magi** → 작업창.
2. 「지원 API」 줄 — 2021/365 면 숨어 있어야 한다(전부 ✓). 펴져 있으면 무엇이 없는지 읽는다.
3. 붙기 → `준비됐습니다 — 도구 72 개.`
4. 범위를 고르고 「인용」 → `[인용] sheet="…" range=… size=…` 조각.
5. 「이 시트 목차 읽어 줘」 → `시트 목차 읽기` 줄, 권한 물음 없이.
6. 「B2:B6 를 천 단위로」 → 권한 물음에 `set_number_format` 과 인자(시트·범위·형식)가 그려진다 → 허용 → Excel
   화면이 바뀐다.
7. 「합계를 수식으로 바꾸라고 제안만 해 줘」 → 제안 카드 → 「적용」 → 바뀐다.
8. `render_range` 를 한 번 — 그림이 대화에 뜬다(1.7 `Range.getImage`).
9. Excel 창을 하나 더 → 브랜드 줄 `문서 2`, 대화가 갈린다.
10. 헬퍼를 껐다 켠다 → 작업창이 스스로 되살아난다.

## 6. 여기서 안 재는 것

Office.js 가 실제로 어떻게 답하는가(`InvalidArgument` 의 `errorLocation`, 병합 범위 위의 정렬, 표 위의 조건부
서식, 피벗 계층 이름). 전부 실물의 몫이고, 그날의 기록이 §5 를 채운다.

## 5.6 2026-09-06 밤 — 도구 62~65 (replace_all · copy_range · fill_range · remove_duplicates)

실물 통합 문서의 새 시트 `magi-tools` 에서: 찾아 바꾸기(셀 2개, 없는 말은 「바꾼 것이 없습니다」 거절), 수식 하나를 `fill_range`
로 D2:D5 까지(참조가 행마다 밀린다 — `=B3*C3`…), 1·2 를 등차로 F6 까지(3·4·5·6), 값만 행열 바꿔 복사, 블록 전체 복사, 품목 열로
중복 1행 제거(남은 3행이 위로 당겨지고 마지막 행은 빈다). 열네 호출 중 거절 하나(일부러), 실패 0. `replaceAll`·`copyFrom`·
`autoFill`·`removeDuplicates` 전부 1.9 실물에서 그대로.

## 5.7 2026-09-06 밤 — 도구 66~69 (set_rows_columns · set_tab_color · set_sheet_view · set_workbook_properties)

실물 통합 문서의 새 시트에서 행 3:4 숨김/보임·높이, 열 B:C 너비·그룹/해제, 행 하나("2")·열 하나("B"), 탭 색과 해제, 눈금선·머리글
끄고 켜기, 문서 속성(제목·작성자)까지 실패 0. 첫 판: 헬퍼의 「1부터」 검사가 `rows: "3:4"` 같은 **구간 글**을 수로 읽어 거절했다 →
글은 수로 읽힐 때만 재고(`"2"` 는 통과, `"0"` 은 거절), 구간·열 글자는 지나간다(`TestOneBasedCheckSkipsSpanStrings`).

## 5.8 2026-09-06 밤 — 차트 계열·피벗 값 서식·참조 추적·시트 가져오기·CSV (도구 70~72 + format_chart·add_pivot 확장)

실물: `trace_cell` 이 `=B2-C2` 의 원천을 `'magi-x'!B2:C2` 로, B2 를 읽는 수식을 D2 로 답함(1.12/1.13). `format_chart` 에 계열 색·이름·
추세선·표식, 축 최소·최대·서식, 원본 바꾸기 — 꺾은선·막대 둘 다 OK. 첫 판: 꺾은선 계열에 `fill.setSolidColor` 가 InvalidOperation →
선 차트(Line·Scatter·Radar)는 `format.line.color` 로. `add_pivot` 값에 `number_format`·`name` → 「매출 합」 열이 `#,##0` 으로.
`insert_sheets_from_file` 로 다른 .xlsx 의 시트가 들어오고(첫 판은 시트 수를 「0개」로 답함 — 같은 컬렉션 프록시를 앞뒤로 읽으면 같은
스냅숏이라, 답으로 온 id 로 이름을 다시 묻게 고침), `.csv` 는 「.xlsx 파일만」 거절. `import_csv` 는 새 시트 `stock` 에 4×3(수는 수로,
따옴표 안의 쉼표 유지), 기존 시트의 K1 에도.

## 통합 헬퍼 재확인

- **2026-09-06 저녁, 통합 헬퍼(`magi office`, 3000 의 `/xl`) 재확인**: 새 인증서 하나를 넣고 Excel 을 다시 켠 뒤 작업창이 붙어(`wb-…`) 바인딩(도구 61)·`list_sheets`·`read_range`·`add_sheet`·`write_range`·`delete_sheet` OK. ⚠떠 있던 프로그램은 옛 매니페스트를 물고 있다 — 매니페스트를 바꾸면 껐다 켜야 새 주소를 읽는다.
