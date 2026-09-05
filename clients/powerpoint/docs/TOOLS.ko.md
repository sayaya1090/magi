# 모델이 받는 것 — 도구·가이드·지시

[매뉴얼](MANUAL.ko.md) · [아키텍처](ARCHITECTURE.ko.md) · [다이어그램](DIAGRAMS.ko.md) · [시험](TESTING.ko.md) · [↑ 설계](../DESIGN.md)

> **현행 참고 문서.** 2026-09-05 실물(`tools/list` 응답, 데몬 pid 23062, 헬퍼 09:56 빌드)에서 셌습니다. 표는 손으로 적은 것이 아니라 그 응답에서 뽑은 것이라, 도구를 더하거나 빼면 이 표부터 낡습니다 — 다시 뽑는 명령은 §6 에 있습니다.

## 0. 한 턴에 모델이 보는 것

| 층 | 무엇 | 몇 개 | 어디서 오나 |
|---|---|---|---|
| 덱 도구 | `mcp__ppt__*` | **48** | 헬퍼 `tools.go` → MCP `tools/list` → 데몬이 이 대화에만 광고(`port.Owned.VisibleTo`) |
| 코어 내장 도구 | `bash`, `read`, `edit`, `websearch`, `skill`, `todowrite` … | 레지스트리 26 이름(§3) | 데몬 `internal/adapter/tool/builtin` |
| 플러그인 도구 | `land` | 1 | `<config>/plugins/landing`(Lua) |
| 가이드(스킬) | `deck-design`, `design-guide`, `visual-deck`, `academic-deck`, `research` | 5 (17,483자) | 워크스페이스 `.magi/skills/*.md` — `skill` 도구로 읽음 |
| 지속 지시 | `AGENTS.md` | 지금은 빈 파일 | 워크스페이스 뿌리, `/api/instructions` 로 사람이 편집 |
| 첫 도구 설명의 머리말 | "A DECK IS ALREADY OPEN IN POWERPOINT…" | 1 | `list_slides` 의 description 첫 줄 |

**정확히 어느 코어 도구가 이 대화에 광고되는지는 아직 실물로 못 셉니다.** 데몬에 「이 대화가 보는 도구 목록」을 답하는 문이 없습니다(DESIGN §5.9.6 5번 `mcp-list`, ⏳). 위 26 은 레지스트리에 있는 이름이고, 승인기 로그에서 실제로 불린 것은 `skill`·`todowrite`·`read`·`list`·`glob`·`grep`·`websearch`·`webfetch`·`land` 아홉입니다.

## 1. 덱 도구 48 — 무리별

인자 수는 `document`(어느 덱인지, 모든 도구가 받음)를 뺀 것입니다. 필수도 마찬가지입니다.
### 읽기

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `list_slides` | A DECK IS ALREADY OPEN IN POWERPOINT AND THESE TOOLS ARE ATTACHED TO IT. You do not create, open or upload a deck, and there is no tool t… | 2 | — |
| `read_slide` | One slide, described the way PowerPoint models it: placeholders by role (title, body, …) with their text, and non-placeholder shapes with… | 2 | — |
| `find_shapes` | Shapes across the whole deck matching a filter — the way into a bulk change ("every shape whose font is X"). Returns identity and the mat… | 8 | — |
| `list_layouts` | The layouts this deck offers, by master, with the placeholder roles each one carries. Read this before add_slide: layout names come from … | 0 | — |
| `describe_style` | What this deck actually looks like: the font, size and colour its titles and bodies consistently use, and how many placeholders that was … | 0 | — |
| `read_notes` | The speaker notes on one slide. read_slide cannot see these — notes live outside the object model — so this is the only way to know what … | 2 | — |
| `read_theme_colors` | The twelve theme colours of a layer as they are now: dark1/dark2, light1/light2, accent1-6, hyperlink, followedHyperlink. Read this befor… | 3 | — |
| `read_tags` | Notes you left on a slide or shape earlier, stored INSIDE the deck. This is your memory between conversations: shape ids are bare numbers… | 3 | — |
| `read_animation` | What already animates on one slide, and in what order. read_slide cannot see this. Read it before animate_slide on a deck you did not bui… | 2 | — |
| `read_suggestions` | Fix-suggestions sitting in this deck, from you or from an earlier conversation. They live in the FILE, so they survive the deck being clo… | 2 | — |
| `export_slide_ooxml` | The slide's own OOXML, narrowed to a part — for what the object model stays silent about (chart data, speaker notes, animation). Reading … | 4 | — |

