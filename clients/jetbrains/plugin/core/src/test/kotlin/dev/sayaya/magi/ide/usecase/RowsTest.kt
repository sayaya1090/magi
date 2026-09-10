package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Actor
import dev.sayaya.magi.ide.model.LogEvent
import kotlinx.serialization.json.Json
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 셰이퍼의 골든. 무엇을 붙드는지는 `docs/TRANSCRIPT.ko.md` §7 이 정한다 — 다섯 벌:
 * 몸통 · 호출+결과 한 행 · delta 무행 · 재배치는 행을 안 늘린다 · 컴팩션은 안 지운다.
 *
 * 첫째가 첫째인 이유: 이 시험들이 생기기 전 화면은 `#seq type (actor)` 만 적어도 초록이었다.
 * 사람이 친 글도 컴패니언의 답도 화면에 없는 채로 — 살아 있는 샌드박스에서 실측한 구멍이다.
 */
class RowsTest {

    private var seq = 0L

    private fun ev(type: String, data: String, actor: Actor? = null) = LogEvent(
        seq = ++seq, type = type, actor = actor, ts = "2026-08-29T10:0$seq:00Z",
        data = Json.parseToJsonElement(data),
    )

    private fun user(text: String, id: String, extra: String = "") = ev(
        "prompt.submitted",
        """{"messageId":"$id","parts":[{"kind":"text","text":"$text"}]$extra}""",
        Actor(kind = "user"),
    )

    private fun answer(text: String, id: String = "a1") =
        ev("part.appended", """{"messageId":"$id","role":"assistant","part":{"kind":"text","text":"$text"}}""")

    /**
     * **확인 못 한 채 끝난 턴은 끝난 턴이 아니다.**
     *
     * `TurnFinishedData.Unverified` 는 실행-증거 게이트가 확인하지 못한 종료다 — 최상위 턴이
     * 산출물을 바꿨는데 **지금 판으로 통과한 독립 실행이 없다**. 코어가 그 플래그를 두는 이유를
     * 제 말로 적어 두었다: *"labeled UNVERIFIED rather than laundered into a confident success."*
     * 이 셰이퍼가 그것을 세탁하고 있었다 — 대기 표시만 지우고 아무 말도 안 했다(2026-09-09 실측).
     *
     * ⚠ `omitempty` 가 붙은 Go bool 이라 **거짓은 전선에 안 나간다**: 평범한 종료는 칸이 아예
     * 없는 것이다. 그래서 평범한 종료가 경고를 안 다는지도 같이 못박는다 — 안 그러면 이 규칙은
     * 모든 턴에 경고를 붙이는 변경을 통과시킨다.
     */
    /**
     * **한 사건 종류가 대기 상태의 양 끝을 다 나른다.**
     *
     * 코어는 같은 메시지에 `interjection.deferred` 를 두 번 쓴다 — 대기에 들어갈 때
     * `resolved:false`, 큐를 **떠날 때** `resolved:true`(인라인 흡수·라우팅·포기). `recordDeferral`
     * 호출부 일곱 중 **다섯이 true** 다(2026-09-09 실측).
     *
     * 종류만 읽으면 떠나는 순간마다 「대기 중」이 붙고, 그것을 지우는 사건 **뒤에** 오므로 틀린
     * 말이 마지막 말이 된다.
     *
     * ⚠ `omitempty` 가 붙은 Go bool 이라 **거짓은 전선에 안 나간다** — 대기하는 쪽이 칸이 아예
     * 없는 경우다. `resolved:false` 를 기다리는 시험은 오지 않는 모양을 잰다.
     */
    /**
     * **접기는 줄인 양을 적는다 — 사람이 빼게 두지 않는다.**
     *
     * 코어가 이 문장의 정본을 들고 있고(`CompactionData.SizeNote`), 그 주석이 용도를 적는다:
     * *"backs the human-facing … line in **both the headless printer and the TUI**, so the size
     * difference is stated explicitly rather than **left for the reader to subtract**."*
     * 터미널·헤드리스·VS Code 가 다 적는데 이 판만 두 수를 던져 놓고 있었다(2026-09-10 실측).
     *
     * ⚠ **요약이 원본보다 커지는 갈래를 먼저 가른다.** 코어의 `Reduction()` 은 음수를 0으로 누르는데
     * (「줄인 양」이라는 이름에는 맞다) 그것으로 문장을 지으면 「−0, −0%」가 되어 **유일하게 눈에 띌
     * 값이 있는 결과가 숨는다.**
     */
    /**
     * **자리의 렌즈와 라운드의 규칙은 다른 사실이라 다른 칸에 담는다.**
     *
     * 코어에서 둘은 따로다(`CouncilConvenedData.Rule` 과 `CouncilVerdictData.Lens`). 이 셰이퍼가
     * 한동안 둘을 한 칸에 담았고, 그래서 화면은 열린 행의 **규칙만** 그리고 멤버의 **렌즈는 안
     * 그렸다** — 판정 셋이 서로 바꿔 놔도 같은 글이 됐다. 카운슬에 자리가 셋인 이유가 그 렌즈인데.
     *
     * 라이브 전사를 이 셰이퍼에 통과시켜 찾았다: 열린 행이 `lens=majority` 로 나왔다.
     */
    @Test
    fun `자리의 렌즈와 라운드의 규칙은 다른 칸이다`() {
        val r = Rows()
        r.feed(user("고쳐줘", "m1"))
        r.feed(ev("council.convened",
            """{"round":1,"members":["Melchior","Balthasar"],"rule":"majority","task":"t"}"""))
        r.feed(ev("council.verdict",
            """{"round":1,"member":"Melchior","lens":"correctness","decision":"done"}"""))
        val rows = r.list().filter { it.who == Who.Council }
        val opened = rows.first { it.opened }
        val verdict = rows.first { !it.opened }

        assertEquals("majority", opened.rule, "라운드의 규칙이 제 칸에 없다")
        assertNull(opened.lens, "라운드의 규칙이 렌즈 칸에 들어갔다 — 한 칸이 두 뜻을 지면 하나는 안 그려진다")
        assertEquals("correctness", verdict.lens, "멤버의 렌즈가 사라졌다")
        assertNull(verdict.rule, "판정 행에 라운드 규칙이 붙었다")
    }

