package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import java.awt.Dimension
import javax.swing.JPanel
import javax.swing.JScrollPane

/**
 * idea.log 16:30:55: a row above the reader left the transcript, every height below it slid one row
 * (91->26, 26->123 ... 26->988, 988->26) and the anchor, kept by row number, jumped ~900px. The reader
 * must stay on what they were reading, so heights and the anchor follow the row, not its index.
 */
class VirtualTranscriptAnchorTest : BasePlatformTestCase() {
    private val sizes = intArrayOf(50, 120, 80, 60, 90, 70, 110, 40, 130, 100)

    private fun row(h: Int) = JPanel().apply { preferredSize = Dimension(100, h) }

    private fun flush(pane: JScrollPane) = repeat(3) {
        pane.doLayout(); pane.viewport.doLayout(); (pane.viewport.view as? java.awt.Container)?.doLayout()
        UIUtil.dispatchAllInvocationEvents()
    }

    private fun paint(column: VirtualTranscript, pane: JScrollPane, keys: List<String>, panels: List<JPanel>) {
        column.rekey(keys)
        column.range(keys.size, pane.viewport.viewPosition.y, pane.viewport.extentSize.height)
        column.start(0)
        column.removeAll(); panels.forEach { column.add(it) }
        column.revalidate(); flush(pane)
    }

    fun testARowLeavingAboveKeepsTheReaderOnTheSameContent() {
        val column = VirtualTranscript {}
        val pane = JScrollPane(column).apply { size = Dimension(200, 150) }
        val panels = sizes.map { row(it) }.toMutableList()
        val keys = sizes.indices.map { "k$it" }.toMutableList()
        paint(column, pane, keys, panels)
        val rowFive = sizes.take(5).sum()
        pane.viewport.viewPosition = java.awt.Point(0, rowFive + 10)
        flush(pane)
        assertEquals(rowFive + 10, pane.viewport.viewPosition.y)

        panels.removeAt(1); keys.removeAt(1)
        paint(column, pane, keys, panels)

        assertEquals("still 10px into the row that was k5", rowFive - sizes[1] + 10, pane.viewport.viewPosition.y)
        assertEquals("total height follows the rows, not their old slots", sizes.sum() - sizes[1], column.preferredSize.height)
    }
    fun testPreferredSizeQueriesDoNotMeasureOrScheduleWork() {
        var changed = 0
        var layouts = 0
        val column = VirtualTranscript { changed++ }
        val panel = object : JPanel() {
            override fun doLayout() { layouts++; super.doLayout() }
        }.apply { preferredSize = Dimension(100, 300) }
        column.setSize(400, 100)
        column.rekey(listOf("a")); column.start(0); column.add(panel); column.doLayout()
        val before = layouts to changed
        repeat(1000) { assertEquals(300, column.preferredSize.height) }
        assertEquals(before, layouts to changed)
    }

    fun testNavigationInvalidatesQueuedCorrection() {
        val body = JPanel().apply { preferredSize = Dimension(100, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(200, 300) }
        pane.doLayout(); pane.viewport.doLayout()
        val coordinator = TranscriptCoordinator.of(pane)
        coordinator.request(2000, "anchor")
        coordinator.cancel(-1)
        pane.verticalScrollBar.value = 400
        UIUtil.dispatchAllInvocationEvents()
        assertEquals(400, pane.verticalScrollBar.value)
    }

    fun testFocusedRowsCanStayAttachedWithoutRenderingTheInterveningHistory() {
        val column = VirtualTranscript {}
        column.setSize(400, 100)
        column.rekey(List(10_000) { "r$it" })
        column.attach(listOf(2, 9999))
        column.add(row(120)); column.add(row(150)); column.doLayout()
        assertEquals(2, column.componentCount)
        assertEquals(200, column.getComponent(0).y)
        assertEquals(999920, column.getComponent(1).y)
    }

    fun testAnOscillatingRendererDoesNotKeepChangingCommittedGeometry() {
        var changed = 0
        val column = VirtualTranscript { changed++ }
        val body = object : JPanel() {
            override fun getPreferredSize() = Dimension(100, if (height >= 200) 100 else 300)
        }
        column.setSize(400, 100); column.rekey(listOf("a")); column.add(body)
        column.doLayout()
        val height = column.preferredSize.height
        val count = changed
        repeat(20) { column.doLayout(); assertEquals(height, column.preferredSize.height) }
        assertEquals(count, changed)
    }

    fun testLogicalRestoreMeasuresTheTargetBeforeClampingItsDepth() {
        val column = VirtualTranscript {}
        val pane = JScrollPane(column).apply { size = Dimension(200, 300) }
        val tail = TailScroll(pane)
        try {
            column.rekey(List(100) { "r$it" })
            tail.restore(0, false)
            assertTrue(column.restore(TranscriptGeometry.Anchor("r50", 700)))
            assertEquals(50, column.restorationIndex())
            column.attach(listOf(50)); column.add(row(1000))
            flush(pane)
            assertEquals(5700, pane.viewport.viewPosition.y)
        } finally { tail.close() }
    }


    /**
     * IDE log 21:10:35: scrolling up through a big answer while another answer streamed, the view jumped
     * -1395 then +2498 as a row entered at the 100px guess. Every streamed row rekeyed the column, which
     * dropped the retained range, so output below a view that is not at the end re-attached and
     * re-measured rows above it. Rows appended after the existing ones leave the attached range alone.
     */
    fun testRowsAppendedBelowAViewOffTheEndKeepTheAttachedRange() {
        val column = VirtualTranscript {}
        val pane = javax.swing.JScrollPane(column).apply { size = java.awt.Dimension(300, 400) }
        val ids = (0 until 200).map { "r$it" }
        column.rekey(ids)
        val before = column.range(200, 10000, 400)
        TranscriptCoordinator.of(pane).cancel(-1)            // the person is scrolling up
        column.rekey(ids + "r200")
        assertEquals("output below the view does not recompute the attached range", before, column.range(201, 10000, 400))
        column.rekey(listOf("r-1") + ids + "r200")
        assertFalse("a row inserted above does recompute it", before == column.range(202, 10000, 400))
    }
}
