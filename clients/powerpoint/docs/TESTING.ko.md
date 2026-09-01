# 무엇을 어디서 재나 — PowerPoint 레인

[↑ 설계](../DESIGN.md) · [사용자 매뉴얼](./MANUAL.ko.md) · [헬퍼](../helper/README.md) · [애드인](../addin/README.md)

> **왜 이 문서가 있나.** 이 레인의 시험은 층이 다섯이고, 층마다 **도는 조건이 다르다.** 어떤
> 것은 `go test` 한 줄이면 돌고, 하나는 진짜 데몬이 있어야 돌고, 두 층은 **PowerPoint 와 사람의
> 손**이 있어야 돈다. 그 조건을 모르면 「초록이니까 됐다」가 「그 층은 애초에 안 돌았다」와 같아
> 보인다 — 이 레인은 그 착각을 오래 안고 있었다. 헬퍼가 전부 초록인 채로 **PowerPoint 에 한 번도
> 안 붙어 본 날들**이 있었고, 붙인 첫날 5층에서만 결함 열둘이 나왔고, 에이전트가 장을 만들 수 있게 된 이튿날 아홉이 더 나왔다(§5.1).

**마지막 실측: 2026-09-02 (도구 27개)**

| 층 | 수 | 결과 |
|---|---|---|
| 1·2·3. Go (`helper/`) | 65 | 통과 (2.8s) |
| 4. JS 순수층 (`addin/tools/`) | 702 | 통과 (smoke 502 · officehand 164 · hand 36) |
| 코어 이식성 (`internal/adapter/daemon`) | 2 | 통과 |
| 5. 실물 PowerPoint | **도구 27개 전수 34항목** | 통과 34 · 실패 0 (§5.4) |

「초록이었다」는 기억이 아니라 **날짜와 수**로 남아야, 다음 사람이 무엇을 근거로 믿는지 안다.

---

## 층 다섯

| 층 | 어디 | 언제 도나 | 무엇을 아나 |
|---|---|---|---|
| 1. 계약 | `helper/mcp_test.go`·`hand_test.go`·`handhttp_test.go`·`bridge_test.go` | `go test` | 프로토콜·문·손이 규약대로 구는가 |
| 2. 유도 가드 | `helper/names_test.go`·`tools_test.go`·`args_test.go` | `go test` | 두 벌 적힌 것이 갈렸는가 |
| 3. 상호운용 | `helper/mcp_test.go` 의 `MagisOwnClientAttachesToThisHelper`·`attach_test.go` | `go test` | **magi 자신**과 맞물리는가 |
| 4. 화면 규칙 | `addin/tools/*.mjs` | `node tools/smoke.mjs` | 화면이 무엇을 적기로 정하는가 |
| 5. 실물 | PowerPoint + 데몬 + 모델 | 사람이 | 호스트가 실제로 어떻게 답하는가 |

---

## 1. 계약 — 규약대로 구는가

**이 층이 무는 것은 「우리가 남과 어떻게 맞물리기로 했는가」다.** 그래서 시험 이름이 곧 규칙이다.

