# magi Word 애드인 — 사용자 매뉴얼

[무엇을 어디서 재나](./TESTING.ko.md) · [구조](./ARCHITECTURE.ko.md) · [도구 하나하나](./TOOLS.ko.md) · [설치](./INSTALL.ko.md) · [헬퍼](../helper/README.md) · [애드인](../addin/README.md) · [엑셀 판](../../excel/docs/MANUAL.ko.md) · [파워포인트 판 설계](../../powerpoint/DESIGN.md)

> **문서 개요:** 본 문서는 사용자 관점에서 magi Word 애드인의 전체 기능(목적, 사용 방법, 화면 구성)을 설명합니다. 아키텍처 및 세부 설계 배경은 [`DESIGN.md`](../../powerpoint/DESIGN.md)를 참조하십시오.
>
> **검증 현황:** 핵심 도구 44종은 실제 Word 환경(macOS Word, 2026-09-06 기준)에서 MCP 호출을 통해 정상 동작(총 63회 호출 실패 0건)을 검증했습니다([`TESTING.ko.md`](./TESTING.ko.md) §5.1). 초기 실측 시 식별된 4가지 호스트 편차(언어별 스타일 명칭, 목록 상속 구조, 이미지 삽입 위치, 날짜형 속성 기록)에 대한 보정이 반영되어 있습니다. 작업창 UI 조작(인용 카드·제안 적용·검토)의 수동 테스트 현황은 §5.2를 참조하십시오.

---

## 1. 무엇을 하는 물건인가

Word 작업창 안에서 magi 컴패니언과 대화하고, **컴패니언이 열려 있는 문서를 직접 읽고 수정합니다** — 문단·스타일·글자 서식·표·목록·그림·머리글/바닥글·찾아 바꾸기·메모·책갈피·변경 추적까지.

- 도구는 **MCP(Model Context Protocol)** 인터페이스로 제공됩니다(`mcp__word__*` 66종, §6). 데몬 및 타 클라이언트와의 통신 규약은 완전히 동일하며, 기존 대화 흐름이 그대로 유지됩니다.
- **문서 조작은 Office.js 애드인이 직접 수행합니다.** 헬퍼나 데몬이 디스크 파일을 직접 열지 않으며, 셸 명령으로 새 `.docx` 파일을 임의 생성하여 덮어쓰지 않습니다.
- PowerPoint·Excel 연동과의 차이점: **Word는 페이지 단위 이미지 렌더링을 직접 제공하지 않습니다.** 따라서 시각적 판독에는 서식(굵기·크기·색상) 및 구조(목록·표)를 온전히 전달하는 `read_html` 도구를 사용합니다.

```
Word 작업창(애드인)  ←https→  magi office(헬퍼, /word)  ←unix socket→  magi --daemon  →  모델
     └── 조작 어댑터: Word.js로 문서 수정     └── MCP 서버: 도구 66개를 데몬에 붙인다
```

PowerPoint와 달리 별도의 **COM 어댑터가 필요하지 않습니다.** Word 2019, 2021, Microsoft 365는 모두 `WordApi 1.3` 이상을 지원하므로 웹 작업창 애드인이 직접 문서를 수정합니다. 2016 이하 버전에서는 작업창이 뜨더라도 편집이 제한되며 안내 문구가 표시됩니다(§3.1). 메모·책갈피·변경 추적(`WordApi 1.4`), 변경 검토(`WordApi 1.6`) API는 Microsoft 365 및 Word 2024에서만 지원되며, 2019 및 2021 버전에서는 해당 도구 호출 시 미지원 오류를 명시적으로 반환합니다.

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

`WordApi 1.3 / 1.4 / 1.5 / 1.6 / 1.7 / 1.8 / 1.9`, `WordApiDesktop 1.1`, `SharedRuntime 1.1` 요구 사항을 개별 검사합니다. 모든 항목을 만족하면 줄이 자동으로 숨겨집니다.
2019·2021 버전은 1.3까지만 지원하므로 미지원 항목을 펼쳐 표시하고, 1.4 이상이 필요한 도구 호출 시 미지원 오류를 명시적으로 반환합니다. `WordApi 1.3`을 지원하지 않는 환경에서는 편집 기능이 제공되지 않습니다.

### 3.2 브랜드 줄 · 3.3 대화 줄 · 3.4~3.9

엑셀 판 [`MANUAL.ko.md`](../../excel/docs/MANUAL.ko.md) §3.2~§3.9 와 같다. 붙는 과정은 **「준비됐습니다 — 도구 66 개.」** 로
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

