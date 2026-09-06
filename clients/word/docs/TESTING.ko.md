# 무엇을 어디서 재나 — Word 판

[↑ 매뉴얼](./MANUAL.ko.md) · [구조](./ARCHITECTURE.ko.md) · [엑셀 판 TESTING](../../excel/docs/TESTING.ko.md)

## 0. 한 줄로

```bash
go test ./clients/office/helper/                     # 헬퍼(세 판 공용): 계약·유도 가드·문서 대조
node clients/word/addin/tools/smoke.mjs              # 작업창: 화면 규칙·인용·안내·제안·가짜 손 66개
node clients/word/addin/tools/smoke-hand.mjs         # 손 노릇: 스트림 → 손 → 답, 역할(손/화면), 헬퍼 어댑터
node clients/word/addin/tools/wordhand.mjs           # 진짜 손(WordHand)을 가짜 Word.js 위에서 66개 전부
TOKEN=… node clients/word/addin/tools/livehand.mjs   # 가짜 손을 살아 있는 헬퍼에 붙인다
node clients/word/addin/tools/sweep.mjs [--docx x.docx]  # 실물: 붙은 문서에 66개 전부 — 끝에 붙인 문단 아래에서만 놀고 지운다(§5.3)
```

2026-09-06: 헬퍼 전부 통과, smoke 356 ok, smoke-hand 71 ok, wordhand 44/44.

## 1~4. 층 넷

엑셀 판과 같다. 워드에서 새로 잰 것: 문단 범위 산수(`span` — from 만이면 그 문단, to 는 문단 수에서 자름, 목차는 from 만 주면
끝까지), 선택을 글로 찾아 번호 매기기(`locate` — 같은 글이 둘이면 approx), 안내의 문단 번호가 수를 넘으면 「없습니다」, 스냅숏은
OOXML, 기록은 255자, 제안은 settings(1.4).

**stub 은 없는 메서드를 못 잡는다** — 엑셀 판 첫 실물에서 `getDataBodyRangeOrNullObject`·`getItemByCellOrNullObject` 가
그렇게 잡혔다. `wordhand.mjs` 가 지나간 것은 우리가 부르는 모양뿐이다.

## 5. 실물 — Word 와 사람의 손

### 5.1 도구 44개 — 실물 문서에 전수(2026-09-06 오후)

Mac Word 16.x, 새 문서 「문서1」(8문단). 작업창이 붙어 `wd-doc-…` 로 광고됐고(WordApi 1.9), 스크래치 드라이버(`wordreal.py`)가
**MCP 로 44개 전부**를 불렀다 — 문서 끝에 「4. 시험 절」을 넣고 그 안에서만 고쳤다. **첫 판 실패 4종 → 고침 → 63호출 실패 0.**
실패는 전부 우리 쪽이었고, 넷 중 셋이 「stub 이 못 잡는 것」(위 §4)이었다:

| 도구 | 실물이 한 말 | 원인 | 고침 |
|---|---|---|---|
| `insert_paragraphs{style:"Heading 1"}` | `InvalidArgument — Paragraph.style` | 한국어 Word 엔 「Heading 1」이란 스타일이 없다(「제목 1」) | 내장 이름(`Heading1`·"Heading 1"·`normal`)은 언어와 무관한 `styleBuiltIn` 으로, 나머지만 `style`(`applyStyle`) — `set_style{style}` 도 같은 길 |
| `insert_list` | `GeneralException — Paragraph.startNewList` / `attachToList` | 목록 항목 뒤에 `insertParagraph` 한 문단은 그 목록을 **물려받는다**; 이미 항목인 문단에 둘 다 터진다 | 물려받은 것은 떼고(`detachInherited`), 안 붙은 것만 붙인다(`attachMissing`); `insert_paragraphs` 도 뗀다 — 넣으라는 말은 문단이지 항목이 아니다 |
| `insert_image` | 「그림 바이트가 안 왔습니다」 → `InvalidArgument — insertInlinePictureFromBase64` | 헬퍼가 `add_image`(파워포인트·엑셀 이름)에만 바이트를 실었다; 문단의 그 메서드는 `Replace·Start·End` 만 받는다 | 헬퍼는 `insert_image` 에 싣고, 그림은 앞/뒤에 빈 문단을 하나 얻어 그 `Start` 에 |
| `read_tags` | `"2026-09-06"` 이 `2026-09-06T00:00:00.000Z` 로 | 사용자 지정 속성은 형이 있다 — 날짜꼴 문자열을 Word 가 Date 로 굳힌다 | `type` 을 같이 읽어 자정 날짜는 날짜만 돌려준다 |