| 규칙 | 시험 |
|---|---|
| 알림에는 **202** 를 준다(MCP 스펙의 MUST) | `ANotificationIsAcceptedWithTwoOhTwo` |
| 거절은 **도구 오류**로 오지 프로토콜 오류로 안 온다 | `RefusalsComeBackAsToolErrorsNotProtocolErrors` |
| 손이 없으면 **실패하고 사유를 댄다** | `WithNoAddinAttachedToolsFailAndSayWhy` |
| 문 둘 다 토큰을 요구한다 | `TheMCPDoorWantsTheToken`·`TheHandDoorWantsTheToken` |
| **연결이 둘이다** — 전사가 흐르는 동안에도 status 가 답한다 | `StatusAnswersWhileTheTranscriptIsStreaming` |
| 커서를 미는 것은 `seq > 0` 인 사건뿐 | `OnlyASeatedEventMovesTheCursor` |
| 한 번에 한 호출, 그러나 **한 호출은 반드시 돈다** | `OneCallAtATimeButOnlyOneCall` |
| 시한이 지나면 **누가 잘랐고 몇 초였는지** 말한다 | `ATimeoutSaysWhoCutItAndAtWhat` |
| 부르는 쪽이 포기한 것은 **애드인 잘못이 아니다** | `TheCallerGivingUpIsNotTheAddinsFault` |
| 아무도 안 기다리는 답은 **소리 내어** 거절한다 | `AnAnswerNobodyWaitsForIsRefusedOutLoud` |
| 모르는 문서는 **짐작 안 하고** 거절한다 | `AnUnknownDocumentIsRefusedNotGuessed` |
| 안 실린 문서는 **마지막으로 말한 덱**이 받는다 | `OmittingTheDocumentUsesTheDeckThatSpokeLast` |
| 저장 안 한 덱 둘은 **키가 둘**이다 | `TwoUnsavedDecksGetTwoKeys` |
| 같은 프레젠테이션으로 다시 붙으면 **키가 유지된다** | `RejoiningTheSamePresentationKeepsItsKey` |
| 판번호를 모르면 **「모른다」지 「안 바뀌었다」가 아니다** | `AMissingRevisionSaysUnknownNotUnchanged` |
| 인증서는 **이름 하나만** 덮는다(127.0.0.1) | `TheCertificateCoversTheOneName` |
| 개인키는 소유자만 | `ThePrivateKeyIsOwnerOnly` |
| 잡힌 포트는 **멈춤이지 우회가 아니다** | `ATakenPortIsAStopNotADetour` |
| 남의 리스너는 **지우지도 붙지도 않는다** | `AStrangerOnThePortIsRefusedNotAdopted`·`ATLSStrangerIsStillAStranger` |
| 애드인이 받는 것은 **캐시되지 않는다** | `NothingTheAddinLoadsIsCached` |
| 페이지가 **자기 토큰을 들고 나간다** | `ThePageCarriesItsOwnToken` |

## 2. 유도 가드 — 두 벌이 갈리는 것을 막는다

목록을 두 벌 적으면 안 재지는 쪽이 갈린다. 그래서 이 층은 **원천에서 유도한다.**

| 가드 | 무엇을 유도하나 | 시험 |
|---|---|---|
| 이름 넷은 한 문자열 | 매니페스트 XML 실물을 읽어 바인드·SAN·전송과 대조 | `TheFourNamesAreOneString` — 처음 돌렸을 때 매니페스트의 여덟 자리를 짚었다 |
| 애드인은 오리진을 **안 적는다** | `addin/` 전체를 훑는다 | `TheAddinDoesNotWriteTheOriginDown` |
| 매니페스트 스키마 순서 | XML 주석을 걷어 내고 `<Requirements>` 가 `<Hosts>` 앞인지 | `TheManifestKeepsTheSchemaOrder` |
| 허용 규칙은 「덱을 고치는가」로 갈린다 | 도구 표에서 규칙을 **만들어** 대조 | `AllowRulesCoverExactlyWhatDoesNotChangeTheDeck` |
| 스키마가 인자 검사를 켜 둔다 | 도구 27개의 `properties`·`required`·`additionalProperties` | `EverySchemaKeepsTheArgumentCheckOn` |
| 도구 이름은 sanitize 를 견딘다 | magi 의 이름 규칙에 태워 본다 | `ToolNamesSurviveSanitizing`·`TheServerNameSanitizesToItself` |
| 서버 이름이 **내장 도구 이름과 안 겹친다** | 코어의 내장 목록과 대조 | `TheServerNameIsNotABuiltinToolName` |
| 읽기 도구도 **끝났다고 선언하는 법**을 설명에 싣는다 | 27개 설명문 | `ReadToolsSayHowToDeclareFinished` |
| **매뉴얼이 코드가 만든 허용 규칙을 그대로 옮겼는가** | `AllowRulesTOML()` 과 매뉴얼의 `allow = […]` 덩어리를 글자로 견준다 | `TheManualQuotesTheRulesWeGenerate` |
| **매뉴얼이 도구를 하나도 안 빠뜨렸는가** | 도구 표를 훑어 매뉴얼에서 이름을 찾는다 — 표에 없는 도구는 사람에게 없는 기능이다 | `TheManualNamesEveryTool` |
| 생략했을 때의 뜻을 스키마가 적는가 | `slideProps` 와 도구 표 | `OmittingTheSlideMeansTheOneInFront` |
| 광고하지 않은 능력을 광고하지 않는다 | `set_table_cells` 는 서식을 안 건드린다 | `SetTableCellsAdvertisesNoFormatting` |

