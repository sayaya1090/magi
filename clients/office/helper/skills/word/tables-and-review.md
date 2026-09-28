---
name: tables-and-review
description: 표·그림·머리글/바닥글, 그리고 끝내기 전 검토 — 분량(쪽 수) 재기와 PDF 로 내보내기까지.
---

# 표·그림·검토

## 공통 적용 기준

사용자가 지정한 산출물 언어와 문체를 우선합니다. 지정이 없으면 기존 문서의 언어·표기 관례를 유지하고, 새 문서는 사용자 요청의 언어를 따릅니다. 대화 언어와 산출물 언어가 다를 수 있습니다. 혼합 언어, 직접 인용, 고유명사는 임의로 번역하지 않습니다. 한국어 개조식·공문 형식을 다른 언어에 강제하지 않습니다. 날짜·소수점·통화·단위는 대상 문서의 지역 관례를 따릅니다. 도구 이름·인자·수식 문법은 번역하지 않고 API 규약을 따릅니다.

아래 수치·배치·호출은 예시입니다. 사용자 지정 내용과 기존 템플릿이 우선하며, 예제의 숫자·날짜·이름을 사실로 복사하지 않습니다. 현재 도구 목록과 인자 설명을 확인한 뒤 제공된 도구만 호출합니다. 운영체제나 Office 연도만으로 지원을 단정하지 않습니다. 완료 검증 도구가 없으면 호출하지 않고 수행 결과·검증 범위·미완료 항목을 답변으로 보고합니다.


- **표는 `insert_table{values}`** — 첫 행이 머리글(has_header). 문서에 캡션 관행이 있으면 캡션 문단을 위에 따로 넣는다.
  칸은 `set_table_cells{cells:[{row,column,value}]}`(0-based), 행은 `add_table_rows`, 모양은 `format_table{table_style}`.
  `read_table` 로 되읽는다.
- **열을 늘릴 때** `edit_table{add_columns:{values:["머리","값1","값2"]}}` — 평평한 목록 하나는 **한 열**(위→아래)이다.
  여러 열이면 `{count:2, values:[[…],[…]]}`.
- **그림은 경로만** — `insert_image{path, after, width, alt}`. 헬퍼가 파일을 읽어 싣는다. base64 를 직접 만들지 않는다.
  `alt`(대체 텍스트)는 비우지 않는다 — 그림이 무엇을 말하는지 한 문장.
- **머리글·바닥글은 절 단위** — `set_header_footer{which, text, section, kind}`. 쪽 번호 필드가 있는 바닥글은 통째로 바꾸면
  번호가 사라지니 먼저 `read_document` 로 본다. 쪽 번호는 `insert_field{which:"footer", field:"page"}`.

## 표를 읽기 쉽게

- 머리글 행은 굵게·채우기(`format_table_cells{rows:[0], bold:true, fill:"#1F4E79", color:"#FFFFFF"}`) 또는 표 스타일의 머리글.
- **숫자 열은 오른쪽 정렬**, 글자 열은 왼쪽. 단위는 머리글에(「매출(억 원)」) — 칸마다 「억 원」을 붙이지 않는다.
- 합계 행은 맨 아래, 굵게. 표 제목과 「단위: 억 원」은 표 **위** 문단.
- 칸 병합은 머리글 묶음에만(`edit_table{merge}`). 데이터 칸을 병합하면 정렬·복사가 깨진다.

## 끝내기 전

1. `list_paragraphs` 로 번호·스타일이 맞는지, `read_html` 로 모양이 맞는지 본다.
2. **분량 제한**이 있으면(「A4 두 장 이내」) Windows 에서 `document_stats {}` 로 쪽 수를 잰다 — Office.js 는 쪽 수를 모른다.
   넘치면 무엇을 줄일지 사람에게 제안한다(마음대로 지우지 않는다).
3. 한 쪽을 그림으로 확인하려면 `render_page{page}`.
4. 바꾼 것을 손잡이(문단 번호·표 번호·이름)와 함께 답에 적는다. 안 한 것은 안 했다고 적는다.

## PDF 로 보낼 때

`export_pdf {}` (Windows) — 경로를 안 주면 문서 옆에 같은 이름으로, 저장 안 한 문서면 「문서」 폴더에 생긴다. 있는 파일은
`overwrite:true` 없이는 안 덮는다. 만든 경로와 크기를 답에 적는다. 문서 자체는 안 바뀐다.

## 예제 — 「이 표 보기 좋게 다듬고 PDF 로 뽑아 줘」

```
read_table {table:1}                                         # 무엇이 들었나
format_table {table:1, table_style:"GridTable4_Accent1", header_row:true, banded_rows:true}
format_table_cells {table:1, rows:[0], bold:true, align:"Centered"}
format_table_cells {table:1, columns:[1,2,3], align:"Right"}  # 숫자 열
add_table_rows {table:1, rows:[["합계","250","262.1","105%"]]}
format_table_cells {table:1, rows:[4], bold:true}
read_html {}                                                 # 눈으로
document_stats {}                                            # 쪽 수 확인 (Windows)
export_pdf {}                                                # → 문서 옆 같은 이름.pdf
```
