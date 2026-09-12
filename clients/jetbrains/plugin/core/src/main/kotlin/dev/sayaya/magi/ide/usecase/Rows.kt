package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/**
 * 전사 셰이퍼 — 이벤트 스트림을 사람이 읽는 행으로.
 *
 * 무엇을 행으로 만들고 무엇을 안 만드는지는 `docs/TRANSCRIPT.ko.md` §3 의 표가 정하고, 여기는
 * 그 표의 구현이다. 어휘는 웹 콘솔의 `clients/web/server/main.go` 의 `line` 에서 옮겼다 — 필드로
 * 나르고, 렌더된 문장에서 도로 파내지 않는다. 문장은 화면의 붓이다.
 *
 * **여기가 왜 usecase 인가.** 페이로드를 파는 것은 이 층의 일이다 — [Problems.of] 가 선례고,
 * `Wire.kt` 의 "이 층은 안 읽는다"는 전송·규칙 경계에 하는 말이다. 그리고 이 층에는 시험이
 * 산다 — 셰이퍼는 골든으로 붙들어야 하는 물건이다(`RowsTest`).
 *
 * **증분이 안전한 근거.** 컴팩션은 이벤트를 지우지 않고(`internal/app/reconstruct.go` 의
 * `reconstructWhole` — 사람 뷰는 전부 보존한다), 스트림이 시키는 변이는 재배치 둘·표시 둘·
 * 접붙임 하나뿐이다. 통째로 다시 짓는 것은 스트림 자체가 다시 시작할 때([clear])뿐이다.
 */
/**
 * 한 행이 나르는 말하는 이.
 *
 * ⚠ **여덟이고, 여섯이 아니다.** 이 열거는 오래 여섯이었고 `Info` 하나가 셋을 겸했다 — 세션이
 * 저에 대해 하는 말, **실패**, 그리고 붙은 그림. 그 셋은 화면이 다르게 그려야 하는 것이라(사람이
 * 문제를 찾을 때 훑는 것은 그중 하나뿐이다) 한 낱말에 접어 두면 **되돌릴 수가 없다** — 여덟에서
 * 여섯으로는 접히지만 여섯에서 여덟로는 못 펴진다.
 *
 * 코어의 `internal/adapter/idebridge/rows.go` 가 여덟을 정본으로 적고 있고(2026-09-10 결정),
 * VS Code 도 여덟이다. 그 두 벌만 서로를 붙들고 이 열거는 밖에 있었다 —
 * `PROJECT_REVIEW` 가 「각 클라이언트가 이벤트를 따로 해석하는 양을 줄이라」고 적은 그 자리다.
 */
enum class Who { User, Agent, Thinking, Tool, Council, System, Error, Image }

/**
 * 행 하나. 웹 `line` 의 어휘를 옮긴 순수 데이터다 — 자세한 필드 사전은 `docs/TRANSCRIPT.ko.md` §2.
 *
 * [ok] 가 nullable 인 것이 이 어휘의 핵심 구분이다: null 은 "아직 안 끝난 호출"이고 false 는
 * "실패한 호출"이다. [note] 는 셋째 갈래 — 되고 나서 읽을 것이 남은 호출(사후 훅, 린트)은
 * 와이어에선 에러로 오지만 실패로 그리면 화면이 거짓말한다(코어 `ToolResult.Advisory`).
 */
data class Row(
    val who: Who,
    val text: String,
    val at: String? = null,
    // tool 행
    val tool: String? = null,
    val args: String? = null,
    val ok: Boolean? = null,
    val note: Boolean = false,
    /** 실패한 호출이 말한 것. [args] 를 덮지 않는다 — 물은 것과 답한 것은 다른 사실이다. */
    val out: String? = null,
    // user 행 표시
    val pending: Boolean = false,
    val queued: Boolean = false,
    val abandoned: Boolean = false,
    // council 행
    val member: String? = null,
    val round: Int = 0,
    val decision: String? = null,
    /**
     * 이 자리가 **무엇으로 판단하는가**(`correctness`·`verification`·`completeness`).
     * 카운슬에 자리가 셋인 이유가 이것이라, 없으면 한 라운드의 판정 셋이 서로 바꿔 놔도 같은 글이다.
     */
    val lens: String? = null,
    /**
     * 라운드가 **무엇으로 갈리는가**(`majority`·`unanimous`·`veto:…`). 라운드가 열린 행의 것이다.
     *
     * ⚠ 한동안 이 값을 [lens] 에 담고 있었다 — 한 칸이 행에 따라 「자리의 렌즈」와 「라운드의 규칙」
     * 두 가지를 뜻했고, 그래서 화면이 **열린 행의 규칙만** 그리고 **멤버의 렌즈는 안 그렸다**.
     * 한 사실은 한 칸에 둔다(이 트리가 되풀이해 적는 규칙).
     */
    val rule: String? = null,
    val why: String? = null,
    val keep: String? = null,
    val cite: String? = null,
    /**
     * 이 멤버가 답하기 전에 **한 생각** — 프로바이더의 추론 흐름이다.
     *
     * ⚠ **표가 아니다.** 이 행의 다른 칸은 전부 멤버가 낸 JSON 을 읽어서 채워지는데, 이것은
     * 같은 스트림의 **다른 채널**에서 파서를 안 거치고 온다(코어 `drain`). 모델이 소리내어
     * 생각한 것이 표가 되면 안 되고, 갈라 둔 이유가 그것이다.
     *
     * 있는 이유는 **[silent] 인 표** 때문이다. 회신이 추론으로만 온 멤버는 「답이 없었다」로
     * 그려졌고, 실제로 온 수천 자는 어디에도 남지 않았다 — 왜 답이 없었는지의 유일한 증거다.
     */
    val thought: String? = null,
    /**
     * 이 멤버가 얼마나 확신했나. 0..1, 코어가 적는 대로 **자기 보고**이고 **가중된다**.
     *
     * 장식이 아니다. 집계의 `doneWeight`/`contWeight` 가 확신으로 가중한 합이라, 0.2 짜리 `done` 과
     * 0.95 짜리 `done` 은 같게 세이지 않는다. 낱말만 그리면 표는 보이고 **규칙이 그것으로 무엇을
     * 했는지는 가려진다**.
     *
     * 실측(이 기계의 로그 전량, 2026-09-10): 평결 2938 중 2879 에 실려 오고 값은 0.1~0.95 로 퍼져
     * 있다. `omitempty` 라 없으면 멤버가 아무 말도 안 한 것 — 0% 로 그리면 반대를 확신한 멤버로
     * 읽히므로 그때는 아무것도 안 그린다.
     */
    val confidence: Double? = null,
    /** 코어가 「아무도 안 준 평결」이라 표시한 것 — 본문(rationale)은 그래도 그린다. */
    val silent: Boolean = false,
    /** 이 행이 **라운드가 열렸다**는 것인가(평결이 아니라). */
    val opened: Boolean = false,
    /** 멤버들이 본 것 — 접어 둔다. 「어떻게 투표했나」 옆의 「무엇을 보고」다. */
    val evidence: String? = null,
    /**
     * 아직 흐르는 중인 조각들로 지은 행. 사실(`part.appended`)이 오면 그 자리에서 사실로
     * 덮인다 — 조각은 **새 줄이 아니라 같은 줄의 고쳐 쓰기**다(TRANSCRIPT §8 의 타자기).
     */
    val draft: Boolean = false,
    /** 셰이퍼 내부 짝맞춤 열쇠. 화면은 안 읽는다. */
    val msgId: String = "",
    val callId: String = "",
)

