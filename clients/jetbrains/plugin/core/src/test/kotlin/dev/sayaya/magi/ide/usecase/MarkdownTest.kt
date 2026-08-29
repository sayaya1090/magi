package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class MarkdownTest {
    @Test
    fun `이스케이프가 먼저다 — 남의 글자는 태그로 안 먹힌다`() {
        assertEquals("a &lt;b&gt; c", Markup.markdown("a <b> c"))
    }

    @Test
    fun `부분집합이 펴진다 — 굵게 코드 목록 머리글 펜스`() {
        assertEquals("<b>제목</b>", Markup.markdown("# 제목"))
        assertEquals("&nbsp;&bull; 하나", Markup.markdown("- 하나"))
        assertEquals("<b>굵게</b> 와 <code>코드</code>", Markup.markdown("**굵게** 와 `코드`"))
        assertEquals("<i>기울임</i>", Markup.markdown("*기울임*"))
        assertEquals("<pre>x = 1\n</pre>", Markup.markdown("```py\nx = 1\n```"))
    }

    @Test
    fun `코드 안에서는 강조가 안 먹는다 — 옮겨 적을 것은 그대로`() {
        assertEquals("<code>a**b**c</code>", Markup.markdown("`a**b**c`"))
        assertEquals("<b>굵게</b> 와 <code>a*b*c</code>", Markup.markdown("**굵게** 와 `a*b*c`"))
    }

    @Test
    fun `모르는 문법은 원문 그대로 — 어설픈 반쪽 렌더보다 정직한 원문`() {
        val md = "[이름](http://x) 그대로"
        assertTrue(Markup.markdown(md).contains("[이름](http://x)"))
    }

    @Test
    fun `닫는 펜스가 안 와도 실린 데까지 고정폭이다 — 잘린 답도 답이다`() {
        assertEquals("답:<br/><pre>tail\n</pre>", Markup.markdown("답:\n```\ntail"))
    }
}
