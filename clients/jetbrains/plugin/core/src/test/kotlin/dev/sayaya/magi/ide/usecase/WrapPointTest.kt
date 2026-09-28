package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class WrapPointTest {
    @Test
    fun `낱말 한가운데면 마지막 공백 뒤로 물린다`() {
        // 「반영: 데|몬 재시작 후」 — 스크린샷에서 「데 / 몬」으로 끊겼다.
        val line = "반영: 데몬 재시작 후"
        assertEquals(4, WrapPoint.keepWords(line, 5))
    }

    @Test
    fun `이미 경계면 그대로 둔다`() {
        assertEquals(4, WrapPoint.keepWords("반영: 데몬", 4))
        assertEquals(3, WrapPoint.keepWords("반영: 데몬", 3))
    }

    @Test
    fun `공백이 없는 긴 낱말은 원래 자리에서 끊는다`() {
        assertEquals(3, WrapPoint.keepWords("가나다라마바", 3))
    }

    @Test
    fun `끝이면 그대로 둔다`() {
        assertEquals(6, WrapPoint.keepWords("가나 다라마", 6))
    }
}