/**
 * 이벤트를 먹고 행 목록을 유지한다. [feed] 는 전사 워커 스레드에서, [list] 는 EDT 에서 오므로
 * 목록 접근은 잠근다 — 목록을 **복사해** 내주는 이유이기도 하다(그리는 동안 바뀌면 안 된다).
 */
class Rows {

    private val rows = mutableListOf<Row>()

    /** 열린 턴의 프롬프트 id. null 이면 턴이 닫혀 있다. */
    private var openPrompt: String? = null

    /** 턴이 열린 시각(이벤트 ts). 경과는 받는 쪽이 자기 시계로 이어 센다 — 시계 비교는 안 한다. */
    @Volatile var openedAt: String? = null
        private set

    /**
     * 열려 있는 카운슬 라운드. 판정이 서면 그 라운드, 합의가 나면 null — 터미널 머리의
     * `⚖ council rN` 칩이 아는 것과 같은 사실이다. 행이 아니라 세션의 사실이라 따로 든다.
     */
    @Volatile var councilRound: Int? = null

    /** 이 흐름이 연 소집의 수. 평결은 가장 최근 것에 속한다 — 사유는 [verdict] 의 주석. */
    private var convene = 0

    /** 멤버별 평결이 [rows] 의 어디에 있나. 사실이 프리뷰 **옆이 아니라 자리에** 내려앉게 한다. */
    private val verdictAt = mutableMapOf<String, Int>()

    /**
     * 지금 누구에게 묻고 있는가 — `council.deliberating` 이 말하는 것.
     *
     * 행이 아니라 세션의 사실이라 따로 든다(라운드와 같은 이유). 평결이 오면 그 멤버는 답한
     * 것이므로 비운다: 「묻는 중」이 답이 온 뒤에도 서 있으면 화면이 거짓말을 한다.
     */
    @Volatile var councilAsking: String? = null
        private set

    /**
     * 에이전트의 계획(TodoWrite). 행이 아니라 세션의 사실이다 — 매번 **전체**가 온다(코어
     * `TodosChangedData`: 델타를 재생하는 독자는 처음부터 틀린다). `todos.changed` 는 사실
     * 타입이라 재생에 실리므로, 다시 붙은 창도 마지막 계획을 안다. 새 문이 필요 없던 이유다.
     */
    @Volatile var todos: List<Todo> = emptyList()
        private set

    /**
     * 컨텍스트 계기. `context.usage` 는 **전이**라 재생에 안 실린다 — 다시 붙으면 다음 턴이
     * 돌 때까지 모른다이고, 그 모름은 0% 로 그리지 않는다.
     */
    @Volatile var context: Ctx? = null
        private set

    /** 지금 모델. `session.created` 가 심고 `model.changed` 가 갈아끼운다 — 둘 다 사실이라 재생된다. */
    @Volatile var model: String? = null
        private set

    val open: Boolean get() = synchronized(rows) { openPrompt != null }

    fun list(): List<Row> = synchronized(rows) { rows.toList() }

    /** 스트림이 다시 시작했다(재접속 전량 재생, 세션 이동). 처음부터 다시 짓는다. */
    fun clear(): Unit = synchronized(rows) {
        rows.clear()
        openPrompt = null
        openedAt = null
        councilRound = null
        councilAsking = null
        // 소집 셈과 평결 자리도 처음부터 — 안 지우면 새 대화의 첫 평결이 옛 대화의 행 자리에
        // 내려앉는다(이 함수의 `touched` 와 같은 사유).
        convene = 0
        verdictAt.clear()
        todos = emptyList()
        context = null
        model = null
        // 디스크 대장도 처음부터 — 세션을 갈아타면 옛 대화의 변이가 새 대화의 첫 드레인에
        // 실려 나가면 안 된다(리뷰 F2: clear 계약 위반). 변경 목록도 같은 사유.
        touched.clear()
        everTouched.clear()
        unknownDisk = false
        broadPending = false
    }