**매니페스트 순서 가드는 자기 바늘에 한 번 걸렸다.** 첫 판은 XML 주석 안의 `<Hosts>` 를 보고
울었다. 예외를 만들지 않고 **주석을 걷어 내고 보게** 고쳤다 — 스캐너가 제 바늘에 걸리는 것을
예외로 빼면 그 예외가 진짜 위반도 같이 가려 준다.

## 3. 상호운용 — 가짜 상대는 상대를 검사하지 않는다

**`MagisOwnClientAttachesToThisHelper`** 는 magi 의 진짜 MCP 클라이언트(`mcp.Manager.Attach`)를
이 헬퍼에 붙인다. 가짜 상대로 도는 시험은 **내가 상대를 어떻게 이해했는지**를 검사하지 상대를
검사하지 않는다 — 스펙이 MUST 로 적은 202 를 magi 가 유일하게 거절하던 시절, 스펙대로 만든
서버만 쫓겨났다.

**`attach_test.go`** 는 한 걸음 더 간다: `daemon.Listen`+`Serve` 로 **진짜 데몬을 세우고**
`mcp-detach`→`mcp-attach` 를 왕복한다.

| 규칙 | 시험 |
|---|---|
| 붙기 전에 **언제나 detach** | `AttachingAlwaysDetachesFirst` |
| 둘째 창이 첫째의 등록을 **안 뺏는다** | `ASecondWindowDoesNotStealTheFirstsRegistration` |
| 문 없는 컴패니언은 **못 고른다** | `ACompanionWithNoDoorCannotBeChosen` |
| 「못 물어봤다」 ≠ 「못 받는다」 | `NotAskedIsNotTheSameAsCannot` |
| 붙인 주소가 **우리가 내주는 그 주소**다 | `TheAttachedURLIsTheOneWeServe` |
| **진짜 데몬이 정말 하나는 섰다** | `ZZZAtLeastOneRealDaemonWasStarted` |

마지막 줄이 이 층의 계측기다. 데몬을 못 세우면 나머지 시험들이 조용히 「건너뜀」이 되는데, 그
스위트는 여전히 초록이다. **`ZZZ` 접두는 마지막에 돌라는 뜻이고**, 그 하나가 「이 층은 실제로
돌았다」를 증언한다.

## 4. 화면 규칙 — 값으로 재는 자리

작업창의 결정은 전부 `addin/src/ui/screen.js` 와 `src/domain/*` 에 있다. **DOM 을 안 만지고 값만
답하므로 잴 수 있다.** 뷰(`view.js`)가 하는 일은 부르고 대입하는 것뿐이다.

> 이 갈래는 실측에서 나왔다. 뷰에 심은 돌연변이 32개 중 **30개가 살아남았고**, 살아남은 줄은
> 거의 전부 DOM 코드 **안에 박힌 결정**이었다. 가짜 DOM 을 지어 재는 자리를 늘리는 것보다
> 결정을 재지는 자리로 옮기는 것이 쌌다.

`node tools/smoke.mjs` (502) 이 무는 것 중 몇 가지:

