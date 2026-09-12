package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Published
import dev.sayaya.magi.ide.model.Response
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 「무언가 답했다」와 「내가 띄운 자식이 답했다」를 가르는 규칙.
 *
 * 두 칸은 와이어에도 이 클라이언트의 모델에도 처음부터 있었고 — 그 KDoc 에 §4 의 규칙까지 적혀
 * 있었고 — 아무도 안 읽었다. 실려 있는데 안 그려지는 그 무늬다.
 */
class GenerationTest {

    private fun rec(pid: Int, instance: String? = null, owner: String? = null) =
        Published(pid = pid, instance = instance, owner = owner)

    private fun said(instance: String? = null) = Response(ok = true, instance = instance)

    @Test
    fun `기록과 답이 같은 세대를 대야 준비다`() {
        assertTrue(Generation.same(rec(42, "i-1"), said("i-1"), 42L))
        assertFalse(
            Generation.same(rec(42, "i-1"), said("i-2"), 42L),
            "다른 프로세스가 답했는데 창이 스스로 준비됐다고 했다",
        )
    }

    @Test
    fun `아무도 안 답했거나 남의 자식이면 준비가 아니다`() {
        assertFalse(Generation.same(rec(42, "i-1"), null, 42L))
        assertFalse(Generation.same(null, said("i-1"), 42L))
        assertFalse(Generation.same(rec(9, "i-1"), said("i-1"), 42L), "기록의 pid 가 우리 자식이 아니다")
        assertFalse(Generation.same(rec(42, "i-1"), said("i-1"), null))
    }

    @Test
    fun `둘 다 세대를 안 대는 구형 코어만 pid 로 떨어진다`() {
        assertTrue(Generation.same(rec(7), said(), 7L))
        assertFalse(Generation.same(rec(8), said(), 7L), "그래도 pid 는 우리 것이어야 한다")
    }

    /**
     * ⚠ **한쪽만 대는 것은 구형 코어가 아니다.** 기록을 쓴 것도 답하는 것도 같은 프로세스라,
     * 기록에 세대가 적혀 있으면 그 데몬은 `about` 에서도 그것을 댄다 — 한쪽만 있다는 것은 다른
     * 프로세스가 답했다는 뜻이고, 구형 호환으로 흘려보내면 이 검사가 막으려던 그 경우가 통과한다.
     */
    @Test
    fun `한쪽만 세대를 대면 다른 프로세스가 답한 것이다`() {
        assertFalse(Generation.same(rec(7, "i-1"), said(), 7L), "기록은 신형인데 답이 세대를 안 댄다")
        assertFalse(Generation.same(rec(7), said("i-1"), 7L), "답은 신형인데 기록이 세대를 안 댄다")
        assertFalse(Generation.same(rec(7, "  "), said("i-1"), 7L), "빈 칸은 「안 댄 것」이다")
    }

    /** 답이 실패면 답이 아니다 — 「거절했다」를 「모른다」로 섞으면 안 된다. */
    @Test
    fun `실패한 답으로는 준비가 아니다`() {
        assertFalse(Generation.same(rec(7, "i-1"), Response(ok = false, instance = "i-1"), 7L))
        assertFalse(Generation.same(rec(7), Response(ok = false), 7L))
    }

    @Test
    fun `계보가 같으면 핸들이 없어도 제 데몬이다`() {
        assertEquals(false, Generation.foreign(rec(1, owner = "own-1"), "own-1"))
        assertEquals(true, Generation.foreign(rec(1, owner = "own-2"), "own-1"))
    }

    @Test
    fun `계보를 모르면 판단하지 않는다`() {
        assertNull(Generation.foreign(rec(1), "own-1"), "터미널에서 띄운 데몬을 남의 것으로 몰면 안 된다")
        assertNull(Generation.foreign(rec(1, owner = "own-1"), null))
        assertNull(Generation.foreign(null, "own-1"))
    }
}
