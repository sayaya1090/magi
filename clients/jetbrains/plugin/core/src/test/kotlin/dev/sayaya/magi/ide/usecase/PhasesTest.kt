package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * §3 의 그림이 타입이 됐는지 — **그림에 있는 화살표가 다 있고, 없는 것은 없는지** 잰다.
 *
 * 「전이표가 맞나」만 재면 표를 그대로 베낀 시험이 된다. 그래서 반대쪽도 잰다: 표에 없는 조합은
 * 전부 거절돼야 하고, 그 수를 세어 **표가 조용히 넓어지는 것**도 잡는다.
 */
class PhasesTest {

    private val arrows = listOf(
        Triple(Phase.Discovering, Move.Answered, Phase.Ready),
        Triple(Phase.Discovering, Move.Absent, Phase.Starting),
        Triple(Phase.Discovering, Move.Unusable, Phase.Blocked),
        Triple(Phase.Starting, Move.Answered, Phase.Ready),
        Triple(Phase.Starting, Move.LaunchFailed, Phase.Backoff),
        Triple(Phase.Ready, Move.Lost, Phase.Backoff),
        Triple(Phase.Backoff, Move.Retry, Phase.Discovering),
        Triple(Phase.Backoff, Move.Exhausted, Phase.Blocked),
        Triple(Phase.Blocked, Move.Asked, Phase.Discovering),
        Triple(Phase.Closing, Move.Settled, Phase.Closed),
    )

    @Test
    fun `그림의 화살표가 전부 있다`() {
        for ((from, move, to) in arrows) {
            assertEquals(to, Phases.next(from, move), "$from --$move--> $to 가 없다")
        }
    }

    @Test
    fun `창은 아무 때나 닫힌다`() {
        for (p in Phase.entries) {
            if (p == Phase.Closed) continue
            assertEquals(Phase.Closing, Phases.next(p, Move.Close), "$p 에서 창을 못 닫는다")
        }
    }

    @Test
    fun `닫힌 인스턴스는 아무것도 안 받는다`() {
        for (m in Move.entries) {
            assertNull(Phases.next(Phase.Closed, m),
                "닫힌 인스턴스가 $m 로 되살아난다 — 닫으면서 취소한 일이 같이 살아난다(§3)")
        }
    }

    @Test
    fun `표에 없는 조합은 전부 거절된다`() {
        val known = arrows.map { it.first to it.second }.toSet()
        var refused = 0
        for (p in Phase.entries) for (m in Move.entries) {
            if (p == Phase.Closed || m == Move.Close || (p to m) in known) continue
            assertNull(Phases.next(p, m), "$p 에서 $m 로 가는 화살표는 그림에 없다")
            refused++
        }
        // 바닥: 아무것도 안 거절하고 있으면 위 루프는 통과하면서 아무 말도 안 한다.
        assertTrue(refused > 40, "거절한 조합이 ${refused}개뿐이다 — 이 시험이 표를 안 훑고 있다")
    }

    @Test
    fun `없는 전이는 상태를 안 옮긴다`() {
        val p = Progress(Phase.Ready)
        assertFalse(p.on(Move.Retry), "Ready 에서 Retry 는 그림에 없다")
        assertEquals(Phase.Ready, p.phase, "거절해 놓고 옮겼다")
        assertTrue(p.on(Move.Lost))
        assertEquals(Phase.Backoff, p.phase)
    }

    /**
     * ⚠ **세대는 「닫혔나」로 대신할 수 없다.** 닫고-다시-연 창은 다시 열려 있으므로, 떠났던
     * 작업이 「아직 열려 있나」를 물으면 참을 받고 남의 창에 결과를 쓴다. 번호는 그것을 가른다.
     */
    @Test
    fun `닫힘이 세대를 올리고, 늦게 끝난 일은 제 결과를 버린다`() {
        val p = Progress()
        val mine = p.generation
        assertTrue(p.still(mine))

        assertTrue(p.on(Move.Close))
        assertFalse(p.still(mine), "닫혔는데 떠났던 일이 아직 제 것이라고 믿는다")

        // 그리고 다시 열려도 옛 번호는 살아나지 않는다.
        assertTrue(p.on(Move.Settled))
        val fresh = Progress()
        assertTrue(fresh.still(fresh.generation))
        assertFalse(p.still(mine))
    }

    @Test
    fun `두 번 닫아도 세대는 한 번만 오른다`() {
        val p = Progress()
        assertTrue(p.on(Move.Close))
        val after = p.generation
        assertTrue(p.on(Move.Close), "dispose 는 중복 호출된다 — 두 번째 닫힘도 받아야 한다")
        assertEquals(after, p.generation,
            "닫을 때마다 번호가 오르면 멀쩡히 도는 일이 제 결과를 버린다")
    }

    @Test
    fun `닫힌 뒤에는 세대를 물어도 제 것이 아니다`() {
        val p = Progress()
        val mine = p.generation
        assertTrue(p.on(Move.Close))
        assertTrue(p.on(Move.Settled))
        assertFalse(p.still(mine))
        assertNotNull(Phase.Closed)
    }
}
