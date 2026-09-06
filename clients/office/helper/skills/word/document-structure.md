---
name: document-structure
description: 문서를 짜는 법 — 스타일로 제목을 세우고, 문단 번호로 자리를 잡고, 이 문서의 관행에 맞춘다
---

# 문서 구조

1. **먼저 읽는다.** `list_paragraphs` 한 번이 목차다 — 번호·스타일·목록 단계·표 자리. 긴 문서는 `from/to` 로 넘긴다.
   `describe_style` 이 이 문서가 무슨 스타일과 글꼴을 쓰는지 말한다. 새로 쓰는 글은 **그 스타일 이름**을 쓴다.
2. **제목은 스타일이다.** 굵은 16pt 는 제목이 아니다 — 탐색 창·목차·개요는 스타일(Heading 1·2·3)만 읽는다.
   `insert_paragraphs{style:"Heading 2"}` 또는 `set_style{builtin:"Heading2"}`. 한국어 Word 의 「제목 1」은 `builtin` 으로 잡는다.
3. **자리는 문단 번호다.** `after`/`before` 로 끼워 넣고, 답의 `now`·새 번호를 다음 호출에 쓴다 — 끼워 넣으면 아래 번호가 민다.
   한 절을 쓸 때는 제목과 본문을 `lines` 하나에 순서대로 넣어 한 번에 넣는다.
4. **본문은 Normal(또는 이 문서의 본문 스타일)**, 목록은 `insert_list`/`set_list`, 인용은 Quote. 빈 문단으로 간격을 만들지 말고
   `format_paragraph{space_after}` 를 쓴다.
5. **다 넣었으면 `read_html` 로 그 구간을 한 번 본다** — Word 는 그림을 못 주므로 이것이 눈이다. 그리고 `list_paragraphs` 로
   번호가 어긋나지 않았는지 확인한다.