### 6.1 도구 66개

**읽는 것 (19) — 안 물어보고 도는 무리**

| 도구 | 하는 일 |
|---|---|
| `list_paragraphs` | 목차 — 문단마다 번호·스타일·목록 단계·표 안인지·첫 80자. **제일 먼저 부른다.** 긴 문서는 from/to/max |
| `read_paragraphs` | from..to 의 전문과 서식(스타일·정렬·글꼴·간격) |
| `read_document` | 속성·문단/표/구역/그림 수·머리글·바닥글·변경 추적 모드·이 호스트의 WordApi |
| `find` | 글 찾기 — 문단 번호와 앞뒤 글 |
| `read_table` | 표 하나 — 칸 전부 |
| `read_html` | 구간을 HTML 로 — **눈이다.** Word 는 그림을 못 준다 |
| `read_comments` | 메모 스레드(1.4) |
| `render_page` | 한 쪽을 그림으로 — Word 가 문서 전체를 PDF 로 내주고 헬퍼가 그 쪽을 PNG 로(이 머신에 pdftoppm 이 있어야; Mac 은 없으면 sips 로 첫 쪽만) |
| `list_images` | 본문의 그림 — 번호·문단·크기·대체 텍스트 |
| `read_content_controls` | 콘텐츠 컨트롤 — id·태그·제목·글·자리 글·잠금·문단 |
| `list_shapes` | 떠 있는 도형·글 상자 — id·이름·종류·자리·크기·글(WordApiDesktop 1.2) |
| `read_footnotes` | 각주·미주 — 번호·문단·걸린 글·내용(1.5) |
| `read_tracked_changes` | 변경 내역(1.6) |
| `describe_style` | 쓰이는 스타일과 수, 본문·제목 글꼴, 제목 목록 |
| `snapshot_paragraphs` | 구간의 OOXML 을 찍어 둔다 — `restore_paragraphs` 의 재료 |
| `read_tags` | 문서에 남긴 기록(사용자 지정 속성 MAGI.*) |
| `read_suggestions` | 붙어 있는 제안(1.4) |
| `advise` | 작업창에 안내 포스트잇 — 문서는 안 고친다 |
| `clear_advice` | 포스트잇을 지운다 |

**문서를 고치는 것 (47) — 권한을 묻는 무리**

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
| `insert_file` | 다른 .docx 를 이 문서에(경로만 — 헬퍼가 읽고 Word 문서가 아니면 거절) |
| `move_paragraphs` | 문단 덩어리를 다른 자리로(서식·표 같이) — 새 번호를 답한다 |
| `set_page_setup` | 구역의 쪽 설정 — 방향·용지·여백·머리글/바닥글 거리·첫 쪽 따로(WordApiDesktop 1.1) |
| `insert_content_control` · `set_content_control` · `delete_content_control` | 문단(또는 그 안의 글)을 태그 단 서식 슬롯으로 · 태그/id 로 채우기·잠그기 · 떼기(글은 남김) |
| `insert_shape` · `format_shape` · `delete_shape` | 글 상자·기하 도형(사각형·타원·화살표·별…) 넣기(자리·크기·채우기·선·글) · id/이름으로 고치기 · 지우기(WordApiDesktop 1.2) |
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
| `suggest` · `drop_suggestion` | **수정 제안** — 즉시 수정하지 않고 카드로 제시. 실행 가능한 도구는 replace_paragraph·format_text·format_paragraph·set_style·replace_all·insert_paragraphs(1.4) 6종 |

`land` 도구는 이 표에 포함되지 않습니다 — 카운슬이 비활성화된 대화에서 턴을 종료할 때 사용하는 선언 도구입니다.

### 6.2 문단 식별 방식 — 인덱스 번호 기반 식별

Word.js API에는 문단에 부여되는 불변 고유 ID가 없습니다. `list_paragraphs`가 반환하는 문단 인덱스 번호가 식별자 역할을 수행하며, 문단을 삽입하거나 삭제하면 **이후 문단 번호가 순차적으로 재계산**됩니다. 도구 실행 결과로 `now.paragraphs`(총 문단 수)가 반환되며, 삽입 시 새로 할당된 인덱스 번호가 전달되므로 후속 호출에서는 이를 참조합니다.

### 6.3 아직 안 되는 것

