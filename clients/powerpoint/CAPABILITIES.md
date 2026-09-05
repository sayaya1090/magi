# 파워포인트가 실제로 내주는 것 — 그리고 모델이 읽을 수 있는 형태

Status: **조사 결과. 설계 결정은 [`DESIGN.md`](DESIGN.md)에 있다.** (2026-08-28)

> **이 문서가 하는 일.** `DESIGN.md`는 *무엇을 만들 것인가*를 정한다. 이 문서는 그 앞 질문 —
> **파워포인트가 우리에게 무엇을 내주는가** — 에만 답한다. 쓸 수 있는 것을 세고, 못 쓰는 것을
> 세고, 마지막에 §10에서 **그것을 모델이 읽을 수 있는 모양으로 어떻게 세울지**를 다룬다.
>
> `DESIGN.md` 부록 A와 겹치지 않게 썼다. 거기엔 요구사항 집합 표와 `Slide`·`Shape` 멤버 목록이
> 이미 있다. 여기서는 **그 목록으로는 안 보이는 것** — 파일 전체를 얻는 길, 덱 안에 우리 것을
> 적어 둘 자리, 픽셀을 얻는 세 경로, 그리고 없는 것의 증명 — 을 적는다.

> **인용 규약.** `DESIGN.md`와 같다. 커밋은 제목으로, 코드는 심볼로 짚는다.

> **확인 기준.** 이 문서에서 버전 태그(1.5, 1.8, …)나 시그니처가 붙은 문장은 2026-08-28에
> Microsoft Learn 레퍼런스에서 직접 읽은 것이다. **읽지 않고 적은 것은 §9와 부록에 따로 모아
> "확인하지 않음"이라고 표시했다.** 섞어 두면 다음 사람이 전부를 의심하게 된다.

---

## 0. 한 문장

**파워포인트는 덱을 *고치는* 길은 넓게 열어 뒀고, 덱이 *어떻게 보이는지*를 말해 주는 길은
거의 열지 않았다.** 그래서 쓸 수 있는 기능의 목록보다, 그 목록의 구멍을 무엇으로 메울지가
설계를 결정한다.

---

## 1. 파일 전체를 손에 넣는 길

애드인 샌드박스 안에서 **열려 있는 덱의 원본 바이트를 통째로 받을 수 있다.** 이게 이 조사에서
가장 값이 큰 발견이다. Office.js 객체 모델로 못 읽는 것(발표자 노트, 애니메이션, 전환, 주석)이
전부 그 바이트 안에는 들어 있기 때문이다.

`Office.context.document.getFileAsync(fileType, options, callback)` — `Office.File` 을 돌려주고,
`File.getSliceAsync(index, …)` 로 조각을 받아 이어 붙인 뒤 **`File.closeAsync()` 로 닫는다.**

| | 웹 | Windows | Mac | iPad |
|---|---|---|---|---|
| `Compressed` (= .pptx, OOXML) | ✅ | ✅ | ✅ | ✅ |
| `Pdf` | ❌ | ✅ | ✅ | ✅ |
| `Text` | ❌ | ❌ | ❌ | ❌ |

**웹에서만 PDF가 없다.** 이 한 칸이 §6의 렌더러 선택을 갈라놓는다.

조각 크기는 `options.sliceSize` 로 준다. 문서가 못 박은 한계:

- 최대 **4194304 바이트(4MB)**. iPad 만 **65536 바이트(64KB)**.
- 한계를 넘겨 주면 *"Internal Error"* 로 실패한다 — 크기를 나무라는 메시지가 아니다.
- 기본값은 레퍼런스 본문에 없다. **모르는 채로 두지 말고 명시해서 넘긴다.**

`getFilePropertiesAsync` 는 파일 URL을 준다. 아직 저장한 적 없는 덱이면 빈 문자열이고,
문서 자신의 예제가 그 경우를 *"The file hasn't been saved yet"* 으로 처리한다.
**`DESIGN.md` §5.0이 "덱이 있는 디렉토리에 데몬을 띄운다"고 할 때, 그 디렉토리를 알아내는
길이 이것이다** — 그리고 빈 문자열이 돌아오는 경우가 있다는 것도 같이 온다.

### 쓸 곳

- **읽기 천장을 넘는 유일한 길.** 노트·전환·주석이 필요하면 여기서 .pptx 를 받아 OOXML 을 직접
  판다. 객체 모델에는 없다(§8).
- **되돌릴 곳을 만드는 길.** `DESIGN.md` §2.1의 "되돌릴 곳이 없다"에 대한 가장 정직한 답은
  편집 전에 덱 전체 바이트를 한 벌 떠 두는 것이다. 비싸다 — 그래서 언제 뜰지가 §9의 열린 질문.
- **비용.** 4MB 조각으로도 40MB 덱이면 10회 왕복이고, 각 조각은 base64 로 온다.

---

## 2. 덱 안에 우리 것을 적어 둘 자리 — 셋

`DESIGN.md` §5.0의 전제는 **PPT 파일에는 워크스페이스 개념이 없다**는 것이었다. 그런데
파워포인트는 *덱 자체를 저장소로* 쓰는 자리를 세 개 준다. 워크스페이스가 없는 게 아니라,
**워크스페이스가 파일 안에 있다.**

| | 범위 | API | 요구 집합 | 성질 |
|---|---|---|---|---|
| `tags` | 프레젠테이션 / 슬라이드 / 도형 | `TagCollection` | 1.3 | 문자열 키·값. 슬라이드 하나에 붙는다 |
| `customXmlParts` | 프레젠테이션 / 슬라이드 / 도형 | `CustomXmlPartCollection` | 1.7 | 임의 XML 한 덩이. 네임스페이스로 찾는다 |
| `Office.context.document.settings` | 문서 전체 | `Office.Settings` | Settings (공통) | 이름/값 가방. 값은 JSON 직렬화 |