    @Test
    fun `접기는 줄인 양을 적고, 커진 경우를 숨기지 않는다`() {
        val core = java.io.File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val payload = java.io.File(core, "internal/core/event/payload.go")
        assertTrue(payload.isFile, "코어의 payload 를 못 찾았다(${payload.absolutePath})")
        val note = payload.readText().substringAfter("func (d CompactionData) SizeNote()").substringBefore("\n}")
        assertTrue("LARGER than what it replaced" in note,
            "코어가 커진 갈래를 더는 안 가른다 — 이 규칙의 근거가 사라졌으니 다시 읽어라")

        val r = Rows()
        r.feed(user("하이", "m1"))
        r.feed(ev("compaction", """{"summary":"s","tokensBefore":1000,"tokensAfter":250}"""))
        val folded = r.list().first { "접었다" in it.text }
        assertTrue("−750" in folded.text, "줄인 양을 안 적는다 — 사람이 빼야 한다: ${folded.text}")
        assertTrue("−75%" in folded.text, "몫을 안 적는다: ${folded.text}")

        // 커진 경우: 「−0, −0%」로 숨으면 안 된다.
        val g = Rows()
        g.feed(user("하이", "m2"))
        g.feed(ev("compaction", """{"summary":"s","tokensBefore":100,"tokensAfter":180}"""))
        val grew = g.list().first { "접었다" in it.text }
        assertTrue("+80" in grew.text, "요약이 커진 것을 안 적는다: ${grew.text}")
        assertFalse("−0" in grew.text, "커진 결과가 「−0」으로 숨었다: ${grew.text}")

        // 못 읽은 수는 0이 아니다 — 아무 말도 안 한다.
        assertNull(Rows().sizeNote(null, 10), "모르는 값을 셈에 넣었다")
        assertNull(Rows().sizeNote(10, null), "모르는 값을 셈에 넣었다")
    }

    @Test
    fun `큐를 떠나는 것은 큐에 드는 것과 같은 사건이 아니다`() {
        val core = java.io.File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val payload = java.io.File(core, "internal/core/event/payload.go")
        assertTrue(payload.isFile, "코어의 payload 를 못 찾았다(${payload.absolutePath})")
        assertTrue("""Resolved  bool   `json:"resolved,omitempty"`""" in payload.readText(),
            "와이어가 `resolved` 를 이 규칙이 읽는 모양으로 안 싣는다")

        val parked = Rows()
        parked.feed(user("테스트는?", "q1"))
        parked.feed(ev("interjection.deferred", """{"messageId":"q1"}"""))
        assertTrue(parked.list().first { it.msgId == "q1" }.queued,
            "대기하는 말이 대기로 안 보인다 — 흔한 경우가 망가졌다")

        val left = Rows()
        left.feed(user("테스트는?", "q1"))
        left.feed(ev("interjection.deferred", """{"messageId":"q1"}"""))
        left.feed(ev("interjection.deferred", """{"messageId":"q1","resolved":true}"""))
        assertFalse(left.list().first { it.msgId == "q1" }.queued,
            "큐를 **떠난** 말이 아직 대기 중으로 적힌다 — 칸을 무시했다")

        // 지워진 뒤에 오는 순서 — 이것이 틀린 말을 영구히 만들던 자리다.
        val after = Rows()
        after.feed(user("테스트는?", "q1"))
        after.feed(ev("interjection.deferred", """{"messageId":"q1"}"""))
        after.feed(ev("interjection.answered", """{"messageId":"q1"}"""))
        after.feed(ev("interjection.deferred", """{"messageId":"q1","resolved":true}"""))
        assertFalse(after.list().first { it.msgId == "q1" }.queued,
            "답받은 말이 다시 대기로 표시됐다 — 틀린 말이 마지막 말이 된다")

        // 큐를 떠난 것이 답을 받은 것은 아니다.
        assertTrue(left.list().first { it.msgId == "q1" }.pending,
            "큐를 떠난 것을 답받은 것으로 읽었다")
    }

    @Test
    fun `확인 못 한 채 끝난 턴은 그렇게 적힌다`() {
        val core = java.io.File(System.getProperty("user.dir")).parentFile.parentFile.parentFile.parentFile
        val payload = java.io.File(core, "internal/core/event/payload.go")
        assertTrue(payload.isFile, "코어의 턴 payload 를 못 찾았다(${payload.absolutePath})")
        assertTrue("""Unverified bool   `json:"unverified,omitempty"`""" in payload.readText(),
            "와이어가 `unverified` 를 이 규칙이 읽는 모양으로 안 싣는다")

        val ok = Rows()
        ok.feed(user("하이", "m1"))
        ok.feed(ev("turn.finished", """{"usage":{"in":1,"out":2}}"""))
        assertTrue(ok.list().none { "확인 못 함" in it.text },
            "평범한 종료가 확인 못 한 것으로 적힌다 — 흔한 경우에 경고가 붙었다")

        val bad = Rows()
        bad.feed(user("고쳐줘", "m2"))
        bad.feed(ev("turn.finished",
            """{"usage":{},"unverified":true,"reason":"빌드를 한 번도 안 돌렸다"}"""))
        val said = bad.list().firstOrNull { "확인 못 함" in it.text }
        assertTrue(said != null, "확인 못 한 종료가 확인된 종료와 똑같이 그려진다")
        assertTrue("빌드를 한 번도 안 돌렸다" in said!!.text,
            "사유가 버려졌다 — 무언가 잘못됐다고만 말하고 손잡이를 안 준다")

        val bare = Rows()
        bare.feed(user("고쳐줘", "m3"))
        bare.feed(ev("turn.finished", """{"usage":{},"unverified":true}"""))
        assertTrue(bare.list().any { "확인 못 함" in it.text }, "사유 없는 미확인 종료가 조용하다")
    }

