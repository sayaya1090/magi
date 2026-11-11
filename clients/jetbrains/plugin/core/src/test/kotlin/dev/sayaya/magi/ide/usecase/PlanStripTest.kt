package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class PlanStripTest {
    private fun t(c: String, s: String) = Rows.Todo(c, s)

    @Test
    fun `진행 중인 단계가 몇 번째인지와 그 과정을 말한다`() {
        val s = PlanStrip.of(listOf(t("a", "completed"), t("b", "completed"), t("c", "in_progress"), t("d", "pending")))!!
        assertEquals(3, s.step)
        assertEquals(4, s.total)
        assertEquals(2, s.done)
        assertEquals("c", s.current)
        assertTrue(!s.finished)
    }

    @Test
    fun `진행 중 표시가 없으면 다음에 할 단계를 현재로 본다`() {
        val s = PlanStrip.of(listOf(t("a", "completed"), t("b", "pending"), t("c", "pending")))!!
        assertEquals(2, s.step)
        assertEquals("b", s.current)
    }

    @Test
    fun `다 끝났으면 끝났다고 하고, 계획이 없으면 줄을 세우지 않는다`() {
        val s = PlanStrip.of(listOf(t("a", "completed"), t("b", "completed")))!!
        assertTrue(s.finished)
        assertEquals(2, s.step)
        assertEquals(2, s.done)
        assertNull(PlanStrip.of(emptyList()))
        assertNull(PlanStrip.of(null))
    }
}
