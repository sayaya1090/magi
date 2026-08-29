package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test

class MeasureTest {
    @Test
    fun `폭 판정은 콜드 패스에서 안 갈라진다`() {
        // 레이아웃 전(폭 0·음수)은 상한이다 — 0 을 돌려주면 선호크기 패스와 레이아웃 패스가
        // 서로 되돌리는 진동이 된다(리뷰 실측 지점).
        assertEquals(480, Measure.proseWidth(0, 480))
        assertEquals(480, Measure.proseWidth(-1, 480))
        // 좁은 판은 판 폭을 따른다 — 상한은 넓은 판에서만 뜻이 있다.
        assertEquals(300, Measure.proseWidth(300, 480))
        assertEquals(1, Measure.proseWidth(1, 480))
        // 상한 이상은 상한.
        assertEquals(480, Measure.proseWidth(480, 480))
        assertEquals(480, Measure.proseWidth(1200, 480))
    }
}
