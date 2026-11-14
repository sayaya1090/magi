package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil

class ScrollTraceTest : BasePlatformTestCase() {
    private val dumps = mutableListOf<String>()

    override fun setUp() {
        super.setUp()
        ScrollTrace.reset()
        ScrollTrace.sink = { dumps += it }
    }

    override fun tearDown() {
        try { ScrollTrace.sink = null; ScrollTrace.reset() } finally { super.tearDown() }
    }

    private fun pump(ms: Long) {
        val until = System.currentTimeMillis() + ms
        while (System.currentTimeMillis() < until) { UIUtil.dispatchAllInvocationEvents(); Thread.sleep(5) }
    }

    fun testOneLargeJumpNearABigRowIsRecordedWithWhatFollowedIt() {
        // Overshooting past a big bubble is one jump, not a back-and-forth: it never reached the
        // reversal count, so nothing was ever written about it.
        ScrollTrace.note("model", 1000, 1020, "following=false")
        ScrollTrace.note("anchor", 1020, 2420, "row=40")
        ScrollTrace.note("model", 1020, 2420, "following=false")
        ScrollTrace.note("model", 2420, 2460, "following=false")
        assertTrue("dumped before what followed was seen: $dumps", dumps.isEmpty())
        pump(1300)
        assertEquals(dumps.toString(), 1, dumps.size)
        assertTrue(dumps[0], dumps[0].startsWith("magi: transcript scroll jump anchor 1020->2420"))
        assertTrue("the moves after the jump are part of the record: ${dumps[0]}", "2420->2460" in dumps[0])
    }

    fun testOrdinaryScrollingAndSmallCorrectionsWriteNothing() {
        var y = 5000
        repeat(40) { ScrollTrace.note("model", y, y - 20, "following=false"); y -= 20 }
        ScrollTrace.note("anchor", y, y + 120, "row=3")
        ScrollTrace.note("cef-wheel", y, y + 90)
        pump(1300)
        assertTrue(dumps.toString(), dumps.isEmpty())
    }

    fun testALargeForwardedWheelFromAPageIsRecorded() {
        ScrollTrace.note("cef-wheel", 3000, 3800)
        pump(1300)
        assertEquals(dumps.toString(), 1, dumps.size)
        assertTrue(dumps[0], dumps[0].startsWith("magi: transcript scroll jump cef-wheel 3000->3800"))
    }

    /** A large move nobody marked (IDE 21:10:35, -1395 then +2498) names its caller in idea.log. */
    fun testALargeMoveRecordsTheCallerThatMadeIt() {
        ScrollTrace.moved(5000, 4980)
        assertTrue("small moves write nothing: $dumps", dumps.isEmpty())
        ScrollTrace.moved(5000, 3605)
        assertEquals(dumps.toString(), 1, dumps.size)
        assertTrue(dumps[0], dumps[0].startsWith("magi: transcript scroll move 5000->3605 by"))
        assertTrue("the stack names the caller: ${dumps[0]}", "testALargeMoveRecordsTheCallerThatMadeIt" in dumps[0])
        ScrollTrace.moved(3605, 6103)
        assertEquals("one stack per 1.5s", 1, dumps.size)
    }
}
