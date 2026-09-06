# 무엇을 어디서 재나 — Word 판

[↑ 매뉴얼](./MANUAL.ko.md) · [구조](./ARCHITECTURE.ko.md) · [엑셀 판 TESTING](../../excel/docs/TESTING.ko.md)

## 0. 한 줄로

```bash
go test ./clients/office/helper/                     # 헬퍼(세 판 공용): 계약·유도 가드·문서 대조
node clients/word/addin/tools/smoke.mjs              # 작업창: 화면 규칙·인용·안내·제안·가짜 손 45개
node clients/word/addin/tools/smoke-hand.mjs         # 손 노릇: 스트림 → 손 → 답, 역할(손/화면), 헬퍼 어댑터
node clients/word/addin/tools/wordhand.mjs           # 진짜 손(WordHand)을 가짜 Word.js 위에서 45개 전부
TOKEN=… node clients/word/addin/tools/livehand.mjs   # 가짜 손을 살아 있는 헬퍼에 붙인다
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

### 5.2 사람의 손 — 아직

점검표 4~9(인용·권한 물음·제안 적용·`read_html` 대화·창 둘)는 아직 사람이 안 눌렀다. 점검표:

1. Word 를 열고 홈 탭 **Magi** → 작업창(처음엔 「추가 기능 › 개발자 추가 기능 › Magi」).
2. 「지원 API」 줄 — 365 면 숨어 있어야 한다. 2021 은 1.3 까지 ✓ 라 펴져 있다.
3. 붙기 → `준비됐습니다 — 도구 45 개.`
4. 문단을 잡고 「인용」 → `[인용] paragraphs=…`.
5. 「목차 읽어 줘」 → `문단 목차 읽기` 줄, 권한 물음 없이.
6. 「3번 문단을 다시 써 줘」 → 권한 물음에 `replace_paragraph` 와 인자 → 허용 → Word 화면이 바뀐다.
7. 「제목 2 로 바꾸라고 제안만 해 줘」(365) → 제안 카드 → 「적용」.
8. `read_html` 한 번 — HTML 이 대화에 뜬다.
9. 창 둘 → 브랜드 줄 `문서 2`.
10. 헬퍼를 껐다 켠다 → 작업창이 스스로 되살아난다.

## 통합 헬퍼 재확인

- **2026-09-06 저녁, 통합 헬퍼(`magi office`, 3000 의 `/word`) 재확인**: 작업창이 붙어(`wd-…`) 바인딩(도구 44)·`list_paragraphs`·`insert_paragraphs`·`format_text`·`read_document`·`delete_paragraphs` OK. 첫 턴 전 컨텍스트 띠가 system 2,824·tools 17,339 를 보인다(데몬 `context` 문이 그 자리에서 잰다).
