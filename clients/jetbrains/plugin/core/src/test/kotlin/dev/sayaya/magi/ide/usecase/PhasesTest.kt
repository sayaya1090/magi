package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNotSame
import org.junit.jupiter.api.Assertions.assertSame
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

    /**
     * **닫는 것을 고치고 닫힌 뒤를 안 고쳤다.**
     *
     * `Closed` 가 아무 전이도 안 받는 것은 옳고, 창의 상태 맵이 창보다 오래 사는 것도 옳다 —
     * 늦게 끝난 비동기가 제 번호를 견주려면 그 인스턴스가 남아 있어야 한다. 그런데 그 둘이
     * 겹치면 같은 경로를 **다시 연** 창의 첫 전이(`Absent`)가 거절당하고 **데몬이 영영 안 뜬다.**
     * 상태 기계를 배선한 커밋이 만든 결함이라, 배선이 없던 때보다 나쁘다.
     */
    @Test
    fun `다시 열린 창은 닫힌 상태를 물려받지 않는다`() {
        val closed = Progress()
        assertTrue(closed.on(Move.Close))
        assertTrue(closed.on(Move.Settled))
        assertEquals(Phase.Closed, closed.phase)

        val next = Phases.reopened(closed)
        assertNotSame(closed, next, "닫힌 인스턴스를 그대로 물려줬다 — §3 은 새 창을 새 소유자로 본다")
        assertTrue(next.on(Move.Absent), "다시 연 창이 데몬을 못 띄운다 — 기동이 영구히 막힌다")

        // 살아 있는 것은 갈아 끼우지 않는다. 그러면 지금 도는 기동의 상태가 매번 새것이 되어
        // 세대도 예산도 아무것도 기억하지 못한다.
        val live = Progress()
        assertTrue(live.on(Move.Absent))
        assertSame(live, Phases.reopened(live), "멀쩡히 도는 상태를 새것으로 갈아 끼웠다")
        assertEquals(Phase.Starting, Phases.reopened(live).phase)

        // 처음 보는 경로는 새것이다.
        assertEquals(Phase.Discovering, Phases.reopened(null).phase)
    }

    /**
     * **표에 있고 아무도 안 보내던 전이.**
     *
     * §3 은 `Blocked --> Discovering: explicit retry` 를 적어 두었는데 `Move.Asked` 를 보내는
     * 자리가 어디에도 없었다. 그래서 사람이 직접 누른 기동이 유예에 걸린 창과 예산이 바닥난
     * 창에서 **조용히 아무것도 안 했다** — 단추가 로그 한 줄만 남기는 것은 이 트리가 되풀이해
     * 겪은 「실려 오지만 안 그려짐」의 또 한 얼굴이다.
     *
     * ⚠ 사람이 물어도 **열리지 않는 자리**가 있다는 것이 이 규칙의 나머지 절반이다. 닫는 중인
     * 창에서 열어 주면 아무도 끄지 않는 데몬이 남는다.
     */
    @Test
    fun `사람이 시키면 유예와 바닥난 예산을 넘어 다시 본다`() {
        // 유예 중 — 사람이 물은 것이 §3 의 `retry permitted` 다.
        val backoff = Progress()
        backoff.on(Move.Absent); backoff.on(Move.LaunchFailed)
        assertEquals(Phase.Backoff, backoff.phase)
        assertTrue(Phases.asked(backoff), "유예 중인 창에서 수동 기동이 조용히 아무것도 안 한다")
        assertEquals(Phase.Discovering, backoff.phase)

        // 예산이 바닥났다 — 여기가 `explicit retry` 의 본래 자리다.
        val blocked = Progress()
        blocked.on(Move.Absent); blocked.on(Move.LaunchFailed); blocked.on(Move.Exhausted)
        assertEquals(Phase.Blocked, blocked.phase)
        assertTrue(Phases.asked(blocked), "예산이 바닥난 창은 사람이 시켜도 안 뜬다 — 되돌릴 길이 없다")
        assertEquals(Phase.Discovering, blocked.phase)

        // 붙어 있다고 믿는데 부르는 쪽이 소켓을 못 찾았다 — 끊긴 것이므로 그 사실부터 적는다.
        val ready = Progress()
        ready.on(Move.Absent); ready.on(Move.Answered)
        assertEquals(Phase.Ready, ready.phase)
        assertTrue(Phases.asked(ready), "낡은 Ready 가 수동 기동을 막는다")
        assertEquals(Phase.Discovering, ready.phase)

        // 이미 띄우는 중이면 또 띄우지 않는다.
        val starting = Progress()
        starting.on(Move.Absent)
        assertFalse(Phases.asked(starting), "이미 기동 중인데 또 띄운다")

        // 닫는 중이면 열어 주지 않는다 — 아무도 끄지 않는 데몬이 남는다.
        val closing = Progress()
        closing.on(Move.Close)
        assertFalse(Phases.asked(closing), "닫는 중인 창이 데몬을 띄운다")
        val gone = Progress()
        gone.on(Move.Close); gone.on(Move.Settled)
        assertFalse(Phases.asked(gone), "닫힌 창이 데몬을 띄운다")
    }

    /**
     * **기동이 던진 뒤 원인을 고치고 다시 시키면 뜬다.**
     *
     * 안 뜬 것이 「뜨는 중」으로 남으면 그 창은 IDE 를 다시 켜야 데몬을 띄운다 — `Starting` 에서는
     * 자동 기동의 `Absent` 도, 사람이 시킨 기동도 갈 곳이 없다. 그래서 실패는 **반드시 적혀야**
     * 하고, 적힌 뒤에는 사람이 되돌릴 수 있어야 한다. 이 규칙은 그 왕복을 한 줄로 잰다.
     */
    @Test
    fun `기동이 던져도 실패를 적으면 사람이 되돌릴 수 있다`() {
        val p = Progress()
        assertTrue(p.on(Move.Absent))
        assertEquals(Phase.Starting, p.phase)

        // 실패를 안 적으면 여기서 끝이다 — 사람이 시켜도 갈 곳이 없다.
        assertFalse(Phases.asked(p), "뜨는 중인 창에서 수동 기동이 통하면 중복 기동이다")

        assertTrue(p.on(Move.LaunchFailed), "기동 실패를 적을 자리가 없다")
        assertEquals(Phase.Backoff, p.phase)
        assertTrue(Phases.asked(p), "실패를 적었는데도 사람이 되돌릴 수 없다 — IDE 를 다시 켜야 한다")
        assertEquals(Phase.Discovering, p.phase)
        assertTrue(p.on(Move.Absent), "되돌아왔는데 띄우지 못한다")
        assertTrue(p.on(Move.Answered), "두 번째 기동이 붙지 못한다")
        assertEquals(Phase.Ready, p.phase)
    }
}