**셋이 답하는 질문이 다르다.**

- **`tags`** — *"이 슬라이드가 무엇인가"*. 슬라이드에 직접 붙으므로 슬라이드를 옮기고 지워도
  따라다닌다. 에이전트가 만든 슬라이드에 표시를 남기는 데 이보다 나은 자리가 없다.
- **`customXmlParts`** — *"이 덱에 대해 우리가 아는 것"*. 구조화된 한 덩이. 계획, 스타일 규약,
  이전 실행의 결론처럼 문자열 쌍으로 안 접히는 것.
- **`Settings`** — *"이 애드인이 이 문서에서 기억할 것"*. 애드인마다·문서마다 격리된다고 문서가
  명시한다. 다른 애드인은 못 읽는다.

**`Settings` 의 함정 셋** — 전부 레퍼런스가 굵게 적어 둔 것이다.

1. `set`/`remove` 는 **메모리만** 고친다. `saveAsync` 를 부르지 않으면 다음에 열었을 때 없다.
2. `saveAsync` 조차 문서 *파일*을 저장하지 않는다. *"the changes to the document file itself are
   saved only when the user (or AutoRecover setting) saves the document"* — **사람이 저장하지
   않으면 사라진다.** 에이전트의 기억을 여기 두면 사람의 Ctrl+S 에 의존하게 된다.
3. 공동 편집이면 인스턴스마다 사본이 갈린다. `refreshAsync` 가 그걸 위해 있다. 그리고
   `settingsChanged` 이벤트는 **Excel 웹 공동편집에서만 발화한다** — 파워포인트에서는 안 온다.
   문서가 그렇게 적었다.

셋 다 같은 함정 하나를 공유한다: **덱 안에 있으므로 덱과 함께 다른 사람에게 간다.**
`DESIGN.md` §8의 신뢰 경계가 여기까지 온다 — 여기 적는 것은 전부 공개된다고 보고 적어야 한다.

> **설계 의견.** 셋 중 `tags` 를 기본으로 삼는 게 맞다고 본다. 사람의 저장에 안 걸리고(도형·
> 슬라이드 속성이라 덱의 일부다), 슬라이드를 따라 움직이고, 요구 집합 1.3이라 1.8 기준선 아래에서도
> 있다. `customXmlParts` 는 한 덩이가 필요할 때만. `Settings` 는 §2-2 때문에 **에이전트의 기억을
> 두는 데 쓰지 않는다** — 잃어도 되는 UI 상태(마지막에 열었던 패널 같은 것)까지다.

---

## 3. "이거" 를 가리키는 채널 — 선택

사람이 화면을 보며 *"이거 좀 줄여"* 라고 말할 때, 그 *이거*가 실제로 전달되는 통로다.
**읽기와 쓰기가 둘 다 열려 있다는 게 핵심이다.** 읽기는 사람의 지시를 받는 길이고, 쓰기는
에이전트가 **자기가 무엇을 건드렸는지 보여주는** 길이다.

읽는 쪽 (전부 1.5):

- `presentation.getSelectedSlides()` → `SlideScopedCollection`. **첫 항목이 활성 슬라이드다.**
- `presentation.getSelectedShapes()` → `ShapeScopedCollection`. 없으면 빈 컬렉션.
- `presentation.getSelectedTextRange()` → 선택된 텍스트가 없으면 **예외를 던진다.**
  `getSelectedTextRangeOrNullObject()` 가 던지지 않는 짝이다. 에이전트가 부르는 쪽은 후자여야
  한다 — "선택 없음"은 오류가 아니라 답이다.
- 공통 API 쪽에도 `Office.context.document.getSelectedDataAsync(Office.CoercionType.SlideRange)`
  가 있고 **선택된 슬라이드의 ID·제목·인덱스**를 준다. 파워포인트 전용 강제 타입이다.

쓰는 쪽:

- `presentation.setSelectedSlides(slideIds)` (1.5) — 빈 배열이면 선택 해제.
- `slide.setSelectedShapes(shapeIds)` (1.5)
- `textRange.setSelected()` — 텍스트 범위를 골라 준다.

### 쓸 곳

`DESIGN.md` §7("끝났다고 말하는 법")에 한 줄 더한다: **에이전트가 고친 자리를 선택해 두고
끝내면, 사람은 확인을 위해 찾아다니지 않아도 된다.** 렌더 이미지를 보내는 것보다 싸고,
사람 화면에서 즉시 보인다. 되돌리기가 없는 환경(§2.1)에서 "무엇이 바뀌었는지"를 알리는
가장 값싼 수단이다.

---

## 4. 생성의 바탕 — 레이아웃·마스터·자리표시자

빈 슬라이드에 좌표로 텍스트 상자를 놓는 것과, **레이아웃을 적용하고 자리표시자를 채우는 것**은
결과물이 다르다. 후자는 테마를 따르고, 나중에 사람이 디자인을 바꾸면 같이 바뀐다. 전자는
영원히 그 자리에 못 박힌다.

- `presentation.slideMasters` (1.3) → `SlideMaster` → `.layouts` → `SlideLayout`
- `slide.layout` (1.3), `slide.applyLayout(...)` (1.8)
- `slides.add(options)` — 어떤 레이아웃으로 만들지 지정한다
- `shape.placeholderFormat` → `PlaceholderFormat` — **도형이 어떤 역할인지**(제목·본문·부제…)
- `presentation.insertSlidesFromBase64(base64File, options)` (1.2) —
  `InsertSlideFormatting.useDestinationTheme` 로 **다른 덱의 슬라이드를 이 덱의 테마로 들여온다**