    /**
     * 이벤트 하나를 행으로 편다. 돌려주는 값은 **목록이 바뀌었는가** — 화면이 다시 그릴지 여부다.
     *
     * `part.delta` 는 **여기서 초안 행으로 받는다**(§8 타자기). 부르는 쪽은 그리는 삯만 묶는다 —
     * 스트림 소유권과 같은 자리에 두어야 "누가 거르나"가 한 곳에 남는다.
     */
    fun feed(e: LogEvent): Boolean = synchronized(rows) {
        when (e.type) {
            "prompt.submitted" -> prompt(e)
            "part.delta" -> delta(e)
            "part.appended" -> part(e)
            // ⚠ **한 사건 종류가 이 상태의 양 끝을 다 나른다.** 코어는 같은 메시지에 두 번 쓴다 —
            // 대기에 들어갈 때 `resolved:false`, 나중에 큐를 **떠날 때**(인라인 흡수·라우팅·포기)
            // `resolved:true`. 호출부 일곱 중 **다섯이 true** 다.
            //
            // 종류만 읽고 칸을 무시하면 **떠나는 순간마다 「대기 중」 표시가 붙는다.** 그리고 그것을
            // 지우는 사건(`interjection.answered`, 인라인 답) 뒤에 오므로 **틀린 말이 마지막 말이
            // 된다** — 이 트리가 되풀이해 값을 치른 「늙은 단언」 그 모양이다.
            //
            // ⚠ `omitempty` 가 붙은 Go bool 이라 **거짓은 전선에 안 나간다**: 대기하는 쪽이 «칸이
            // 아예 없는» 경우다. 그래서 「true 가 아니다」로 묻지 「false 다」로 묻지 않는다.
            //
            // 움직이는 것은 `queued` 뿐이다. 큐를 떠난 것이 답을 받은 것은 아니다 — `pending` 은
            // `interjection.answered` 와 답 자신의 것이고, 여기서 지우면 포기된 인터젝션이
            // 답받은 것으로 읽힌다.
            "interjection.deferred" -> mark(str(e, "messageId")) {
                it.copy(queued = e.data?.jsonObject?.get("resolved")?.jsonPrimitive?.content != "true")
            }
            "interjection.answered" -> answered(e)
            "prompt.abandoned" -> mark(str(e, "msgId")) { it.copy(abandoned = true, queued = false, pending = false) }
            "compaction" -> compaction(e)
            "turn.finished" -> {
                // ⚠ **확인 못 한 채 끝난 턴은 끝난 턴이 아니다.**
                //
                // 코어는 실행-증거 게이트가 확인하지 못했을 때 `unverified` 를 세운다 — 최상위
                // 턴이 산출물을 바꿨는데 **지금 판으로 통과한 독립 실행이 없다**는 뜻이라,
                // 선언된 결과가 성공이든 「불가능」이든 실행으로 뒷받침되지 않았다. 플래그를
                // 두는 이유를 코어가 제 말로 적어 두었다: "labeled UNVERIFIED rather than
                // **laundered into a confident success**".
                //
                // 이 창이 그것을 세탁하고 있었다 — 대기 표시만 지우고 아무 말도 안 했다.
                // 터미널은 플래그가 선 뒤로 줄곧 그려 왔다(`⚠ Unverified`).
                //
                // ⚠ `omitempty` 가 붙은 Go bool 이라 **거짓은 전선에 안 나간다** — 평범한 종료는
                // 칸이 아예 없는 것이고, `false` 를 기다리면 오지 않는 모양을 재게 된다.
                //
                // 어휘는 `error` 갈래와 같다 — 한 사실을 두 낱말로 적으면 안 재지는 쪽이 갈린다는
                // 이 파일의 규칙 그대로다. 글자에 표식을 박지 않는다: **무엇인지는 행이 나르고,
                // 어떻게 보이는지는 그리는 쪽이 정한다.** 마지막 행의 표식이 아니라 제 행인 것은,
                // 이 사실이 **턴**의 것이고 마지막 행은 산출물과 무관한 툴 호출일 수 있어서다.
                e.data?.jsonObject?.let { d ->
                    if (d["unverified"]?.jsonPrimitive?.content == "true") {
                        val why = d["reason"]?.jsonPrimitive?.content?.takeIf { it.isNotBlank() }
                        // ⚠ **`system` 이다, `error` 가 아니다.** 정본(`idebridge.Rows`)이 이 행을
                        // `WhoSystem` 으로 짓는다 — 확인 못 한 끝맺음은 **실패가 아니라 끝맺음에
                        // 대한 말**이고, 실패로 적으면 정말 터진 것과 같은 색으로 그려진다.
                        // 어휘를 여덟로 맞춘 커밋이 이 한 자리를 `Error` 로 옮겨 정본과 갈라 놓았다
                        // (같은 픽스처를 두 접기에 통과시켜 실측, 2026-09-12) — 낱말이 같아진 것과
                        // 같은 낱말을 **같은 사건에** 붙이는 것은 다른 사실이다.
                        rows += Row(Who.System,
                            "확인 못 함 — 이 판으로 통과한 실행이 없습니다" +
                                (why?.let { ": $it" } ?: ""), at = e.ts)
                    }
                }
                // **턴이 끝나면 고아 초안을 쓴다.** 코어에는 조각만 흘리고 사실을 안 쓰는 길이
                // 여럿이다(스핀 가드가 버린 응답, 본문으로 온 툴콜, 중단·프로바이더 에러,
                // 실패한 인터젝션 미니턴 — 리뷰가 다섯을 짚었다). 안 쓸면 붙어 있던 창에만
                // 남는 반쪽 답이 서고, 그것이 이 기능이 막으려던 갈림 그 자체다.
                sweptDraft = rows.removeAll { it.draft }
                // 경로를 모르는 변이가 이 턴에 있었으면, 이제 한 번 훑을 때다([drainDisk]).
                if (unknownDisk) { broadPending = true; unknownDisk = false }
                finished() || sweptDraft
            }
            // ⚠ **떠난 대화는 떠났다고 적는다.** 코어는 이 사실을 **떠나는 쪽** 대화에 적고 사유를
            // 함께 적어 두었다 — "what a reader of it needs is the reason its transcript stops".
            // 그리고 그 침묵의 값도: 이 줄이 없으면 「대화가 그냥 멈춘 것이고, 데몬이 죽은 것과
            // 구별되지 않는다 — 사람이 그 읽기대로 행동한다」.
            //
            // 이 창은 갈아타기를 **따라간다**(툴윈도가 다시 붙는다). 그런데 못 따라가는 자리가
            // 둘이다: **고정 탭**은 그 대화를 계속 보는 것이 존재 이유라 안 움직이고, 나중에 그
            // 대화를 다시 열면 재생으로 이 사실이 또 온다. 두 자리 모두 아무 말도 없이 끝나고
            // 있었다 — 실제 전사 하나를 두 클라이언트의 셰이퍼에 통과시켜 쟀다(2026-09-10):
            // 짝은 한 행, 이쪽은 0행.
            //
            // 어휘와 화살표는 터미널·짝과 같다. 한 사실을 세 낱말로 적으면 어느 것이 안 재지는지
            // 갈린다는 이 파일의 규칙 그대로다.
            "session.moved" -> {
                val to = str(e, "to")?.takeIf { it.isNotBlank() }
                rows += Row(Who.System,
                    "\u21E2 " + (to?.let { "컴패니언이 $it (으)로 옮겨 갔습니다" }
                        ?: "컴패니언이 다른 대화로 옮겨 갔습니다") + " — 이 대화는 여기서 끝납니다",
                    at = e.ts)
                true
            }
            "session.created" -> {
                model = e.data?.jsonObject?.get("model")?.jsonObject?.get("model")?.jsonPrimitive?.content
                false
            }
            "model.changed" -> {
                model = str(e, "model") ?: model
                false
            }
            "context.usage" -> {
                val d = e.data?.jsonObject
                context = Ctx(
                    d?.get("tokens")?.jsonPrimitive?.content?.toIntOrNull() ?: 0,
                    d?.get("window")?.jsonPrimitive?.content?.toIntOrNull() ?: 0,
                    d?.get("percent")?.jsonPrimitive?.content?.toDoubleOrNull() ?: 0.0,
                )
                false
            }
            "todos.changed" -> {
                todos = e.data?.jsonObject?.get("todos")?.jsonArray.orEmpty().mapNotNull { t ->
                    t.jsonObject.let { o ->
                        Todo(
                            o["content"]?.jsonPrimitive?.content.orEmpty(),
                            o["status"]?.jsonPrimitive?.content.orEmpty(),
                        )
                    }
                }
                false // 전사 행은 아니다 — 계획 판이 따로 읽는다
            }
            "error" -> {
                // 에러로 끝나는 턴은 turn.finished 가 안 올 수 있다 — 여기서도 쓴다.
                val swept = rows.removeAll { it.draft }
                error(e) || swept
            }
            "council.convened" -> convened(e)
            // 심의 중은 **행이 아니다.** 라운드마다 멤버 수만큼 오는 순간의 사실이라 전사에
            // 쌓으면 판정보다 시끄러워진다 — 코어도 이것만 transient 로 낸다(영속 안 됨).
            // 세션의 사실로 들고, 표시줄이 읽는다.
            "council.deliberating" -> deliberating(e)
            "council.verdict" -> verdict(e)
            "council.decided" -> decided(e)
            // 나머지는 전사가 아니라 다른 자리의 사실이다 — 표가 그렇게 정한다.
            else -> false
        }
    }

