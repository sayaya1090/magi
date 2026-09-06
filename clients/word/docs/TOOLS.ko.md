# 모델이 받는 것 — Word 판

[매뉴얼](MANUAL.ko.md) · [아키텍처](ARCHITECTURE.ko.md) · [시험](TESTING.ko.md)

## 0. 한 턴에 모델이 보는 것

| 층 | 무엇 | 몇 개 | 어디서 오나 |
|---|---|---|---|
| 문서 도구 | `mcp__word__*` | **45** | 헬퍼 `tools.go` → MCP `tools/list` → 데몬이 이 대화에만 광고 |
| 코어 내장 도구 | `bash`, `read`, `edit`, `skill`, `todowrite` … | 코어 레지스트리 | 데몬 |
| 플러그인 도구 | `land` | 1 (카운슬 끈 대화) | `<config>/plugins/landing` |
| 가이드 | `document-structure`, `editing`, `tables-and-review` | 3 | 헬퍼가 `word/skills/` 에서 심는다 |
| 지속 지시 | `AGENTS.md` | 브리프 7단계 | `instructions.go` |
| 첫 도구 설명의 머리말 | "A DOCUMENT IS ALREADY OPEN IN WORD…" | 1 | `list_paragraphs` |

## 1. 도구 45 — 무리별과 요구 집합

| 무리 | 도구 | 요구 집합 |
|---|---|---|
| 목차·읽기 | `list_paragraphs` `read_paragraphs` `read_document` `find` `read_table` `read_html` `describe_style` | 1.3 |
| 문단 | `insert_paragraphs` `replace_paragraph` `delete_paragraphs` `set_style` `format_text` `format_paragraph` `insert_break` | 1.3 |
| 표 | `insert_table` `set_table_cells` `add_table_rows` `delete_table` `format_table` | 1.3 |
| 목록 | `insert_list` `set_list` | 1.3 |
| 그림·머리글·링크·바꾸기 | `insert_image` `set_header_footer` `set_hyperlink` `replace_all` | 1.3 |
| 메모·책갈피·추적 | `read_comments` `add_comment` `reply_comment` `resolve_comment` `add_bookmark` `delete_bookmark` `set_track_changes` | 1.4 |
| 변경 검토 | `read_tracked_changes` `review_changes` | 1.6 |
| 필드 | `insert_field` — 목차·쪽 번호·전체 쪽수·날짜·시각·제목·작성자·파일 이름, `template` 로 글과 섞어 본문·머리글·바닥글에 | 1.5 |
| 되돌리기·속성·기억 | `snapshot_paragraphs` `restore_paragraphs` `set_properties` `read_tags` `set_tag` | 1.3 |
| 제안 | `read_suggestions` `suggest` `drop_suggestion` | 1.4 (settings) |
| 작업창 | `advise` `clear_advice` | — |

봉투는 `{document, label, result, changed[], epoch, count}` — 엑셀·파워포인트와 같다. 고친 뒤에는 `result.now.paragraphs`.

## 2. 가이드 셋 · 3. 끝내는 문

엑셀 판과 같다. 착지 문 계약(이름은 `land`, did 는 문자열 배열, 「바꾼 것 없음」 수용)도 같다.