- `presentation.pageSetup` (1.10) — 슬라이드 크기·방향
- `slide.themeColorScheme` (1.10), `slide.background` (1.10)

**1.8 기준선에서 뒤 셋은 없다.** `DESIGN.md` §3.3이 기준선을 1.8로 정했으므로,
`pageSetup`·`themeColorScheme`·`background` 는 **없는 셈 치고 설계해야 한다.** 슬라이드가
16:9인지 4:3인지, 테마 강조색이 무엇인지를 **API로는 모른다**는 뜻이다. 알아야 하면 §1의
.pptx 바이트에서 읽거나, §6의 렌더 이미지에서 재야 한다.

`insertSlidesFromBase64` 는 조용히 강력하다. **에이전트가 슬라이드를 좌표로 짓는 대신,
템플릿 덱을 만들어 두고 거기서 들여올 수 있다.** 사람이 만든 슬라이드의 품질을 그대로 얻고,
좌표·글꼴·정렬을 하나도 결정하지 않는다.

---

## 5. 표와 텍스트

- `Table`·`TableRow`·`TableColumn`·`TableCell`·`TableStyleSettings` — 클래스로 존재한다.
  요구 집합 **1.8이 표를 들여왔고 1.9가 서식·관리를 더했다.**
- `shape.getTable()` 로 도형에서 표를 얻는다.
- `TextFrame` → `TextRange` → `ParagraphFormat` → `BulletFormat`. 글머리표까지 내려간다.
- `ShapeFont` 로 글꼴·크기·굵기·색.
- `Hyperlink`/`HyperlinkCollection` (1.6, 1.10에서 보강), `shape.setHyperlink(...)`.

**여기에 없는 것이 §10을 결정한다: 텍스트가 상자에 들어가는지 물어볼 방법이 없다.**
글자 수도, 렌더된 줄 수도, 넘쳤는지 여부도 API가 말해 주지 않는다. `TextFrame` 에 자동 맞춤
관련 속성이 있어도 그것은 *설정*이지 *결과*가 아니다. `DESIGN.md` §2.2("됐다"의 증거가 다르다)의
근본 원인이 이 한 줄이다.

---

## 6. 픽셀을 얻는 세 경로

> **먼저 정해진 것.** 사용자가 방향을 정했다 — **데이터를 이미지로 주고받는 것은 최대한 자제한다.
> 꼭 필요하면 보내되, 그것을 기본 경로로 삼지 않는다.** 붙을 모델이 멀티모달이라는 보장이 없기
> 때문이다. 그래서 이 절은 **쓸 것의 목록이 아니라 마지막 수단의 목록**으로 읽어야 한다. 같은
> 질문에 그림 없이 답하는 길은 §10.6에 있다.

| | 무엇 | 요구 집합 / 환경 | 얻는 것 | 값 |
|---|---|---|---|---|
| A | `slide.getImageAsBase64(options)` | **1.8** | PNG, 슬라이드 하나 | 파워포인트 자신의 렌더. 진실 그 자체 |
| B | `shape.getImageAsBase64(options)` | **1.10** | PNG, 도형 하나 | 1.8 기준선엔 **없다** |
| C | `getFileAsync(Pdf)` | 웹 제외 | 덱 전체 PDF | 한 번에 전부. **웹에서 불가** |
| D | Microsoft Graph `/content?format=pdf` | 클라우드 저장 필요 | 덱 전체 PDF | 서버가 렌더. 클라이언트 부담 0 |

A는 `slide.exportAsBase64()` (1.8, 슬라이드 하나짜리 .pptx) 와 짝이고, 그 문서가 붙인 주의
— *"This method is optimized to export a single slide. Exporting multiple slides can impact
performance."* — 는 A에도 사실상 같이 걸린다고 봐야 한다. **덱 전체를 A로 훑는 것은 비싸다.**

그래서 실제 조합은 이렇게 갈린다:

- **한 슬라이드를 방금 고쳤다** → A. 왕복 하나.
- **덱 전체를 봐야 한다, 데스크톱** → C. 한 번에 받고 로컬에서 페이지로 쪼갠다.
- **덱 전체를 봐야 한다, 웹** → C가 없다. A를 N번 돌거나 D로 간다.
- **B는 1.8에 없다** — 도형 하나만 보고 싶어도 슬라이드 전체를 렌더해서 잘라야 한다.

D(Graph)의 확인된 사실: `GET /drive/items/{id}/content?format=pdf`, 원본으로 pptx·ppt·pptm·
pot·potm·potx·pps·ppsx·ppsxm·odp 를 받고, **v1.0에서 나가는 형식은 PDF 하나뿐**이며, 지원하지
않는 조합은 `406` 이다. 사용자 지정 글꼴은 **치환될 수 있다**고 문서가 경고한다 — 즉 D의 픽셀은
파워포인트 화면과 다를 수 있다. 검증 오라클로 쓸 때 이 차이는 그냥 넘길 수 없다.

> **아직 안 정한 것.** A와 C·D는 *같은 것을 보고 있다는 보장이 없다*. §9에 남긴다.

---

## 7. 이벤트 — 실제로 오는 것과 안 오는 것

`DESIGN.md` 초안에서 "이벤트는 프리뷰뿐"이라고 적을 뻔했는데 **틀렸다.** 층이 둘이다.

- **공통 API 층은 정식이다.** `Office.context.document.addHandlerAsync(...)` 로
  `Office.EventType.Document.SelectionChanged` 와 `Document.ActiveViewChanged` 를 받는다
  (요구 집합 `DocumentEvents`). `getActiveViewAsync` 는 현재 뷰가 `"edit"` 인지 `"read"` 인지
  준다 (요구 집합 `ActiveView`) — *edit* 은 기본/여러 슬라이드/개요, *read* 는 슬라이드 쇼와
  읽기용 보기다.