    private fun prompt(e: LogEvent): Boolean {
        // 서브에이전트 보고 주입은 소음이다 — 본문은 그 자식의 자리에 있다(터미널이 삼키는 그대로).
        if (e.actor?.kind == "agent") return false
        val d = e.data?.jsonObject ?: return false
        // ⚠ **조각은 이어 쓴 것이지 줄이 아니다.** 한 메시지의 `parts` 는 스트리밍이 쪼갠 조각이라
        // (`"fix the "` + `"failing test"`), 개행으로 이으면 사람이 안 쓴 줄바꿈이 생긴다. 정본
        // (`idebridge.partsText`)은 붙여 쓴다 — 같은 픽스처를 두 접기에 통과시켜 실측했다
        // (2026-09-12): 이쪽만 「fix the ⏎failing test」였다.
        val text = d["parts"]?.jsonArray.orEmpty()
            .mapNotNull { it.jsonObject.takeIf { p -> p["kind"]?.jsonPrimitive?.content == "text" } }
            .mapNotNull { it["text"]?.jsonPrimitive?.content }
            .joinToString("")
        if (e.actor?.kind == "system") {
            // 플래너·카운슬 노트. 안 그리면 이 화면이 헤드리스보다 덜 보여 준다(터미널의 실측).
            // 첫 줄만 — 전문은 로그에 있고, 노트가 대화를 밀어내면 안 된다.
            val who = e.actor.id ?: "system"
            rows += Row(Who.System, "⟳ $who note: ${text.lineSequence().firstOrNull().orEmpty()}", at = e.ts)
            return true
        }
        val id = d["messageId"]?.jsonPrimitive?.content.orEmpty()
        val from = d["resurfacedFrom"]?.jsonPrimitive?.content
        if (!from.isNullOrEmpty()) {
            // 대기하던 인터젝션이 제 턴으로 재부상했다. 물음이 답 위에 서도록 **그 행을 끝으로
            // 옮긴다** — 지우고 새로 만들면 표시(queued 등)의 역사가 사라진다.
            val i = rows.indexOfLast { it.who == Who.User && it.msgId == from }
            if (i >= 0) {
                val r = rows.removeAt(i).copy(text = text, queued = false, msgId = id.ifEmpty { from })
                rows += r
                begin(id.ifEmpty { from }, e.ts)
                return true
            }
        }
        rows += Row(Who.User, text, at = e.ts, msgId = id)
        begin(id, e.ts)
        return true
    }

    /** [drainDisk] 의 답 — 다시 볼 파일들과, 워크스페이스를 통째 훑으라는 신호. */
    class Disk(val paths: List<String>, val broad: Boolean)

    private var sweptDraft = false
    private val touched = mutableListOf<String>()
    private val everTouched = linkedSetOf<String>() // 이 대화에서 만진 파일 — 드레인에 안 비워진다
    private var unknownDisk = false // 경로를 모르는 변이(bash)가 이 턴에 있었다
    private var broadPending = false // …그리고 턴이 끝났다 — 한 번 훑을 때다

    /**
     * 컴패니언이 이 화면 밖에서 고친 디스크 — 뷰가 가져다 IDE 의 파일 캐시(VFS)를 깨운다.
     *
     * IDE 는 남(데몬 프로세스)이 고친 디스크를 저절로 안 본다: 재스캔은 창 포커스가 나갔다
     * 돌아올 때인데, magi 판이 IDE 안에 있으니 포커스가 안 나가고 에디터는 낡은 사본을 보여
     * 준다(라이브 실측 — 사람이 「파일이 전혀 안 고쳐졌다」고 읽었다. 고쳐져 있었다). 판정이
     * 여기(core) 있는 이유는 폭 판정과 같다 — intellij 모듈엔 시험이 없다.
     *
     * 경로를 아는 변이(edit·write·multiedit, **성공한 결과만** — 거부된 편집으로 캐시를
     * 깨우면 안 바뀐 파일을 다시 읽는 낭비다)는 그 파일만 싣고, 경로를 모르는 변이(bash)는
     * 턴이 끝날 때 한 번 훑으라는 신호로 접는다 — 매 bash 마다 통째 훑기는 큰 프로젝트에서
     * 비싸고, 턴 중간의 낡음은 다음 사실이 오면 곧 걷힌다.
     *
     * 한계(리뷰 F6b): 이 대장은 builtin 이름만 안다 — exec 를 선언한 플러그인/MCP 도구가
     * 디스크를 고치면 여기 안 실린다. 그쪽까지 접으려면 도구 선언(쓰기 능력)을 셰이퍼가
     * 알아야 하고, 그것은 새 문이라 별건이다.
     */
    fun drainDisk(): Disk = synchronized(rows) {
        // feed 와 같은 잠금이다 — 평시엔 같은 워커 스레드지만, 재접속은 워커를 갈고 갈아탄
        // 직후 옛 세션 프레임 하나가 착지할 수 있다(frame 가드의 실측 주석). 무락 읽기는
        // 그 창에서 동시 변이 + 가시성 미보장이다(리뷰 F3).
        val d = Disk(touched.toList(), broadPending)
        touched.clear()
        broadPending = false
        d
    }

    /**
     * 이 대화에서 컴패니언이 만진 파일들(성공한 edit·write·multiedit, 경로 기준 중복 제거) —
     * 파일별 diff 리뷰의 목록이다. diff 자체는 IDE 의 VCS 가 그린다(작업트리 대 HEAD):
     * 우리가 재계산하는 것이 아니라 IDE 가 이미 잘 그리는 것에 문만 단다.
     */
    fun touchedThisTurn(): List<String> = synchronized(rows) { everTouched.toList() }

    private fun noteDisk(tool: String?, args: String?) {
        when (tool) {
            "edit", "write", "multiedit" -> {
                val o = runCatching {
                    kotlinx.serialization.json.Json.parseToJsonElement(args.orEmpty())
                }.getOrNull() as? kotlinx.serialization.json.JsonObject
                val path = (o?.get("path") as? kotlinx.serialization.json.JsonPrimitive)
                    ?.takeIf { it.isString }?.content
                // 경로를 못 읽으면 안전 방향은 「모른다」다 — 안 깨우는 것이 아니라 훑는 쪽.
                if (path.isNullOrBlank()) unknownDisk = true else { touched += path; everTouched += path }
            }
            // bash_output/bash_kill 도 접는다: &-detach 된 백그라운드 프로세스는 툴 결과
            // **이후**에도 쓴다 — 이어지는 턴이 폴링만 하면 신호가 영영 안 선다(리뷰 F6a).
            "bash", "bash_input", "bash_output", "bash_kill" -> unknownDisk = true
        }
    }

