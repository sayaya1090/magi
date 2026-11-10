package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import java.awt.Dimension
import javax.swing.JComponent
import javax.swing.JPanel
import javax.swing.JScrollPane
import javax.swing.ScrollPaneConstants
import javax.swing.SwingUtilities

class AppendFollowTest : BasePlatformTestCase() {
    private fun pump(ms: Long) {
        val until = System.currentTimeMillis() + ms
        while (System.currentTimeMillis() < until) { UIUtil.dispatchAllInvocationEvents(); Thread.sleep(4) }
    }

    fun testRowsLandingWhileFollowingNeverPullTheViewBack() {
        // A tool-call turn lands a short row every few frames. A new row used to count as 100px
        // until laid out, so the followed end jumped past it and fell back once it was measured.
        val rows = MutableList<JComponent>(150) { JPanel(null).apply { preferredSize = Dimension(10, 24 + (it * 37) % 200) } }
        lateinit var redraw: () -> Unit
        val column = VirtualTranscript { redraw() }
        val pane = JScrollPane(column).apply {
            verticalScrollBarPolicy = ScrollPaneConstants.VERTICAL_SCROLLBAR_ALWAYS
            horizontalScrollBarPolicy = ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER }
        val frame = javax.swing.JFrame("append").apply {
            rootPane.glassPane = com.intellij.openapi.wm.impl.IdeGlassPaneImpl(rootPane)
            contentPane.add(pane); setSize(500, 380); isVisible = true }
        val tail = TailScroll(pane)
        var dirty = false
        redraw = {
            if (!dirty) { dirty = true; SwingUtilities.invokeLater {
                dirty = false
                val extent = pane.viewport.extentSize.height
                val y = if (tail.followingEnd) column.endPosition(rows.size, extent) else pane.viewport.viewPosition.y
                val range = column.range(rows.size, y, extent)
                column.start(range.first.coerceAtLeast(0))
                val want = range.map { rows[it] }
                if (column.components.toList() != want) { column.removeAll(); want.forEach { column.add(it) } }
                column.revalidate(); column.repaint()
            } }
        }
        pane.viewport.addChangeListener { redraw() }
        try {
            redraw(); pump(500)
            val bar = pane.verticalScrollBar
            val values = mutableListOf(bar.value)
            bar.model.addChangeListener { if (bar.value != values.last()) values += bar.value }
            repeat(12) { i ->
                rows += JPanel(null).apply { preferredSize = Dimension(10, if (i % 2 == 0) 20 else 36) }
                redraw(); pump(60)
            }
            pump(300)
            val back = values.zipWithNext().count { (a, b) -> b < a }
            assertTrue("followed end moved back $back times: $values", values.size > 1)
            assertEquals("followed end moved back: $values", 0, back)
            assertEquals(bar.maximum - bar.model.extent, bar.value)
        } finally { tail.close(); frame.dispose() }
    }
}