- **PowerPointApi 층은 프리뷰다.** `presentation.onSlideSelectionChanged` 는
  *BETA (PREVIEW ONLY)* 이고 문서가 *"Do not use this API in a production environment"* 라고
  적었다. 같은 딱지가 `getActiveSlideOrNullObject()` 와 `sensitivityLabel` 에도 붙어 있다.

**따라서 "사람이 다른 슬라이드로 갔다"는 신호는 받을 수 있다** — 공통 API의
`SelectionChanged` 로. 그다음 무엇이 선택됐는지는 §3의 읽기 API로 묻는다. 프리뷰를 쓰지 않고도
된다.

`Document.SelectionChanged` 는 **프레젠테이션 모드로 들어가고 나오는 것**도
`ActiveViewChanged` 로 알려 준다. 사람이 발표 중일 때 덱을 고치지 않는 규칙을 세울 근거가 된다.

---

## 8. 없는 것 — 목록이 아니라 증명

`powerpoint` 패키지의 **클래스 전체 목록**을 받아 확인했다. 아래 이름은 **패키지에 존재하지
않는다.** 버전이 낮아서 못 쓰는 게 아니라, 프리뷰에도 없다.

| 찾은 이름 | 결과 |
|---|---|
| `Comment`, `CommentCollection` | **없음** |
| `Notes`, `NotesSlide`, `SpeakerNotes` | **없음** |
| `Animation`, `AnimationCollection` | **없음** |
| `Transition` | **없음** |
| `Chart` | **없음** |
| `SmartArt` | **없음** |

`Shape.type` 열거에는 `"Chart"`·`"SmartArt"` 가 있다 — **무엇인지 알아볼 수는 있고 고칠 수는
없다.** `DESIGN.md` 부록 A가 그 비대칭을 이미 적었고, 여기서 클래스 부재로 뒷받침한다.

그리고 `Presentation` 에는:

- **`save()` 도 `saveAs()` 도 `close()` 도 없다.** 메서드 목록을 끝까지 읽어 확인했다.
  에이전트는 덱을 저장할 수 없다. 저장은 사람 또는 자동 복구가 한다. `Settings.saveAsync` 의
  경고(§2)가 같은 사실의 다른 얼굴이다.
- **트랜잭션도 실행 취소도 없다.** `context.sync()` 는 큐를 밀어 넣는 것이지 원자적 커밋이
  아니다. 중간에 실패하면 절반이 적용된 상태로 남는다.

**이 다섯 줄이 `DESIGN.md` §2를 전부 낳았다.** 되돌릴 곳이 없는 이유(저장·취소 없음),
증거가 다른 이유(넘침을 물을 수 없음), 읽기 천장과 쓰기 천장이 다른 이유(노트·애니메이션·
전환·주석이 파일엔 있고 API엔 없음).

---

## 9. API로 못 닿는데 제품엔 있는 것 — 그리고 그 결론

파워포인트에서 사람에게 가장 값이 큰 기능들이 **애드인 API 밖에 있다.**

| 기능 | API로 | 우리 결론 |
|---|---|---|
| 디자이너 (Designer) | ❌ | 만든 슬라이드를 사람이 디자이너에 태우게 **넘긴다** |
| 모프 전환 (Morph) | ❌ (전환 자체가 없음) | 손대지 않는다 |
| 슬라이드 다시 사용 | ❌ | `insertSlidesFromBase64` 가 기능적으로 같은 자리(§4) |
| 접근성 검사 | ❌ | `altText`·`isDecorative` 로 **입력만** 채운다(§아래) |
| 비교/병합 | ❌ | §1의 바이트 두 벌을 우리가 비교한다 |

> **확인하지 않음.** 위 표에서 "❌"는 §8의 클래스 목록과 `Presentation`/`Slide`/`Shape` 멤버
> 목록에 해당 이름이 없다는 사실에서 나온 것이고, **마이크로소프트가 "제공하지 않는다"고
> 명시한 문서를 읽은 것은 아니다.** 부재는 문서에 안 적힌다. 이 표는 그 성질의 추론이다.

접근성 쪽은 실제로 열려 있다: `shape.altTextDescription`·`altTextTitle`·`isDecorative` 는
읽고 쓸 수 있고, 요구 집합 1.10이 접근성을 더 들여왔다. **검사기는 못 돌리지만 검사기가 볼
필드는 채울 수 있다.** 에이전트가 슬라이드를 만들 때마다 대체 텍스트를 채우면, 사람이 나중에
검사기를 돌렸을 때 통과한다. 이건 값싸고 확실한 이득이다.

**결론: 재구현하지 않고 넘긴다.** 디자이너를 흉내 내는 것은 진 싸움이다. 에이전트가 할 일은
디자이너가 좋아할 모양(자리표시자를 쓴 슬라이드, §4)을 만들어 두는 것이다.

---

## 10. 모델링 — 모델이 읽을 수 있는 형태

앞의 아홉 절은 *파워포인트가 무엇을 주는가*였다. 이 절은 **그것을 모델 앞에 어떤 모양으로
놓을 것인가**다. 도구 목록을 정하는 것보다 이쪽이 먼저다 — 도구의 반환 모양이 곧 모델이
보는 세계이기 때문이다.

### 10.1 날것 셋은 셋 다 못 쓴다

