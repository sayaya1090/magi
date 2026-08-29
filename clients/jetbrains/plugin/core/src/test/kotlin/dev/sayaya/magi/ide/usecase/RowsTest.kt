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
    fun `조각에는 행이 없다 — 같은 말이 사실로 뒤따른다`() {
        val r = Rows()
        // 거르는 것은 부르는 쪽의 일이지만(Transcript.echoesFact), 셰이퍼 자신도 delta 타입에
        // 행을 만들지 않는다 — 두 겹이 다 뚫려야 새는 구조가 아니게.
        r.feed(ev("part.delta", """{"messageId":"a1","part":{"kind":"text","text":"안"}}"""))
        r.feed(ev("part.delta", """{"messageId":"a1","part":{"kind":"text","text":"안녕"}}"""))
        assertEquals(0, r.list().size)
        r.feed(answer("안녕"))
        assertEquals(1, r.list().size)
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
    fun `카운슬은 제자리에 온다 — 스트림이 차례를 이미 안다`() {
        val r = Rows()
        r.feed(user("끝났나 봐줘", "m1"))
        r.feed(ev("council.verdict", """{"round":1,"member":"Casper","lens":"correctness","decision":"continue","rationale":"시험이 없다","keep":"2단계 픽스처"}"""))
        r.feed(ev("council.decided", """{"round":1,"decision":"continue","tally":{"done":1,"continue":2},"feedback":"시험을 붙일 것"}"""))
        val (v, d) = r.list().drop(1)
        assertEquals("Casper", v.member)
        assertEquals("continue", v.decision)
        assertEquals("2단계 픽스처", v.keep, "승인 멤버의 keep 도 화면에 닿아야 한다 — 코어가 같은 결함을 고친 적 있다")
        assertNull(d.member, "라운드 결과 행은 누구의 것도 아니다")
        assertEquals("시험을 붙일 것", d.why)
    }
}
