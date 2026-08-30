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
}