### 그림으로 확인

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `render_slide` | A PNG of one slide as PowerPoint draws it. **The most expensive tool here** — one picture costs what thousands of characters cost, and on… | 4 | — |
| `render_shape` | A picture of ONE shape. render_slide is the most expensive tool here and most checks are about a single chart or table — drawing the whol… | 4 | `shape_id` |

### 슬라이드

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `add_slide` | Add a slide — and, in the same call, put it where it belongs and fill its title and body. Filling a layout's placeholders is not the same… | 5 | — |
| `add_slides` | Build several slides in one call from an outline — the right tool when someone hands you a plan for a deck. One permission prompt instead… | 2 | `slides` |
| `duplicate_slide` | Copy one slide, formatting and all, and put the copy right after it. The way to build a deck that looks consistent: make one slide well, … | 2 | — |
| `delete_slide` | Remove one slide. Every slide after it shifts down one, and the result says so. Snapshot first if the person might want it back — nothing… | 2 | — |
| `reorder_slide` | Move a slide to another position. Every position after the move is a different slide than it was, so re-read before addressing anything b… | 3 | `to` |
| `apply_layout` | Put one slide on a different layout. Layout names come from list_slides — they are the deck's own vocabulary. This CHOOSES a layout; it n… | 3 | `layout` |
| `set_background` | Paint one slide's background a solid colour — the theme decides it otherwise, and nothing here could change it before. The slide KEEPS IT… | 10 | — |
| `snapshot_slide` | Take a snapshot of one slide before changing it — the .pptx bytes of that slide, kept by the helper. It reads the deck and changes nothin… | 2 | — |
| `restore_slide` | Put a snapshot back. The restored slide is INSERTED and the original removed, so it comes back with a NEW id: the result carries that id,… | 3 | `snapshot` |

### 글과 서식

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `set_text` | Replace the text of one shape. The result carries the old text and the new one, because that pair is the only record of the change that r… | 5 | `text` |
| `format_text` | Format PART of a shape's text — one word, a number, a phrase — without touching the rest: bold, colour, size, underline, or a hyperlink o… | 18 | `shape_id` |
| `format_shape` | Change the look of one shape: font family, size, bold, italic, colour, alignment, fill. | 32 | `shape_id` |
| `apply_style` | Restyle text across many slides in one call — titles, bodies, or with `all` every shape that holds text. "Make every title blue". Placeho… | 6 | — |
| `set_notes` | Write the speaker notes on one slide, replacing whatever was there. Newlines become paragraphs. Read them first with read_notes unless yo… | 4 | `text` |
| `set_hyperlink` | Set or clear the hyperlink on one shape. ⚠ Needs PowerPointApi 1.10: 1.6 gave the hyperlink COLLECTION, which only reads — setting one ar… | 5 | `shape_id`, `url` |

### 도형·배치

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `add_shape` | Add a shape to a slide — a text box or a geometric shape — at a position given in points. Shapes carry text: pass text and it goes inside… | 25 | — |
| `move_shape` | Move or resize ONE shape. Units are points, the same units read_slide reports. To line SEVERAL shapes up or space them out, call align_sh… | 8 | `shape_id` |
| `align_shapes` | Line shapes up or space them evenly — "centre these", "even out the gaps". Without it the coordinates have to be worked out by hand and m… | 4 | `how` |
| `group_shapes` | Group several shapes on one slide into ONE shape that moves and resizes together — a diagram of boxes and arrows, a KPI tile. The result … | 3 | `shape_ids` |
| `ungroup_shapes` | Take a group apart; its members become ordinary shapes again with their own ids (read_slide to see them). Needs PowerPointApi 1.8. | 3 | `shape_id` |
| `delete_shape` | Delete one shape. There is no undo for a delete: the tag journal cannot restore what it was written on, so the snapshot is the only way b… | 3 | `shape_id` |
| `add_image` | Put a picture from the person's own computer on a slide. Give the FILE PATH — never base64: the helper reads the file itself, so the byte… | 10 | `path` |

