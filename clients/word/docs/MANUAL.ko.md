# magi Word 애드인 — 사용자 매뉴얼

[무엇을 어디서 재나](./TESTING.ko.md) · [구조](./ARCHITECTURE.ko.md) · [도구 하나하나](./TOOLS.ko.md) · [설치](./INSTALL.ko.md) · [헬퍼](../helper/README.md) · [애드인](../addin/README.md) · [엑셀 판](../../excel/docs/MANUAL.ko.md) · [파워포인트 판 설계](../../powerpoint/DESIGN.md)

> **이 문서가 무엇인가.** 구현된 기능 전부를 **쓰는 사람의 눈**으로 적는다. 왜 이렇게 설계했는지는 파워포인트 판의
> [`DESIGN.md`](../../powerpoint/DESIGN.md)가 말한다 — 워드 판은 엑셀 판을 그대로 가져와 손(도구)만 바꿨다.
>
> **실물 Word 에서 도구 44개가 돌았다(2026-09-06)** — Mac Word 의 실물 문서에 MCP 로 전부 불러 63호출 실패 0([`TESTING.ko.md`](./TESTING.ko.md) §5.1).
> 첫 판엔 엑셀 판처럼 지어낸 모양 넷이 잡혔다(언어별 스타일 이름·목록 상속·그림 위치·날짜형 기록). 작업창 단추(인용·제안 적용·검토)는
> **아직 사람이 안 눌렀다** — 그것은 §5.2 에 남아 있다.

---

## 1. 무엇을 하는 물건인가

Word 작업창 안에서 magi 컴패니언과 대화하고, **컴패니언이 열려 있는 문서를 직접 읽고 고친다** — 문단·스타일·글자 서식·
표·목록·그림·머리글/바닥글·찾아 바꾸기·메모·책갈피·변경 추적까지.

- 도구는 **MCP** 로 나간다(`mcp__word__*` 54개, §6). 컴패니언 쪽에는 새 계약이 없다.
- **문서를 만지는 손은 애드인이다.** 헬퍼도 데몬도 파일을 안 연다. 셸로 `.docx` 를 새로 쓰는 일은 **하지 않는다**.
- 파워포인트·엑셀 판과 다른 점 하나: **Word 는 페이지를 그림으로 못 준다.** 눈은 `read_html` 이다 — 굵기·크기·색·목록·표가
  HTML 로 온다.

```
Word 작업창(애드인)  ←https→  magi office(헬퍼, /word)  ←unix socket→  magi --daemon  →  모델
     └── 손: Word.js 로 문서를 고친다         └── MCP 서버: 도구 54개를 데몬에 붙인다
```

COM 손은 없다. Word 2019·2021·Microsoft 365 는 모두 `WordApi 1.3` 이상이라 작업창이 그대로 손이 된다. 2016 이하는 작업창이
뜨되 편집을 못 하고, 그렇게 말한다(§3.1). 메모·책갈피·변경 추적(1.4)·변경 검토(1.6)는 Microsoft 365·2024 에서만 되고,
2019·2021 에서는 그 도구가 **이름을 대고 거절한다**.

---

## 2. 설치 — 처음 한 번

### 2.1 필요한 것

| 무엇 | 왜 |
|---|---|
| Word 2019 이상 (Windows/Mac) 또는 Microsoft 365 | 요구 집합 `WordApi 1.3` + `SharedRuntime 1.1`. 메모·책갈피·변경 추적은 1.4, 변경 검토는 1.6 |
| Go 1.22+ | 헬퍼를 빌드한다 |
| `magi` | 데몬 |

### 2.2 헬퍼 빌드

```bash
go build -o magi ./cmd/magi        # 헬퍼는 magi 안에 있다 — `magi office` 가 파워포인트·엑셀·워드 셋을 한 프로세스로 띄운다
```

### 2.3 인증서 — 사람이 신뢰 저장소에 넣는다

첫 기동이 `<config>/office-helper-cert.pem` 을 만든다(세 프로그램 공용 — 하나만 넣는다). **그 인증서를 이 계정의 신뢰 저장소에 넣어야 한다** — 데몬도 그 인증서로
헬퍼의 MCP 문에 붙는다(엑셀 판에서 그것을 안 넣어 도구가 안 붙었다). 방법은 `./magi office -cert-hint` 가 찍는다.

### 2.4 애드인 사이드로드