호출 시간은 평균 0.65초, 가장 긴 것 1.6초(`insert_list`). 새로 잰 것: `smoke.mjs` 가 내장 스타일 목록을 `enums.go` 와 대조하고
`applyStyle` 의 세 갈래를 문다.

눈으로 본 것: 리본 홈 탭 오른쪽 끝에 **Magi** 단추(사이드로드 뒤 「추가 기능 › 개발자 추가 기능 › Magi」를 한 번 눌러야 생긴다),
브랜드 줄 `MAGI word · 대화 s_…`, 헬퍼를 껐다 켜면 작업창이 스스로 되살아나 다시 광고한다(점검표 3·10 ✓). 토큰은 기동마다 새로
난다 — 스크립트는 `taskpane.html` 의 부트 JSON 에서 읽는다. ⚠헬퍼 바이너리를 **제자리에 `cp` 로 덮으면** macOS 가 서명 캐시
불일치로 SIGKILL 한다(아무 말 없이 죽는다) — `rm` 뒤 `cp`.

### 5.1.1 insert_field — 실물(2026-09-06 저녁, 통합 헬퍼)

도구 45번째. 실물 문서에 목차(`field:"toc"`, 제목 1 셋을 항목으로), 바닥글 `template:"{page} / {pages}"` → 「1 / 1」, 본문
`"작성일 {date} {time} · {title} · {file}"` → 「작성일 9/6/26 6:13 PM · · 문서1」, `num_pages` 까지 넷 다 OK. 첫 판 실패 둘:

| 실물이 한 말 | 원인 | 고침 |
|---|---|---|
| `host.insertField is not a function` | Mac Word 16.x(WordApi 1.9)에 `Paragraph.insertField` 가 없다 — `Range` 에만 있다 | 범위로 넣는다 |
| 첫 필드 뒤의 조각이 전부 사라짐(「작성일 9/6/26」만 남음) | 필드의 `result` 범위에 이어 붙이면 그 뒤 insert 가 증발한다 | 글을 통째로 먼저 적고(보이지 않는 표식 `\u2063F<i>\u2063`), 표식을 `search` 로 찾아 `insertField('Replace')` 로 제자리에서 바꾼다 |

부수로: 본문 끝에 넣은 문단이 끝 문단의 제목 스타일을 물려받아 필드 줄이 「제목 2」였다 → 본문 자리엔 `Normal` 을 준다.

### 5.1.2 각주·미주 — 실물(2026-09-06 저녁)

도구 46~48(`read_footnotes`·`insert_footnote`·`delete_footnote`). 실물 문서에 각주 둘(하나는 「12%」 뒤, 하나는 문단 끝)·미주 하나를 달고
읽고 지웠다 — 여섯 호출 실패 0. 실물이 가르쳐 준 것: 각주의 `reference` 범위 글은 표식 문자(`\u0002`) 하나이고 각주 본문도 그
문자로 시작한다 → 걸린 글은 그 문단에서 표식 바로 앞 30자로 보이고(같은 문단에 여럿이면 n 번째 표식), 본문에서는 표식을 지운다.

### 5.1.3 set_style_format — 실물(2026-09-06 밤)

도구 49번째. 실물 문서에서 `Heading1`(내장 이름 → 현지 「제목 1」을 그 스타일인 문단에서 찾음) 크기 14·굵게·색·간격 → 제목 셋이
한 번에 바뀜(read_paragraphs 로 확인), 현지 이름 「제목 2」로 기울임, `create` 로 「보고서 본문」 새 스타일을 만들어 문단에 입힘,
없는 이름은 문서의 스타일 목록(한국어 Word 는 「각주 텍스트」「글머리 기호」… 수십 개)을 대고 거절. `document.getStyles()`·
`Style.font`·`Style.paragraphFormat`·`addStyle` 전부 1.9 실물에서 그대로 됐다.

