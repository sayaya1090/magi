package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.RecoveryState
import dev.sayaya.magi.ide.model.UncertainCall
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecoveryStripTest {

    @Test
    fun `남은 것이 없거나 데몬이 말하지 않으면 띠를 세우지 않는다`() {
        assertNull(RecoveryStrip.of(null), "낡은 데몬은 recovery 칸을 안 보낸다 — 없는 것을 그리지 않는다")
        assertNull(RecoveryStrip.of(RecoveryState(kind = "none")))
        assertNull(RecoveryStrip.of(RecoveryState(kind = "something_new")), "모르는 상태를 아는 척하지 않는다")
    }

    @Test
    fun `끊긴 턴은 이어가기만 권한다`() {
        val s = RecoveryStrip.of(RecoveryState(kind = "resumable", messageId = "m_user"))!!
        assertEquals(RecoveryStrip.Kind.Resumable, s.kind)
        assertTrue(s.calls.isEmpty())
    }

    @Test
    fun `확인이 필요한 호출마다 도구와 대상을 싣고 미룬 것은 미뤘다고 말한다`() {
        val s = RecoveryStrip.of(RecoveryState(kind = "needs_check", uncertain = listOf(
            UncertainCall(callId = "c_w", tool = "write", target = "notes.md"),
            UncertainCall(callId = "c_bash", tool = "bash", target = "make deploy", verdict = "deferred"),
        )))!!
        assertEquals(RecoveryStrip.Kind.NeedsCheck, s.kind)
        assertEquals(listOf("c_w", "c_bash"), s.calls.map { it.callId })
        assertEquals("write", s.calls[0].tool)
        assertEquals("notes.md", s.calls[0].target)
        assertTrue(!s.calls[0].deferred)
        assertTrue(s.calls[1].deferred)
        // 데몬이 받는 판정 단어는 넷뿐이다 — 화면이 다른 낱말을 보내면 문이 말로 거절한다.
        assertEquals(listOf("ran", "not_ran", "rerun_ok", "deferred"), RecoveryStrip.VERDICTS)
    }

    @Test
    fun `복구 실패는 데몬이 말한 사유를 그대로 싣는다`() {
        val s = RecoveryStrip.of(RecoveryState(kind = "recovery_failed", reason = "disk full"))!!
        assertEquals(RecoveryStrip.Kind.Failed, s.kind)
        assertEquals("disk full", s.reason)
    }
}
