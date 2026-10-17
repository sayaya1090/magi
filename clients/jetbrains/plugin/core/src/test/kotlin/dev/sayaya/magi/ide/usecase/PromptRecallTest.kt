package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class PromptRecallTest {
    @Test fun `recalled multiline input stays navigable until edited`() {
        val r = PromptRecall()
        r.load(listOf("first\nsecond", "older"))
        assertEquals("first\nsecond", r.older("draft"))
        org.junit.jupiter.api.Assertions.assertTrue(r.isRecalled("first\nsecond"))
        assertFalse(r.isRecalled("first\nsecond edited"))
        assertEquals("older", r.older("first\nsecond"))
        assertEquals("first\nsecond", r.newer())
        assertEquals("draft", r.newer())
    }

    @Test
    fun `위로는 오래된 쪽, 아래로 끝까지 가면 쓰던 글이 돌아온다`() {
        val r = PromptRecall()
        r.load(listOf("셋째", "둘째", "첫째"))
        assertEquals("셋째", r.older("쓰던 글"))
        assertEquals("둘째", r.older("셋째"))
        assertEquals("첫째", r.older("둘째"))
        assertNull(r.older("첫째"), "더 없으면 입력을 안 바꾼다")
        assertEquals("둘째", r.newer())
        assertEquals("셋째", r.newer())
        assertEquals("쓰던 글", r.newer(), "맨 아래는 맡겨 둔 쓰던 글")
        assertNull(r.newer())
        assertFalse(r.browsing)
    }

    @Test
    fun `보낸 글은 다시 묻기 전에도 맨 앞에 선다`() {
        val r = PromptRecall()
        r.load(listOf("옛 글", "같은 글"))
        r.remember("같은 글")
        assertEquals("같은 글", r.older(""))
        assertEquals("옛 글", r.older("같은 글"), "같은 글은 한 번만")
    }

    @Test
    fun `둘러보는 중에 온 새 목록은 자리를 흔들지 않는다`() {
        val r = PromptRecall()
        r.load(listOf("a", "b"))
        r.older("")
        r.load(listOf("x", "a", "b"))
        assertEquals("b", r.older("a"))
    }

    @Test
    fun `목록이 비면 아무것도 안 한다`() {
        val r = PromptRecall()
        assertNull(r.older("쓰던 글"))
        assertNull(r.newer())
    }
}
