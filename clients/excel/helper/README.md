# magi-xl — Excel 애드인의 헬퍼

[사용자 매뉴얼](../docs/MANUAL.ko.md) · [무엇을 어디서 재나](../docs/TESTING.ko.md) · [아키텍처](../docs/ARCHITECTURE.ko.md) · [애드인](../addin/README.md) · [파워포인트 판 헬퍼](../../powerpoint/helper/README.md)

애드인과 magi 데몬 사이에 서는 프로세스. **사용자당 하나**다. 파워포인트 판 헬퍼(`magi-ppt`)를 **복사해 이름과
도구만 바꿨다** — 얼굴 셋(데몬 쪽 MCP 서버 · 애드인 쪽 https 페이지+손 · 같은 연결의 반대 방향으로 흐르는 전사)과
그것을 무는 시험은 저쪽 README 그대로다. 여기는 다른 자리만.

## 돌려 보기

```sh
go build -o magi-xl ./clients/excel/helper
./magi-xl                       # 기본값: 127.0.0.1:3001, 애드인은 clients/excel/addin
./magi-xl -allow-rules          # config.toml 에 붙여 넣을 허용 규칙(읽기 18개)을 찍는다
./magi-xl -cert-hint            # 인증서를 신뢰 저장소에 넣는 법
```

첫 기동이 인증서를 만든다(`<config>/xl-helper-cert.pem`, 키는 `0600`). **신뢰 저장소에 넣는 것은 사람이 한다.**

## 파워포인트 판과 다른 파일

| 파일 | 무엇 |
|---|---|
| `names.go` | `xl` · 3001 · `xl-helper-cert` · `magi-xl` · 워크스페이스 `excel` · 문서 키 `wb-` · 쿼리 `workbook` |
| `tools.go` | 도구 61개(읽기 18 · 쓰기 43). `withSheet`(별칭 `worksheet`)·`withRange`(별칭 `range`)·`documentProp`(별칭 `workbook`). 별칭의 설명은 「Same as X — prefer X.」 |
| `enums.go` | Excel.js 열거형 — 차트 21, 정렬 8, 세로 정렬 5, 테두리 8, 표시/숨김 3, 범례 6, 조건부 서식 종류 7·연산자 12, 유효성 종류 7·연산자 8, 표 스타일 60. 인자 이름으로 잰다(`checkEnums`) |
| `instructions.go` | AGENTS.md 에 심는 통합 문서 브리프 7단계 |
| `excel/skills/` | 컴패니언에 심는 가이드 셋 |

나머지는 복사다 — **파워포인트 판이 고치는 결함이 여기에 자동으로 오지 않는다.** 공용 패키지로 가르는 것이 빚.

## 지키는 규칙 — 그리고 그것을 무는 시험

`go test ./clients/excel/helper/`. 파워포인트 판의 시험을 이름만 바꿔 옮겼고(`list_sheets`·`read_range`·
`write_range`·`set_number_format` 의 별칭 `number_format`·`move_sheet{to}`), 엑셀에서 더한 것:

| 규칙 | 무는 시험 |
|---|---|
| 예시 값은 계약 — 설명문의 값이 열거형 표에 있다 | `enums_test.go` |
| 시트를 받는 도구는 전부 `sheet`+`worksheet` | `tools_test.go` 「시트를 받는 도구」 |
| 허용 규칙 = 통합 문서를 고치지 않는 것 18개 | `TestAllowRulesCoverExactlyWhatDoesNotChangeTheDeck` |
| 매뉴얼의 허용 규칙·도구 이름·수가 코드와 같다 | `TestTheManualQuotesTheRulesWeGenerate` · `TestTheManualNamesEveryTool` · `TestTheDocsCountTheToolsWeAdvertise` |

## 아직 아닌 것

- 실물 Excel 에는 2026-09-06 에 붙어 도구 61개가 전부 돌았다(`docs/TESTING.ko.md` §5.1). 작업창 화면의 실물 점검은 아직.
- 공용 패키지로 갈라야 한다(위).