    /**
     * 흐르는 조각 하나. **행을 새로 쌓지 않는다** — 같은 messageId 의 초안 행을 고쳐 쓴다.
     * 조각은 전이라 재생에 안 실리므로, 붙어 있던 창과 다시 붙은 창이 갈리지 않으려면
     * 사실이 왔을 때 초안이 **그 자리에서** 사실로 바뀌어야 한다([part] 가 그렇게 한다).
     *
     * 생각(reasoning)도 같은 대접이다: 흐르는 동안 보이고, 사실이 오면 접힌 행이 된다.
     */
    private fun delta(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        val piece = d["text"]?.jsonPrimitive?.content ?: return false
        if (piece.isEmpty()) return false
        val id = d["messageId"]?.jsonPrimitive?.content.orEmpty()
        val who = when (d["kind"]?.jsonPrimitive?.content) {
            "reasoning" -> Who.Thinking
            "text", null -> Who.Agent
            else -> return false // 도구 조각 등은 초안 행을 안 만든다 — 그 행은 호출이 짓는다
        }
        val i = rows.indexOfLast { it.draft && it.msgId == id && it.who == who }
        if (i < 0) rows += Row(who, piece, at = e.ts, msgId = id, draft = true)
        else rows[i] = rows[i].copy(text = rows[i].text + piece)
        return true
    }

    private fun part(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        val part = d["part"]?.jsonObject ?: return false
        val msg = d["messageId"]?.jsonPrimitive?.content.orEmpty()
        when (part["kind"]?.jsonPrimitive?.content) {
            "text" -> {
                // 초안이 서 있으면 **그 자리에서** 사실로 덮는다 — 조각과 사실이 같은 말이라,
                // 새 줄로 쌓으면 흐르는 동안 본 사람만 답을 두 벌 본다.
                dropDraft(msg, Who.Agent)
                rows += Row(Who.Agent, part["text"]?.jsonPrimitive?.content.orEmpty(), at = e.ts, msgId = msg)
                // 인라인로 답한 물음은 제 턴으로 재부상하지 않는다 — 그 물음 행을 이 답 위로
                // 끌어와 [물음 → 답] 짝으로 읽히게 한다(터미널의 `moveUserBlockBefore` 그대로).
                val reply = d["inReplyTo"]?.jsonPrimitive?.content
                if (!reply.isNullOrEmpty()) {
                    val q = rows.indexOfLast { it.who == Who.User && it.msgId == reply }
                    if (q >= 0 && q < rows.size - 1) rows.add(rows.size - 1, rows.removeAt(q).copy(queued = false))
                }
                settle()
            }
            "reasoning" -> {
                dropDraft(msg, Who.Thinking)
                rows += Row(Who.Thinking, part["text"]?.jsonPrimitive?.content.orEmpty(), at = e.ts, msgId = msg)
            }
            "tool-call" -> {
                val c = part["toolCall"]?.jsonObject ?: return false
                rows += Row(
                    Who.Tool, c["name"]?.jsonPrimitive?.content.orEmpty(), at = e.ts,
                    tool = c["name"]?.jsonPrimitive?.content, args = c["args"]?.toString(),
                    msgId = msg, callId = c["callId"]?.jsonPrimitive?.content.orEmpty(),
                )
            }
            "tool-result" -> {
                // 새 행이 아니다. 호출과 결과는 한 행 — 갈라 그리면 "됐나"를 아래 행을 찾아
                // 열어야 안다(웹이 계약으로 적어 둔 그 사유). 병렬 호출은 순서 밖으로 완료되므로
                // 짝은 자리가 아니라 callId 로 맞춘다.
                val r = part["toolResult"]?.jsonObject ?: return false
                val call = r["callId"]?.jsonPrimitive?.content.orEmpty()
                val i = rows.indexOfLast { it.who == Who.Tool && it.callId == call }
                if (i < 0) return false
                val isError = r["isError"]?.jsonPrimitive?.content == "true"
                val advisory = r["advisory"]?.jsonPrimitive?.content == "true"
                // 값을 읽지 표현을 읽지 않는다 — content 가 JSON 문자열일 때 toString() 은
                // 이스케이프를 남긴다([Problems.of] 가 시험으로 잡은 그 함정).
                val raw = r["content"]
                val said = ((raw as? JsonPrimitive)?.takeIf { it.isString }?.content) ?: raw?.toString().orEmpty()
                rows[i] = rows[i].copy(
                    ok = !isError || advisory, note = advisory,
                    out = if (isError && !advisory) said else null,
                )
                if (rows[i].ok == true) noteDisk(rows[i].tool, rows[i].args)
            }
            // ⚠ **폴드가 이름 대지 않는 part 종류는 빈 행이 아니라 애초에 없던 행이다** — 그리고
            // 아무것도 그렇게 말하지 않는다. 웹 콘솔이 같은 것을 겪고 그 주석을 남겼다: 그림과
            // 에러가 둘 다 로그에 닿았는데 둘 다 화면에 안 닿았다. 여기도 그 둘이 빠져 있었다.
            "image" -> {
                // 그림이 아니라 **경로**다. 이 층은 IDE 를 모르고(§1) 그림을 그리는 것은 화면의
                // 일이다 — 여는 자리는 손의 `show` 가 이미 있다. 가리킬 곳이 없으면 행도 없다:
                // 경로가 이 행의 전부다.
                val path = part["image"]?.jsonObject?.get("path")?.jsonPrimitive?.content.orEmpty()
                if (path.isBlank()) return false
                rows += Row(Who.Image, path, at = e.ts, msgId = msg)
            }
            "error" -> {
                // 사건 `error` 와 **같은 어휘로** 적는다. 한 사실을 두 낱말로 적으면 안 재지는
                // 쪽이 갈린다 — 이 파일이 되풀이해 지키는 규칙이다.
                val said = part["error"]?.jsonPrimitive?.content.orEmpty()
                if (said.isBlank()) return false
                rows += Row(Who.Error, said, at = e.ts, msgId = msg)
            }
            else -> return false
        }
        return true
    }

    /** 이 messageId 의 초안 행을 걷는다 — 사실이 그 자리를 대신한다. */
    private fun dropDraft(msg: String, who: Who) {
        val i = rows.indexOfLast { it.draft && it.msgId == msg && it.who == who }
        if (i >= 0) rows.removeAt(i)
    }