### 5.1.4 format_table_cells · edit_table — 실물(2026-09-06 밤)

도구 50·51. 실물 문서에 4×3 표를 넣고 머리글 행 채우기·흰 글자·굵게·가운데, 숫자 칸 오른쪽·크기 10, 칸 하나 채우기·세로 가운데·너비,
열 추가(끝·0 뒤, 위→아래 값), 열·행 삭제, (0,0)–(0,1) 병합까지 열 호출 실패 0 — `TableCell.shadingColor`·`horizontalAlignment`·
`verticalAlignment`·`columnWidth`·`body.font`·`addColumns`·`insertColumns('After')`·`deleteColumns`·`deleteRows`·`mergeCells` 전부 1.9
실물에서 그대로. 병합한 뒤 `read_table` 은 그 행의 칸 수가 하나 줄어 보인다(병합 칸은 하나) — 값은 `순번\r매출` 처럼 줄바꿈으로 합쳐진다.

### 5.1.5 list_images · format_image · delete_image — 실물(2026-09-06 밤)

도구 52~54. 실물 문서에 그림을 넣고 목록(번호·문단·크기·대체 텍스트), 너비만 주면 비율 유지(80×80 → 40×40), 둘 다 주면 그대로(60×20),
문단 가운데 정렬, 지우기까지 여덟 호출 실패 0. `InlinePicture.paragraph`·`lockAspectRatio`·`altTextDescription`·`delete` 가 1.9
실물에서 그대로.

### 5.1.6 move_paragraphs — 실물(2026-09-06 밤)

도구 55. 실물 문서에서 제목+본문 두 문단을 다른 제목 앞으로, 다시 본문 끝으로 옮겼다 — 스타일(제목 1·표준)이 따라가고 문단 수가 그대로
(14)이며 답한 새 번호가 목차와 맞는다. 첫 판: `Paragraph.insertOoxml(…, 'Before')` 가 InvalidArgument — 그 메서드는 Replace·Start·End
뿐이라(그림·필드와 같은 규칙) 빈 문단을 앞/뒤에 세우고 `Replace` 로 통째 바꾼다.

### 5.1.7 insert_file — 실물(2026-09-06 밤)

도구 56. 헬퍼가 `.docx`(OOXML zip 만, 20MB 까지)를 읽어 실어 주고 손이 `insertFileFromBase64` 로 넣는다. 실물: 두 문단짜리 docx 를 본문
끝과 문단 2 뒤에 넣어 문단 2개씩 늘었고, `.png` 는 「.docx 파일만 받습니다」, 없는 경로는 「그런 파일이 없습니다」. 첫 판: body 의
`End` 에 바로 넣으니 첫 문단이 마지막 빈 문단에 합쳐져 수가 하나 모자랐다 → 앞/뒤/끝/처음 전부 빈 문단을 세워 `Replace`.

### 5.1.8 render_page — 실물(2026-09-06 밤)

도구 57. 작업창이 `Office.context.document.getFileAsync(Pdf)` 로 문서 전체(조각 65KB)를 받아 base64 로 헬퍼에 주고, 헬퍼가 `pdftoppm`
(없으면 Mac 의 `sips` 로 첫 쪽만)으로 그 쪽을 PNG 로 만들어 MCP 그림 블록으로 싣는다. 실물: 1쪽 600px 44KB·400px 20KB — 목차·제목·
각주·바닥글 「1 / 1」이 그대로 보인다. 없는 쪽(9)은 「이 문서는 1쪽입니다」로 거절(쪽 수는 PDF 의 `/Type /Page` 를 센다). 첫 판 둘:
그림 블록을 싣는 자리보다 뒤에서 변환해 글로만 갔다(순서 고침), 손이 답한 `page` 가 JSON 을 거쳐 float64 라 0 으로 읽혀 9쪽이
1쪽으로 그려졌다(`intOf`).

