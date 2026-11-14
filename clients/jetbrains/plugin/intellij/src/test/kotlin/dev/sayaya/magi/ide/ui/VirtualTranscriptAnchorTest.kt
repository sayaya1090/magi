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
}