매니페스트는 `clients/word/addin/manifest.xml` — `<SourceLocation>` 이 `https://127.0.0.1:3000/word/taskpane.html` 이다 — 헬퍼 하나가 3000 에서 `/ppt`·`/xl`·`/word` 세 판을 내준다.

- **macOS** — `~/Library/Containers/com.microsoft.Word/Data/Documents/wef/` 에 `manifest.xml` 을 복사하고 Word 를 완전히 끝냈다
  다시 연다. 홈 탭 「추가 기능 › 개발자 추가 기능 › Magi」를 한 번 누르면 리본 오른쪽 끝에 **Magi** 단추가 생긴다.
- **Windows** — 엑셀 판 [`INSTALL.ko.md`](../../excel/docs/INSTALL.ko.md) 와 같은 절차, 매니페스트만 이것. Word 2021 도 신뢰
  카탈로그 키가 하나여야 하는지는 아직 안 쟀다. 한 줄 설치기는 `clients/office/install.ps1`(§9).

### 2.5 데몬과 헬퍼 띄우기

```bash
magi --daemon
./magi office            # 127.0.0.1:3000 — /word 아래, 애드인 소스는 clients/word/addin
```

---

## 3. 작업창 읽는 법

엑셀 판과 같은 판이다 — 브랜드 줄, 대화, 안내 포스트잇, 제안 카드, 입력, `⋯` 판(컨텍스트 띠·프로바이더·모델·카운슬). 다른 것은
인용의 모양(§5)과 안내·제안이 가리키는 곳(문단 번호)뿐이다.

### 3.1 접힌 「지원 API」 줄

`WordApi 1.3 / 1.4 / 1.5 / 1.6 / 1.7 / 1.8 / 1.9`, `WordApiDesktop 1.1`, `SharedRuntime 1.1` 을 각각 잰다. 전부 ✓ 면 숨는다.
2019·2021 은 1.3 까지 ✓ 라 줄이 펴져 무엇이 없는지 적고, 1.4+ 가 필요한 도구가 부르면 이름을 대고 거절한다. `1.3` 이 없으면
이 창은 손이 아니다.

### 3.2 브랜드 줄 · 3.3 대화 줄 · 3.4~3.9

엑셀 판 [`MANUAL.ko.md`](../../excel/docs/MANUAL.ko.md) §3.2~§3.9 와 같다. 붙는 과정은 **「준비됐습니다 — 도구 54 개.」** 로
끝난다. 가이드는 워드 것 셋(`document-structure`·`editing`·`tables-and-review`).

---

## 4. 붙는 법

엑셀 판 §4 와 같다. 문서마다 대화가 따로 선다 — 문서 안의 사용자 지정 속성 `MAGI.DOC` 이 이름표다(2021 에도 있는 WordApi 1.3).

---

## 5. 말 보내기

**인용** 단추는 지금 잡은 문단들을 말 앞에 붙인다:

```
[인용] paragraphs=3-4
상반기 매출은 전년 동기 대비 12% 늘었고 …
```

- 문단 번호는 본문 순서(1부터)다. 선택을 본문에서 **글로 찾아** 번호를 매기므로 같은 글이 둘이면 `approx=true` 가 붙는다.
- 빈 문단도 인용이다 — 「(빈 문단)」. 못 읽었으면 「(글을 못 읽었습니다)」 — 다른 문장이다.
- **검토 부탁** 단추는 「문단 3–5 을 검토해 주세요」를 만들어 준다.

---

## 6. 무엇을 시킬 수 있나

### 6.1 도구 54개

**읽는 것 (16) — 안 물어보고 도는 무리**

