package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.ContextParts
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** The context gauge, by the Office task pane's rule (`contextMeter`): the scale is the model's window. */
class ContextGaugeTest {
    private fun ctx(tokens: Int, window: Int, parts: ContextParts? = null) =
        Rows.Ctx(tokens, window, if (window > 0) tokens * 100.0 / window else 0.0, parts)

    @Test
    fun `the scale is the window, so what is left stays empty`() {
        // The estimated parts sum to 8k while the measured total is 9k — the core says they will not agree.
        val g = ContextGauge.of(ctx(9_000, 131_000, ContextParts(system = 2_000, tools = 4_000, talk = 1_000, calls = 500, results = 500)))!!
        assertEquals(listOf(ContextGauge.Part.System, ContextGauge.Part.Tools, ContextGauge.Part.Talk, ContextGauge.Part.Calls, ContextGauge.Part.Results),
            g.segments.map { it.part }, "Office's order")
        val filled = g.segments.sumOf { it.fraction }
        // The filled length is the MEASURED total's share of the window, not the estimated parts' sum.
        assertEquals(9_000.0 / 131_000, filled, 1e-9)
        assertEquals(7, g.percent)
    }

    @Test
    fun `parts speak as shares of their own sum, never as totals`() {
        // An estimated breakdown that does not add up to `used` (the core's rule: honest as proportions).
        val g = ContextGauge.of(ctx(8_200, 131_000, ContextParts(system = 2_400, tools = 5_800)))!!
        assertEquals(listOf(29, 71), g.segments.map { it.share })
        assertEquals(0.29, g.segments[0].fraction / g.segments.sumOf { it.fraction }, 0.01)
    }

    @Test
    fun `without a window the parts fill the bar — full is what unknown looks like`() {
        val g = ContextGauge.of(ctx(900, 0, ContextParts(system = 300, tools = 600)))!!
        assertEquals(1.0, g.segments.sumOf { it.fraction }, 1e-9)
        assertNull(g.percent)
    }

    @Test
    fun `nothing known draws nothing, and empty parts draw no segment`() {
        assertNull(ContextGauge.of(null))
        assertNull(ContextGauge.of(ctx(0, 0)))
        assertEquals(emptyList<ContextGauge.Segment>(), ContextGauge.of(ctx(5_000, 131_000))!!.segments)
    }

    @Test
    fun `tokens read the way Office writes them`() {
        assertEquals("999", ContextGauge.kilo(999))
        assertEquals("1.5k", ContextGauge.kilo(1_500))
        assertEquals("2k", ContextGauge.kilo(2_000))
        assertEquals("131k", ContextGauge.kilo(131_072))
    }
}