    @Test
    fun `몸통 — 사람이 친 글과 컴패니언의 답이 행에 있다`() {
        val r = Rows()
        r.feed(user("하이", "m1"))
        assertTrue(r.open, "프롬프트가 서면 턴이 열린다")
        assertTrue(r.list().single().pending, "답 없는 물음은 pending 이다")
        r.feed(answer("안녕하세요"))
        r.feed(ev("turn.finished", """{"usage":{"in":1,"out":2}}"""))
        val rows = r.list()
        assertEquals(listOf(Who.User to "하이", Who.Agent to "안녕하세요"), rows.map { it.who to it.text })
        assertFalse(rows[0].pending, "답이 왔으면 pending 이 아니다")
        assertFalse(r.open, "turn.finished 가 턴을 닫는다")
    }

    @Test
    fun `호출과 결과는 한 행 — 병렬이 순서 밖으로 완료돼도 callId 로 짝이 맞는다`() {
        val r = Rows()
        r.feed(ev("part.appended", """{"messageId":"a1","part":{"kind":"tool-call","toolCall":{"callId":"ca","name":"write","args":{"path":"x.go"}}}}"""))
        r.feed(ev("part.appended", """{"messageId":"a1","part":{"kind":"tool-call","toolCall":{"callId":"cb","name":"bash","args":{"cmd":"go test"}}}}"""))
        // cb 가 먼저 실패로, ca 는 나중에 advisory 로 끝난다.
        r.feed(ev("part.appended", """{"messageId":"a1","part":{"kind":"tool-result","toolResult":{"callId":"cb","isError":true,"content":"exit 1"}}}"""))
        r.feed(ev("part.appended", """{"messageId":"a1","part":{"kind":"tool-result","toolResult":{"callId":"ca","isError":true,"advisory":true,"content":"lint: unused var"}}}"""))
        val rows = r.list()
        assertEquals(2, rows.size, "결과는 새 행이 아니다")
        val (wa, wb) = rows
        assertEquals("write", wa.tool)
        assertEquals(true, wa.ok, "advisory 는 실패가 아니다 — 일은 일어났다")
        assertTrue(wa.note)
        assertNull(wa.out, "advisory 의 지적은 out 이 아니라 문제 판의 몫이다")
        assertEquals(false, wb.ok)
        assertEquals("exit 1", wb.out, "실패가 말한 것은 args 를 덮지 않고 out 에 실린다")
        assertEquals("""{"cmd":"go test"}""", wb.args)
    }