| 도구 | 하는 일 |
|---|---|
| `list_paragraphs` | 목차 — 문단마다 번호·스타일·목록 단계·표 안인지·첫 80자. **제일 먼저 부른다.** 긴 문서는 from/to/max |
| `read_paragraphs` | from..to 의 전문과 서식(스타일·정렬·글꼴·간격) |
| `read_document` | 속성·문단/표/구역/그림 수·머리글·바닥글·변경 추적 모드·이 호스트의 WordApi |
| `find` | 글 찾기 — 문단 번호와 앞뒤 글 |
| `read_table` | 표 하나 — 칸 전부 |
| `read_html` | 구간을 HTML 로 — **눈이다.** Word 는 그림을 못 준다 |
| `read_comments` | 메모 스레드(1.4) |
| `list_images` | 본문의 그림 — 번호·문단·크기·대체 텍스트 |
| `read_footnotes` | 각주·미주 — 번호·문단·걸린 글·내용(1.5) |
| `read_tracked_changes` | 변경 내역(1.6) |
| `describe_style` | 쓰이는 스타일과 수, 본문·제목 글꼴, 제목 목록 |
| `snapshot_paragraphs` | 구간의 OOXML 을 찍어 둔다 — `restore_paragraphs` 의 재료 |
| `read_tags` | 문서에 남긴 기록(사용자 지정 속성 MAGI.*) |
| `read_suggestions` | 붙어 있는 제안(1.4) |
| `advise` | 작업창에 안내 포스트잇 — 문서는 안 고친다 |
| `clear_advice` | 포스트잇을 지운다 |

**문서를 고치는 것 (38) — 권한을 묻는 무리**

| 도구 | 하는 일 |
|---|---|
| `insert_paragraphs` | 문단 넣기 — lines 배열, 스타일, after/before/at. 새 번호를 답한다 |
| `replace_paragraph` · `delete_paragraphs` | 한 문단의 글 바꾸기(스타일은 남음) · from..to 지우기(마지막 하나는 못 지움) |
| `set_style` | 스타일 걸기 — 문서의 이름 또는 `builtin`(Heading1…) |
| `format_text` · `format_paragraph` | 글자 서식(굵게·기울임·밑줄·취소·크기·색·형광·글꼴, `text` 로 낱말만) · 문단 서식(정렬·간격·들여쓰기) |
| `insert_table` · `set_table_cells` · `add_table_rows` · `delete_table` · `format_table` | 표 넣기(2차원 배열, 머리글, 스타일 105종) · 칸(0-based) · 행 · 지우기 · 모양 |
| `insert_list` · `set_list` | 목록 넣기(글머리/번호, 단계) · 문단을 목록으로/목록에서 빼기 |
| `insert_image` | 그림 — 경로만 말하면 헬퍼가 바이트를 실어 준다 |
| `format_image` | 그림 크기(한쪽만 주면 비율 유지)·대체 텍스트·문단 정렬 |
| `delete_image` | 번호로 그림 하나 지우기 — 문단은 그대로 |
| `insert_break` | 쪽·구역·줄 나누기 |
| `format_table_cells` | 표 칸의 채우기·글자색·굵게·크기·가로세로 정렬·너비 — `cells` 목록이나 `rows`/`columns` 사각형(0부터) |
| `edit_table` | 표 모양 — 행·열 삭제, 열 추가(위→아래 값), 칸 병합(1.4) |
| `set_style_format` | 스타일 자체를 고친다 — 글꼴·크기·굵게·색·정렬·간격·들여쓰기; 그 스타일의 문단이 전부 바뀐다. `create` 로 새 스타일(1.5) |
| `insert_footnote` | 각주(기본)·미주 — `paragraph` 안 `text` 뒤 또는 문단 끝에(1.5) |
| `delete_footnote` | 번호로 각주·미주 하나 지우기 — 글은 그대로 |
| `insert_field` | 필드 — 목차·쪽 번호·전체 쪽수·날짜·시각·제목·작성자·파일 이름. `template: "{page} / {pages}"` 로 글과 섞어 바닥글에도(1.5) |
| `set_header_footer` | 구역의 머리글·바닥글 |
| `set_hyperlink` | 링크 달기·떼기(낱말만도) |
| `replace_all` | 찾아 바꾸기 — 몇 곳·어느 문단인지 답한다 |
| `add_comment` · `reply_comment` · `resolve_comment` | 메모(1.4) |
| `add_bookmark` · `delete_bookmark` | 책갈피(1.4) |
| `set_track_changes` · `review_changes` | 변경 추적 모드(1.4) · 수락/거부(1.6) |
| `set_properties` | 제목·주제·작성자·키워드 |
| `restore_paragraphs` | 스냅숏으로 되돌린다 — 그 사이 위쪽 번호가 밀렸으면 먼저 확인 |
| `set_tag` | 기록 남기기(255자까지 — Word 의 한계) |
| `suggest` · `drop_suggestion` | **수정 제안** — 고치지 않고 카드로. 누를 수 있는 손은 replace_paragraph·format_text·format_paragraph·set_style·replace_all·insert_paragraphs(1.4) |

