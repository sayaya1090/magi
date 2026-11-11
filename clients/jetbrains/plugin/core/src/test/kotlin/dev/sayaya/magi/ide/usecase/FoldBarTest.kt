package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Progress
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

class FoldBarTest {
    @Test
    fun `조각이 여럿이면 끝난 조각의 비율로 채운다`() {
        val b = FoldBar.of(Progress(done = 1, total = 3, chars = 840, elapsedMs = 12_500))
        assertEquals(1.0 / 3, b.fraction!!, 1e-9)
        assertEquals("2/3", b.part)
        assertEquals(840, b.chars)
        assertEquals(12, b.seconds)
        val g = b.glyph(6)
        assertEquals(6, g.length)
        assertEquals(2, g.count { it.toString() == FoldBar.FULL }, "3 개 중 1 개 끝남 = 6 칸 중 2 칸")
    }

    @Test
    fun `조각이 하나면 비율을 지어내지 않는다`() {
        // 요약이 얼마나 길어질지 아무도 모른다. 모르는 것을 60% 로 그리면 그 막대는 거짓말이다.
        val b = FoldBar.of(Progress(done = 0, total = 1, chars = 120, elapsedMs = 3_000))
        assertNull(b.fraction)
        assertEquals("1/1", b.part)
        assertEquals(FoldBar.SPIN, b.glyph())
        // 몇 개인지조차 모르면 조각 표시도 비운다.
        assertEquals("", FoldBar.of(Progress()).part)
    }
}