| 후보 | 왜 안 되는가 |
|---|---|
| **OOXML 원본** (§1) | 슬라이드 하나가 수천 토큰. `<a:off x="1524000" y="1143000"/>` 를 모델이 세어 읽게 하는 것은 컨텍스트 낭비이자 오독의 원천 |
| **Office.js 객체 그래프** | 그건 문서가 아니라 **원격 호출 그래프**다. `load()`/`sync()` 왕복 구조가 그대로 노출되면 모델이 문서가 아니라 프로토콜을 상대하게 된다 |
| **좌표 도형 목록** | 슬라이드당 20~60개. `{left: 91.4, top: 68.6, …}` 는 사실이지만 **의미가 없다.** "이게 제목인가"에 답하지 못한다 |

### 10.2 파워포인트가 이미 모델을 준다 — 그걸 쓴다

이 조사에서 가장 쓸모 있는 발견은 **모델을 우리가 발명할 필요가 없다**는 것이다.
파워포인트 자신의 문서 모델이 이미 계층적이고 의미가 있다:

```
SlideMaster  →  SlideLayout  →  Slide  →  Shape
                                            └─ placeholderFormat.type  ("제목"·"본문"·"부제"…)
```

`placeholderFormat` 하나가 **`(91.4, 68.6)에 있는 텍스트 상자`를 `제목`으로 바꾼다.**
좌표를 의미로 옮기는 사전이 API 안에 이미 있는 것이다. 모델링의 핵심은 그래서
**좌표 층이 아니라 레이아웃 층에서 덱을 서술하는 것**이다:

- 자리표시자에 들어간 도형 → **역할 이름으로** 말한다. 좌표를 싣지 않는다.
- 자리표시자가 아닌 도형 → 여기서만 좌표가 필요하다. 그것도 절대값보다 **슬라이드 대비 위치**
  (좌상/중앙/우하…)가 모델에게 더 읽힌다. 단위는 확인했다: **`left`·`top`·`width`·`height` 는
  포인트, `rotation` 은 도(degree)** 다.
- 레이아웃 이름은 **덱 자신의 어휘**다. "제목 및 내용"이라는 이름은 그 덱을 만든 사람의 의도이고,
  모델이 새 슬라이드를 만들 때 고를 목록이 된다.

### 10.3 읽는 모델과 쓰는 모델은 같은 물건이 아니다

가장 하기 쉬운 실수는 **하나의 스키마로 읽기와 쓰기를 다 하는 것**이다. `DESIGN.md` §2.3이
"읽기 천장과 쓰기 천장이 다르다"고 한 것의 실무적 귀결이 이거다.

| | 읽는 모델 (모델 → 사람의 이해) | 쓰는 모델 (모델 → 덱) |
|---|---|---|
| 목표 | **압축.** 100장을 컨텍스트에 넣어야 한다 | **정확.** 애매하면 엉뚱한 것을 고친다 |
| 손실 | 있어도 된다. 글꼴 크기 0.5pt 차이는 안 싣는다 | 없어야 한다 |
| 식별 | 사람이 부르는 말 ("3번 슬라이드 제목") | 기계 ID |
| 좌표 | 웬만하면 뺀다 | 필요하면 정확히 |

읽는 모델은 **요약**이고 쓰는 모델은 **주소**다. 둘을 섞으면 요약이 무거워지고 주소가
흐려진다.

### 10.4 무엇을 가리킬 것인가 — 식별자 문제

편집은 두 턴에 걸친다: 이번 턴에 읽고, 다음 턴에 고친다. 그 사이에 가리킨 것이 그대로여야
한다. 후보 셋을 확인했고 **1.8 기준선에서 쓸 수 있는 것은 사실상 둘뿐이다.**

| | 요구 집합 | 성질 |
|---|---|---|
| `shape.id` / `slide.id` | 1.3 / 1.2 | 세션 안에서 유효. **문서를 닫았다 열면 같다는 보장을 문서가 말하지 않는다** |
| `shape.creationId` | **1.10** | `null` 일 수 있다고 문서가 명시. **1.8 기준선엔 없다** |
| `Binding` | 1.8 | 덱에 저장되는 명시적 핸들. 붙여 두면 지속된다 |
| `slide.index` | 1.8 | **가장 위험하다.** 슬라이드 하나만 넣어도 그 뒤가 전부 밀린다 |

**결론: 모델에게는 `index` 로 말하게 하고, 도구 안에서 `id` 로 옮긴다.** 사람도 모델도
"3번 슬라이드"라고 말하지 `id=257` 이라고 말하지 않는다. 그러나 그 인덱스를 잡아 두는 순간
낡는다. 그래서 **읽는 모델은 인덱스를 싣되, 그 스냅숏이 언제 것인지도 같이 싣고**, 편집
도구는 인덱스를 받는 즉시 ID로 바꾼 다음 일한다. 인덱스가 낡았는지는 그때 알 수 있다.

`Binding` (1.8) 은 이보다 강한 답이지만 **덱에 흔적을 남긴다** — §2의 신뢰 경계가 걸린다.
여러 턴에 걸쳐 반복해 고칠 도형에만 붙이고, 끝나면 떼는 게 맞다고 본다.

### 10.5 스케치

확정이 아니라 §9 검증에 태울 첫 후보다.

```jsonc
{
  "deck":  { "slides": 42, "layouts": ["제목 슬라이드", "제목 및 내용", ...] },
  "as_of": { "read_at": "...", "note": "인덱스는 이 시점 기준" },
  "slide": {
    "index": 3,
    "layout": "제목 및 내용",
    "tags":   { "magi.origin": "generated" },      // §2
    "placeholders": [
      { "role": "title", "text": "3분기 매출" },
      { "role": "body",  "text": "- 전년 대비 12%\n- ..." }
    ],
    "extras": [                                   // 자리표시자가 아닌 것만
      { "kind": "picture", "name": "차트캡처", "where": "우하", "alt": null }
    ],
    "unreadable": ["notes", "transition"]         // §8. 없는 게 아니라 못 읽는다고 말한다
  }
}
```