| 규칙 | 왜 |
|---|---|
| **대화 이름을 우리가 짓지 않는다** | 지어낸 이름에 붙으면 진짜 이벤트가 신원 그물에 전부 걸린다. 실물에서 그 화면을 봤다 — 덱은 바뀌었는데 창은 「보냈습니다」에 멈춰 있었다 |
| 남의 대화 이벤트는 안 섞인다 | 신원이 다른 이벤트를 조용히 섞으면 두 컴패니언의 로그가 한 화면에 선다 |
| 답은 **호출한 줄에** 접힌다 (`callId` 로) | 이름으로 짝지으면 세 번째 호출의 답이 첫 번째 줄에 붙는다 |
| **`isError` 하나로 ✗ 를 안 찍는다** | `advisory` 는 「했는데 읽을 것이 붙었다」다. 창 둘이 성공한 쓰기를 실패로 그린 적이 있다 |
| 짝 없는 답도 **줄이 선다** | 로그 중간부터 읽기 시작했다는 사실이다 |
| 말 없는 표를 **기권으로 안 적는다** | `CouncilVerdictData.Silent` — 백엔드가 죽은 것과 판단한 기권은 다르다 |
| 모르는 종류를 **안 버린다** | 버리면 화면이 「아무 일도 없었다」처럼 보인다 |
| **안 그리기로 한 것**과 **못 그리는 것**은 다른 줄 | 한 문장으로 합치면 「고칠 게 있다」와 「이대로가 맞다」가 같은 말이 된다 |
| `prompt.submitted` 는 **배우를 보고** 그린다 | 안 보면 정책이 한 일이 사용자 말풍선으로 붙고, 사용자 줄 수가 늘어 **사람이 적던 글이 지워진다** |
| 델타와 완성본은 **같은 줄** | 둘 다 쌓으면 모델의 답이 두 번 뜬다 |
| 붙기 전의 창은 **「고장 났다」고 말하지 않는다** | 고르라는 화면 위에 「데몬에 안 닿습니다」가 겹쳐 뜨면 사람은 고르기 전에 고장 난 줄 안다 |
| 끊김과 **되살아남**을 둘 다 알린다 | 죽음만 알리면 화면은 한 번 끊긴 뒤로 영영 끊긴 채다 |
| 검증 못 한 착지를 **보통 끝처럼 안 그린다** | 덱을 그대로 발표하느냐 열어 보느냐를 가른다 |
| 빈 선택과 **포커스가 가져간 선택**을 가른다 | 누르기 **전** 읽기가 있어야 갈린다 |
| 안내는 도구가 **광고한 철자**로 읽는다 | 철자를 `helper/tools.go` 에서 뽑아 먹인다 — 손으로 적으면 이 시험도 두 벌 중 하나가 된다 |
| 막힌 물음은 **화면 안으로 끌어온다** | 대화 아래에 선 칸은 접힌 자리 밖이다. 답할 것이 없는 칸(못 닿음·직전 물음)은 안 끌어온다 — 읽던 자리를 뺏을 이유가 없다 |
| 컴패니언이 **다시 뜬 것**은 「닿는다」와 다르다 | 그 사실이 바뀌는 순간에만 종을 치고, 조립 자리는 몰래 다시 안 붙인다 — 다시 붙이는 것은 사람이 하는 말이다 |

`tools/officehand.mjs` (164) 는 `PowerPoint.run` 을 흉내 낸 stub 위에서 **우리가 고른 가지**를
잰다 — 1.8 이 없으면 index 를 안 묻는가, 빈 선택은 왕복 한 번인가, 글을 잃어도 신원은 사는가.
**그건 호스트가 아니다.** 호스트가 실제로 어떻게 답하는지는 5층의 일이다.

`tools/smoke-hand.mjs` (36) 는 손의 서빙(`ServeHand`)과 스트림 어댑터를 잰다.

### 4.1 이 스위트가 스스로에게 거는 규율

- **빈 것에 참을 주지 않는다.** `[].every(f)` 는 늘 참이라, 훑을 것이 없는 단언은 술어가
  무엇이든 초록이다. 이 파일에 그런 줄이 실제로 서 있었다(번호 붙은 슬라이드가 없는 판에서
  번호표를 훑었다). 그래서 `everyOf` 는 **길이가 0 이면 거짓**이고, 「없다」를 물을 때는
  `.length === 0` 으로 묻는다.
- **`!== null` 은 화면이 쓰는 물음이 아니다.** 화면은 `if (v.refusal)` 로 읽으므로 빈 문자열이면
  아무 줄도 안 그리는데 `!== null` 은 초록이다. 그 틈으로 「시험에서만 참인 상태」가 지나간다.
- **예를 지키는 시험이 되지 않게 한다.** `council.verdict` 와 `tool-result` 는 오랫동안 「모르는
  종류」의 예였는데 이제 그린다. 예가 그려지는 날 그 시험은 **규칙이 아니라 예를 지키는 것**이
  되므로, 아직 진짜로 안 그리는 종류로 갈아 끼웠다.
- **필드 드롭 계측.** 값을 통째로 비워도 스위트가 조용하면 그 칸은 아무도 안 읽는 것이다. 그렇게
  찾아낸 죽은 칸이 여럿 있었고(`Row.type`/`ts`, `view.cursor`), 지웠다.

## 5. 실물 — PowerPoint 와 사람의 손

**이 층은 자동화가 없다.** Office 애드인을 창 없이 세우는 길이 이 저장소에는 없고, 사람이
리본을 눌러 확인한다. 그래서 **점검표로 관리한다.**

