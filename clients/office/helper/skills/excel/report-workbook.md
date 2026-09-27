---
name: report-workbook
description: 보고용 통장을 처음부터 짓는 법 — 데이터 시트·계산·요약(대시보드) 시트, KPI 칸, 스파크라인·슬라이서, 인쇄 설정과 PDF. 「보고서 만들어 줘」「대시보드」「월간 실적 정리」「PDF 로 보내게」가 나오면 sheet-design 다음에 읽는다.
---

# 보고용 통장

보고 받는 사람은 **요약 한 장**을 본다. 데이터와 계산은 그 뒤에 있다. 그래서 짓는 순서는
**데이터 → 계산 → 요약 → 인쇄**이고, 보이는 순서는 **요약이 첫 탭**이다.

## ✔ 체크리스트

| # | 확인 | 지우는 기준 |
|---|---|---|
| 1 | 데이터는 표 | 원본은 머리글 한 줄짜리 표(`add_table`), 병합·빈 행 없음 |
| 2 | 요약은 수식 | 요약 시트의 숫자는 전부 데이터를 가리키는 수식 — 값을 옮겨 적지 않았다 |
| 3 | 결론이 맨 위 | 요약 시트 1–3행에 제목과 **한 줄 결론**(「3분기 매출 125억, 목표 104%」) |
| 4 | KPI 는 서너 개 | 큰 숫자 칸 3–4개(매출·이익·달성률·증감) — 더 많으면 아무것도 안 보인다 |
| 5 | 추이는 작게 | 행마다 추이는 스파크라인, 큰 그림 하나는 차트 |
| 6 | 필터는 단추 | 사람이 거를 열이 있으면 슬라이서(Windows) — 필터 드롭다운보다 찾기 쉽다 |
| 7 | 인쇄가 한 장 | `set_page_setup{fit_width:1, orientation}` 로 폭 맞춤, `print_area` 지정 |
| 8 | 요약이 첫 탭 | `move_sheet{sheet:"요약", to:1}` |
| 9 | 눈으로 봤다 | 요약 블록에 `render_range` 한 번 |

## 뼈대

| 시트 | 내용 |
|---|---|
| 요약 | 제목·한 줄 결론·KPI 칸·차트 하나·표(지역별/품목별)·스파크라인. 사람이 보는 유일한 장 |
| 계산 | SUMIFS 집계, 증감률, 목표 대비 — 요약이 가리키는 곳. 입력이 바뀌면 여기가 따라간다 |
| 데이터 | 원본 행들(표). 새 달이 오면 여기에 행만 붙인다 |
| 입력(있으면) | 목표·환율·가정 — 색 칸(`sheet-design` §4.5) |

## KPI 칸 만들기

머리글(값)과 숫자(수식)는 **두 번에** 넣는다 — `write_range` 한 번은 `values` 나 `formulas` 중 한 모양이다.
```
write_range {sheet:"요약", address:"B4:E4", values:[["매출(억 원)","영업이익(억 원)","목표 달성률","전년 대비"]]}
write_range {sheet:"요약", address:"B5:E5", formulas:[["=계산!B2","=계산!B3","=계산!B4","=계산!B5"]]}
format_range {sheet:"요약", address:"B5:E5", size:20, bold:true, align:"Center"}
format_range {sheet:"요약", address:"B4:E4", color:"#595959", align:"Center"}
set_number_format {sheet:"요약", address:"B5:C5", format:"#,##0.0"}
set_number_format {sheet:"요약", address:"D5:E5", format:"0.0%"}
add_conditional_format {sheet:"요약", address:"E5", cf_kind:"cell_value", operator:"LessThan", value:"0", color:"#C00000"}   # 감소면 빨강
```

- 조건부 서식은 **의미가 있는 곳 한두 군데**만(감소=빨강, 목표 미달=주황). 온 시트를 색칠하면 아무것도 안 보인다.

## 예제 — 「이 판매 데이터로 월간 보고서 만들어 줘」

```
list_sheets {}                                                   # 데이터 시트 이름·범위
describe_sheet {sheet:"판매"}                                     # A1:E500, 머리글 날짜·지역·품목·수량·매출
add_table {sheet:"판매", address:"A1:E500", name:"판매표", has_headers:true}
add_sheet {name:"요약", activate:true}
write_range {address:"A1", values:[["9월 판매 보고"]]}
format_range {address:"A1", size:18, bold:true}
write_range {address:"A2", formulas:[["=\"매출 \"&TEXT(SUM(판매표[매출])/1e8,\"0.0\")&\"억 원\""]]}   # 한 줄 결론(수식) — 전월 비교는 계산 시트의 칸을 이어 붙인다
write_range {address:"A8:C8", values:[["지역","매출","비중"]]}
write_range {address:"A9:A12", values:[["서울"],["부산"],["대구"],["광주"]]}
write_range {address:"B9:C12", formulas:[["=SUMIFS(판매표[매출],판매표[지역],A9)","=B9/SUM($B$9:$B$12)"], …]}
sort_range {address:"A8:C12", has_headers:true, by:[{column:"매출", ascending:false}]}
add_chart {source:"A8:B12", chart_type:"BarClustered", title:"지역별 매출", left:320, top:120}
format_chart {chart:"<답의 이름>", legend:"none", y_min:0, data_labels:true}
add_slicer {source:"판매표", field:"품목", sheet:"요약", left:20, top:300}          # Windows
add_sparklines {address:"D9:D12", source:"계산!B2:M5", kind:"line"}                 # 월별 추이가 계산 시트에 있으면
set_page_setup {sheet:"요약", orientation:"Landscape", fit_width:1, print_area:"A1:H30"}
move_sheet {sheet:"요약", to:1}
render_range {sheet:"요약", address:"A1:H30", max_width:900}                       # 눈으로
export_pdf {sheet:"요약"}                                                          # Windows — 통장 옆 「이름 - 요약.pdf」
```

- 월이 바뀌어도 쓰이게: 요약은 전부 `판매표[…]` 를 가리키는 수식이다. 데이터에 행만 붙이면 요약이 따라간다.
- 모르는 숫자(목표·전월 값)는 지어내지 않는다 — 입력 칸으로 비워 두고 답에 「채울 곳」으로 적는다.

## PDF

`export_pdf{sheet}` 는 그 시트만, `sheet` 를 빼면 통장 전체. 인쇄 영역·방향·폭 맞춤(`set_page_setup`)이 그대로 PDF 가 된다 —
**PDF 전에 인쇄 설정부터.** 빈 시트는 Excel 이 PDF 로 안 낸다.
