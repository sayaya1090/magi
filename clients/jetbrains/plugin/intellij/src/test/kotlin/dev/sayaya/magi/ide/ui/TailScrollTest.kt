package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import java.awt.Dimension
import java.awt.event.MouseEvent
import java.awt.event.MouseWheelEvent
import javax.swing.JPanel
import javax.swing.JScrollPane

class TailScrollTest : BasePlatformTestCase() {
    private fun flush(pane: JScrollPane) {
        pane.doLayout(); pane.viewport.doLayout(); UIUtil.dispatchAllInvocationEvents()
    }
    private fun assertAtEnd(pane: JScrollPane) {
        val bar = pane.verticalScrollBar
        assertEquals("must follow final layout height", bar.maximum - bar.visibleAmount, bar.value)
    }
    fun `test follows height changes after more than two layout passes`() {
        val body = JPanel().apply { preferredSize = Dimension(300, 1200) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane)
        try {
            flush(pane); assertAtEnd(pane)
            for (height in listOf(1800, 3200, 5100, 9000)) {
                body.preferredSize = Dimension(300, height)
                flush(pane); assertAtEnd(pane)
            }
            pane.size = Dimension(240, 180)
            flush(pane); assertAtEnd(pane)
        } finally { tail.close() }
    }
    fun `test rich wheel cancels queued pins and reaching bottom resumes following`() {
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane)
        try {
            flush(pane)
            tail.contentChanged()
            TailScroll.forwardWheel(pane, -800.0)
            val reading = pane.verticalScrollBar.value
            flush(pane)
            assertEquals(reading, pane.verticalScrollBar.value)
            body.preferredSize = Dimension(300, 8000)
            flush(pane)
            assertEquals("late growth must not pull a reader to the bottom", reading, pane.verticalScrollBar.value)
            TailScroll.forwardWheel(pane, 100000.0)
            flush(pane); assertAtEnd(pane)
            body.preferredSize = Dimension(300, 12000)
            flush(pane); assertAtEnd(pane)
        } finally { tail.close() }
    }
    fun `test native wheel pauses and disposal cancels pending pin`() {
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane)
        try {
            flush(pane)
            pane.dispatchEvent(MouseWheelEvent(pane, MouseEvent.MOUSE_WHEEL, 0, 0,
                10, 10, 0, false, MouseWheelEvent.WHEEL_UNIT_SCROLL, 3, -1))
            val reading = pane.verticalScrollBar.value
            body.preferredSize = Dimension(300, 8000)
            flush(pane)
            assertEquals(reading, pane.verticalScrollBar.value)
            tail.followEnd(); tail.close()
            flush(pane)
            assertEquals(reading, pane.verticalScrollBar.value)
        } finally { tail.close() }
    }
    fun `test actual transcript view keeps following late content sizing`() {
        val view = MagiToolWindow.View(project)
        try {
            val pane = view.javaClass.getDeclaredField("scroll").apply { isAccessible = true }.get(view) as JScrollPane
            val body = pane.viewport.view as javax.swing.JComponent
            pane.size = Dimension(400, 300)
            body.preferredSize = Dimension(300, 2000)
            flush(pane); assertAtEnd(pane)
            body.preferredSize = Dimension(300, 10000)
            flush(pane); assertAtEnd(pane)
        } finally { view.dispose() }
    }

}