### 5.1 실물에서 나온 것들 — 2026-09-01(열둘)과 2026-09-02(아홉)

2026-09-01 이 이 애드인이 **PowerPoint 에 처음 붙은 날**이다. 헬퍼 61개와 JS 500여 개가 전부
초록인 상태에서 붙였고, 그날 열둘이 나왔다. 이튿날 에이전트가 **장을 만들 수 있게 되자** 둘이
더 나왔다(표 아래 여섯) — 새 기능이 새 슬라이드를 만들자마자 그 슬라이드를 읽는 길이 막혀
있던 것이 드러났고, 도구를 전수로 불러 보자 둘이 더 나왔다. 아래 스물하나는 전부 **자동 시험이 하나도 못 잡던 것**이고, 지금은 전부 물려 있다.

| 실물에서 본 것 | 무엇이었나 | 지금 무는 자리 |
|---|---|---|
| 리본에 애드인이 **아예 없다** (오류도 없음) | `VersionOverrides` 안 `<Requirements>` 가 `<Hosts>` 뒤 | `TheManifestKeepsTheSchemaOrder` |
| 슬라이드 목록이 비었는데 `total: 2` | `count: 0` 을 「없음」으로 읽었다 | `args_test.go` 의 0 거절 |
| 노란 배너 둘이 겹쳐 뜸(고르기도 전에) | 안 붙었는데 「데몬에 안 닿는다」·「스트림 끊김」 | smoke: 「붙기 전의 창은 고장 났다고 말하지 않는다」 |
| 스트림이 살아났는데 화면은 끊긴 채 | 죽음만 알리고 되살아남을 안 알렸다 | smoke-hand: 「끊김과 되살아남을 둘 다 알린다」 |
| 「보내기」가 화면 밖으로 밀림 | 판이 늘 때마다 컴포저가 밀렸다 | (CSS — 사람 눈) |
| 판 가운데가 **통째로 사라짐** | `.scroll` 이 `#pane > *` 에 특정도로 졌다 | (CSS — 사람 눈) |
| 덱은 바뀌었는데 창은 「보냈습니다」에 멈춤 | 창이 **지어낸 대화 이름**에 붙어 있었다 | smoke: 「지어낸 이름은 가짜 갈래에서만 쓴다」 |
| 안내 포스트잇이 **한 장도 안 눌렸다** | 스키마는 `slide_id`·`shape_ids` 로 광고하는데 접는 쪽은 낙타등만 봤다. 화면은 「어디를 가리키는지 안 실렸습니다」라고 적었고 그건 **모델을 탓하는 거짓말**이었다 | smoke: 철자를 `tools.go` 에서 뽑아 먹인다 |
| 권한 물음이 **화면 밖에** 섰다 | 물음 칸은 대화 아래에 서는데 대화가 길면 접힌 자리 밖이다 — 데몬은 답을 기다리고 사람은 물음을 못 본다 | smoke: `askReveal` |
| 320px 판에 **가로 막대**가 섰다 | 모델이 쓴 `mcp__ppt__set_text --slide-id` 같은 한 덩어리가 `pre-wrap` 으로는 안 접힌다 | smoke: CSS 규칙을 글자로 문다 |
| 데몬을 다시 띄웠는데 카드가 **「이미 붙어 있음」** | 등록을 소켓 경로로 셌다. 경로는 워크스페이스에서 유도되므로 다시 떠도 같고, 죽은 것은 프로세스다 | `TestARestartedDaemonIsNotStillAttached` |
| 그때 창은 **「대화 연결됨」**이라고 적었다 | 닿기는 닿는데 우리 등록도 이 창이 든 대화 이름도 남의 생애의 것이었다. 모델에게는 덱 도구가 하나도 없었고 셸로 우회하려 들었다 | smoke: 「다시 뜬 것을 값에 싣는다」 외 |
| 표를 만든 **바로 그 장을 못 읽었다** | 도형 목록에 `placeholderFormat` 을 같이 걸었는데, 자리표시자가 아닌 도형에 그 칸을 걸면 호스트가 묶음 전체를 죽인다(GeneralException). 표·그림·글상자가 있는 장은 전부 그랬다 | officehand: 「표·그림이 섞인 장도 읽힌다」 |
| 「3행 5열 표 만들어 줘」에 **되물었다** | 손은 진작부터 보고 있는 장으로 떨어지는데 스키마가 그 말을 안 했다 — 모델이 읽는 것은 스키마뿐이다 | `OmittingTheSlideMeansTheOneInFront` |
| 표가 **눈에 안 보였다** | `addTable` 에 칸 서식을 하나라도 주면 테마의 표 스타일이 통째로 벗겨진다. 값도 선도 없는 표는 화면에서 아무것도 아니다 — 사람이 「만들었다는데 없다」고 신고했다 | officehand: 「서식을 안 청하면 아무것도 안 넘긴다」 |
| 표를 **고쳐 달랬더니 하나 더 만들었다** | 고칠 길이 없었고(1.9), 게다가 스키마가 「못 고치니 만들 때 주라」고 가르치고 있었다 | `replace_table` 과 officehand 의 표 묶음 |
| `find_shapes` 가 통째로 죽었다 | 글로 거를 때 표·그림에도 글을 물었다 — `read_slide` 와 같은 함정 | officehand: 글틀 없는 종류는 건너뛴다 |
| `apply_layout` 이 통째로 죽었다 | 레이아웃 **id 문자열**을 넘겼는데 호스트가 거절했다 | 객체로 먼저 부르고 실패하면 id 로 — 둘 다 실패하면 사유 둘을 다 싣는다 |
| 「그릴 줄 모르는 이벤트 27건」 | 도구 답·권한·카운슬을 안 그렸다 | smoke: §4 의 결과·판정 묶음 |
| **헬퍼를 다시 띄우니 판이 영영 죽었다** — 「붙었습니다 — 도구 28개」라고 적힌 채 | 토큰은 기동마다 새로 난다. 열려 있던 페이지의 토큰은 영영 거부되고, `EventSource` 는 200 이 아닌 답에는 규격대로 **재연결을 포기한다.** 살리는 길이 파워포인트 재시작뿐이었다 | smoke-hand: 「낡은 토큰이면 창을 다시 불러온다」 외 6 — 401·200·못 물어봄을 가른다 |
| 「간격 똑같이」가 도형을 **거꾸로 겹쳐 쌓았다** — 그러고도 「고르게 했습니다」 | 폭을 맨 뒤 **도형의** 뒷모서리로 쟀다. 넓은 배너가 가운데 있으면 폭이 짧게 잡혀 틈이 음수가 된다 | officehand: 「가운데를 왼쪽으로 밀어 겹치게 놓지 않는다」·「자리가 모자라면 겹쳐 놓지 않고 말한다」 |
| 도형 쓰기를 통째로 빼도 **시험이 전부 초록**이었다 | 스텁의 `reveal` 이 `left` 를 값으로 복사만 해서, 손이 쓴 값이 view 에 붙었다 사라졌다 | 스텁이 픽스처로 써 내려간다 + officehand: 「픽스처의 자리가 실제로 바뀐다」 |