### 표·차트

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `add_table` | Add a NEW table. If the slide already has one and the person is asking to change it, this is the wrong tool: write cells with set_table_c… | 22 | `rows`, `columns` |
| `replace_table` | Rebuild an existing table in place: the old one is removed and a new one is created at the same position and size, with the rows, columns… | 23 | — |
| `edit_table` | Change the STRUCTURE or style of an existing table without rebuilding it: add or delete rows and columns, merge cells, set column widths … | 17 | `shape_id` |
| `set_table_cells` | Write text into cells of an existing table — the first thing to reach for when someone wants a table changed. Text only: an existing tabl… | 4 | `shape_id`, `cells` |
| `format_table_cells` | Restyle cells of a table that already exists — fill, text colour, size, bold, italic, alignment — WITHOUT rebuilding it, so the table KEE… | 15 | `shape_id` |
| `add_chart` | Put a NATIVE PowerPoint chart on a slide — a real chart object the person can restyle, not a picture and not a pile of rectangles. Give i… | 11 | `categories`, `series` |

### 테마·애니메이션

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `set_theme_colors` | Change the deck's theme colours by name. This repaints everything that inherits them — placeholders, chart series, table styles — which i… | 4 | `colors` |
| `animate_slide` | Make things appear one at a time on a slide — what people mean by "애니메이션 넣어 줘" and "한 줄씩 나타나게". Doing it by hand in PowerPoint is fiddly … | 3 | `steps` |

### 덱 안의 기억

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `set_tag` | Leave a note on a slide or shape that stays in the FILE. Invisible to anyone looking at the deck — it is not speaker notes and never show… | 5 | `key` |
| `suggest` | Attach a fix-suggestion to a slide or shape, the way a person leaves a comment in Word. It is written INTO THE DECK, so it is still there… | 6 | `what` |
| `drop_suggestion` | Take one suggestion off, without doing what it asked. The person pressing Apply in the pane already removes it; use this when the suggest… | 4 | `key` |

### 작업창

| 도구 | 한 줄 | 인자 | 필수 |
|---|---|---|---|
| `advise` | Pin advice in the task pane without touching the deck: what to change and why, optionally pointing at a slide and shapes. Clicking an ite… | 1 | `items` |
| `clear_advice` | Take the pinned advice down. Nothing to undo: it was never in the deck. | 0 | — |

### 도구 하나가 답하는 모양

- 읽기는 PowerPoint 가 모델링하는 대로 답합니다 — 자리표시자는 역할(title/body)로, 좌표는 포인트로.
- **변이는 바뀐 뒤의 객체를 `now` 에 싣습니다.** 다시 읽는 왕복을 없애려는 것입니다. 실측: 그 전엔 `set_text` 뒤 `read_slide` 가 거의 매번 따라왔습니다.
- 값이 틀리면 **서버가 이름을 대고 거절**합니다: 모르는 키(`args.go`), 1 미만의 위치, 별칭과 정본 동시 전송, 그리고 글머리 기호 열거형 밖의 이름(`bullets.go`). 호스트까지 가서 `InvalidArgument` 한 단어로 죽는 것보다 쌉니다.
- 호스트가 거절하면 결과에 `code — message — errorLocation` 이 실립니다(`ServeHand.officeWhy`). 어느 속성이 거절됐는지가 모델에게 갑니다.

## 2. 가이드 다섯

| 이름 | 글자 | 언제 읽나(설명 첫 줄) |
|---|---|---|
| `deck-design` | 4,264 | 항상 먼저 — 이 애드인이 실제로 할 수 있는 것과 없는 것 |
| `design-guide` | 3,767 | 기본값이자 사내 표준(임원 보고·실적·제안서). 색과 크기의 정본 |
| `visual-deck` | 3,330 | 인상이 목적인 덱의 시각 스타일 다섯(대외 발표·데모데이) |
| `academic-deck` | 5,262 | 학술·연구 발표의 논증 구조. `design-guide` 와 함께 |
| `research` | 860 | 발표자료에 쓸 숫자를 웹에서 찾는 요령 |

