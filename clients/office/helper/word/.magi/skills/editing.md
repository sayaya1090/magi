---
name: editing
description: 남의 글을 고치는 법 — 변경 추적, 스냅숏, 찾아 바꾸기, 메모
---

# 고치기

- **남의 문서는 변경 추적을 켜고 고친다** — `set_track_changes{mode:"TrackAll"}`(WordApi 1.4+, Microsoft 365/2024). 2019·2021 에서는
  그 도구가 이름을 대고 거절하니, 그때는 고치기 전에 `snapshot_paragraphs` 를 찍고 무엇을 바꿨는지 답에 적는다.
- **찾아 바꾸기는 `replace_all`** — 먼저 `find` 로 몇 군데인지 본다. 흔한 낱말이면 `from/to` 로 범위를 좁히고 스냅숏을 찍는다.
- **한 문단을 다시 쓰려면 `replace_paragraph`** — 스타일은 남고 글자 서식은 사라진다. 굵은 낱말이 있었으면 `format_text{text}` 로 되살린다.
- **의견은 고치지 말고 남긴다** — `add_comment`(1.4+), 안 되면 `advise` 로 작업창에. 「이렇게 바꾸는 게 어떠냐」는 `suggest` 로
  카드를 붙이면 사람이 한 번에 적용한다(1.4+).
- **끝나면 `read_html` 로 고친 구간을 보고, `read_paragraphs` 로 되읽어 확인한 것을 답에 적는다.**