세 가지를 의도했다:

1. **`unreadable` 을 명시한다.** 모델에게 노트가 *없다*고 말하면 모델은 노트가 없는 덱이라고
   믿는다. *못 읽는다*고 말해야 필요할 때 §1의 경로를 쓴다. `DESIGN.md`가 인용한
   `docs/ARCHITECTURE.md` §3의 "모른다 ≠ 없다"가 여기서 그대로 적용된다.
2. **`as_of`.** 인덱스가 스냅숏이라는 것을 데이터가 스스로 말한다(§10.4).
3. **`alt: null`.** 채울 자리가 비어 있다는 것을 보여 준다 — §9의 접근성 이득이 모델 눈에
   보이게 하는 값싼 방법.

### 10.6 픽셀은 마지막 수단이다 — 그 앞에 숫자가 있다

초안은 이 절을 "구조는 JSON으로, 판정은 픽셀로"라고 썼다. **사용자가 뒤집었다** — 데이터를
이미지로 주고받는 것은 최대한 자제하고, 꼭 필요할 때만 보내되 기본 경로로 삼지 않는다. 이유가
설계에 그대로 걸린다: **붙을 모델이 멀티모달이라는 보장이 없다.** 애드인이 고르는 컴패니언은
로컬 모델일 수 있고, magi의 `seesImages`가 false면 그림은 실리지 않는다 — 픽셀 위에 판정을 세워
두면 그 모델에서는 증거가 약해지는 게 아니라 **판정 기능 자체가 사라진다.**

그래서 질문을 바꿔야 한다. "이 슬라이드가 괜찮아 보이는가"가 아니라 **"괜찮지 않다면 무엇이
숫자로 드러나는가"**다. §5는 "텍스트가 상자에 들어가는지 API가 말해 주지 않는다"고 적었는데,
그건 API가 **판정**을 안 준다는 뜻이지 **재료**를 안 준다는 뜻이 아니다.

**그림 없이 나오는 것 — 확인된 재료로.**

| 질문 | 재료 | 계산 |
|---|---|---|
| 도형이 슬라이드 밖으로 나갔나 | `left`/`top`/`width`/`height` (포인트, 1.4) + 페이지 크기<sup>†</sup> | 사각형 비교 |
| 도형 둘이 겹치나 | 같은 값들 + `zOrderPosition` (1.8) | 교집합 넓이 |
| 본문이 자리표시자보다 긴가 | `textFrame` (1.4) 의 상자 크기 + 글자 수·글꼴 크기 | 근사 줄 수 × 줄 높이 |
| 자리표시자를 비워 뒀나 | `placeholderFormat.type` (1.8) + `textFrame.hasText` | 존재 여부 |
| 레이아웃을 벗어난 배치인가 | 레이아웃의 자리표시자 좌표 vs 실제 도형 좌표 | 차이 |

<sup>†</sup> **페이지 크기는 1.8 기준선에 없다.** `presentation.pageSetup` 은 **1.10**이다
(`DESIGN.md` §3.3이 기준선을 1.8로 박았다). 그러니 이 한 줄만 재료가 API 밖에 있다 — §1로 받은
파일이나 `slide.exportAsBase64()` 의 OOXML(`sldSz`)에서 읽어야 한다. 16:9 기본값을 가정하는 것은
안 된다. 4:3 덱과 사용자 지정 크기가 실제로 돌아다닌다.

전부 **수 몇 개**다. 토큰으로도 싸고, 텍스트 전용 모델이 그대로 읽는다. 위 표의 셋째 줄만
근사인데, 그 근사가 얼마나 맞는지는 재야 안다(§10.7).

**근사를 진짜로 바꾸는 길 둘 — 아직 확인하지 않았다.**

- **애드인 안에서 재기.** 애드인은 브라우저다. 캔버스의 `measureText` 로 같은 글꼴·크기의 실제
  폭을 잴 수 있다. 문제는 파워포인트가 쓰는 글꼴이 WebView에 있느냐인데, **확인하지 않았다.**
- **PDF에서 좌표째 뽑기.** §6 C의 `getFileAsync(Pdf)` 는 파워포인트 자신의 레이아웃 엔진이
  낸 결과다. 그 PDF에서 **텍스트를 좌표와 함께** 뽑으면 넘침·잘림이 픽셀이 아니라 **숫자**로
  나온다. 이게 제일 정직한 길로 보이는데, **PowerPoint가 내보낸 PDF에서 실제로 그게 뽑히는지
  확인하지 않았다.** 확인되면 §6 C는 "픽셀 경로"가 아니라 **레이아웃 진실을 텍스트로 받는
  경로**가 되고, 이 문서에서 가장 값이 큰 항목이 된다.

**그래도 픽셀이 남는 질문.** 글꼴 치환, 자동 축소(`autoSizeSetting`)가 실제로 몇 포인트까지
줄였는지, 그림 위에 글자가 얹혀 읽히는지 — 이런 것은 렌더에만 있다. 그때는 보낸다. 다만
**렌더의 첫 독자는 사람**이고(콘솔이 이미 그린다), 모델에게 보내는 것은 `seesImages` 가 참이고
숫자로 답이 안 나올 때다.

