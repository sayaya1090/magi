# magi Word 클라이언트 — 아키텍처(지어진 대로)

[매뉴얼](MANUAL.ko.md) · [모델이 받는 것](TOOLS.ko.md) · [시험](TESTING.ko.md) · [설치](INSTALL.ko.md) · [엑셀 판 아키텍처](../../excel/docs/ARCHITECTURE.ko.md)

> 엑셀 판을 **복사해 손만 바꾼** 구조다. 여기는 워드 판이 다른 자리만 적는다. 2026-09-06 기준.

## 0. 한 문장

Word 작업창이 헬퍼 `magi-word`(3002)에 붙고, 헬퍼가 데몬에 문서마다 대화 하나를 열어 도구 44개(MCP 서버 `word`)를 단다.

## 1. 프로세스 넷 — 그리고 넷뿐

엑셀 판과 같다. COM 손은 없다 — Word 2019 부터 `WordApi 1.3` 이라 작업창이 손이다.

## 2. 헬퍼 — 엑셀 판과 다른 자리

| 자리 | 엑셀 | 워드 |
|---|---|---|
| 이름(`names.go`) | `xl`, 3001, `xl-helper-cert`, `magi-xl`, 워크스페이스 `excel` | `word`, **3002**, `word-helper-cert`, `magi-word`, 워크스페이스 `word` |
| 문서 키 | `wb-` | `wd-`(문서의 사용자 지정 속성 `MAGI.DOC`) |
| 스트림 쿼리 | `?workbook=` | `?doc=` |
| 도구(`tools.go`) | 61 | 44 — 문단은 `from/to`·`paragraph`(1부터), 표는 번호 |
| 열거형(`enums.go`) | 차트·표 스타일 60 | 정렬·밑줄·형광·목록·구분·머리글·추적 모드·내장 스타일 28·표 스타일 105 |
| 인자 검사(`args.go`) | `rows` 목록 예외 | `from`·`limit` 은 1부터; 엑셀의 `move_sheet{to}` 규칙은 없다 |
| 스킬 | 3벌 | 3벌(`word/skills/`: `document-structure`·`editing`·`tables-and-review`) |
| 그림(`image.go`) | render 결과를 이미지 블록으로 | 같은 기전이지만 **부르는 도구가 없다** — Word 는 그림을 못 준다. `insert_image` 의 파일 읽기만 쓴다 |

나머지는 이름만 바꾼 복사다 — **빚이다**(엑셀 판과 같은 말). 헬퍼 셋(ppt·xl·word)을 공용 패키지로 가르는 것이 셋째 판 뒤의 일.

## 3. 작업창 — 네 층, 워드 고유 파일

| 층 | 엑셀 | 워드 |
|---|---|---|
| `port/` | `WorkbookPort` | `DocumentPort` — `selection()`(문단 범위+글), `point(paragraph)`, `paragraphCount()`, `capabilities()` |
| `adapter/` | `OfficeWorkbook`·`ExcelHand`·`a1` | `OfficeDocument`(선택을 본문에서 글로 찾아 번호 매김, `locate`)·`WordHand`·`FakeDocument`·`FakeHand`·`handCore`(`span`)·`pickDoc` |
| `domain/` | `Quote`(시트·주소) | `Quote`(from·to·글), `Advice`(paragraph)·`ParagraphIndex`(문단 수를 물은 세대), `Suggestion`(누를 수 있는 손 여섯) |
| `usecase/` | `HandRole`(1.7) | `HandRole` 바닥 `WordApi 1.3` |
| `ui/` | `bookFixture`·격자 | `docFixture`(보고서 열한 문단·표 하나)·`fakeCanvas`(문단 목록) |

`WordHand` 는 매 호출 본문 문단 목록을 새로 읽는다 — 번호는 저장하지 않는다. 고친 답에 `now.paragraphs`.

## 4~6.

엑셀 판과 같다. 문서의 안정된 이름은 settings 가 아니라 사용자 지정 속성(1.3)에 — 2021 워드에 settings(1.4)가 없어서.

## 7. 알려진 틈

- 실물 Word 에서 도구 44개는 돌았다(2026-09-06, TESTING §5.1) — 작업창 단추는 아직 사람이 안 눌렀다. 목록 항목 뒤에 넣은 문단이 목록을 물려받는 것, 내장 스타일 이름이 언어별인 것은 손이 안다.
- 헬퍼가 복사다. Windows 한 줄 설치가 없다.
- 각주·미주·필드·콘텐츠 컨트롤·도형은 손에 없다. 제안은 2021(1.3)에 없다.
