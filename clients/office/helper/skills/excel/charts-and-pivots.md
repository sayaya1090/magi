---
name: charts-and-pivots
description: 차트와 피벗 — 종류 고르기, 원본 범위 규칙, 자리 잡기, 확인. 「그래프」「추이」「비교」「집계」가 나오면 읽는다.
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

- 원본 범위는 **머리글을 포함**한다(`source:"A1:C7"`). 첫 행이 계열 이름, 첫 열이 항목이 된다. 열 방향이 아니면 `series_by:"Rows"`.
- 자리는 데이터 오른쪽·아래로, 사용 범위와 겹치지 않게. 크기 기본 480×300pt.
- 제목·축 제목을 단다(`title`, `format_chart{x_title, y_title}`). 범례는 계열이 하나면 `legend:"none"`.
- 넣고 나서 `render_chart` 로 한 번 본다 — 범례가 데이터를 가리는지, 축이 0 에서 시작하는지.

## 피벗
- `add_pivot{source, destination, rows, columns, values}` — 원본은 머리글 있는 한 덩어리, destination 은 빈 자리(겹치면 거절).
- 값 함수 기본 Sum, 개수는 Count. 만든 뒤 `read_range` 로 결과 표를 되읽어 보고한다.
- 원본이 바뀌면 `refresh_pivot`.
