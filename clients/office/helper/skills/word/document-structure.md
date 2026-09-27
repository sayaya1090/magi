---
name: document-structure
description: 문서를 짜는 법 — 스타일로 제목을 세우고, 문단 번호로 자리를 잡고, 이 문서의 관행에 맞춘다. 목차·쪽 번호·쪽 설정까지. 문서에 손대는 모든 부탁에 먼저 읽는다.
---

# 문서 구조

1. **먼저 읽는다.** `list_paragraphs` 한 번이 목차다 — 번호·스타일·목록 단계·표 자리. 긴 문서는 `from/to` 로 넘긴다.
   `describe_style` 이 이 문서가 무슨 스타일과 글꼴을 쓰는지 말한다. 새로 쓰는 글은 **그 스타일 이름**을 쓴다.
2. **제목은 스타일이다.** 굵은 16pt 는 제목이 아니다 — 탐색 창·목차·개요·화면 낭독기는 스타일(Heading 1·2·3)만 읽는다.
   `insert_paragraphs{style:"Heading 2"}` 또는 `set_style{builtin:"Heading2"}`. 한국어 Word 의 「제목 1」은 `builtin` 으로 잡는다.
3. **자리는 문단 번호다.** `after`/`before` 로 끼워 넣고, 답의 `now`·새 번호를 다음 호출에 쓴다 — 끼워 넣으면 아래 번호가 민다.
   한 절을 쓸 때는 제목과 본문을 `lines` 하나에 순서대로 넣어 한 번에 넣는다.
4. **본문은 Normal(또는 이 문서의 본문 스타일)**, 목록은 `insert_list`/`set_list`, 인용은 Quote. 빈 문단으로 간격을 만들지 말고
   `format_paragraph{space_after}` 를 쓴다.
5. **다 넣었으면 `read_html` 로 그 구간을 한 번 본다** — Word 는 그림을 잘 못 주므로 이것이 눈이다. 그리고 `list_paragraphs` 로
   번호가 어긋나지 않았는지 확인한다.

## 제목 단계의 규칙

- 문서 제목은 **Title 하나**(또는 이 문서가 Heading 1 을 제목으로 쓰면 그것 하나).
- 절은 Heading 1 → 하위 절 Heading 2 → 그 아래 Heading 3. **단계를 건너뛰지 않는다**(1 다음에 바로 3 금지) — 목차와
  화면 낭독기의 탐색이 거기서 끊긴다.
- 제목은 짧게, **핵심 낱말을 앞에**: 「2. 4분기 예산 — 마케팅 10% 증액」. 제목만 훑어도 문서 줄거리가 읽혀야 한다.
- 번호(「1.」「가.」)를 글자로 칠지, 개요 번호를 쓸지는 이 문서를 따른다 — `list_paragraphs` 의 제목 문단 글에 번호가 들어 있으면 글자다.

## 목차·쪽 번호·쪽 설정

- **목차**는 제목 스타일로 만들어진다 — 제목 정리가 끝난 **뒤에** 넣는다:
  `insert_field{field:"toc", after:<제목 뒤 번호>, levels:"1-3"}`. 제목을 더 고쳤으면 목차 필드는 Word 에서 「필드 업데이트」가
  필요하다고 답에 적는다.
- **쪽 번호**는 바닥글 필드: `insert_field{which:"footer", field:"page", align:"Centered"}`, 「3 / 10」 꼴이면
  `insert_field{which:"footer", template:"{page} / {pages}"}`. 바닥글에 글이 이미 있으면 `read_document` 로 먼저 본다.
- **쪽 설정**: 용지·방향·여백은 `set_page_setup{paper:"A4", orientation:"Portrait", margins:{top,bottom,left,right}}`(pt).
  한국 보고서 관행은 A4 세로, 여백 위 20mm·아래 15mm·좌우 20mm 안팎(1mm ≈ 2.83pt) — 이 문서에 이미 설정이 있으면 건드리지 않는다.
- **표지**가 필요하면 제목 문단 뒤에 `insert_break{kind:"page"}`.

## 예제 — 「이 메모를 보고서 모양으로 정리해 줘」

```
list_paragraphs {}                          # 어떤 문단들이 있고 스타일이 뭔지
describe_style {}                           # 이 문서의 제목/본문 스타일 이름
set_style {from:1, builtin:"Title"}         # 첫 줄이 제목이면
set_style {from:3, builtin:"Heading1"}      # 절 머리들
set_style {from:7, builtin:"Heading1"}
set_list {from:4, to:6, kind:"bulleted"}    # 줄글로 늘어놓은 항목을 목록으로
format_paragraph {from:2, to:2, space_after:12}
insert_field {field:"toc", after:1, levels:"1-2"}   # 절이 다섯 넘으면 목차
insert_field {which:"footer", field:"page", align:"Centered"}
read_html {}                                # 눈으로 한 번
list_paragraphs {}                          # 번호·스타일 되읽기
```

글 자체를 다듬는 일(두괄식·개조식·문장 길이)은 `business-writing` 을 읽는다.
