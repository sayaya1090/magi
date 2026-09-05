# 무엇을 어디서 재나 — Excel 판

[↑ 매뉴얼](./MANUAL.ko.md) · [구조](./ARCHITECTURE.ko.md) · [파워포인트 판 TESTING](../../powerpoint/docs/TESTING.ko.md)

> 파워포인트 판의 층 다섯(계약·유도 가드·상호운용·화면 규칙·실물)을 그대로 쓴다. 여기는 **엑셀 판에서 무엇이
> 어느 층에 있고, 어디까지 잰 것인지**만 적는다. 원칙(「초록을 읽는 법」, 가드를 세울 때 묻는 셋)은 저쪽 §9.

## 0. 한 줄로

```bash
go test ./clients/excel/helper/                      # 헬퍼: 계약·유도 가드·문서 대조
node clients/excel/addin/tools/smoke.mjs             # 작업창: 화면 규칙·인용·안내·제안·가짜 손 61개
node clients/excel/addin/tools/smoke-hand.mjs        # 손 노릇: 스트림 → 손 → 답, 역할(손/화면), 헬퍼 어댑터
node clients/excel/addin/tools/excelhand.mjs         # 진짜 손(ExcelHand)을 가짜 Office.js 위에서 61개 전부
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

## 5. 실물 — Excel 과 사람의 손

**아직 없다(2026-09-06).** 처음 붙이는 날 여기에 쌓는다. 파워포인트 판 §5.2 의 점검표를 엑셀 말로 옮긴 것:

1. Excel 을 열고 리본 「홈」 오른쪽 끝 **AI Assistant › Magi** → 작업창.
2. 「지원 API」 줄 — 2021/365 면 숨어 있어야 한다(전부 ✓). 펴져 있으면 무엇이 없는지 읽는다.
3. 붙기 → `준비됐습니다 — 도구 61 개.`
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