### 5.1.9 set_page_setup · 콘텐츠 컨트롤 넷 — 실물(2026-09-06 밤)

도구 58~62. `set_page_setup`(WordApiDesktop 1.1 — Mac 365 에서 됨): 가로·A4·여백 넷·첫 쪽 따로, 되돌리기, 없는 구역은 수를 대고 거절.
콘텐츠 컨트롤: 문단 통째(`summary`)와 문단 안 글(「표」→`ref`, Tags 모양, 잠금)에 넣고, 읽고(id·태그·문단·글·잠금), 태그로 채우고
제목 바꾸고, 잠금 해제·태그 바꾸기, 없는 태그는 있는 것을 대고 거절, 떼면 글은 남는다 — 열다섯 호출, 일부러 낸 거절 둘 빼고 실패 0.
`insertContentControl`·`cannotEdit/cannotDelete`·`placeholderText`·`appearance`·`delete(keep)` 전부 1.9 실물 그대로.

### 5.1.10 도형 넷 — 실물(2026-09-06 밤)

도구 63~66(WordApiDesktop 1.2 — Mac 365 에서 됨). 글 상자(채우기·글)와 오른쪽 화살표(문단 9 에 닻)를 넣고, 목록(id·이름·종류·자리·크기·
글), 이름으로 글·채우기·자리·너비 고치기, 이름 바꾸기, 없는 이름은 있는 것을 대고 거절, 지우기. 실물이 가르쳐 준 것 둘: `Body` 에는
`insertTextBox`·`insertGeometricShape` 가 없다(문단·범위에만) → 문단에 닻(`paragraph`, 기본 1); `Shape` 프록시에 `outline` 이 없다 →
선 색은 「이 Word 판에서 못 바꿉니다」로 답에 적고 나머지는 한다. 셋째: 글을 바꾼 뒤 같은 배치에서 `fill.setSolidColor` 가
GeneralException → 채우기·자리 먼저, 글은 다음 배치에. 한 번 글 상자를 지우자 화살표까지 사라진 것을 봤는데(첫 판) 세 번 되풀이해도
재현되지 않았다 — 지우기는 하나씩 확인됨.

### 5.2 사람의 손 — 2026-09-06 밤에 눌렀다(cliclick 으로 좌표를 눌러, 사람 손과 같은 길)

| # | 점검 | 결과 |
|---|---|---|
| 1 | 홈 탭 **Magi** → 작업창 | ✓ (사이드로드 첫 번엔 「추가 기능 › 개발자 추가 기능 › Magi」) |
| 2 | 「지원 API」 줄 — 365 면 숨어 있어야 | ✓ 숨음(WordApi 1.9) |
| 3 | 붙기 → `준비됐습니다 — 도구 66 개.` | ✓ |
| 4 | 문단을 잡고 「인용」 → `[인용] paragraphs=8-8 …` 카드 | ✓ 잡은 문단이 카드로 서고, 보내면 사용자 말풍선에 그대로 실린다 |
| 5 | 「목차 읽어 줘」 → `문단 목차 읽기` 줄, 권한 물음 없이 | ✓ |
| 6 | 「문단 8을 다시 써 줘」 → Word 화면이 바뀐다 | ✓ `문단 읽기`·`문단 글 바꾸기`·`끝 신고` 줄, 문단 8 이 두 문장으로. **권한 물음은 안 떴다** — 이 컴패니언은 `allow` 라(설치기 기본) 물을 것이 없다; 물음 카드 자체는 smoke 가 잰다 |
| 7 | 「제목 2 로 바꾸라고 제안만」 → 제안 카드 → 「적용」 | ✓ `제안 붙이기` 뒤 「제안 1건」 카드, 「적용」을 누르니 문단 9 가 제목 2 로. 카드는 전사가 조용해진 뒤 2초 뒤에 선다(바로 안 보여도 고장이 아니다). ⚠카드의 「무엇을 합니다」 줄이 빈 회색 띠였다 — 진짜 손은 그 줄을 안 주고 화면이 손에서 뽑지 않았다 → 세 판 다 `fixLabel` 로 뽑게 고침 |
| 8 | `read_html` 한 번 — HTML 이 대화에 | ✓ `모양 보기(HTML)` 줄과 태그 요약 |
| 9 | 창 둘 → 브랜드 줄 `문서 2` | ✓ 둘째 문서의 작업창이 제 대화(s_05f2…)로 붙고 `… · 문서 2`(창이 좁으면 말줄임에 가린다 — 넓히면 보인다) |
| 10 | 헬퍼를 껐다 켠다 → 작업창이 스스로 되살아난다 | ✓ 이날 스무 번 넘게 |

