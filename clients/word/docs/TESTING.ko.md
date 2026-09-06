# 무엇을 어디서 재나 — Word 판

[↑ 매뉴얼](./MANUAL.ko.md) · [구조](./ARCHITECTURE.ko.md) · [엑셀 판 TESTING](../../excel/docs/TESTING.ko.md)

## 0. 한 줄로

```bash
go test ./clients/word/helper/                       # 헬퍼: 계약·유도 가드·문서 대조
node clients/word/addin/tools/smoke.mjs              # 작업창: 화면 규칙·인용·안내·제안·가짜 손 44개
node clients/word/addin/tools/smoke-hand.mjs         # 손 노릇: 스트림 → 손 → 답, 역할(손/화면), 헬퍼 어댑터
node clients/word/addin/tools/wordhand.mjs           # 진짜 손(WordHand)을 가짜 Word.js 위에서 44개 전부
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

**아직 없다(2026-09-06).** 처음 붙이는 날 여기에 쌓는다. 점검표:

1. Word 를 열고 홈 탭 **AI Assistant › Magi** → 작업창.
2. 「지원 API」 줄 — 365 면 숨어 있어야 한다. 2021 은 1.3 까지 ✓ 라 펴져 있다.
3. 붙기 → `준비됐습니다 — 도구 44 개.`
4. 문단을 잡고 「인용」 → `[인용] paragraphs=…`.
5. 「목차 읽어 줘」 → `문단 목차 읽기` 줄, 권한 물음 없이.
6. 「3번 문단을 다시 써 줘」 → 권한 물음에 `replace_paragraph` 와 인자 → 허용 → Word 화면이 바뀐다.
7. 「제목 2 로 바꾸라고 제안만 해 줘」(365) → 제안 카드 → 「적용」.
8. `read_html` 한 번 — HTML 이 대화에 뜬다.
9. 창 둘 → 브랜드 줄 `문서 2`.
10. 헬퍼를 껐다 켠다 → 작업창이 스스로 되살아난다.
