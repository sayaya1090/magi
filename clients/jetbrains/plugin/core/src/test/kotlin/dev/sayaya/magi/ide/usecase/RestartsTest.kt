package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** 되살리기 규칙. 시계를 주입하므로 이 시험은 안 잔다. */
class RestartsTest {

    @Test
    fun `간격 안에는 두 번 안 띄운다`() {
        val r = Restarts(attempts = 3, interval = 60_000)
        assertTrue(r.take(0L))
        assertFalse(r.take(3000L), "3초 뒤 또 띄우면 폴마다 프로세스를 낳는다")
        assertTrue(r.take(60_000L))
    }

    @Test
    fun `다 쓰면 멈춘다`() {
        val r = Restarts(attempts = 2, interval = 0)
        assertTrue(r.take(0L)); assertTrue(r.take(1L))
        assertFalse(r.take(2L), "계속 실패하는 것을 되풀이하는 것은 회복이 아니라 소음이다")
        assertTrue(r.spent)
    }

    @Test
    fun `붙으면 셈이 되돌아간다`() {
        // 오래 도는 IDE 에서 「예전에 세 번 실패함」이 영구 금지가 되면 안 된다.
        val r = Restarts(attempts = 2, interval = 0)
        r.take(0L); r.take(1L)
        assertTrue(r.spent)
        r.ok()
        assertFalse(r.spent)
        assertTrue(r.take(2L))
    }

    @Test
    fun `한 번도 안 띄운 상태는 다 쓴 것이 아니다`() {
        assertFalse(Restarts().spent)
        assertEquals(true, Restarts().take(0L))
    }
}