**교훈 한 줄.** 이 레인에서 자동 시험이 못 보는 것은 **호스트와 화면**이고, 그 둘이 이 제품의
얼굴이다. 층 1~4 는 「우리가 정한 것을 지키는가」를 재고, 「사람이 보는 것이 사실인가」는
여전히 5층이 잰다.

**2026-09-02 의 마지막 셋은 결이 다르다.** 앞의 것들은 실물에 붙여 보고 나온 것인데, 이 셋은
**리뷰가 읽어서** 나왔다 — 겹쳐 쌓기는 계산으로 짚은 뒤 실행으로 재현했고, 스텁 구멍은 「쓰기를
지우고 시험을 돌려 보니 전부 초록」으로 증명됐다. 5층이 못 본 것을 리뷰가 봤다는 뜻이 아니라,
**시험이 스스로 눈이 먼 자리는 시험을 돌려서는 못 찾는다**는 뜻이다. 그 자리를 찾는 방법은 지금
둘뿐이다: 실물에 붙여 보기, 그리고 다른 눈이 코드를 읽기.

### 5.2 점검표 — 판을 낼 때 손으로 돈다

**설치**
1. 매니페스트를 사이드로드하고 PowerPoint 재시작 → 홈 → 추가 기능에 `magi` 가 있다.
2. 누르면 작업창이 열리고 **흰 화면이 아니다**(인증서가 신뢰됐다).
3. 접힌 「요구 집합」 줄이 **여섯 개 모두 지원**이라고 적는다.

