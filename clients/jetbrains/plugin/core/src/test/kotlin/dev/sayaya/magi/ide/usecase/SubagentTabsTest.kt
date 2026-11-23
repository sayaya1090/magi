package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.ChildJob
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

/**
 * 우측 자리의 탭 줄(사용자 결정, 2026-11-23): 첫 탭은 세션 제목, 그 옆에 도는 서브에이전트가 하나씩
 * 붙는다. 끝난 서브에이전트의 탭은 닫히되, 지금 보고 있으면 남겼다가 다른 탭으로 옮기면 닫힌다.
 */
class SubagentTabsTest {
    private fun job(id: String, task: String?, running: Boolean) = ChildJob(id = id, task = task, running = running)

    @Test
    fun `도는 서브에이전트마다 탭이 붙고 이름은 과제의 첫 줄이다`() {
        val tabs = SubagentTabs.next(emptyList(), listOf(job("s_aaaaaa1", "read docs\nmore", true), job("s_bbbbbb2", null, true)), null)
        assertEquals(listOf("s_aaaaaa1" to "read docs", "s_bbbbbb2" to "·bbbbb2"), tabs.map { it.id to it.label })
    }

    @Test
    fun `끝난 서브에이전트는 보고 있지 않으면 닫히고 보고 있으면 남는다`() {
        val open = SubagentTabs.next(emptyList(), listOf(job("a", "A", true), job("b", "B", true)), null)
        val done = listOf(job("a", "A", false), job("b", "B", false))
        val kept = SubagentTabs.next(open, done, selected = "b")
        assertEquals(listOf("b"), kept.map { it.id })
        assertEquals(false, kept.single().running)
        assertEquals(emptyList<String>(), SubagentTabs.next(kept, done, selected = null).map { it.id })
    }

    @Test
    fun `목록에서 사라진 자식도 끝난 것으로 본다 - 순서는 붙은 순서다`() {
        val open = SubagentTabs.next(emptyList(), listOf(job("a", "A", true)), null)
        val more = SubagentTabs.next(open, listOf(job("c", "C", true), job("a", "A", true)), null)
        assertEquals(listOf("a", "c"), more.map { it.id })
        assertEquals(listOf("c"), SubagentTabs.next(more, listOf(job("c", "C", true)), null).map { it.id })
    }

    @Test
    fun `세션 탭 이름은 제목의 첫 줄이고 길면 자르고 없으면 대신할 말이다`() {
        assertEquals("고쳐 줘", SubagentTabs.sessionLabel("  고쳐 줘\n둘째 줄", "채팅"))
        assertEquals("채팅", SubagentTabs.sessionLabel("   ", "채팅"))
        assertEquals("채팅", SubagentTabs.sessionLabel(null, "채팅"))
        assertEquals("x".repeat(31) + "…", SubagentTabs.sessionLabel("x".repeat(50), "채팅"))
    }
}