    private fun answered(e: LogEvent): Boolean {
        // 에이전트가 "내 답이 이 대기 메시지를 이미 다뤘다"고 말했다. 물음을 마지막 답 위로
        // 옮기고 — **이미 제자리여도 대기 표시는 거둔다.** 자리에 있는 것과 아직 기다리는 것은
        // 같은 말이 아니다(터미널이 실측으로 적어 둔 함정).
        val id = str(e, "messageId") ?: return false
        val q = rows.indexOfLast { it.who == Who.User && it.msgId == id }
        if (q < 0) return false
        val a = rows.indexOfLast { it.who == Who.Agent && !it.draft }
        if (a > q) {
            val r = rows.removeAt(q).copy(queued = false)
            rows.add(a - 1, r) // removeAt(q) 가 답을 한 칸 당겼다(a-1) — 그 자리에 끼우면 물음이 답 바로 위다
        } else {
            rows[q] = rows[q].copy(queued = false)
        }
        return true
    }

    private fun compaction(e: LogEvent): Boolean {
        // 지우지 않는다. 컴팩션이 접는 것은 모델에게 보낼 창이지 사람의 기록이 아니다 — 사람
        // 뷰를 모델 뷰에서 읽으면 읽던 스크롤백이 제 요약으로 바뀐다(`reconstructWhole` 의 교훈).
        val d = e.data?.jsonObject ?: return false
        val before = d["tokensBefore"]?.jsonPrimitive?.content?.toIntOrNull()
        val after = d["tokensAfter"]?.jsonPrimitive?.content?.toIntOrNull()
        // **줄어든 양을 적는다 — 사람이 빼게 두지 않는다.** 코어가 이 문장의 정본을 들고 있고
        // (`CompactionData.SizeNote`), 그것이 "backs the human-facing … line in **both the headless
        // printer and the TUI**, so the size difference is stated explicitly rather than **left for
        // the reader to subtract**" 라고 적혀 있다. 이 판만 두 수를 던져 놓고 있었다.
        //
        // ⚠ **요약이 원본보다 커지는 경우가 있다.** 코어의 `Reduction()` 은 음수를 0으로 눌러
        // 두는데(「줄인 양」이라는 이름에는 맞다) 그것으로 문장을 지으면 「−0, −0%」가 되어
        // **유일하게 눈에 띌 값이 있는 결과가 숨는다.** 그래서 그 갈래를 먼저 가른다 — 코어의
        // `SizeNote` 가 정확히 그 순서로 되어 있다.
        rows += Row(Who.System, "↯ 컨텍스트를 접었다: ~${before ?: "?"}→${after ?: "?"} tok" +
            (sizeNote(before, after)?.let { " ($it)" } ?: ""), at = e.ts)
        return true
    }

    /**
     * 접기가 줄인 양 — 코어 `CompactionData.SizeNote` 의 짝.
     *
     * 둘 중 하나라도 못 읽으면 **아무 말도 안 한다**: 모르는 것을 0으로 그리지 않는다는 이 파일의
     * 규칙 그대로다. 커진 경우를 먼저 가르는 것도 코어와 같은 순서다.
     */
    internal fun sizeNote(before: Int?, after: Int?): String? {
        if (before == null || after == null) return null
        if (after > before) return "+${after - before}, 요약이 원본보다 큽니다"
        val freed = before - after
        val pct = if (before > 0) (freed * 100 + before / 2) / before else 0
        return "−$freed, −$pct%"
    }

    private fun finished(): Boolean {
        openPrompt = null
        openedAt = null
        return settle()
    }

    private fun error(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        val recovered = d["recovered"]?.jsonPrimitive?.content == "true"
        val msg = d["message"]?.jsonPrimitive?.content.orEmpty()
        // 회복된 에러는 끝이 아니다 — 그렇게 읽은 독자 여섯이 런을 죽였던 결함이 코어에 기록돼
        // 있다. 여기서는 갈래를 글자에 싣는 것까지만 한다.
        rows += Row(Who.Error, if (recovered) "(회복됨) $msg" else msg, at = e.ts)
        return true
    }

    /**
     * 라운드가 열렸다 — 누가 앉았고 무엇으로 가르는가.
     *
     * 이 행이 없으면 화면은 **첫 평결이 올 때까지 조용하다.** 심의는 멤버마다 모델을 한 번씩
     * 부르므로 그 침묵이 수십 초다. 판이 열린 것을 안 말하면 사람은 멈춘 줄로 읽는다 —
     * 오늘 회의 판에서 고친 것과 같은 결함이고, 여기서는 아예 안 그리고 있었다.
     *
     * 멤버들이 **무엇을 보고** 판단했는지도 이 이벤트가 나른다(과제·계약·보고·도구가 한
     * 일·변경). 코어 주석이 그 용도를 이름 대어 적어 뒀고("so a UI can show what each member
     * judged, not just how they voted"), 웹은 그것을 라운드별 판으로 그린다. 여기서는 접어
     * 둔다 — 전사는 흐르는 화면이라 증거가 펼쳐진 채 서면 대화를 덮는다.
     */
    private fun convened(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        val round = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: 0
        if (round > 0) councilRound = round
        // 새 소집. 카운슬은 언제나 1회차로 열리므로 회차 번호만으로는 둘을 못 가른다 —
        // 평결을 제 라운드에 매다는 열쇠가 이것이다.
        convene += 1
        val members = (d["members"] as? kotlinx.serialization.json.JsonArray)
            ?.mapNotNull { (it as? kotlinx.serialization.json.JsonPrimitive)?.content }
            .orEmpty()
        // 증거는 코어가 실어 보낸 순서대로 잇는다 — 여기서 고르면 「멤버가 본 것」이 아니라
        // 「우리가 보여 주기로 한 것」이 된다.
        val seen = listOf("task", "plan", "report", "actions", "changes")
            .mapNotNull { k -> d[k]?.jsonPrimitive?.content?.takeIf { it.isNotBlank() }?.let { "$k: $it" } }
            .joinToString("\n\n")
        // ⚠ **빈 `changes` 는 두 가지다.** 이 턴이 아무것도 안 고쳤거나, 고쳤는데 재구성이 실패했거나.
        // 코어는 앞엣것을 `noChanges` 로 따로 말하고 터미널은 증거 판의 바로 이 자리에 적는다 —
        // "(no files changed — a read-only / answer turn)". 그 줄이 없으면 멤버들이 **무엇을 보고
        // 판단했는지** 읽는 사람이 두 경우를 못 가른다.
        //
        // 실측(이 기계의 로그 전량, 2026-09-10): 라운드 994 중 374 가 읽기 전용 턴이다 — 흔한 쪽이라
        // 더더욱, 빈 칸으로 두면 흔한 경우가 고장으로 읽힌다.
        val readOnly = d["noChanges"]?.jsonPrimitive?.content == "true"
        val evidence = listOf(seen, if (readOnly) "고친 파일 없음 — 읽기만 한 턴입니다" else "")
            .filter { it.isNotBlank() }.joinToString("\n\n")
        rows += Row(
            Who.Council,
            members.joinToString(", "),
            at = e.ts,
            round = round,
            rule = d["rule"]?.jsonPrimitive?.content,
            evidence = evidence.ifBlank { null },
            opened = true,
        )
        return true
    }