`land` 는 이 표 밖이다 — 카운슬을 끈 대화에서 턴을 끝내는 문.

### 6.2 「어느 문단」 — 번호가 손잡이다

Word.js 에는 문단의 안정된 id 가 없다. `list_paragraphs` 가 준 번호가 손잡이이고, 끼워 넣거나 지우면 **아래 번호가 민다** —
고친 답에 `now.paragraphs`(문단 수)가 실리고, 넣기는 새 번호를 답한다. 다음 호출은 그 번호를 쓴다.

### 6.3 아직 안 되는 것

- **페이지 그림** — Word.js 가 안 준다. `read_html` 이 눈이다.
- **각주·미주·필드·콘텐츠 컨트롤·도형** — 손에 안 달았다.
- **저장** — Office.js 가 안 준다. 사람이 저장한다.

---

## 7. 권한

읽는 도구 16개는 안 묻고 돈다. 규칙은 코드가 만든다(`./magi office -allow-rules=word`) — 아래는 그것을 그대로 옮긴 것이다:

```toml
allow = [
  "mcp__word__advise(**)",
  "mcp__word__clear_advice(**)",
  "mcp__word__describe_style(**)",
  "mcp__word__find(**)",
  "mcp__word__list_images(**)",
  "mcp__word__list_paragraphs(**)",
  "mcp__word__read_comments(**)",
  "mcp__word__read_document(**)",
  "mcp__word__read_footnotes(**)",
  "mcp__word__read_html(**)",
  "mcp__word__read_paragraphs(**)",
  "mcp__word__read_suggestions(**)",
  "mcp__word__read_table(**)",
  "mcp__word__read_tags(**)",
  "mcp__word__read_tracked_changes(**)",
  "mcp__word__snapshot_paragraphs(**)",
]
```

---

## 8. 브라우저에서 열면 (목업 모드)

```bash
PORT=3010 node clients/word/addin/tools/serve.mjs     # http://localhost:3010/taskpane.html
```

Word 없이 작업창이 뜬다. 왼쪽에 **가짜 문서**(보고서 열한 문단과 표 하나)가 붙고, 손은 `FakeHand` — 54개가 메모리 문서 위에서
**정말로** 돈다.

---

## 9. 알아두면 좋은 동작 · 아직 아닌 것

- **실물 Word 에서 도구 44개는 돌았다**(2026-09-06, [`TESTING.ko.md`](./TESTING.ko.md) §5.1). 작업창 단추는 아직 사람이 안 눌렀다.
- **목록 항목 뒤에 넣은 문단은 Word 가 그 목록에 이어 붙인다** — `insert_paragraphs` 는 그것을 떼어 문단으로 두고, 항목이 필요하면 `insert_list`·`set_list`.
- **스타일 이름은 언어별이다** — 한국어 Word 의 「제목 1」은 `style:"Heading 1"` 로 못 찾는다. 내장 이름(`Heading1`, `Normal`, `ListParagraph`)은 언어와 무관하게 통하고, 문서 고유 스타일은 문서가 보여 주는 이름 그대로.
- **한 줄 설치 스크립트**는 `clients/office/install.ps1` — 세 프로그램 공용이고 Windows 에서 아직 안 돌렸다.
- **선택 → 문단 번호는 글로 찾는다** — 같은 문단이 둘이면 첫 것이고 `approx` 가 붙는다.
- **기록·제안의 값은 짧다** — 사용자 지정 속성은 255자. 제안은 settings(1.4)에 살아 2021 에는 없다.

---

## 10. 안 될 때

엑셀 판 §10 과 같다 — 하얀 창은 인증서, 「준비됐습니다」가 안 오면 데몬·헬퍼, 도구가 전부 「연결된 손이 없습니다」면 헬퍼 재기동
뒤 작업창이 스스로 되살아난다.

---

## 11. 이 문서와 시험의 관계

이름 대는 도구는 전부 카탈로그에 있어야 하고(`TestTheManualNamesEveryTool`), §7 의 규칙은 코드가 만드는 것과 글자까지 같아야
하며(`TestTheManualQuotesTheRulesWeGenerate`), 「도구 54개」「읽는 것 16」「고치는 것 38」「준비됐습니다 — 도구 54 개」는 수를
세는 시험이 문다(`TestTheDocsCountTheToolsWeAdvertise`).
