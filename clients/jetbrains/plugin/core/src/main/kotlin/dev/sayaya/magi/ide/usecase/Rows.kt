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
 * 그 표의 구현이다. 어휘는 웹 콘솔의 `cmd/magi-web/main.go` 의 `line` 에서 옮겼다 — 필드로
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
enum class Who { User, Agent, Thinking, Tool, Council, Info }

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
    val lens: String? = null,
    val why: String? = null,
    val keep: String? = null,
    val cite: String? = null,
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
        todos = emptyList()
        context = null
        model = null
        // 디스크 대장도 처음부터 — 세션을 갈아타면 옛 대화의 변이가 새 대화의 첫 드레인에
        // 실려 나가면 안 된다(리뷰 F2: clear 계약 위반).
        touched.clear()
        unknownDisk = false
        broadPending = false
    }

    /**
     * 이벤트 하나를 행으로 편다. 돌려주는 값은 **목록이 바뀌었는가** — 화면이 다시 그릴지 여부다.
     *
     * `part.delta` 를 거르는 것은 여기가 아니라 부르는 쪽이다([Transcript.echoesFact] 를 쓴다) —
     * 스트림 소유권과 같은 자리에 두어야 "누가 거르나"가 한 곳에 남는다.
     */
    fun feed(e: LogEvent): Boolean = synchronized(rows) {
        when (e.type) {
            "prompt.submitted" -> prompt(e)
            "part.appended" -> part(e)
            "interjection.deferred" -> mark(str(e, "messageId")) { it.copy(queued = true) }
            "interjection.answered" -> answered(e)
            "prompt.abandoned" -> mark(str(e, "msgId")) { it.copy(abandoned = true, queued = false, pending = false) }
            "compaction" -> compaction(e)
            "turn.finished" -> {
                // 경로를 모르는 변이가 이 턴에 있었으면, 이제 한 번 훑을 때다([drainDisk]).
                if (unknownDisk) { broadPending = true; unknownDisk = false }
                finished()
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
            "error" -> error(e)
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
        val text = d["parts"]?.jsonArray.orEmpty()
            .mapNotNull { it.jsonObject.takeIf { p -> p["kind"]?.jsonPrimitive?.content == "text" } }
            .mapNotNull { it["text"]?.jsonPrimitive?.content }
            .joinToString("\n")
        if (e.actor?.kind == "system") {
            // 플래너·카운슬 노트. 안 그리면 이 화면이 헤드리스보다 덜 보여 준다(터미널의 실측).
            // 첫 줄만 — 전문은 로그에 있고, 노트가 대화를 밀어내면 안 된다.
            val who = e.actor.id ?: e.actor.name ?: "system"
            rows += Row(Who.Info, "⟳ $who note: ${text.lineSequence().firstOrNull().orEmpty()}", at = e.ts)
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

    private val touched = mutableListOf<String>()
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

    private fun noteDisk(tool: String?, args: String?) {
        when (tool) {
            "edit", "write", "multiedit" -> {
                val o = runCatching {
                    kotlinx.serialization.json.Json.parseToJsonElement(args.orEmpty())
                }.getOrNull() as? kotlinx.serialization.json.JsonObject
                val path = (o?.get("path") as? kotlinx.serialization.json.JsonPrimitive)
                    ?.takeIf { it.isString }?.content
                // 경로를 못 읽으면 안전 방향은 「모른다」다 — 안 깨우는 것이 아니라 훑는 쪽.
                if (path.isNullOrBlank()) unknownDisk = true else touched += path
            }
            // bash_output/bash_kill 도 접는다: &-detach 된 백그라운드 프로세스는 툴 결과
            // **이후**에도 쓴다 — 이어지는 턴이 폴링만 하면 신호가 영영 안 선다(리뷰 F6a).
            "bash", "bash_input", "bash_output", "bash_kill" -> unknownDisk = true
        }
    }

    private fun part(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        val part = d["part"]?.jsonObject ?: return false
        val msg = d["messageId"]?.jsonPrimitive?.content.orEmpty()
        when (part["kind"]?.jsonPrimitive?.content) {
            "text" -> {
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
            "reasoning" -> rows += Row(Who.Thinking, part["text"]?.jsonPrimitive?.content.orEmpty(), at = e.ts, msgId = msg)
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
            else -> return false
        }
        return true
    }

    private fun answered(e: LogEvent): Boolean {
        // 에이전트가 "내 답이 이 대기 메시지를 이미 다뤘다"고 말했다. 물음을 마지막 답 위로
        // 옮기고 — **이미 제자리여도 대기 표시는 거둔다.** 자리에 있는 것과 아직 기다리는 것은
        // 같은 말이 아니다(터미널이 실측으로 적어 둔 함정).
        val id = str(e, "messageId") ?: return false
        val q = rows.indexOfLast { it.who == Who.User && it.msgId == id }
        if (q < 0) return false
        val a = rows.indexOfLast { it.who == Who.Agent }
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
        val before = d["tokensBefore"]?.jsonPrimitive?.content ?: "?"
        val after = d["tokensAfter"]?.jsonPrimitive?.content ?: "?"
        rows += Row(Who.Info, "↯ 컨텍스트를 접었다: ~$before→$after tok", at = e.ts)
        return true
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
        rows += Row(Who.Info, if (recovered) "⚠ (회복됨) $msg" else "⚠ $msg", at = e.ts)
        return true
    }

    private fun verdict(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        councilRound = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: councilRound
        val silent = d["silent"]?.jsonPrimitive?.content == "true"
        rows += Row(
            Who.Council,
            if (silent) "답이 없었다" else d["rationale"]?.jsonPrimitive?.content.orEmpty(),
            at = e.ts,
            member = d["member"]?.jsonPrimitive?.content,
            round = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: 0,
            decision = d["decision"]?.jsonPrimitive?.content,
            lens = d["lens"]?.jsonPrimitive?.content,
            keep = d["keep"]?.jsonPrimitive?.content,
            cite = d["cite"]?.jsonPrimitive?.content,
        )
        return true
    }

    private fun decided(e: LogEvent): Boolean {
        val d = e.data?.jsonObject ?: return false
        councilRound = null // 합의가 라운드를 닫는다
        rows += Row(
            Who.Council,
            d["note"]?.jsonPrimitive?.content.orEmpty(),
            at = e.ts,
            round = d["round"]?.jsonPrimitive?.content?.toIntOrNull() ?: 0,
            decision = d["decision"]?.jsonPrimitive?.content,
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
        val answered = rows.indexOfLast { it.who == Who.Agent } > last
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
    data class Ctx(val tokens: Int, val window: Int, val percent: Double)

    private fun str(e: LogEvent, key: String): String? =
        e.data?.jsonObject?.get(key)?.jsonPrimitive?.content
}
