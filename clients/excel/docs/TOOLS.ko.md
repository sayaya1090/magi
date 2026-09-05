# 모델이 받는 것 — Excel 판

[매뉴얼](MANUAL.ko.md) · [아키텍처](ARCHITECTURE.ko.md) · [시험](TESTING.ko.md) · [파워포인트 판 TOOLS](../../powerpoint/docs/TOOLS.ko.md)

## 0. 한 턴에 모델이 보는 것

| 층 | 무엇 | 몇 개 | 어디서 오나 |
|---|---|---|---|
| 통합 문서 도구 | `mcp__xl__*` | **61** | 헬퍼 `tools.go` → MCP `tools/list` → 데몬이 이 대화에만 광고 |
| 코어 내장 도구 | `bash`, `read`, `edit`, `skill`, `todowrite` … | 코어 레지스트리 | 데몬 |
| 플러그인 도구 | `land` | 1 (카운슬 끈 대화) | `<config>/plugins/landing` |
| 가이드(스킬) | `sheet-design`, `formulas`, `charts-and-pivots` | 3 | 워크스페이스 `.magi/skills/*.md` — 헬퍼가 `excel/skills/` 에서 심는다 |
| 지속 지시 | `AGENTS.md` | 첫 기동 때 브리프 7단계를 심는다(`instructions.go`) | 워크스페이스 뿌리, `/api/instructions` 로 사람이 편집 |
| 첫 도구 설명의 머리말 | "A WORKBOOK IS ALREADY OPEN IN EXCEL…" | 1 | `list_sheets` 의 description 첫 줄 |

## 1. 통합 문서 도구 61 — 무리별

정확한 설명·인자·열거형은 `helper/tools.go`·`enums.go` 가 정본이고, 사람 말 요약은 매뉴얼 §6.1 이다. 여기는 무리와
요구 집합만.

| 무리 | 도구 | 요구 집합 |
|---|---|---|
| 목차·읽기 | `list_sheets` `describe_sheet` `read_range` `read_table` `read_chart` `read_names` `describe_style` | 1.7 (바닥) |
| 찾기 | `find` | 1.9 |
| 그림 | `render_range` `render_chart` | 1.7 (`Range.getImage`), 차트는 1.2 |
| 메모 | `read_comments` `add_comment` `resolve_comment` | 1.10, 해결은 1.11 |
| 유효성·피벗 | `read_validation` `set_validation` `add_pivot` `refresh_pivot` | 1.8 |
| 조건부 서식 | `read_conditional_formats` `add_conditional_format` `clear_conditional_formats` | 1.6 |
| 값·서식 | `write_range` `set_number_format` `format_range` `clear_range` `merge_cells` `unmerge_cells` `insert_cells` `delete_cells` `autofit` `set_hyperlink` | 1.7 |
| 시트 | `add_sheet` `delete_sheet` `rename_sheet` `move_sheet` `copy_sheet` `set_sheet_visibility` `activate_sheet` `freeze_panes` `protect_sheet` `unprotect_sheet` | 1.7 (복사·고정 창이 1.7) |
| 표 | `add_table` `set_table_cells` `add_table_rows` `remove_table` `sort_range` `filter_table` | 1.7, 자동 필터 1.9 |
| 차트 | `add_chart` `format_chart` `delete_chart` | 1.7 |
| 이름 | `set_name` `delete_name` | 1.4 |
| 그림 넣기 | `add_image` | 1.9 (`shapes.addImage`) |
| 되돌리기 | `snapshot_range` `restore_range` | 1.7 |
| 문서 안의 기억 | `read_tags` `set_tag` `read_suggestions` `suggest` `drop_suggestion` | 1.4 (`settings`) |
| 작업창 | `advise` `clear_advice` | — |

### 도구 하나가 답하는 모양

`{document, label, result, changed[], epoch, count}` — 파워포인트 판과 같은 봉투. `changed` 는 사람이 읽는 한국어 한
줄씩(「매출!B2:B6 표시 형식 → #,##0」), 카운슬이 「이 턴에 바뀐 것」으로 읽는 유일한 자리. 고친 뒤에는 `result.now.sheet`
가 실려 다음 호출이 어느 시트인지 되묻지 않는다. 그림은 헬퍼가 MCP 이미지 블록으로 바꿔 **한 번만** 싣는다.

## 2. 가이드 셋

| 이름 | 무엇 |
|---|---|
| `sheet-design` | 시트를 어떻게 짜나 — 머리글 한 줄, 표로 만들기, 표시 형식, 틀 고정, 열 너비, 색은 이 문서의 관행(`describe_style`) |
| `formulas` | 수식으로 쓰기(값을 박지 않기), 이름 정의, 절대/상대 참조, 오류 값 검사, 되읽어 확인 |
| `charts-and-pivots` | 차트 종류 고르기, 원본 범위는 머리글 포함, 피벗의 행·열·값, 그림으로 한 번 확인 |

## 3. 끝났다고 말하는 문 — 하나

파워포인트 판 §4 와 같다: 카운슬이 켜진 대화는 지침의 마지막 단계(검토)가 문이고, 끈 대화는 `land` 다. `land` 는
통합 문서를 건드리지 않는다.