**붙기**
4. 데몬을 안 띄운 채 열면 「켜져 있는 컴패니언이 하나도 없습니다」와 **띄우는 법**이 뜬다.
5. 데몬을 띄우고 「다시 훑기」 → 카드에 **권한 모드와 백엔드 주소**가 그대로 적힌다.
6. 「여기에 붙이기」 → `… 에 붙었습니다 — 도구 19 개.`
7. 브랜드 줄이 `<이름> · 대화 연결됨 · 덱 1` 로 바뀐다.
8. 작업창을 껐다 켜면 **다시 안 골라도** 붙어 있는 것으로 뜬다.

**말하기**
9. 입력줄에 시키고 보내기 → 「보냈습니다」가 서고, **메아리가 오면 적은 글이 지워진다.**
10. 대화에 혼잣말·도구 호출·인자가 순서대로 선다.
11. 도구 줄에 **권한 한 줄**과 **✓/✗/⚠** 가 붙는다.
12. 카운슬이 거절하면 **⚖ 줄에 사유가** 적힌다.
13. 턴이 끝나면 `— 턴 끝 —`, 검증 못 했으면 그렇게 적힌다.

**진짜 덱이 바뀌는가** ← 이 층의 이유
14. PowerPoint 화면에서 제목이 실제로 바뀐다.
15. COM 으로 읽어도 같다:
    ```powershell
    $ppt = [Runtime.InteropServices.Marshal]::GetActiveObject("PowerPoint.Application")
    foreach ($s in $ppt.ActivePresentation.Slides) {
      foreach ($sh in $s.Shapes) {
        if ($sh.HasTextFrame -and $sh.TextFrame.HasText) { "$($s.SlideIndex): $($sh.TextFrame.TextRange.Text)" }
      }
    }
    ```
16. PowerPoint 를 껐다 켠 뒤 옛 문서 주소로 부르면 **거절당하고 지금 열린 덱을 알려 준다.**

**권한**
17. `--permission ask` 로 띄우면 덱을 고치기 전에 **작업창 안에서** 묻는다.
18. 「항상」 단추가 **세션 전체를 연다는 것**이 문구에 있다.
19. `-allow-rules` 를 넣으면 읽기 도구는 안 묻는다.

### 5.3 화면을 직접 눌러 재는 법 (Windows)

사람 대신 UI Automation 으로 리본을 눌러 작업창을 열고 화면을 캡처할 수 있다. 이 저장소가
2026-09-01 에 쓴 방법이다.

```powershell
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes
$A = [System.Windows.Automation.AutomationElement]
$T = [System.Windows.Automation.TreeScope]
$win = $A::RootElement.FindFirst($T::Children,
  (New-Object System.Windows.Automation.PropertyCondition($A::ClassNameProperty, "PPTFrameClass")))
# 홈 탭 → 추가 기능 플라이아웃 → magi
$fly = $win.FindFirst($T::Descendants,
  (New-Object System.Windows.Automation.PropertyCondition($A::AutomationIdProperty, "OfficeExtensionsShowAddinFlyout")))
```

작업창 자체는 WebView2 라 **안쪽이 접근성 트리에 안 보인다.** 그래서 좌표로 누르되, 좌표는
**작업창 사각형에서 뽑는다**(창을 옮기거나 크기를 바꿔도 안 깨진다) — 컴포저와 브랜드 줄은 판
아래에 고정이라 사각형만 알면 자리가 정해진다.

> ⚠ PowerShell 5.1 은 BOM 없는 UTF-8 파일을 ANSI 로 읽는다. 한글이 든 `.ps1` 을 다른 도구로
> 쓰면 **파서 오류**가 난다 — BOM 을 붙여야 한다.


### 5.4 전수 점검 — 도구를 하나씩 실물에 대고 불러 본다

`clients/powerpoint/tools/sweep.ps1` 이 **광고된 도구 전부**를 순서대로 부른다. 층 1~4 는 「우리가 정한 것을
지키는가」를 재고, 이 스크립트는 **호스트가 실제로 어떻게 답하는가**를 잰다.

```powershell
& C:\Users\velve\Workspace\ppt-test\sweep.ps1
```

