package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 행의 글자 규칙들. **이 시험이 이 이동의 값이다** — 이 함수들은 창 클래스 안에 살면서 시험
 * 소스셋이 없는 모듈에 갇혀 있었고, 그래서 「화면을 봐야만」 확인됐다.
 */
class RowTextTest {

    private fun row(
        msgId: String = "", who: Who = Who.Agent, callId: String = "",
        text: String = "", at: String? = null, ok: Boolean? = null,
        tool: String? = null, args: String? = null,
    ) = Row(who = who, text = text, msgId = msgId, callId = callId, at = at, ok = ok, tool = tool, args = args)

    // ── 옮겨 적기 ────────────────────────────────────────────────────────────
    //
    // 화면은 색·아이콘·접힘으로 사실을 말한다. 글자로 나갈 때 그것들이 사라지면 붙여넣은 쪽은
    // **무슨 일이 있었는지 모르는 전사**를 받는다. 아래는 그 사실들이 글자에 남는지를 잰다.

    @Test
    fun `실패한 툴은 글자에서도 실패로 보인다`() {
        val bad = RowText.plain(
            Row(who = Who.Tool, text = "", tool = "bash", ok = false, out = "exit 1"),
        )
        assertTrue("failed" in bad, "실패가 안 보인다: $bad")
        assertTrue("bash" in bad, "툴 이름이 없다: $bad")
        assertTrue("exit 1" in bad, "답한 것이 빠졌다: $bad")
        // 반대쪽도 같이 — 한쪽만 재면 「늘 failed 라고 적는다」와 구분이 안 간다.
        val good = RowText.plain(Row(who = Who.Tool, text = "", tool = "bash", ok = true))
        assertTrue("(ok)" in good, "성공이 실패로 보인다: $good")
        assertFalse("failed" in good, "성공인데 failed 라고 적는다: $good")
        // 아직 안 끝난 호출은 셋째 갈래다 — 실패가 아니다.
        val running = RowText.plain(Row(who = Who.Tool, text = "", tool = "bash", ok = null))
        assertTrue("running" in running, "도는 중이 실패로 보인다: $running")
    }

    @Test
    fun `누가 말했는지가 글자에 남는다`() {
        assertTrue(RowText.plain(Row(who = Who.User, text = "고쳐줘")).startsWith("You"))
        assertTrue(RowText.plain(Row(who = Who.Agent, text = "고쳤다")).startsWith("magi"))
        // 생각은 화면에서 접혀 있지만 글자로는 편다 — 접힘은 보는 사람의 편의지 사실이 아니다.
        val think = RowText.plain(Row(who = Who.Thinking, text = "무엇을 먼저 볼까"))
        assertTrue(think.startsWith("thinking"), "생각이라는 것이 안 보인다: $think")
        assertTrue("무엇을 먼저 볼까" in think, "접혀서 본문이 빠졌다: $think")
    }

    @Test
    fun `물은 것과 답한 것을 둘 다 적는다`() {
        val t = RowText.plain(
            Row(who = Who.Tool, text = "", tool = "read", args = "path=a.kt", out = "no such file", ok = false),
        )
        assertTrue("a.kt" in t, "물은 것이 빠졌다: $t")
        assertTrue("no such file" in t, "답한 것이 빠졌다: $t")
    }

    @Test
    fun `흐르는 중인 답에 커서 글리프를 안 붙인다`() {
        // 화면에서는 반쪽 답이 반쪽으로 보여야 하지만, 글자로 나간 뒤엔 그 글리프가 답의
        // 일부처럼 읽힌다. 커서를 붙이는 것은 붓의 일이지 이 함수의 일이 아니다.
        val t = RowText.plain(Row(who = Who.Agent, text = "절반쯤 쓴", draft = true))
        assertFalse("\u258c" in t, "커서 글리프가 글자에 섞였다: $t")
    }

    @Test
    fun `여러 행은 빈 줄로 갈린다`() {
        val t = RowText.plain(
            listOf(Row(who = Who.User, text = "물음"), Row(who = Who.Agent, text = "답")),
        )
        assertTrue("\n\n" in t, "행 사이가 안 갈렸다: $t")
        assertTrue(t.indexOf("물음") < t.indexOf("답"))
    }

    fun `못 읽는 시각은 빈 글자다 — 지어내지 않는다`() {
        assertEquals("", RowText.clock(null))
        assertEquals("", RowText.clock(""))
        assertEquals("", RowText.clock("어제쯤"))
    }

    @Test
    fun `읽히는 시각은 나노초 없이 선다`() {
        val t = RowText.clock("2026-08-31T01:02:03.123456789Z")
        // 표준시간대는 이 기계의 것이라 값을 못박지 않는다. 못박는 것은 **모양**이다:
        // 소수점 아래가 남으면 전사 한 줄이 그 숫자로 다 찬다.
        assertTrue(Regex("""^\d\d:\d\d:\d\d$""").matches(t), "시:분:초 가 아니다: $t")
    }

    @Test
    fun `한 줄로 줄이면 줄바꿈이 사라지고 넘치면 말줄임이 붙는다`() {
        assertEquals("a b c", RowText.oneLine("a\nb\nc", 40))
        assertEquals("abcde", RowText.oneLine("abcde", 5), "딱 맞으면 자르지 않는다")
        assertEquals("abcd…", RowText.oneLine("abcdef", 4))
    }

    @Test
    fun `접힘 열쇠는 글자가 바뀌면 달라진다`() {
        // 자리가 같아도 내용이 바뀌면 다른 행이다. 열쇠가 같으면 사람이 안 편 것이 펴진 채 선다.
        val a = RowText.foldKey(row(msgId = "m1", text = "먼저"))
        val b = RowText.foldKey(row(msgId = "m1", text = "나중"))
        assertTrue(a != b, "글자가 바뀌었는데 열쇠가 같다")
    }

    @Test
    fun `리치 열쇠는 msgId 를 쓰고 없으면 시각으로 떨어진다`() {
        assertEquals("m1", RowText.richKey(row(msgId = "m1", at = "t")))
        assertEquals("t", RowText.richKey(row(msgId = "", at = "t")))
        assertEquals("", RowText.richKey(row(msgId = "", at = null)), "둘 다 없으면 캐시를 안 탄다")
    }

    @Test
    fun `실패한 편집에는 보일 변화가 없다`() {
        // 열쇠 이름은 데몬의 것이다(`old`/`new`/`path`) — 처음에 `old_string` 으로 적었더니
        // 「실패면 null」이 아니라 **아무 args 나 null** 이 되어, 이 시험이 재려던 갈림을
        // 안 재고 통과할 뻔했다. 픽스처가 이름대로가 아니면 시험은 결함 쪽을 지킨다.
        val args = """{"path":"a.txt","old":"x","new":"y"}"""
        assertNull(RowText.diffSides(row(ok = false, tool = "edit", args = args)), "안 일어난 일을 그리지 않는다")
        assertNull(RowText.diffSides(row(ok = null, tool = "edit", args = args)), "모르는 것도 그리지 않는다")
        assertTrue(RowText.diffSides(row(ok = true, tool = "edit", args = args)) != null)
    }
}