한글 입력만 사람 손이 남았다: 접근성 keystroke 는 한국어 IME 를 안 타 자모가 들어가서, 부탁 글은 헬퍼의 `/api/submit`(작업창의
보내기 단추가 두드리는 그 문)으로 넣었다. 인용·적용·⋯ 단추와 그려지는 것은 전부 작업창에서 눌러 본 것이다.

### 5.3 2026-09-07 — Windows LTSC 2021 에서 66개를 하나씩 (`tools/sweep.mjs`)

이 머신(Office LTSC 2021 16.0.14334, 볼륨 판)에는 Word 가 빠져 있었다(Click-to-Run 제외 목록에 word). Office 배포 도구로
같은 제품(`ProPlus2021Volume`, x64, ko-kr)을 Word 만 뺀 제외 목록으로 다시 구성해 더했다(INSTALL §1.1) — 약 2분. 카탈로그에는
매니페스트가 이미 있어서(통합 설치기) 「삽입 → 내 추가 기능 → 공유 폴더 → Magi(AI Assistant) → 추가」만 했고, 창이 바로
「준비됐습니다 — 도구 66 개 · 지원 API: 8개 없음」으로 열렸다. 없는 여덟: WordApi 1.4~1.9 여섯, WordApiDesktop 1.1, SharedRuntime 1.1
(SharedRuntime 이 ✗ 라도 창은 뜨고 손 노릇을 한다 — 매니페스트가 그것을 최상위 요구에 안 적는다).

엑셀·파워포인트 판과 같은 물음(「기능을 하나하나 실행해서 2021에서도 동일하게 적용되는지」)에 `tools/sweep.mjs` 를 지어 답했다:
문서 끝에 문단 셋을 붙이고 그 아래에서 66개를 80호출로 부르고, 끝에 그 문단부터 지운다. **66/66 호출 · 오류 27 · 5.9초.**
27 중 26 은 1.3 호스트가 **이름을 대고 거절**한 것 — 1.4(메모 넷·책갈피 둘·변경 추적·제안 셋·`edit_table{merge}`), 1.5(필드 둘·각주 셋·
스타일 서식), 1.6(변경 읽기·검토), WordApiDesktop(쪽 설정 둘·도형 넷) — INSTALL §1 의 표 그대로다. 나머지 하나는 `render_page`:
이 머신에 poppler 의 pdftoppm 이 없다(판의 한계가 아니라 머신의 것). 우리 것의 고장은 0.

들어간 것(각 답의 `changed` 와 되읽기로 확인): 문단 넣기·바꾸기·옮기기·지우기, 스타일·글자·문단 서식, 찾기·바꾸기, 링크, 표(만들기·칸·행·
서식·칸 서식·열 추가·지우기), 목록(넣기·바꾸기·떼기), 그림(넣기·크기·지우기), 줄 나누기, 바닥글, 콘텐츠 컨트롤 넷, 다른 문서 넣기, 속성,
기록(태그), 조언, 스냅숏·되돌리기. 끝에 문서는 처음의 문단 넷으로 돌아왔고(Word 가 못 지우는 마지막 빈 문단 하나가 남는다) 바닥글은 비었다.

## 통합 헬퍼 재확인

- **2026-09-06 저녁, 통합 헬퍼(`magi office`, 3000 의 `/word`) 재확인**: 작업창이 붙어(`wd-…`) 바인딩(도구 44)·`list_paragraphs`·`insert_paragraphs`·`format_text`·`read_document`·`delete_paragraphs` OK. 첫 턴 전 컨텍스트 띠가 system 2,824·tools 17,339 를 보인다(데몬 `context` 문이 그 자리에서 잰다).