magi 쪽 능력은 이미 서 있다 — `internal/adapter/mcp` 의 `keepImages` 가 그림을 붙잡고,
`internal/adapter/llm/openai` 의 `pickImages` 가 예산 안에서 태운다. **서 있는 것과 기본으로
쓰는 것은 다르다.** 100장 덱이 그 예산을 어떻게 쓰는지는 여전히 재야 하지만(`DESIGN.md` §9의
S7), 이 방향에서는 그 측정의 목적이 바뀐다 — "예산이 버티는가"가 아니라 **"픽셀 없이 몇 %를
잡는가"**다.

### 10.7 아직 안 정한 것

- **선택자 언어를 둘 것인가.** `slide[3].body.para[2]` 같은 경로로 가리키게 하면 모델이
  자연스럽게 쓰지만, 파서와 그 오류 메시지를 우리가 다 져야 한다. ID 왕복은 지루하지만 정직하다.
- **100장 덱의 토큰 비용.** §10.5 스케치가 슬라이드당 몇 토큰인지 재지 않았다. 40장을 넘어가면
  요약 층이 하나 더 필요할 수 있다(덱 개요 → 슬라이드 상세를 따로 부르는 두 단계).
- **`extras` 의 `where`.** "우하" 같은 말이 좌표보다 나은지는 추측이다. 재야 한다.
- **PDF 텍스트 추출이 되는가.** §10.6의 두 번째 길. 되면 §6의 픽셀 경로 대부분이 필요 없어진다.
  **먼저 잴 것 1순위다.**
- **줄 수 근사의 오차.** §10.6 표 셋째 줄. 넘침을 몇 % 놓치고 몇 % 헛짚는지 모른다.

---

## 부록. 출처와 확인 상태

**2026-08-28에 Microsoft Learn에서 직접 읽은 것**

