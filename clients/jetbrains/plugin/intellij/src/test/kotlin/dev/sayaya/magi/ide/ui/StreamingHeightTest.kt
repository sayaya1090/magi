package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.awt.Component
import java.awt.Dimension
import java.awt.Point
import java.awt.Rectangle
import javax.swing.JPanel
import javax.swing.JTextArea

class StreamingHeightTest : BasePlatformTestCase() {
    fun testPartialDomMeasurementsCannotPullStreamingContentUpAndDown() {
        val sizing = StreamingHeight(true)
        val actual = listOf(1800, 72, 2200, 96, 2600).map { sizing.accept(it, 400) }
        assertEquals(listOf(1800, 1800, 2200, 2200, 2600), actual)
        sizing.streaming = false
        assertEquals(2400, sizing.accept(2400, 400))
    }
    fun testWidthChangesMayShrinkAndDetachedMeasurementsAreIgnored() {
        val sizing = StreamingHeight(true)
        assertEquals(3000, sizing.accept(3000, 300))
        assertNull(sizing.accept(80, 0))
        assertEquals(3000, sizing.accept(80, 300))
        assertEquals(1500, sizing.accept(1500, 600))
    }
    fun testCaretRevealCannotMoveTranscriptWhenFocusIsInComposer() {
        var focused: Component? = JTextArea()
        val viewport = TranscriptViewport { focused }
        val text = JTextArea("body")
        val content = JPanel().apply { add(text); preferredSize = Dimension(300, 4000) }
        viewport.setView(content)
        viewport.size = Dimension(300, 300)
        viewport.doLayout()
        viewport.viewPosition = Point(0, 3700)
        viewport.scrollRectToVisible(Rectangle(0, -3700, 10, 20))
        assertEquals("background caret must not move the reader", 3700, viewport.viewPosition.y)
        focused = text
        viewport.scrollRectToVisible(Rectangle(0, -3700, 10, 20))
        assertEquals("focused transcript navigation remains available", 0, viewport.viewPosition.y)
    }
    fun testTransientMeasurementsDoNotMoveTheFollowingScrollbarBackwards() {
        val body = JPanel().apply { preferredSize = Dimension(300, 1800) }
        val pane = javax.swing.JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane)
        val sizing = StreamingHeight(true)
        try {
            var previous = 0
            for (sample in listOf(1800, 72, 2200, 96, 2600)) {
                body.preferredSize = Dimension(300, sizing.accept(sample, 300)!!)
                pane.doLayout(); pane.viewport.doLayout()
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                assertTrue("scrollbar moved backwards for sample $sample", pane.verticalScrollBar.value >= previous)
                previous = pane.verticalScrollBar.value
            }
            TailScroll.forwardWheel(pane, -500.0)
            val reading = pane.verticalScrollBar.value
            body.preferredSize = Dimension(300, sizing.accept(3200, 300)!!)
            pane.doLayout(); pane.viewport.doLayout()
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
            assertEquals("new content must not pull a person reading above the bottom", reading, pane.verticalScrollBar.value)
        } finally { tail.close() }
    }

}
