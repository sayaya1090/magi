package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import java.awt.Dimension
import javax.swing.JPanel
import javax.swing.JScrollPane

/**
 * Replays two idea.log dumps of the "large answer boundary" scroll. 16:40:13: a row above the reader
 * left, the 1320px answer slid from row 7543 to 7542 (26->1320, 1320->26) and the anchor, kept by row
 * number, jumped 752676->753921 (+1245). 16:21:55: wheel events over an answer at the end of the
 * transcript were logged as a 370px cef-wheel jump although the bar could not move.
 */
class TranscriptDumpReplayTest : BasePlatformTestCase() {
    private fun flush(pane: JScrollPane) = repeat(4) {
        pane.doLayout(); pane.viewport.doLayout(); (pane.viewport.view as java.awt.Container).doLayout()
        UIUtil.dispatchAllInvocationEvents()
    }

    private fun paint(column: VirtualTranscript, pane: JScrollPane, keys: List<String>, panels: List<JPanel>) {
        column.rekey(keys) //REKEY
        column.range(keys.size, pane.viewport.viewPosition.y, pane.viewport.extentSize.height)
        column.start(0)
        column.removeAll(); panels.forEach { column.add(it) }
        column.revalidate(); flush(pane)
    }

    fun testTheRowShiftLoggedAt1640KeepsTheReaderOnTheAnswer() {
        val sizes = intArrayOf(500, 75, 26, 75, 26, 1320, 26, 73, 137, 121, 53)
        val column = VirtualTranscript {}
        val pane = JScrollPane(column).apply { size = Dimension(300, 400) }
        val panels = sizes.map { JPanel().apply { preferredSize = Dimension(100, it) } }.toMutableList()
        val keys = sizes.indices.map { "k$it" }.toMutableList()
        paint(column, pane, keys, panels)
        val before = sizes.take(5).sum() + 600          // 600px into the 1320px answer
        pane.viewport.viewPosition = java.awt.Point(0, before); flush(pane)
        assertEquals(before, pane.viewport.viewPosition.y)
        panels.removeAt(1); keys.removeAt(1)            // a 75px row above leaves
        paint(column, pane, keys, panels)
        val moved = pane.viewport.viewPosition.y - before
        println("REPLAY before=$before after=${pane.viewport.viewPosition.y} moved=$moved")
        assertEquals("the reader moves only by the row that left", -sizes[1], moved)
    }

    fun testAWheelPastTheEndIsNotRecordedAsAJump() {
        val body = JPanel().apply { preferredSize = Dimension(300, 2000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        pane.doLayout(); pane.viewport.doLayout(); UIUtil.dispatchAllInvocationEvents()
        val bar = pane.verticalScrollBar
        bar.value = bar.maximum - bar.visibleAmount
        val end = bar.value
        val got = mutableListOf<String>()
        ScrollTrace.reset(); ScrollTrace.sink = { got += it }
        try {
            TailScroll.forwardWheel(pane, 370.0)
            val until = System.currentTimeMillis() + 1800
            while (System.currentTimeMillis() < until) { Thread.sleep(50); UIUtil.dispatchAllInvocationEvents() }
            assertEquals("the bar is at the end and cannot move", end, bar.value)
            assertTrue("no jump is logged for a move that did not happen: $got", got.none { it.contains("jump cef-wheel") })
        } finally { ScrollTrace.sink = null; ScrollTrace.reset() }
    }
}
