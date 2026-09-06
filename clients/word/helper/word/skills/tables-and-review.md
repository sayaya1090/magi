---
name: tables-and-review
description: 표·그림·머리글/바닥글, 그리고 끝내기 전 검토
---

# 표·그림·검토

- **표는 `insert_table{values}`** — 첫 행이 머리글(has_header). 문서에 캡션 관행이 있으면 캡션 문단을 위에 따로 넣는다.
  칸은 `set_table_cells{cells:[{row,column,value}]}`(0-based), 행은 `add_table_rows`, 모양은 `format_table{table_style}`.
  `read_table` 로 되읽는다.
- **그림은 경로만** — `insert_image{path, after, width}`. 헬퍼가 파일을 읽어 싣는다. base64 를 직접 만들지 않는다.
- **머리글·바닥글은 절 단위** — `set_header_footer{which, text, section, kind}`. 쪽 번호 필드가 있는 바닥글은 통째로 바꾸면
  번호가 사라지니 먼저 `read_document` 로 본다.
- **끝내기 전**: `list_paragraphs` 로 번호·스타일이 맞는지, `read_html` 로 모양이 맞는지, 바꾼 것을 손잡이(문단 번호·표 번호)와
  함께 답에 적는다. 안 한 것은 안 했다고 적는다.
