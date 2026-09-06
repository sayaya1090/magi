# Word 애드인

[사용자 매뉴얼](../docs/MANUAL.ko.md) · [무엇을 어디서 재나](../docs/TESTING.ko.md) · [아키텍처](../docs/ARCHITECTURE.ko.md) · [헬퍼](../helper/README.md) · [엑셀 판 애드인](../../excel/addin/README.md)

엑셀 판 애드인을 **복사해 손을 바꿨다**. 층·대화 스트림·권한·가이드·카운슬·컨텍스트 띠·모델 드롭다운은 그대로다.

| 길 | 무엇으로 | 상태 |
|---|---|---|
| `node tools/smoke.mjs` | `FakeDocument`·`FakeHand` | **돈다** — 356 ok (2026-09-06) |
| `node tools/smoke-hand.mjs` | `FakeHand`·`ServeHand`·`HelperStream` | **돈다** — 71 ok |
| `node tools/wordhand.mjs` | `WordHand` 를 가짜 Word.js 위에서 | **돈다** — 49/49 (없는 메서드 이름은 못 잡는다) |
| `PORT=3010 node tools/serve.mjs` | 가짜 문서 + `FakeHand` | 돈다 |
| **Word 안에서** | `OfficeDocument`·`WordHand` | **돌았다** — 2026-09-06 실물 문서에 44/44, 63호출 실패 0([TESTING §5.1](../docs/TESTING.ko.md)) |

## 구조 — 워드 고유 파일

```
src/port/DocumentPort.js      selection()(문단 범위+글) · point(paragraph) · paragraphCount() · capabilities()
src/adapter/OfficeDocument.js Word.js. MAGI.DOC 사용자 지정 속성에 안정된 이름(stableDocId). 선택은 본문에서 글로 찾아 번호(locate)
src/adapter/WordHand.js       진짜 손 — op 49개를 Word.run 한 묶음씩. 매 호출 본문 문단 목록을 새로 읽는다
src/adapter/FakeHand.js       가짜 손 — 메모리 문서 위에서 49개를 정말로
src/adapter/handCore.js       두 손의 뼈대 — READ_OPS/WRITE_OPS, FIX_TOOLS, span(문단 범위)
src/domain/Quote.js           인용 — from·to·글(1200자)·approx
src/domain/Advice.js          안내 — paragraph, ParagraphIndex(문단 수를 물은 세대)
src/domain/Suggestion.js      제안 카드 — 누를 수 있는 손 여섯
src/usecase/HandRole.js       손인가 화면인가 — WordApi 1.3 바닥
src/ui/docFixture.js          목업 문서(보고서 열한 문단·표 하나·제안 둘)
src/ui/screen.js              도구 49개의 사람 말 라벨, 인용 몸통, 제안·안내 판, 검토 부탁(문단 번호)
```

## Word 에 붙이기

[`docs/INSTALL.ko.md`](../docs/INSTALL.ko.md). 매니페스트는 `manifest.xml`(3002, 최상위 `<Requirements>` 에는 `SharedRuntime 1.1` 만).

## 아직 아닌 것

- 작업창 단추(인용·제안 적용·검토)의 실물 눈검사. 각주·필드·콘텐츠 컨트롤·도형 손이 없다. 목업 문서는 글과 표만 그린다.