    /** 한 멤버에게 묻는 중. 행을 만들지 않는다 — 세션의 사실만 갱신한다. */
    private fun deliberating(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        d["round"]?.jsonPrimitive?.content?.toIntOrNull()?.let { councilRound = it }
        councilAsking = d["member"]?.jsonPrimitive?.content?.takeIf { it.isNotBlank() }
        // 행이 안 바뀌었으므로 false — 다시 그릴 이유가 없다(표시줄은 제 폴로 읽는다).
        return false
    }

    private fun verdict(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        councilRound = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: councilRound
        val silent = d["silent"]?.jsonPrimitive?.content == "true"
        // 답이 왔으면 그 멤버는 더 이상 「묻는 중」이 아니다.
        if (councilAsking == d["member"]?.jsonPrimitive?.content) councilAsking = null
        val row = Row(
            Who.Council,
            // 실려 온 말은 버리지 않는다(라이브 실측: 사용자가 "왜 다 '답이 없었다'야?" —
            // silent:true 인데 rationale 이 온전한 평결이 왔고, 셰이퍼가 말을 버리고 낙하
            // 문구만 그렸다). 코어 정의상 silent 는 "아무도 안 준 평결"이고 그때도 코어가
            // 사유 문장을 rationale 에 싣는다(llm/council.go) — 그러니 rationale 이 있으면
            // 그것이 항상 옳은 본문이고, 낙하 문구는 정말 빈 때만이다. 플래그를 잘못 세우는
            // 생산자가 있어도(데몬 결함 후보로 접수) 이 렌더는 거짓말을 안 한다.
            d["rationale"]?.jsonPrimitive?.content?.takeIf { it.isNotBlank() }
                ?: if (silent) "답이 없었다" else "",
            at = e.ts,
            member = d["member"]?.jsonPrimitive?.content,
            round = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: 0,
            decision = d["decision"]?.jsonPrimitive?.content,
            lens = d["lens"]?.jsonPrimitive?.content,
            keep = d["keep"]?.jsonPrimitive?.content,
            thought = d["thought"]?.jsonPrimitive?.content,
            cite = d["cite"]?.jsonPrimitive?.content,
            confidence = d["confidence"]?.jsonPrimitive?.content?.toDoubleOrNull()?.takeIf { it > 0 },
            silent = silent,
        )
        // ⚠ **프리뷰와 그것이 되는 사실은 한 행이다.**
        //
        // 코어는 라운드를 **오는 대로** 보여 준다 — 평결이 도착할 때마다 버스에 올려서, 사람이
        // 「3 중 3 답함」을 90초 동안 보고 있지 않게 한다 — 그러고 나서 같은 평결을 사실로 쓴다.
        // 전이 사건에는 seq 가 없으므로, 둘 다 쌓는 셰이퍼는 3인 카운슬을 **6행**으로 그렸다
        // (실측 2026-09-11, 이 셰이퍼와 VS Code 셰이퍼 둘 다 6행).
        //
        // 사실이 프리뷰 **자리에** 내려앉는다. 결정까지 바뀐다 — 반박 라운드가 표를 움직이므로
        // 프리뷰를 옆에 남겨 두면 카운슬이 저 자신과 안 맞는 것으로 보인다
        // (`docs/CLIENT_LIFECYCLE` §6).
        //
        // ⚠ **열쇠는 회차가 아니라 소집이다.** 회차 번호는 되풀이된다 — 카운슬은 언제나 1회차로
        // 열린다 — 그래서 한 대화의 두 소집이 서로에게 내려앉는다. [convene] 은 이 흐름이 연
        // 소집의 수이고, 전선이 안 실어도 흐름이 아는 사실이다.
        val key = "$convene/${row.round}/${row.member}"
        val seen = verdictAt[key]
        when {
            seen != null && e.seq > 0 -> rows[seen] = row
            seen == null -> {
                verdictAt[key] = rows.size
                rows += row
            }
            // 사실 뒤에 온 프리뷰(seq 0)는 버린다 — 로그가 든 것보다 새로울 수가 없다.
        }
        return true
    }

    /**
     * ⚠ **`note` 만으로는 반려와 「아무도 못 닿은 카운슬」을 못 가른다.**
     *
     * 코어의 `note` 는 완료를 받아들였는지로 고르는 **고정 문장 둘**뿐이라 누가 투표했는지 말하지
     * 않는다. 그래서 셋이 읽고 반대한 라운드와 셋 다 안 닿은 라운드가 같은 문장에 같은 `continue`
     * 로 와서 양쪽 다 「반려」로 그려졌다. 이슈 #182 가 정확히 두 번째 상태에 갇힌 실행이다 —
     * 매 라운드 3× 침묵 기권.
     *
     * 코어가 집계를 싣는 이유와 뭉갤 때의 값을 적어 두었다: *"a round nobody voted in is NOT a
     * rejection … saying 'reject' claims the members read the work and turned it down — the exact
     * opposite of what happened, and the reader's next move is different: fix the backend, not the
     * work."* 터미널은 집계가 생긴 뒤로 줄곧 갈라 그려 왔고, 이 창은 한 번도 안 읽었다.
     *
     * `silent` 은 **고른 기권이 아니라 실패한 기권**이라 말한 기권은 `abstain - silent` 다 — 둘 다
     * 날로 적으면 죽은 멤버를 두 번 센다.
     *
     * ⚠ 집계가 실제로 있을 때만 적는다. 한 갈래(글자까지 같은 재선언)는 집계 없이 이 사실을
     * 보내고, 그 밑의 「0 done / 0 continue」는 아무도 안 센 수다.
     */
    private fun decided(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        councilRound = null // 합의가 라운드를 닫는다
        councilAsking = null
        val t = d["tally"]?.jsonObject
        fun n(k: String) = t?.get(k)?.jsonPrimitive?.content?.toIntOrNull() ?: 0
        val said = n("abstain") - n("silent")
        val counts = if (t == null) "" else listOf(
            "${n("done")} done", "${n("continue")} continue",
            if (said > 0) "$said abstain" else "",
            if (n("silent") > 0) "${n("silent")} 답 없음" else "",
        ).filter { it.isNotBlank() }.joinToString(" / ")
        // ⚠ **반박 라운드 옆의 집계는 그 «뒤에» 잰 것이다.**
        //
        // 반박은 독립 투표가 갈렸을 때만 돈다. 그래서 2-1로 시작한 3-0이 여기서는 처음부터 없던
        // 합의로 읽힌다 — 코어가 그렇게 적고, 아무도 안 움직인 반박도 말해야 하는 이유를 덧붙였다:
        // *"that the members heard each other and did not budge is the more interesting outcome"*.
        //
        // 이 칸이 왜 생겼는지도 코어가 적어 두었다. 어댑터는 줄곧 "for observability" 로 계산했고
        // 아무도 안 봤다 — 그래서 "whether the round changes anything was therefore unanswerable
        // from a run — which is the only way to know if it earns its three extra calls". 터미널과 웹
        // 콘솔은 그려 왔고, 두 IDE 클라이언트만 안 읽었다.
        //
        // 흔한 경우(독립 투표가 이미 만장일치이거나 논쟁이 꺼짐)에는 안 실려 오므로 그때는 아무 말도
        // 안 한다 — 매 라운드에 「논쟁 없음」이 서면 정작 중요한 줄이 묻힌다.
        val db = d["debate"]?.jsonObject
        val debated = db?.let {
            val changed = it["changed"]?.jsonPrimitive?.content?.toIntOrNull() ?: 0
            val moved = if (changed == 1) "1 member" else "$changed members"
            val before = it["before"]?.jsonPrimitive?.content.orEmpty()
            val after = it["after"]?.jsonPrimitive?.content.orEmpty()
            when {
                changed == 0 -> "debated, no one moved"
                before != after -> "debated: $before\u2192$after, $moved moved"
                else -> "debated: $after held, $moved moved"
            }
        }.orEmpty()
        val note = d["note"]?.jsonPrimitive?.content.orEmpty()
        rows += Row(
            Who.Council,
            listOf(note, counts, debated).filter { it.isNotBlank() }.joinToString(" — "),
            at = e.ts,
            round = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: 0,
            decision = d["decision"]?.jsonPrimitive?.content,
            // 아무도 안 쟀다. 평결 하나가 쓰는 그 낱말을 그대로 쓴다 — 한 사실을 한 화면에서 두
            // 낱말로 적지 않는다.
            silent = t != null && n("voters") == 0 && n("silent") > 0,
            // continue 를 지금 붙들고 있는 반대가 무엇인지. 항목 필드가 아니라 why 에 싣는 것은
            // 1판의 축약이다 — 어휘에는 feedback 이 따로 있다(docs/TRANSCRIPT.ko.md §2).
            why = d["feedback"]?.jsonPrimitive?.content,
        )
        return true
    }

