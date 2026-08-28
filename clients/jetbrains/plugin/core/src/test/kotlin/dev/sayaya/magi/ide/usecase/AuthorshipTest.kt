package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.Wire
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

/**
 * 저자 추적의 계약 — **못 짚는 것을 못 짚는다고 하는지**가 이 시험의 절반이다.
 */
class AuthorshipTest {

    private var seq = 0L
    private fun ev(type: String, data: String) =
        LogEvent(seq = ++seq, type = type, ts = "t$seq", data = Wire.json.parseToJsonElement(data))

    private fun asked(text: String) = ev("prompt.submitted", """{"parts":[{"kind":"text","text":"$text"}]}""")
    private fun edit(path: String, at: Int? = null, to: Int? = null, tool: String = "edit"): LogEvent {
        val lines = buildString {
            if (at != null) append(""","at":"$at"""")
            if (to != null) append(""","to":"$to"""")
        }
        return ev("part.appended", """{"part":{"toolCall":{"name":"$tool","args":{"path":"$path"$lines}}}}""")
    }

    @Test
    fun `파일을 건드린 턴들과 그 요청이 온 차례대로 나온다`() {
        val a = Authorship()
        listOf(asked("가드를 고쳐라"), edit("g.go", 10, 12), asked("시험을 붙여라"), edit("g.go")).forEach(a::feed)
        val t = a.of("g.go")
        assertEquals(2, t.size)
        assertEquals("가드를 고쳐라", t[0].asked)
        assertEquals("시험을 붙여라", t[1].asked)
    }

    @Test
    fun `마지막 편집이 짚은 줄은 답한다`() {
        val a = Authorship()
        listOf(asked("고쳐라"), edit("g.go", 10, 12)).forEach(a::feed)
        assertNotNull(a.wrote("g.go", 11))
        assertEquals("고쳐라", a.wrote("g.go", 11)!!.asked)
    }

    @Test
    fun `그 범위 밖은 못 짚는다`() {
        val a = Authorship()
        listOf(asked("고쳐라"), edit("g.go", 10, 12)).forEach(a::feed)
        assertNull(a.wrote("g.go", 30))
    }

    @Test
    fun `뒤에 편집이 하나만 더 와도 앞의 줄은 못 짚는다`() {
        // 이것이 이 설계의 핵심이다. 뒤의 편집이 줄을 밀어내므로 앞의 `at` 은 지금 파일에서 다른
        // 줄이고, 그걸 그대로 답하면 **틀린 줄을 가리킨다.**
        val a = Authorship()
        listOf(asked("먼저"), edit("g.go", 10, 12), asked("나중"), edit("g.go", 40, 41)).forEach(a::feed)
        assertNull(a.wrote("g.go", 11), "앞 편집의 줄을 여전히 짚었다 — 그 사이 파일이 밀렸다")
        assertNotNull(a.wrote("g.go", 40))
        // 그래도 파일 단위 답은 남는다 — 못 짚는 것이 아무것도 못 말하는 것은 아니다.
        assertEquals(2, a.of("g.go").size)
    }

    @Test
    fun `줄을 안 실은 편집은 줄을 못 짚는다`() {
        val a = Authorship()
        listOf(asked("고쳐라"), edit("g.go")).forEach(a::feed)
        assertNull(a.wrote("g.go", 1))
        assertEquals(1, a.of("g.go").size)
    }

    @Test
    fun `읽기 도구는 저자가 아니다`() {
        val a = Authorship()
        listOf(asked("봐라"), edit("g.go", 1, 2, tool = "read")).forEach(a::feed)
        assertEquals(0, a.of("g.go").size)
    }

    @Test
    fun `요청을 못 찾으면 지어내지 않는다`() {
        val a = Authorship()
        a.feed(edit("g.go", 1, 2)) // 앞에 prompt 가 없다
        assertNull(a.of("g.go").single().asked)
    }
}