- **페이지 이미지 렌더링** — Word.js 자체에는 페이지 이미지 렌더링 API가 없으며, `render_page` 도구가 PDF(`getFileAsync`)를 추출하여 헬퍼에서 이미지로 변환합니다. 실행 환경에 poppler `pdftoppm` 유틸리티가 설치되지 않은 경우 macOS에서는 1페이지만 변환되고 Windows에서는 변환이 거절됩니다(설치 가이드 안내 반환). 일반적인 시각 서식 및 구조 확인은 `read_html` 도구를 기본 권장합니다.
- **도형 조작 제약** — `WordApiDesktop 1.2` 요구 사항으로 인해 Microsoft 365 데스크톱 버전에서만 지원되며, 2019 및 2021 버전에서는 미지원 오류를 반환합니다. 그룹 및 캔버스 형태는 지원하지 않습니다.
- **문서 저장** — Office.js API 제약으로 인해 사용자가 직접 수동 저장해야 합니다.

---

## 7. 권한

조회(읽기) 도구 19종은 사용자 확인 없이 즉시 실행됩니다. 규칙 목록은 `./magi office -allow-rules=word` 명령으로 생성되며, 아래는 해당 설정 내용입니다:

```toml
allow = [
  "mcp__word__advise(**)",
  "mcp__word__clear_advice(**)",
  "mcp__word__describe_style(**)",
  "mcp__word__find(**)",
  "mcp__word__list_images(**)",
  "mcp__word__list_paragraphs(**)",
  "mcp__word__list_shapes(**)",
  "mcp__word__read_comments(**)",
  "mcp__word__read_content_controls(**)",
  "mcp__word__read_document(**)",
  "mcp__word__read_footnotes(**)",
  "mcp__word__read_html(**)",
  "mcp__word__read_paragraphs(**)",
  "mcp__word__read_suggestions(**)",
  "mcp__word__read_table(**)",
  "mcp__word__read_tags(**)",
  "mcp__word__read_tracked_changes(**)",
  "mcp__word__render_page(**)",
  "mcp__word__snapshot_paragraphs(**)",
]
```

---

## 8. 브라우저에서 열면 (목업 모드)

```bash
PORT=3010 node clients/word/addin/tools/serve.mjs     # http://localhost:3010/taskpane.html
```

Word 없이 작업창이 뜬다. 왼쪽에 **가짜 문서**(보고서 열한 문단과 표 하나)가 붙고, 손은 `FakeHand` — 66개가 메모리 문서 위에서
**정말로** 돈다.

---

## 9. 알아두면 좋은 동작 · 아직 아닌 것

- **실물 Word 에서 도구 44개는 돌았다**(2026-09-06, [`TESTING.ko.md`](./TESTING.ko.md) §5.1). 작업창 단추는 아직 사람이 안 눌렀다.
- **목록 항목 뒤에 넣은 문단은 Word 가 그 목록에 이어 붙인다** — `insert_paragraphs` 는 그것을 떼어 문단으로 두고, 항목이 필요하면 `insert_list`·`set_list`.
- **스타일 이름은 언어별이다** — 한국어 Word 의 「제목 1」은 `style:"Heading 1"` 로 못 찾는다. 내장 이름(`Heading1`, `Normal`, `ListParagraph`)은 언어와 무관하게 통하고, 문서 고유 스타일은 문서가 보여 주는 이름 그대로.
- **한 줄 설치 스크립트**는 `clients/office/install.ps1` — 세 프로그램 공용. Windows 2021 에서 끝까지 돌았다(2026-09-06, 파워포인트 판 TESTING §5.5) — 다만 그날 Word 애드인은 Word 로 열어 보지 않았다.
- **선택 → 문단 번호는 글로 찾는다** — 같은 문단이 둘이면 첫 것이고 `approx` 가 붙는다.
- **기록·제안의 값은 짧다** — 사용자 지정 속성은 255자. 제안은 settings(1.4)에 살아 2021 에는 없다.

---

## 10. 안 될 때

엑셀 판 §10 과 같다 — 하얀 창은 인증서, 「준비됐습니다」가 안 오면 데몬·헬퍼, 도구가 전부 「연결된 손이 없습니다」면 헬퍼 재기동
뒤 작업창이 스스로 되살아난다.

---

## 11. 이 문서와 시험의 관계

이름 대는 도구는 전부 카탈로그에 있어야 하고(`TestTheManualNamesEveryTool`), §7 의 규칙은 코드가 만드는 것과 글자까지 같아야
하며(`TestTheManualQuotesTheRulesWeGenerate`), 「도구 66개」「읽는 것 19」「고치는 것 47」「준비됐습니다 — 도구 66 개」는 수를
세는 시험이 문다(`TestTheDocsCountTheToolsWeAdvertise`).