- [`PowerPoint.Presentation`](https://learn.microsoft.com/en-us/javascript/api/powerpoint/powerpoint.presentation)
  — 속성·메서드·이벤트 전체. `save`/`saveAs`/`close` 부재, 프리뷰 딱지 셋(§8, §7)
- [`PowerPoint.Shape`](https://learn.microsoft.com/en-us/javascript/api/powerpoint/powerpoint.shape)
  — 포인트/도 단위, `creationId` 1.10 및 `null` 가능, `getImageAsBase64` 1.10 (§6, §10.2, §10.4)
- [`Office.Document`](https://learn.microsoft.com/en-us/javascript/api/office/office.document)
  — `getFileAsync` 형식 표와 조각 한계, `getFilePropertiesAsync`, `getActiveViewAsync`,
  `getSelectedDataAsync(SlideRange)`, `addHandlerAsync` (§1, §3, §7)
- [`Office.Settings`](https://learn.microsoft.com/en-us/javascript/api/office/office.settings)
  — 저장 3단 함정, `settingsChanged` 가 Excel 웹 공동편집 전용 (§2)
- [`powerpoint` 패키지 클래스 목록](https://learn.microsoft.com/en-us/javascript/api/powerpoint)
  — §8의 부재 증명. 64개 클래스를 훑었다
- 요구사항 집합 표는 [`DESIGN.md`](DESIGN.md) 부록 A에 있다. 여기서 반복하지 않는다

**확인하지 않은 것 — 이 문서는 아래를 근거로 쓰지 않았다**

- **§9의 데스크톱 전용 기능들**(디자이너·모프·다시 사용·접근성 검사·비교/병합). API에 없다는
  것은 목록에서 확인했지만, 제품이 그것을 어떻게 노출하는지는 문서로 확인하지 않았다
- **Microsoft Graph PDF 변환**(§6 D) — 지원 형식과 406 동작은 Graph 문서에서 읽었으나
  **실제로 호출해 보지 않았다.** 글꼴 치환의 실제 정도도 모른다
- **LibreOffice headless 렌더**(`DESIGN.md` §3에서 후보로만 언급) — 이 문서에서 다루지 않았다
- **python-pptx 등 외부 라이브러리** — 검토하지 않았다
- **§10.6의 두 길**(캔버스 `measureText`, PDF 좌표 추출) — 둘 다 **해 보지 않았다.** 이 문서에서
  가장 값이 클 수 있는 항목이면서 가장 안 확인된 항목이다
- **§10 전체는 설계 제안이다.** 스케치도, 토큰 비용도, 인덱스/ID 왕복의 실제 실패율도 재지
  않았다. `DESIGN.md` §9의 S6·S7에 태울 것

---

## 11. 2026-09-05 재대조 — 요구 집합 1.4~1.10 과 도구 44 를 다시 맞춰 봤다

호스트(Mac PowerPoint 16.105 이상)가 `PowerPointApi 1.10` 까지 답한다(`/api/caps` 실측: 1.2·1.5~1.10 ok, 1.11 없음). 마이크로소프트의 집합별 API 목록(1.4·1.8·1.9·1.10·preview, 2026-07-15 판)을 `tools.go` 와 `OfficeHand.js` 가 실제로 부르는 멤버와 대조했다. 방법: 집합 페이지의 표 전부 + `grep -c` 로 판이 쓰는 멤버 빈도.

### 11.1 이날 도구에 더한 것(호스트에 있는데 도구가 안 받던 칸)

| 멤버(집합) | 어디에 | 인자 |
|---|---|---|
| `Shape.lineFormat` color/weight/dashStyle/visible (1.4) | `format_shape`·`add_shape` | `line`·`line_weight`·`line_dash` |
| `ShapeFill.transparency` (1.4) | 같음 | `transparency` |
| `TextFrame.verticalAlignment`·`wordWrap`·`autoSizeSetting` (1.4) | 같음 | `valign`·`wrap`·`autosize` |
| `ShapeFont.underline` (1.4) · `strikethrough`·`superscript`·`subscript`·`allCaps`·`smallCaps` (1.8) | `format_shape`·`add_shape`·`apply_style{title/body/all}` | `underline`·`strikethrough`·… |
| `Shape.setZOrder` (1.8) | `move_shape` | `z_order` |
| `ShapeCollection.addLine` + `ConnectorType` (1.4) | `add_shape` | `kind:"line"`·`connector` |
| `BulletFormat.type`·`style` (1.10) — 값 검사 | 세 도구 | 열거형 41+3, `enums.go` |
| `Table.mergeCells`·`TableAddOptions.mergedAreas` (1.8/1.9) | `add_table`·`replace_table`·`edit_table` | `merge` |
| `TableStyleSettings`·`TableAddOptions.style` (1.9) | 같음 | `table_style`·`header_row`·`banded_rows`·`first_column`·`banded_columns` |
| `rows.add/getItemAt().delete`·`columns.add/…delete` (1.9) | `edit_table` (새 도구, id 유지) | `add_rows`·`add_rows_at`·`delete_rows`·`add_columns`·… |
| `TableColumn.width`·`TableRow.height`·`TableAddOptions.columns/rows` (1.8/1.9) | `add_table`·`replace_table`·`edit_table` | `column_widths`·`row_heights` |
| `TableCell.verticalAlignment`·`borders` (1.9) | `format_table_cells`·(`valign` 은 만들 때도) | `valign`·`borders`·`border_weight` |

열거형 7종(`BulletStyle`·`BulletType`·`ShapeFontUnderlineStyle`·`TextVerticalAlignment`·`ShapeAutoSize`·`ShapeZOrder`·`ShapeLineDashStyle`·`ConnectorType`·`TableStyle`)은 문서에서 값을 베껴 `helper/enums.go` 한 자리에 두고 스키마 `enum` 으로 광고한다. **예시 값은 계약이다** — `bulletChromaDot` 이라는 지어낸 예시 하나가 8장짜리 `add_slides` 를 `InvalidArgument` 한 단어로 죽였다.

**실측(2026-09-05 10:31~10:45, Mac PowerPoint 1.10, deck2 의 시험 장을 만들어 렌더로 확인).** 위 표의 칸은 전부 호스트가 받았다. 그 과정에서 문서에 없는 사실 둘을 봤다:

- **`Border.transparency` 는 쓰기를 거절한다** — 0 이든 1 이든 `InvalidArgument — Border.transparency` 로 배치 전체가 되돌아온다. 셀 테두리는 색·굵기만 쓰고, 「없음」은 `weight = 0` 으로 한다(렌더로 확인).
- **`ShapeAddOptions` 의 0 은 「안 줌」이다** — `addLine(..., {width: 400, height: 0})` 이 기본 높이로 그려져 수평선이 사선이 됐다. 만든 뒤 `shape.height = 0` 을 따로 쓰면 수평이 된다.
- 병합된 영역 안의 셀은 `getCellOrNullObject` 가 null 을 준다 — 머리행을 병합한 뒤 그 행의 (0,1) 을 고치려 하면 「셀이 없습니다」가 맞는 답이다.

### 11.2 호스트에 있는데 아직 도구가 안 받는 것 — 다음 후보

| 멤버(집합) | 무엇을 열어 주나 | 메모 |
|---|---|---|
| `TableCell.margins`·`split`·`resize` (1.9) | 셀 여백·분할 | 드물다 |
| `TextRange.getSubstring().font`·`setHyperlink` (1.4/1.10) | **글 일부**만 서식·링크(“숫자만 빨갛게”) | 지금은 상자 전체뿐 |
| `SlideBackgroundFill.setPictureOrTextureFill` (1.10) | 배경 그림 | `set_background` 는 단색·그라데이션·패턴 |
| `SlideLayout.background`·`SlideMaster.background` (1.10) | 덱 전체 배경 | 테마 색은 되고 배경은 장마다 |
| `ShapeCollection.addGroup`·`ShapeGroup.ungroup` (1.8) | 묶기/풀기 | |
| `Presentation.properties`·`customProperties` (1.7) | 제목·작성자·주제 메타데이터 | 파일 속성 |
| `Shape.adjustments` (1.10) | 모서리 둥글기 등 도형 조정점 | 드물다 |
| `SlideCollection.exportAsBase64Presentation` (1.10) | 여러 장을 한 파일로 | 스냅샷은 장 단위 |
| `Presentation.bindings` (1.8) | 도형 바인딩 | 쓰임이 안 보인다 |

### 11.3 프리뷰(이 호스트에서는 못 쓴다 — 프리뷰 CDN·인사이더 빌드 필요)

`ShapeCollection.addPicture`(그림을 OOXML 없이), `AddSlideOptions.index`(원하는 자리에 바로), `Presentation.getActiveSlideOrNullObject`, **`onSlideSelectionChanged` 이벤트**(§7 은 「이벤트가 없다」였다 — 프리뷰에는 하나 생겼다), `Graphic.convertToShape`(SmartArt/그래픽 → 도형, §8 의 「고칠 수 없다」에 첫 문이 열린다), `SlideLayout.delete`·`SlideMaster.delete`. 이 판은 1.10 에서 멈추므로 위 항목은 **적어 두기만** 한다.

### 11.4 정정

- §8 의 「`Notes`·`Animation`·`Chart` 는 없다」는 여전히 맞다(preview 에도 없음). 이 도구는 셋 다 OOXML 로 한다(`notesxml`·`animxml`·`chartxml`).
- `BulletStyle` 은 **번호 매김**의 모양이다 — 점·대시·체크 기호를 고르는 문은 없다. 기호 글머리는 `bullet:true` 로 켜면 레이아웃의 기호가 나오고, 다른 기호는 `a:buChar` OOXML 이 있어야 한다.