    @Test
    fun `재부상·인라인 답·버림은 행을 늘리지 않는다 — 옮기거나 표시한다`() {
        val r = Rows()
        r.feed(user("먼저 물은 것", "m1"))
        r.feed(ev("interjection.deferred", """{"messageId":"m1"}"""))
        assertTrue(r.list().single().queued, "미뤄진 물음은 대기 표시를 단다")
        r.feed(user("두 번째", "m2"))
        r.feed(answer("두 번째의 답"))
        val before = r.list().size
        // m1 이 제 턴으로 재부상한다 — 행이 늘지 않고 끝으로 옮겨 간다.
        r.feed(user("먼저 물은 것", "m3", extra = ""","resurfacedFrom":"m1""""))
        val rows = r.list()
        assertEquals(before, rows.size)
        assertEquals(Who.User, rows.last().who)
        assertEquals("먼저 물은 것", rows.last().text)
        assertFalse(rows.last().queued, "재부상했으면 더는 기다리는 것이 아니다")
        // 버림도 표시다 — 취소된 요청이 무시된 질문으로 읽히면 안 된다.
        r.feed(ev("prompt.abandoned", """{"msgId":"m2"}"""))
        assertEquals(before, r.list().size)
        assertTrue(r.list().first { it.msgId == "m2" }.abandoned)
    }

    @Test
    fun `컴팩션은 지우지 않는다 — 접힘 한 줄이 늘 뿐이다`() {
        val r = Rows()
        r.feed(user("질문", "m1"))
        r.feed(answer("답"))
        val before = r.list()
        r.feed(ev("compaction", """{"summary":"…","replacesUpToSeq":2,"tokensBefore":9000,"tokensAfter":800}"""))
        val rows = r.list()
        assertEquals(before.map { it.who to it.text }, rows.dropLast(1).map { it.who to it.text },
            "컴팩션이 접는 것은 모델의 창이지 사람의 기록이 아니다")
        assertEquals(Who.Info, rows.last().who)
        assertTrue("9000" in rows.last().text && "800" in rows.last().text)
    }

    @Test
    fun `계획은 행이 아니라 사실이다 — 매번 전체가 갈아끼워진다`() {
        val r = Rows()
        val n = r.list().size
        r.feed(ev("todos.changed", """{"todos":[{"content":"a","status":"completed"},{"content":"b","status":"in_progress"}]}"""))
        assertEquals(n, r.list().size, "계획은 전사에 줄을 안 만든다")
        assertEquals(listOf("a" to "completed", "b" to "in_progress"), r.todos.map { it.content to it.status })
        r.feed(ev("todos.changed", """{"todos":[{"content":"c","status":"pending"}]}"""))
        assertEquals(listOf("c"), r.todos.map { it.content }, "델타가 아니라 전체 교체다 — 코어가 그렇게 싣는다")
        r.clear()
        assertEquals(0, r.todos.size, "스트림이 다시 시작하면 계획도 모른다로 돌아간다")
    }

    @Test
    fun `계기판의 사실들 — 모델은 재생되고 컨텍스트는 전이라 모른다로 돌아간다`() {
        val r = Rows()
        r.feed(ev("session.created", """{"workdir":"/w","agent":"a","model":{"provider":"openai","model":"gpt-oss:20b"}}"""))
        assertEquals("gpt-oss:20b", r.model, "session.created 가 심는다")
        r.feed(ev("model.changed", """{"model":"gpt-oss:120b-cloud"}"""))
        assertEquals("gpt-oss:120b-cloud", r.model, "model.changed 가 갈아끼운다")
        r.feed(ev("context.usage", """{"tokens":22000,"window":65536,"percent":33.5}"""))
        assertEquals(33.5, r.context?.percent)
        r.clear()
        assertEquals(null, r.context, "전이는 재생이 없다 — 모름을 0% 로 그리면 안 된다")
    }

    @Test
    fun `디스크를 고친 사실은 새로고침 대장에 남는다`() {
        val r = Rows()
        fun call(id: String, tool: String, args: String) = ev(
            "part.appended",
            """{"messageId":"m","part":{"kind":"tool-call","toolCall":{"callId":"$id","name":"$tool","args":$args}}}""",
        )
        fun done(id: String, err: Boolean = false) = ev(
            "part.appended",
            """{"messageId":"m","part":{"kind":"tool-result","toolResult":{"callId":"$id","isError":$err,"content":"ok"}}}""",
        )
        r.feed(call("c1", "edit", """{"path":"src/a.kt","old":"x","new":"y"}"""))
        r.feed(done("c1"))
        r.feed(call("c2", "read", """{"path":"b.kt"}"""))
        r.feed(done("c2"))
        r.feed(call("c3", "write", """{"path":"c.md","content":"z"}"""))
        r.feed(done("c3", err = true))
        var d = r.drainDisk()
        assertEquals(listOf("src/a.kt"), d.paths, "성공한 변이만 — 읽기와 실패는 아니다")
        assertFalse(d.broad)
        assertTrue(r.drainDisk().paths.isEmpty(), "드레인은 비운다")
        // 경로를 모르는 변이(bash)는 턴이 끝날 때 한 번 훑으라는 신호로 접힌다.
        r.feed(call("c4", "bash", """{"command":"touch x"}"""))
        r.feed(done("c4"))
        assertFalse(r.drainDisk().broad, "턴 중에는 아직 아니다")
        r.feed(ev("turn.finished", "{}"))
        assertTrue(r.drainDisk().broad)
        assertFalse(r.drainDisk().broad, "신호도 드레인으로 비워진다")
        // bash_output 도 접힌다 — detach 된 프로세스는 결과 이후에도 쓴다(리뷰 F6a).
        r.feed(call("c5", "bash_output", """{"id":"bg1"}"""))
        r.feed(done("c5"))
        r.feed(ev("turn.finished", "{}"))
        assertTrue(r.drainDisk().broad)
        // clear 는 대장도 처음부터다 — 세션을 갈아타면 옛 변이가 새 대화에 실리면 안 된다.
        r.feed(call("c6", "edit", """{"path":"d.kt","old":"x","new":"y"}"""))
        r.feed(done("c6"))
        r.clear()
        val after = r.drainDisk()
        assertTrue(after.paths.isEmpty() && !after.broad, "clear 뒤 대장은 비어 있다")
    }

    @Test
    fun `카운슬 평결은 실려 온 말을 버리지 않는다`() {
        val r = Rows()
        fun verdict(extra: String) = ev(
            "council.verdict",
            """{"round":1,"member":"Melchior","lens":"correctness","decision":"done"$extra}""",
        )
        // silent 인데 rationale 이 실려 왔다(라이브 실측 모양) — 말이 이긴다.
        r.feed(verdict(""","silent":true,"rationale":"작업이 요구를 충족한다""""))
        assertEquals("작업이 요구를 충족한다", r.list().last().text)
        // silent 이고 정말 빈 평결 — 그때만 낙하 문구다.
        r.feed(verdict(""","silent":true"""))
        assertEquals("답이 없었다", r.list().last().text)
        // silent 아니고 rationale 도 없으면 빈 본문(지어내지 않는다).
        r.feed(verdict(""))
        assertEquals("", r.list().last().text)
    }

    @Test
    fun `조각은 같은 줄을 고쳐 쓰고 사실이 그 자리를 대신한다`() {
        val r = Rows()
        fun piece(t: String, kind: String = "text") = ev(
            "part.delta", """{"messageId":"m1","kind":"$kind","text":"$t"}""",
        )
        r.feed(piece("안"))
        r.feed(piece("녕"))
        assertEquals(1, r.list().size, "조각은 줄을 쌓지 않는다")
        assertEquals("안녕", r.list().last().text)
        assertTrue(r.list().last().draft, "흐르는 중인 줄은 초안이다")
        // 사실이 오면 그 자리를 대신한다 — 흐르는 동안 본 사람만 답을 두 벌 보면 안 된다.
        r.feed(ev("part.appended",
            """{"messageId":"m1","part":{"kind":"text","text":"안녕하세요"}}"""))
        val rows = r.list()
        assertEquals(1, rows.size, "초안이 사실로 덮인다")
        assertEquals("안녕하세요", rows.last().text)
        assertFalse(rows.last().draft)
    }

    @Test
    fun `사실이 안 오는 턴의 초안은 턴이 끝날 때 쓸린다`() {
        val r = Rows()
        r.feed(ev("part.delta", """{"messageId":"m9","kind":"text","text":"반쪽"}"""))
        assertEquals(1, r.list().size)
        // 코어가 사실을 안 쓰고 턴을 닫는 길이 여럿이다 — 그때 반쪽 답이 남으면 붙어 있던
        // 창과 다시 붙은 창이 갈린다(이 기능이 막으려던 바로 그것).
        r.feed(ev("turn.finished", "{}"))
        assertTrue(r.list().none { it.draft }, "고아 초안은 턴 끝에 쓸린다")
    }

    @Test
    fun `라운드가 열린 것도 행이다 — 첫 평결까지 조용하지 않게`() {
        // 심의는 멤버마다 모델을 한 번씩 부른다. 판이 열린 것을 안 그리면 화면은 첫 평결이
        // 올 때까지 수십 초 조용하고, 사람은 멈춘 줄로 읽는다. 코어는 그 사실을 내고 있었고
        // (`council.convened`), 이 셰이퍼가 버리고 있었다.
        val r = Rows()
        r.feed(ev("council.convened",
            """{"round":2,"members":["Melchior","Balthasar","Casper"],"rule":"any veto continues",""" +
                """"task":"add the idempotency key","report":"done, tests pass"}"""))
        val opened = r.list().last()
        assertEquals(Who.Council, opened.who)
        assertTrue(opened.opened, "평결과 갈리는 표가 행에 남는다")
        assertEquals(2, opened.round)
        assertEquals("Melchior, Balthasar, Casper", opened.text, "누가 앉았는지가 본문이다")
        // 규칙은 제 칸으로 온다 — 한동안 `lens` 에 담겨 있었고, 그 겹침 때문에 화면이
        // 규칙만 그리고 멤버의 렌즈는 안 그렸다.
        assertEquals("any veto continues", opened.rule)
        assertEquals(2, r.councilRound, "라운드는 세션의 사실로도 선다")
        // 멤버가 **무엇을 보고** 판단했는지 — 코어가 실어 보낸 순서대로.
        assertTrue(opened.evidence!!.startsWith("task: add the idempotency key"),
            "증거는 코어가 준 차례를 지킨다: ${opened.evidence}")
        assertTrue(opened.evidence!!.contains("report: done, tests pass"))
    }

    @Test
    fun `심의 중은 행이 아니라 세션의 사실이다`() {
        // 라운드마다 멤버 수만큼 오는 순간의 사실이라(코어도 영속을 안 한다) 전사에 쌓으면
        // 판정보다 시끄러워진다. 표시줄이 읽을 자리에만 남기고 행은 안 만든다.
        val r = Rows()
        r.feed(ev("council.convened", """{"round":1,"members":["Melchior"]}"""))
        val before = r.list().size
        r.feed(ev("council.deliberating", """{"round":1,"member":"Melchior","state":"asking"}"""))
        assertEquals(before, r.list().size, "심의 중은 행을 만들지 않는다")
        assertEquals("Melchior", r.councilAsking)

        // 답이 오면 「묻는 중」은 걷힌다 — 답이 온 뒤에도 서 있으면 화면이 거짓말을 한다.
        r.feed(ev("council.verdict", """{"round":1,"member":"Melchior","decision":"done","rationale":"ok"}"""))
        assertNull(r.councilAsking, "평결이 온 멤버는 더 이상 묻는 중이 아니다")
    }

    @Test
    fun `말한 기권과 무응답은 다른 행이다`() {
        // 코어가 둘을 갈라 보낸다(5755dc74): 판정을 안 말한 멤버는 **말한 기권**
        // (abstain, silent 아님)이고, 아무도 안 준 평결만 silent 다. 화면도 갈라야
        // 「일을 보고 판정을 안 한 것」과 「대답이 없던 것」이 안 섞인다.
        val r = Rows()
        r.feed(ev("council.verdict",
            """{"round":1,"member":"Melchior","decision":"abstain","rationale":"판정을 말하지 않았다"}"""))
        val spoken = r.list().last()
        assertEquals("abstain", spoken.decision)
        assertFalse(spoken.silent, "말한 기권은 무응답이 아니다")
        assertEquals("판정을 말하지 않았다", spoken.text)

        r.feed(ev("council.verdict",
            """{"round":1,"member":"Casper","decision":"abstain","silent":true,"rationale":"the council did not answer within 90s"}"""))
        val quiet = r.list().last()
        assertTrue(quiet.silent, "아무도 안 준 평결은 무응답으로 남는다")
        assertEquals("the council did not answer within 90s", quiet.text, "사유는 그대로 싣는다")
    }

    @Test
    fun `나란히-보기 판정은 결손 쪽으로 접는다 — FlexBool 모양까지`() {
        fun args(extra: String = "") =
            """{"path":"src/a.kt","old":"x","new":"y"$extra}"""
        // 일반 치환만 참이다.
        assertEquals(Triple("a.kt", "x", "y"), Rows.EditSides.of("edit", args()))
        // 데몬 FlexBool 의 참 모양 전부 — "yes" 가 단일 치환으로 그려지면 금지된 왜곡이다.
        for (v in listOf("\"yes\"", "\"on\"", "\"True\"", "\"1\"", "1", "true")) {
            assertEquals(null, Rows.EditSides.of("edit", args(""","replaceAll":$v""")),
                "replaceAll=$v 는 전-출현 치환이다 — 두 면을 그리면 안 된다")
        }
        // 앵커: 문자열이든 숫자든 내용이 있으면 결손(숫자 at 가 isString 게이트로 새던 구멍).
        assertEquals(null, Rows.EditSides.of("edit", args(""","at":"fun main"""")))
        assertEquals(null, Rows.EditSides.of("edit", args(""","at":42""")))
        // 별칭 철자·타 도구·필드 결손 — 전부 정직한 미표시.
        assertEquals(null, Rows.EditSides.of("Edit", args()))
        assertEquals(null, Rows.EditSides.of("write", args()))
        assertEquals(null, Rows.EditSides.of("edit", """{"path":"a","old":"x"}"""))
    }

    @Test
    fun `카운슬은 제자리에 온다 — 스트림이 차례를 이미 안다`() {
        val r = Rows()
        r.feed(user("끝났나 봐줘", "m1"))
        r.feed(ev("council.verdict", """{"round":1,"member":"Casper","lens":"correctness","decision":"continue","rationale":"시험이 없다","keep":"2단계 픽스처"}"""))
        assertEquals(1, r.councilRound, "판정이 서면 라운드가 열려 있다 — 상태 표시줄 칩의 원천")
        r.feed(ev("council.decided", """{"round":1,"decision":"continue","tally":{"done":1,"continue":2},"feedback":"시험을 붙일 것"}"""))
        assertNull(r.councilRound, "합의가 라운드를 닫는다")
        val (v, d) = r.list().drop(1)
        assertEquals("Casper", v.member)
        assertEquals("continue", v.decision)
        assertEquals("2단계 픽스처", v.keep, "승인 멤버의 keep 도 화면에 닿아야 한다 — 코어가 같은 결함을 고친 적 있다")
        assertNull(d.member, "라운드 결과 행은 누구의 것도 아니다")
        assertEquals("시험을 붙일 것", d.why)
    }

    /**
     * ★ **폴드가 이름 대지 않는 part 종류는 빈 행이 아니라 애초에 없던 행이다.**
     *
     * 웹 콘솔이 같은 것을 겪고 주석에 남긴 문장이다 — 그림과 에러가 둘 다 로그에 닿았는데 둘 다
     * 화면에 안 닿았다. 이 폴드에도 그 둘이 빠져 있었다: 도구가 그림으로 답하거나 조각이 에러로
     * 오면 전사에 **아무것도** 남지 않았다. 행도, 에러도, 아무것도.
     */
    @Test
    fun `도구가 돌려준 그림과 에러 조각이 행이 된다`() {
        val r = Rows()
        r.feed(ev("part.appended",
            """{"messageId":"m1","role":"assistant","part":{"kind":"image","image":{"path":"/tmp/shot.png","mime":"image/png"}}}"""))
        r.feed(ev("part.appended",
            """{"messageId":"m1","role":"assistant","part":{"kind":"error","error":"그것이 터졌다"}}"""))
        val rows = r.list()
        assertEquals(2, rows.size, "그림이나 에러 조각이 행을 안 만든다: $rows")
        assertTrue(rows[0].text.contains("/tmp/shot.png"), "그림 행이 경로를 안 싣는다: ${rows[0].text}")
        assertEquals(Who.Info, rows[0].who)
        assertTrue(rows[1].text.contains("그것이 터졌다"), "에러 행이 사유를 안 싣는다: ${rows[1].text}")
        // 사건 `error` 와 **같은 어휘**로 적는다 — 한 사실을 두 낱말로 적으면 안 재지는 쪽이 갈린다.
        assertTrue(rows[1].text.startsWith("\u26A0"), "에러 조각이 사건 error 와 다른 낱말로 적힌다: ${rows[1].text}")
    }

    /** 가리킬 곳이 없는 그림은 행이 아니다 — 경로가 그 행의 전부다. */
    @Test
    fun `경로 없는 그림은 행을 안 만든다`() {
        val r = Rows()
        r.feed(ev("part.appended",
            """{"messageId":"m1","role":"assistant","part":{"kind":"image","image":{}}}"""))
        assertTrue(r.list().isEmpty(), "가리킬 곳 없는 그림이 행을 만들었다: ${r.list()}")
    }

    /**
     * ★ **코어가 로그에 넣을 수 있는 part 종류를 전부 그리거나, 일부러 안 그린다고 적었나.**
     *
     * 안 적으면 조용히 사라진다 — 그림과 에러가 바로 그렇게 사라져 있었다. 코어에 종류가 하나
     * 늘면 어느 목록에도 없어 여기서 실패한다. 그래야 선택이 **침묵으로 기본값이 되지 않는다.**
     *
     * ⚠ 자기검사가 판정 대상을 겸하지 않는다. VS Code 쪽에서 같은 가드를 쓸 때 `image` 를
     * 자기검사에 못 박았더니, 그 갈래를 지웠을 때 판정 문장이 아니라 「가드가 깨졌다」가 떴다 —
     * 나중에 안 그리기로 정하면 멀쩡한 결정이 고장으로 보고된다.
     */
    @Test
    fun `코어의 part 종류를 전부 그리거나 안 그린다고 적는다`() {
        val magi = java.io.File(System.getProperty("user.dir"))
            .parentFile.parentFile.parentFile.parentFile
        val src = java.io.File(magi, "internal/core/session/session.go")
        assertTrue(src.isFile, "${src.absolutePath} 가 없다 — 이 시험이 아무것도 안 보고 있다")
        val kinds = Regex("""PartKind = "([a-z-]+)"""").findAll(src.readText())
            .map { it.groupValues[1] }.toSet()
        assertTrue(kinds.size >= 5, "코어에서 part 종류 ${kinds.size}개만 읽었다 — 파서가 낡았다")

        // `user.dir` 은 이 모듈(`plugin/core`)이다 — 위의 네 단계 상승이 그것을 말한다.
        val foldFile = java.io.File(
            System.getProperty("user.dir"),
            "src/main/kotlin/dev/sayaya/magi/ide/usecase/Rows.kt",
        )
        assertTrue(foldFile.isFile, "${foldFile.absolutePath} 가 없다 — 이 시험이 폴드를 안 보고 있다")
        val fold = foldFile.readText().lines().filterNot { it.trimStart().startsWith("//") }.joinToString("\n")
        val drawn = Regex(""""([a-z-]+)"\s*->""").findAll(fold).map { it.groupValues[1] }.toSet()
        assertTrue("text" in drawn, "가드가 제가 읽는 폴드를 못 본다")
        // ⚠ 바닥은 **판정 대상과 겹치지 않게** 낮게 잡는다. 8 로 두었더니 그림·에러 갈래를 지운
        // 변이가 판정 문장이 아니라 이 줄에서 걸렸다 — 자기검사가 판정을 가리면 가드는 무엇을
        // 잡았는지 못 말한다(VS Code 쪽에서 같은 실수를 한 번 했다).
        assertTrue(drawn.size >= 4, "폴드에서 갈래 ${drawn.size}개만 봤다 — 가드가 아무것도 안 재고 있다")

        // 일부러 안 그리는 것은 사유와 함께 여기 적는다.
        val skipped = mapOf(
            // 이 셋은 part 종류가 아니라 **역할**의 어휘다(같은 문자열 공간을 쓴다).
            "user" to "역할", "assistant" to "역할", "system" to "역할", "tool" to "역할",
        )
        val missing = kinds.filter { it !in drawn && it !in skipped }
        assertEquals(
            emptyList<String>(), missing,
            "코어가 이 part 종류를 로그에 넣을 수 있는데 폴드가 이름 대지 않는다 — 그런 행은 " +
                "애초에 없던 행이고 아무것도 그렇게 말하지 않는다. 그리거나, 사유와 함께 skipped 에 적을 것",
        )
    }

    /**
     * **떠난 대화는 떠났다고 적는다.**
     *
     * 코어는 이 사실을 떠나는 쪽 대화에 적고 사유를 함께 적어 두었다 — 읽는 이에게 필요한 것은
     * 「전사가 왜 멈추는가」다. 그리고 그 침묵의 값도 적어 두었다: 이 줄이 없으면 대화가 그냥 멈춘
     * 것이고, **데몬이 죽은 것과 구별되지 않는다**.
     *
     * 이 창은 갈아타기를 따라가지만 못 따라가는 자리가 둘이다 — **고정 탭**과 나중에 다시 연 대화
     * (재생). 두 자리 모두 아무 말도 없이 끝나고 있었다. 실제 전사 하나를 두 클라이언트의 셰이퍼에
     * 통과시켜 쟀다(2026-09-10): 짝은 한 행, 이쪽은 0행. 지금은 둘 다 한 행이다.
     *
     * 툴윈도가 셰이퍼에 먹인 뒤에 움직이는지는 [dev.sayaya.magi.ide.SourceTextTest] 가 따로 본다 —
     * 행을 만드는 것과 그 행을 만들 기회를 주는 것은 다른 사실이다.
     */
    @Test
    fun `옮겨 간 대화는 어디로 갔는지 적는다`() {
        val r = Rows()
        r.feed(user("고쳐줘", "m1"))
        assertTrue(r.feed(ev("session.moved", """{"to":"s_9f3c"}""")),
            "옮겨 간 사실이 화면을 안 바꾼다 — 그리라고 알리지 않으면 그려지지 않는다")
        val said = r.list().firstOrNull { "s_9f3c" in it.text }
        assertTrue(said != null, "전사가 그냥 멈춘다 — 데몬이 죽은 것과 구별되지 않는다")
        assertEquals(Who.Info, said!!.who, "옮겨 간 사실이 누군가의 말로 그려진다")
        assertTrue("끝납니다" in said.text, "이 대화가 끝났다는 말이 없다")

        // 어디로 갔는지가 요점이다. 그것이 없으면 따라갈 데가 없다 — 그래도 **말은 한다**.
        val blind = Rows()
        blind.feed(ev("session.moved", "{}"))
        val vague = blind.list().firstOrNull()
        assertTrue(vague != null, "목적지 없는 갈아타기는 통째로 사라진다")
        assertTrue("다른 대화" in vague!!.text, "목적지가 없는데 지어내거나 아무 말도 안 한다")
    }

    /**
     * **아무도 못 닿은 카운슬은 「아니오」라고 말한 카운슬이 아니다.**
     *
     * 코어의 `note` 는 완료를 받아들였는지로 고르는 고정 문장 둘뿐이라 누가 투표했는지 말하지 않는다.
     * 그래서 셋이 읽고 반대한 라운드와 셋 다 안 닿은 라운드가 같은 문장에 같은 `continue` 로 와서
     * 둘 다 「반려」로 그려졌다. 이슈 #182(2026-09-10)가 정확히 두 번째 상태에 갇힌 실행이다.
     *
     * 코어가 뭉갤 때의 값을 적어 두었다: 반려라고 말하면 멤버들이 읽고 물리쳤다고 주장하는 것이고,
     * 읽는 이의 다음 행동이 다르다 — 작업이 아니라 백엔드를 고쳐야 한다.
     */
    @Test
    fun `아무도 투표 안 한 라운드는 반려로 그려지지 않는다`() {
        val dead = Rows()
        dead.feed(ev("council.decided", """{"round":1,"decision":"continue","note":"받아들이지 않았다",""" +
            """"tally":{"done":0,"continue":0,"abstain":3,"silent":3,"voters":0}}"""))
        val r = dead.list().last()
        assertTrue(r.silent, "아무도 못 닿은 카운슬이 숙고한 반려로 그려진다")
        assertTrue("3 답 없음" in r.text, "아무도 답 안 했다는 말이 없다")
        assertFalse("3 abstain" in r.text, "말도 못 한 멤버를 기권으로도 센다 — 같은 실패를 두 번")
        assertEquals("no answer", RowText.verdict(r.decision, r.silent)?.word)

        val real = Rows()
        real.feed(ev("council.decided", """{"round":1,"decision":"continue","note":"받아들이지 않았다",""" +
            """"tally":{"done":1,"continue":2,"abstain":1,"voters":3}}"""))
        val g = real.list().last()
        assertFalse(g.silent, "투표한 멤버가 있는 라운드가 답 없음으로 그려진다")
        assertTrue("1 done / 2 continue / 1 abstain" in g.text, "집계가 안 적힌다: ${g.text}")
        assertFalse("답 없음" in g.text)

        // ⚠ 한 갈래는 집계 없이 이 사실을 보낸다. 거기서 0 을 세면 아무도 안 센 수다.
        val bare = Rows()
        bare.feed(ev("council.decided", """{"round":1,"decision":"continue","note":"또 그대로 선언했다"}"""))
        val b = bare.list().last()
        assertEquals("또 그대로 선언했다", b.text, "집계 없는 사실에 집계를 지어 붙인다")
        assertFalse(b.silent)
    }

    /**
     * **반박 뒤에 잰 집계는 그렇다고 말해야 한다.** 안 그러면 없던 합의를 주장한다.
     *
     * 반박 라운드는 독립 투표가 **갈렸을 때만** 돈다. 그리고 옆에 적히는 집계는 그 뒤에 잰 것이다.
     * 그래서 2-1로 시작한 3-0이 이름을 안 붙이면 만장일치로 읽힌다. 코어가 그렇게 적고, 아무도 안
     * 움직인 반박도 말해야 하는 이유를 덧붙였다 — 서로 듣고도 안 움직인 쪽이 더 흥미로운 결과다.
     *
     * 이 칸은 **아무도 안 봐서** 생겼다: 어댑터는 줄곧 계산했고 어느 화면도 안 읽어서, 그 라운드가
     * 무엇을 바꾸는지를 실행에서 물을 수 없었다 — 모델 호출 셋을 더 쓸 값이 있는지 알 유일한 길인데도.
     * 터미널과 웹 콘솔은 그려 왔고, 두 IDE 클라이언트만 안 읽었다(2026-09-10 실측).
     */
    @Test
    fun `반박 라운드는 아무도 안 움직였을 때도 이름이 붙는다`() {
        fun say(debate: String?): String {
            val r = Rows()
            r.feed(ev("council.decided", """{"round":1,"decision":"done","note":"받아들였다",""" +
                """"tally":{"done":3,"continue":0,"voters":3}""" +
                (debate?.let { ""","debate":$it""" } ?: "") + "}"))
            return r.list().last().text
        }
        assertTrue("debated: continue\u2192done, 2 members moved" in
            say("""{"before":"continue","after":"done","changed":2}"""),
            "2-1로 시작한 3-0이 처음부터 있던 합의로 읽힌다")
        assertTrue("debated: done held, 1 member moved" in
            say("""{"before":"done","after":"done","changed":1}"""), "하나를 «1 members» 라 부른다")
        assertTrue("debated, no one moved" in
            say("""{"before":"continue","after":"continue","changed":0}"""),
            "아무도 안 움직인 반박이 통째로 사라진다 — 둘 중 더 흥미로운 결과다")

        // ⚠ 흔한 경우에는 안 실려 온다. 매 라운드에 「논쟁 없음」이 서면 정작 중요한 줄이 묻힌다.
        assertFalse("debated" in say(null), "반박이 없던 라운드가 논쟁했다고 적힌다")
    }

    /**
     * **얼마나 확신했나는 낱말이 아니라 수다 — 그리고 집계가 그 수로 가중한다.**
     *
     * `doneWeight`/`contWeight` 는 확신으로 가중한 합이라 0.2 짜리 `done` 과 0.95 짜리 `done` 은 같게
     * 세이지 않는다. 낱말만 그리면 표는 보이고 규칙이 그것으로 무엇을 했는지는 가려진다.
     *
     * 실측(이 기계의 로그 전량, 2026-09-10): 평결 2938 중 **2879** 에 실려 오고 값은 0.1~0.95 로
     * 퍼져 있다. 터미널은 줄곧 그려 왔고 두 IDE 클라이언트만 버렸다.
     *
     * ⚠ `omitempty` 라 없으면 멤버가 아무 말도 안 한 것이다. 0% 로 그리면 **반대를 확신한 멤버**로
     * 읽히므로 그때는 아무것도 안 그린다.
     */
    @Test
    fun `평결은 얼마나 확신했는지까지 나른다`() {
        val r = Rows()
        r.feed(ev("council.verdict", """{"round":1,"member":"Melchior","lens":"correctness",""" +
            """"decision":"done","confidence":0.62,"rationale":"괜찮다"}"""))
        val v = r.list().last()
        assertEquals(0.62, v.confidence)
        assertTrue("62%" in RowText.plain(v), "옮겨 적는 글에 확신이 안 실린다: ${RowText.plain(v)}")

        // 안 실려 오면 아무 말도 안 한다 — 0% 는 반대를 확신한 멤버로 읽힌다.
        val quiet = Rows()
        quiet.feed(ev("council.verdict", """{"round":1,"member":"Casper","decision":"done"}"""))
        val q = quiet.list().last()
        assertEquals(null, q.confidence, "말하지 않은 확신을 0 으로 지어낸다")
        assertFalse("0%" in RowText.plain(q), "확신을 안 밝힌 멤버가 0% 로 그려진다")
    }

    /**
     * **빈 `changes` 는 두 가지다** — 아무것도 안 고쳤거나, 고쳤는데 재구성이 잃었거나.
     *
     * 코어는 앞엣것을 `noChanges` 로 따로 말하고, 터미널은 증거 판의 `changes` 바로 뒤에 적는다.
     * 그 줄이 없으면 「멤버들이 무엇을 보고 판단했나」를 읽는 사람이 두 경우를 못 가른다.
     *
     * 실측(이 기계의 로그 전량, 2026-09-10): 라운드 994 중 **374** 가 읽기 전용 턴이다. 흔한 쪽이라
     * 더더욱, 빈 칸으로 두면 흔한 경우가 고장으로 읽힌다.
     *
     * ⚠ `omitempty` bool 이라 **거짓은 전선에 안 나간다** — 평범한 턴은 칸이 아예 없는 경우다.
     */
    @Test
    fun `아무것도 안 고친 턴을 심의하면 그렇게 적힌다`() {
        val ro = Rows()
        ro.feed(ev("council.convened", """{"round":1,"rule":"majority","task":"물음에 답한다","noChanges":true}"""))
        val e1 = ro.list().last().evidence.orEmpty()
        assertTrue("읽기만 한 턴" in e1, "읽기 전용 턴이 diff 를 잃은 턴과 똑같이 그려진다: $e1")
        assertTrue("task: 물음에 답한다" in e1, "증거가 통째로 사라졌다")

        val edited = Rows()
        edited.feed(ev("council.convened", """{"round":1,"rule":"majority","task":"고친다","changes":"a.kt | 2 +-"}"""))
        val e2 = edited.list().last().evidence.orEmpty()
        assertFalse("읽기만 한 턴" in e2, "파일을 고친 턴을 읽기 전용이라 적는다 — 칸은 없는 것이지 거짓이 아니다")
        assertTrue("changes: a.kt | 2 +-" in e2)

        // ⚠ 문자 그대로의 `false` 는 전선에 안 나가므로 「true 인가」와 「있는가」는 실제로 같게
        // 움직인다 — 그 둘 사이의 변이는 살아남고, 픽스처가 잡은 척하는 것보다 그렇게 적는 편이
        // 정직하다. 대신 **안전한 쪽**을 못박는다: 거짓을 실어 보내는 데몬이 생겨도(다른 언어의
        // 클라이언트, 나중의 변경) 읽기 전용으로 읽히면 안 된다.
        val stated = Rows()
        stated.feed(ev("council.convened", """{"round":1,"task":"고친다","noChanges":false}"""))
        assertFalse("읽기만 한 턴" in stated.list().last().evidence.orEmpty(),
            "적어 보낸 거짓을 읽기 전용으로 읽는다 — 있다는 것을 참으로 삼고 있다")
    }

}