준비(장 하나 만들기) → 읽기 → 도형 → 표 → 스타일 → 안내 → 장 다루기 순으로 돌고, 마지막에
**통과와 실패의 수**를 찍는다. 판정은 `Write-Host` 로 콘솔에 보낸다 — 파이프로 흘리면
`| Out-Null` 이 결과까지 삼켜서 「몇 개 실패」만 남고 **무엇이 실패했는지**가 사라진다(그렇게
한 번 겪었다).

**이 점검이 잡은 것들.** 층 1~4 가 전부 초록인 상태에서 돌렸고, 매번 뭔가 나왔다:

| 언제 | 무엇 |
|---|---|
| 2026-09-02 1차 | `find_shapes` 가 표·그림이 있는 덱에서 통째로 `InvalidArgument` · `apply_layout` 이 레이아웃 id 를 거절당함 |
| 2026-09-02 2차 | 스타일 도구 셋을 얹고 34항목 전수 통과 |

**되돌리기가 마지막인 것은 일부러다.** 스크립트 끝의 `restore_slide` 가 앞에서 그린 것을 전부
되돌린다 — 스냅샷이 실제로 도는지를 재는 자리이면서, 점검이 덱에 쓰레기를 안 남기는 자리다.
그래서 **눈으로 볼 것은 따로 그려야 한다**(§5.2 의 14~16번).

---

## 6. 코어 쪽에 남긴 시험

이 레인이 코어를 두 군데 고쳤고, 둘 다 이식성 문제였다.

| 무엇 | 시험 |
|---|---|
| 소켓은 **Windows 가 속성을 걸 파일이 아니다** — `chmod` 를 플랫폼 이음매 뒤로 | `internal/adapter/daemon/listen_portable_test.go` (2) |
| 죽은 데몬이 남긴 소켓 파일은 **재파스 포인트**라 보통 삭제가 안 먹는다 | 같은 파일 — ⚠ **이 케이스는 실물에서 여전히 재현된다.** 시험은 이음매가 불리는 것까지만 문다 |

두 번째 줄은 **지금 열려 있다.** 커밋 제목이 「해결했다」로 읽히게 적혔던 적이 있는데, 실제로는
손으로 지워야 하는 상황을 그 뒤에도 만났다. 안 고친 것을 고쳤다고 세지 않는다.

---

## 7. 여기서 안 재는 것

| 항목 | 왜 아직 안 재나 |
|---|---|
| 진짜 리본을 눌러 보는 자동 시험 | 작업창은 WebView2 라 접근성 트리에 안쪽이 없다. 좌표로 누르는 스크립트는 있으나(§5.3) CI 에 못 건다 |
| Mac·웹 PowerPoint | 이 머신에 없다. 요구 집합이 다를 수 있고, personality 메뉴 크기도 다르다(Win 12×32 · Mac 34×32) |
| `-race` | 이 머신에 C 툴체인이 없어 `CGO_ENABLED=1` 이 안 선다 |
| 헬퍼가 죽었을 때의 재기동 | 감독자를 안 만들었다. 두 번째 기동이 조용히 물러나는 것은 잰다 |
| 모델이 도구를 **잘 쓰는가** | 그건 코어(`internal/eval`)의 일이다. 이쪽은 도구가 규약대로 답하는가만 본다 |
| 큰 덱(수백 장)에서의 응답 시간 | 안 재 봤다. `list_slides` 의 페이징은 있으나 그 필요를 실측한 적이 없다 |

---

## 8. 가드를 새로 세울 때 묻는 셋

1. **훑는 범위가 규칙이 사는 범위와 같은가.** 매니페스트 순서 가드는 처음에 주석 안까지 봤고,
   오리진 스캔은 `addin/` 전체를 봐야 뜻이 산다.
2. **그 시험이 실제로 도는가.** `attach_test.go` 의 `ZZZ…` 가 그 물음에 답하려고 있다 — 데몬을
   못 세우면 나머지가 조용히 건너뛰는데 스위트는 초록이다.
3. **픽스처가 이름이 말하는 그것인가.** 「모르는 종류」를 이제 그리는 종류로 흉내 내면, 그
   시험은 규칙이 아니라 옛 예를 지킨다.

셋 다 **일부러 실패시켜 빨간 것을 봐야만** 확인된다.