    private fun begin(id: String, ts: String?) {
        openPrompt = id.ifEmpty { "?" }
        openedAt = ts
        settle()
    }

    /**
     * pending 을 다시 매긴다 — 턴이 열려 있을 때 **답 없는 마지막 물음** 하나만. 행의 사실이라
     * 행에 싣고, 턴 경과는 세션의 사실이라 행에 안 싣는다(웹이 프레임을 가른 그 사유).
     */
    private fun settle(): Boolean {
        val last = rows.indexOfLast { it.who == Who.User }
        // 초안은 답이 아니다 — 흐르는 중인 줄로 대기 표시를 걷으면, 사실이 영영 안 오는
        // 턴에서 「답이 왔다」가 거짓으로 남는다(리뷰 F2).
        val answered = rows.indexOfLast { it.who == Who.Agent && !it.draft } > last
        var changed = false
        rows.forEachIndexed { i, r ->
            if (r.who != Who.User) return@forEachIndexed
            val want = openPrompt != null && i == last && !answered && !r.abandoned
            if (r.pending != want) {
                rows[i] = r.copy(pending = want)
                changed = true
            }
        }
        return changed
    }

    private fun mark(id: String?, f: (Row) -> Row): Boolean {
        if (id.isNullOrEmpty()) return false
        val i = rows.indexOfLast { it.who == Who.User && it.msgId == id }
        if (i < 0) return false
        rows[i] = f(rows[i])
        return true
    }

    /**
     * 「인자가 전체 진실」인 편집 판정 — diff 나란히-보기(승인·전사)가 공유하는 **한 벌**이다.
     * 두 벌로 적혔던 동안 데몬의 FlexBool 관용("yes"·"on"·1 도 참)과 갈라져, replaceAll:"yes"
     * 인 전-출현 치환이 단일 치환 두 면으로 그려질 뻔했다(리뷰 실측) — 규칙을 두 벌 적으면
     * 안 재지는 쪽이 갈라진다. 여기는 core 라 골든이 붙는다(`RowsTest`).
     *
     * 안전 방향은 **결손**이다: 모르는 모양(비문자 at, 낯선 replaceAll 값)은 그리지 않는다 —
     * 안 그린 것은 정직한 미표시고, 잘못 그린 두 면은 금지된 왜곡이다.
     */
    object EditSides {
        /** 데몬 FlexBool 의 참 모양들(common.go) — 이 밖의 낯선 값도 결손 쪽으로 접는다. */
        private val truthy = setOf("true", "True", "yes", "on", "1")

        fun of(tool: String?, argsJson: String?): Triple<String, String, String>? {
            if (tool != "edit") return null // 별칭 철자는 결손 — 정확한 builtin 이름만
            val o = runCatching {
                kotlinx.serialization.json.Json.parseToJsonElement(argsJson ?: return null)
            }.getOrNull() as? kotlinx.serialization.json.JsonObject ?: return null
            fun prim(k: String) = o[k] as? kotlinx.serialization.json.JsonPrimitive
            // at 는 문자열이든 아니든 내용이 비지 않으면 앵커다 — isString 게이트로 숫자 at 가
            // 빠져나가던 구멍(리뷰 F4)을 여기서 같이 막는다. 데몬의 TrimSpace 와 같은 셈.
            if (!prim("at")?.content?.trim().isNullOrEmpty()) return null
            prim("replaceAll")?.content?.let { if (it in truthy || it !in setOf("false", "False", "no", "off", "0", "")) {
                if (it in truthy) return null
                // 낯선 값: 데몬 FlexBool 은 에러(호출 실패)지만, 판정은 결손 쪽으로.
                return null
            } }
            val old = prim("old")?.takeIf { it.isString }?.content ?: return null
            val new = prim("new")?.takeIf { it.isString }?.content ?: return null
            val path = prim("path")?.takeIf { it.isString }?.content?.substringAfterLast('/') ?: "변경"
            return Triple(path, old, new)
        }
    }

    /** 계획 한 칸. status 는 코어 낱말 그대로다: pending | in_progress | completed. */
    data class Todo(val content: String, val status: String)

    /** 컨텍스트 계기 한 벌 — 코어 `ContextUsageData` 의 셋. */
    data class Ctx(
        val tokens: Int,
        val window: Int,
        val percent: Double,
        /** 창을 무엇이 채우나 — 문에서만 온다(스트림의 `context.usage` 는 총량만 싣는다). */
        val parts: dev.sayaya.magi.ide.model.ContextParts? = null,
        /** 마지막 접기가 남겨 둔 주제들 — 문에서만 온다. */
        val topics: List<String>? = null,
        /** 몇 번 접혔나 — 문에서만 온다. 0 은 「아직 안 접혔다」이고 모름이 아니다. */
        val compactions: Int = 0,
    )

    private fun str(e: LogEvent, key: String): String? =
        e.data?.jsonObject?.get(key)?.jsonPrimitive?.content
}
