package dev.sayaya.magi.ide.ui

import junit.framework.TestCase
import kotlin.random.Random

class TranscriptModelTest : TestCase() {
    fun testDocumentMarkerSurvivesBodyOnlyPatching() {
        val html = RichAnswer.documentHtml("<html><body class='markdown'>answer</body></html>", "revision")
        assertTrue(html.substringAfter("<body").substringBefore("</body>").contains("content=\"revision\""))
        assertTrue(RichAnswer.documentHtml("<p>fragment</p>", "r").startsWith("<meta"))
    }
    fun testGeometryMatchesReferenceAcrossTenThousandRows() {
        val random = Random(714)
        val sizes = IntArray(10_000) { random.nextInt(1, 1500) }
        val g = TranscriptGeometry()
        g.replace(sizes.indices.map { "r$it" }, null)
        sizes.forEachIndexed { i, h -> g.measure(i, h) }; g.rebuild()
        val offsets = sizes.runningFold(0) { a, b -> a + b }
        repeat(1000) {
            val y = random.nextInt(offsets.last())
            val expected = offsets.indexOfLast { it <= y }.coerceAtMost(sizes.lastIndex)
            assertEquals(expected, g.at(y))
            assertEquals(y, g.position(g.anchor(y)!!))
        }
        assertEquals(offsets.last(), g.total)
        assertEquals(IntRange.EMPTY, TranscriptGeometry().range(0, 400, 0))
    }
    fun testRemovedAnchorUsesNextSurvivorAndInsertionKeepsDepth() {
        val g = TranscriptGeometry()
        g.replace(listOf("a", "b", "c"), null)
        g.measure(0, 250); g.rebuild()
        val anchor = g.anchor(275)!!
        assertEquals(375, g.position(g.replace(listOf("new", "a", "b", "c"), anchor)!!))
        val replacement = g.replace(listOf("new", "a", "c"), anchor)!!
        assertEquals("c", replacement.id)
        assertEquals(350, g.position(replacement))
        assertEquals("a", g.replace(listOf("a"), replacement)!!.id)
    }
    fun testOverscanPrefersTravelDirectionAndNeverOmitsViewport() {
        val g = TranscriptGeometry(); g.replace(List(1000) { "$it" }, null)
        val visible = g.visible(4000, 400)
        val down = g.range(4000, 400, 1)
        val up = g.range(4000, 400, -1)
        assertTrue(down.first <= visible.first && down.last >= visible.last)
        assertTrue(up.first < down.first && down.last > up.last)
        val stationary = g.range(4000, 400, 0)
        assertEquals(32, stationary.first); assertEquals(52, stationary.last)
    }
    fun testCumulativeHeightsDoNotOverflowOrBecomeNegative() {
        val g = TranscriptGeometry(); g.replace(listOf("a", "b", "c"), null)
        repeat(3) { g.measure(it, Int.MAX_VALUE) }; g.rebuild()
        assertEquals(Int.MAX_VALUE - 1, g.total)
        assertTrue(g.offset(2) > 0)
    }
    fun testHeightReportsRejectOldDocumentsWidthsAndOutOfOrderSamples() {
        val reports = HeightReports()
        val first = reports.begin(300)!!
        assertTrue(reports.accept(first.generation, 300, 1, 500, 300))
        assertFalse(reports.accept(first.generation, 300, 1, 20, 300))
        val second = reports.begin(600)!!
        assertFalse(reports.accept(first.generation, 300, 2, 20, 600))
        assertFalse(reports.accept(second.generation, 600, 1, 20, 300))
        assertTrue(reports.accept(second.generation, 600, 2, 20, 600))
        reports.invalidate()
        assertFalse(reports.accept(second.generation, 600, 3, 30, 600))
        reports.close(); assertNull(reports.begin(600))
    }
    fun testFollowIntentAndActualEndAreIndependentAfterGrace() {
        var now = 0L
        val p = ScrollPolicy(1500) { now }
        p.streaming(false)
        now = 1501
        assertTrue(p.following)
        assertFalse(p.headingToEnd(false))
        assertTrue(p.headingToEnd(true))
        p.pinPending = true; assertTrue(p.headingToEnd(false))
        p.dragging = true; assertFalse(p.headingToEnd(true))
        p.pinPending = false; p.dragging = false; p.streaming(true)
        assertTrue(p.headingToEnd(false))
    }
}