가이드는 `skill` 도구로 **읽어야** 들어옵니다. 시스템 프롬프트에 박히지 않습니다 — 그래서 모델이 안 읽으면 없는 것과 같고, `deck-design` 의 "항상 먼저 읽는다" 는 설명문이지 강제가 아닙니다.

## 3. 코어 내장 도구 26

`ask_user` `bash` `bash_input` `bash_kill` `bash_output` `council` `edit` `glob` `grep` `label` `list` `multiedit` `port_owner` `read` `recall_context` `recall_memory` `remember` `route_interjection` `schedule` `search_sessions` `skill` `todowrite` `wait_for` `webfetch` `websearch` `write` (레지스트리 `internal/adapter/tool/builtin`, 2026-09-05 — `Name()` 메서드와 `registry.go` 의 맵 두 양식을 합친 수).

이 컴패니언의 작업 디렉터리는 `~/Library/Application Support/magi/powerpoint` 입니다. `bash`·`edit`·`write` 가 닿는 곳이 거기라, 덱 파일이 아니라 **워크스페이스**를 고칩니다. 덱은 도구 44 로만 닿습니다.

## 4. `land` — 끝났다고 말하는 법

`land{did, verified, left}`. 빈 선언·계획만 있는 선언·읽기만 한 턴의 선언은 거절합니다. 세는 것이지 막는 것이 아닙니다 — 도구 없이 말만 하고 끝나는 턴을 이것으로 없애지는 못했습니다(세 모델 공통, 미해결).

## 5. 검토할 것 — 이 목록을 보고 든 생각

정리 후보입니다. 결정은 안 했습니다.

1. **글을 건드리는 길이 셋**: `set_text`(한 도형), `format_shape`(한 도형의 서식, 인자 20), `apply_style`(여러 장의 역할별 서식). 셋 다 `bullet`·`bullet_type`·`bullet_style` 을 받고, 이번에 `bullets.go` 로 검사를 한 자리로 모았습니다. 설명도 한 자리를 가리키게 할지.
2. **표 도구 넷**: `add_table`·`replace_table`·`set_table_cells`·`format_table_cells`. `replace_table` 은 "있는 표를 바꿔 달라는데 `add_table` 을 부르는" 오용을 막으려고 생겼습니다. 오용이 실측된 뒤의 결정인지 확인할 것.
3. **덱 안 메모가 두 벌**: `set_tag`/`read_tags`(모델의 기억)와 `suggest`/`drop_suggestion`/`read_suggestions`(사람이 Apply 하는 제안). 여기에 작업창 쪽 `advise`/`clear_advice` 까지 셋입니다. 사람에게 보이는 것(제안·조언)과 안 보이는 것(태그)의 경계는 분명하지만, 제안과 조언은 겹칩니다.
4. **`list_slides` 설명이 지시문을 겸합니다.** "A DECK IS ALREADY OPEN … You do not create" 는 도구 설명이 아니라 상황 설명입니다. 그 문장이 거기 있는 이유는 시스템 프롬프트에 넣을 자리가 없어서였고(§0 표), `AGENTS.md` 가 비어 있는 것과 같은 문제입니다.
5. **광고되지만 못 세는 코어 도구**(§0). `mcp-list` 문이 없어서 「이 대화가 보는 목록」을 실물로 못 잽니다. 데몬 문 하나가 이 표의 나머지 절반을 사실로 만듭니다.
6. **`bullet_style` 은 번호 매김뿐입니다.** 점·대시·체크 같은 기호 글머리는 Office.js 에 문이 없습니다(OOXML `a:buChar` 를 직접 써야 합니다). "커스텀 불릿" 요청은 지금 도구로는 못 합니다 — 넣으려면 `ea_font` 처럼 OOXML 재작성 경로입니다.

## 6. 다시 뽑기

```sh
TOK=$(curl -sk https://127.0.0.1:3000/taskpane.html | grep -oE 'token[^a-zA-Z0-9]{1,6}[A-Za-z0-9_-]{16,}' | sed 's/.*[^A-Za-z0-9_-]//')
curl -sk -H "Authorization: Bearer $TOK" -H 'content-type: application/json' \
  -X POST https://127.0.0.1:3000/mcp -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | python3 -c '
import json,sys
for t in json.load(sys.stdin)["result"]["tools"]: print(t["name"], len(t["inputSchema"]["properties"]))'
```
