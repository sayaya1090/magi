# Excel 애드인

[사용자 매뉴얼](../docs/MANUAL.ko.md) · [무엇을 어디서 재나](../docs/TESTING.ko.md) · [아키텍처](../docs/ARCHITECTURE.ko.md) · [헬퍼](../helper/README.md) · [파워포인트 판 애드인](../../powerpoint/addin/README.md)

Excel 작업창. 파워포인트 판 애드인을 **복사해 손을 바꿨다** — 층(`ui → usecase → domain·port`, `adapter` 만 Office 를
안다), 대화 스트림, 권한, 가이드, 카운슬 스위치, 마크다운 전사는 그대로다.

## 먼저, 무엇이 검증됐고 무엇이 안 됐는지

| 길 | 무엇으로 | 상태 |
|---|---|---|
| `node tools/smoke.mjs` | `FakeWorkbook`·`FakeHand` | **돈다** — 344 ok (2026-09-06) |
| `node tools/smoke-hand.mjs` | `FakeHand`·`ServeHand`·`HelperStream` | **돈다** — 71 ok |
| `node tools/excelhand.mjs` | `ExcelHand` 를 가짜 Office.js 위에서 | **돈다** — 76/76, 거절 0 |
| `PORT=3010 node tools/serve.mjs` 로 브라우저에서 | 가짜 격자 + `FakeHand` | 돈다 — 인용·전송·안내·제안 적용까지 |
| `TOKEN=… node tools/livehand.mjs` | `FakeHand` 를 **살아 있는 헬퍼**에 손으로 | **돈다** — MCP `tools/call` 이 SSE 로 내려와 답이 돌아간다(2026-09-06, TESTING §4b) |
| `node tools/sweep.mjs [--xlsx x.xlsx]` | **실물** — 붙은 통장에 76개 전부(「스윕」시트 안에서만, 끝에 지운다) | **돈다** — LTSC 2021: 76/76 호출, 오류는 메모 넷(판이 안 준다)뿐(2026-09-07, TESTING §5.10) |
| **헬퍼가 내준 페이지**(`magi office`) | `HelperApi`·`HelperStream`, 손은 `ExcelHand`(Excel 안) 또는 `FakeHand` | **Excel 안에서 돈다** — 2026-09-06 도구 61개 전부(docs/TESTING §5.1), Windows 2021 창의 보내기로 편집(§5.1.1). 인용·제안·검토 단추는 아직 사람이 안 눌렀다 |

`excelhand.mjs` 의 stub 은 어떤 속성을 읽어도 값을 주고 어떤 메서드도 받아 주므로, 지나간 것은 「우리가 부르는 모양」까지다 —
실제로 `getDataBodyRangeOrNullObject` 같은 없는 이름이 초록으로 지나갔고 실물이 잡았다(docs/TESTING §5.1).

## 구조 — 엑셀 고유 파일

```
src/port/WorkbookPort.js      selection() · point(sheet,address) · sheetNames() · capabilities()
src/adapter/OfficeWorkbook.js Office.js 로 위를 한다. MAGI.BOOK 설정에 안정된 문서 이름을 적는다(stableBookId)
src/adapter/ExcelHand.js      진짜 손 — op 76개를 Excel.run 한 묶음씩으로. 한 줄로 선다(40초 넘게 기다린 호출은 거절)
src/adapter/FakeHand.js       가짜 손 — 메모리 통합 문서 위에서 76개를 정말로 돈다. 그림 둘만 거절
src/adapter/handCore.js       두 손의 뼈대 — READ_OPS/WRITE_OPS/ALL_OPS, FIX_TOOLS, 인자 읽기, 거절, 봉투, 차트 별칭
src/adapter/a1.js             A1 산수 — parseAddress · rangeName · cellName · colName
src/adapter/pickBook.js       Office 가 있나·Excel 인가·늦나 — 가짜로 갈 때 사유를 남긴다
src/domain/Quote.js           인용 — 시트·주소·크기·값 표본(12×12)·못 읽음
src/domain/Advice.js          안내 — 시트·범위 가리키기, SheetIndex(시트가 아직 있나)
src/domain/Suggestion.js      제안 카드 — 누를 수 있는 손 여섯
src/usecase/HandRole.js       손인가 화면인가 — ExcelApi 1.7 바닥
src/ui/bookFixture.js         브라우저 목업의 통합 문서(매출·비용) + 제안 둘
src/ui/screen.js              화면이 정하는 것 — 도구 76개의 사람 말 라벨, 인용 몸통, 제안·안내 판, 검토 부탁
```

두 손이 아는 이름은 헬퍼 `tools.go` 가 광고하는 이름과 같은 집합이어야 한다 — `smoke.mjs` 가 헬퍼 소스를 읽어 대조한다.

## Excel 에 붙이기

[`docs/INSTALL.ko.md`](../docs/INSTALL.ko.md). 매니페스트는 `manifest.xml`(3001, 최상위 `<Requirements>` 에는
`SharedRuntime 1.1` 만 — `ExcelApi` 를 적으면 조건이 안 맞는 판에서 조용히 안 뜬다). 홈 탭 **AI Assistant › Magi**.

## 아직 가짜인 것 · 아직 아닌 것

- 실물 Excel(위).
- 도형·슬라이서·스파크라인 손이 없다.
- 브라우저 목업의 격자는 값만 그린다(서식·차트는 안 그린다).

## ⋯ 판의 컨트롤 (2026-09-06)

브랜드 줄의 `⋯` 로 펴는 줄에 셋이 산다 — 전부 헬퍼 문 위이고, 순수 함수(screen.js)가 정한 것을 view.js 가 그린다:

- **컨텍스트 띠**(`contextMeter`): 창이 얼마나·무엇으로 찼나 — 웹 콘솔과 같은 다섯 조각(시스템·도구 목록·대화·호출·결과),
  `/api/context`. 펼 때 읽고 편 동안 10초마다. 접기 단추는 `/api/compact`, 대화·호출·결과가 0이면 잠긴다.
- **프로바이더·모델 드롭다운**(`modelPicker`): 직접 구현한 M3 exposed dropdown(아이콘 단추와 같은 방식 — 번들 없음).
  `/api/models` 로 채우고 `/api/model` 로 보낸다, 다음 턴부터.
- **카운슬 스위치**: 누르면 먼저 묻는다(`confirmAsk('council')`) — 데몬이 다시 뜨고 같은 데몬의 다른 창·플러그인도 끊긴다.
  통합 문서는 그대로다.
