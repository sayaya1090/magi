---
name: charts-and-pivots
description: 차트·피벗·슬라이서·스파크라인 — 종류 고르기, 원본 범위 규칙, 읽히는 차트의 규칙(0 기준·정렬·3D 금지·제목이 결론), 자리 잡기, 확인. 「그래프」「추이」「비교」「집계」「필터 단추」가 나오면 읽는다.
---

# 차트·피벗 규약

## 차트
| 묻는 것 | 종류 |
|---|---|
| 항목끼리 비교 | ColumnClustered(막대), 항목 이름이 길면 BarClustered(가로막대) |
| 시간에 따른 추이 | Line / LineMarkers(꺾은선) |
| 구성비 | Pie(원) — 항목 6개 이하일 때만, 아니면 막대 |
| 두 변수의 관계 | XYScatter(분산) |
| 누적 | ColumnStacked / AreaStacked |
| 증감의 쌓임(시작 → 요인들 → 끝) | Waterfall |

- 원본 범위는 **머리글을 포함**한다(`source:"A1:C7"`). 첫 행이 계열 이름, 첫 열이 항목이 된다. 열 방향이 아니면 `series_by:"Rows"`.
- 자리는 데이터 오른쪽·아래로, 사용 범위와 겹치지 않게. 크기 기본 480×300pt.
- 제목·축 제목을 단다(`title`, `format_chart{x_title, y_title}`). 범례는 계열이 하나면 `legend:"none"`.
- 넣고 나서 `render_chart` 로 한 번 본다 — 범례가 데이터를 가리는지, 축이 0 에서 시작하는지.

### 읽히는 차트의 규칙

- **막대의 값 축은 0 에서 시작한다** — 막대는 길이로 읽히므로 잘린 축은 차이를 부풀린다. 꺾은선은 0 이 아니어도 된다.
  `format_chart{y_min:0}`.
- **막대는 정렬한다** — 순서가 없는 항목(지역·제품)은 값 큰 순으로 원본을 `sort_range` 한 뒤 차트를 만든다. 시간은 시간 순.
- **3D·그림자·장식 채우기를 쓰지 않는다** — 3D 는 비율을 비틀어 비교를 틀리게 한다.
- **제목이 결론을 말한다**: 「지역별 매출」보다 「서울이 매출의 48% — 전년보다 6%p 늘어」.
- 계열이 다섯을 넘으면 차트를 나누거나 중요한 둘만 색을 주고 나머지는 회색.
- 값 표시(`data_labels:true`)는 막대가 열 개 이하일 때. 그 이상이면 축 눈금으로.

## 피벗
- `add_pivot{source, destination, rows, columns, values}` — 원본은 머리글 있는 한 덩어리 **또는 표 이름**(머리글까지 쓴다),
  destination 은 빈 자리(겹치면 거절). 없는 필드를 대면 있는 머리글을 대며 거절한다.
- 값 함수 기본 Sum, 개수는 Count. 만든 뒤 `read_range` 로 결과 표를 되읽어 보고한다.
- 원본이 바뀌면 `refresh_pivot` — 피벗 칸은 `replace_all` 로 못 바꾼다(원본을 고치고 새로 고친다).

## 슬라이서 — 사람이 누르는 필터 단추 (Windows)

표나 피벗의 한 열을 단추로 거르게 한다. 대시보드·보고 시트에 좋다.

```
add_slicer {source:"매출표", field:"지역", sheet:"대시보드", name:"지역선택", left:20, top:20, width:140, height:200}
remove_slicer {name:"지역선택"}
```

- `source` 는 표 이름 또는 피벗 이름, `field` 는 그 열 이름(없으면 있는 이름을 대며 거절).
- 한 열에 슬라이서는 하나 — 이미 있으면 Excel 이 거절한다.

## 스파크라인 — 칸 안의 작은 추이 (Windows)

표 끝 열에 행마다 추이를 보인다. 차트 여러 개보다 한눈에 읽힌다.

```
add_sparklines {address:"G2:G5", source:"B2:F5", kind:"line", markers:true, color:"#1F4E79"}
remove_sparklines {address:"G2:G5"}
```

- `address` 의 칸 수 = `source` 의 행 수(행마다 한 줄). `kind`: line(추이), column(크기 비교), win_loss(흑자/적자).

## 예제 — 「지역별 매출 차트 하나 그려 줘」

```
describe_sheet {}                                       # 데이터 A1:C5 (지역·매출·비용)
sort_range {address:"A1:C5", has_headers:true, by:[{column:"매출", ascending:false}]}   # 큰 순
add_chart {source:"A1:B5", chart_type:"BarClustered", title:"서울이 매출의 48%", left:360, top:10}
format_chart {chart:"<답의 이름>", legend:"none", data_labels:true, y_min:0, x_title:"매출(억 원)"}
render_chart {chart:"<답의 이름>"}                        # 눈으로 — 0 기준·정렬·라벨 겹침
```
