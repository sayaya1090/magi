package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.awt.Component
import java.awt.Dimension
import java.awt.Point
import java.awt.Rectangle
import javax.swing.JPanel
import javax.swing.JTextArea

class StreamingHeightTest : BasePlatformTestCase() {
    fun testMarkdownBlocksRenderBeforeCompletionAndKeepCompletedDocuments() {
        val view = StreamingMarkdown("# Heading\n\n**Finished** paragraph.\n\n```kotlin\nval x = 1")
        assertEquals(3, view.componentCount)
        val heading = view.getComponent(0)
        val finished = view.getComponent(1) as JPanel
        val editor = finished.getComponent(0) as javax.swing.JEditorPane
        val document = editor.document
        assertTrue(editor.text.contains("<b>Finished</b>"))
        view.update("# Heading\n\n**Finished** paragraph.\n\n```kotlin\nval x = 1\nval y = 2\n```\n\nNext")
        assertSame(heading, view.getComponent(0))
        assertSame(finished, view.getComponent(1))
        assertSame(document, editor.document)
        assertEquals(4, view.componentCount)
        view.update("Replacement <img src=x> **answer**")
        assertEquals(1, view.componentCount)
        val replacement = (view.getComponent(0) as JPanel).getComponent(0) as javax.swing.JEditorPane
        assertFalse(replacement.text.contains("<img"))
        assertTrue(replacement.text.contains("<b>answer</b>"))
        view.update("")
        assertEquals(0, view.componentCount)
    }

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
    fun testSettledAnswerFlippingBetweenTwoHeightsDoesNotShakeTheFollowingView() {
        // A finished CEF answer can reflow and report two heights in turn at one width;
        // following them made the view jump up and down as fast as the reports came.
        val sizing = StreamingHeight(false)
        val actual = listOf(400, 420, 400, 420, 400).map { sizing.accept(it, 300) }
        assertEquals(listOf(400, 420, 420, 420, 420), actual)
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = javax.swing.JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane)
        val flipping = StreamingHeight(false)
        try {
            var previous = 0
            for (sample in listOf(3600, 3640, 3600, 3640, 3600, 3640)) {
                body.preferredSize = Dimension(300, 400 + flipping.accept(sample, 300)!!)
                pane.doLayout(); pane.viewport.doLayout()
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                assertTrue("scrollbar moved backwards for sample $sample", pane.verticalScrollBar.value >= previous)
                previous = pane.verticalScrollBar.value
            }
        } finally { tail.close() }
        // a single genuine shrink, and a new width, still apply
        val once = StreamingHeight(false)
        assertEquals(900, once.accept(900, 300)); assertEquals(700, once.accept(700, 300))
        assertEquals(500, once.accept(500, 300)); assertEquals(640, once.accept(640, 600))
    }
    fun testSettledAnswerReturningToAnEarlierHeightLaterStillApplies() {
        // Expanding then collapsing, or zooming in then out, returns to an earlier height
        // well after the change; only a quick back-and-forth is held.
        var now = 0L
        val sizing = StreamingHeight(false) { now }
        assertEquals(400, sizing.accept(400, 300))
        now = 100; assertEquals(600, sizing.accept(600, 300))
        now = 3100; assertEquals("a return seconds later is a real change", 400, sizing.accept(400, 300))
        now = 3130; assertEquals(600, sizing.accept(600, 300))
        now = 3160; assertEquals("a return within moments is a reflow flip", 600, sizing.accept(400, 300))
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


    fun testAStreamingMermaidFenceShowsAPlaceholderNotItsSource() {
        val view = StreamingMarkdown("Intro\n\n```mermaid\nflowchart LR\n A --> B")
        fun texts(c: java.awt.Container): List<String> = c.components.flatMap { k ->
            listOfNotNull((k as? javax.swing.text.JTextComponent)?.document?.let { it.getText(0, it.length) }, (k as? javax.swing.JLabel)?.text) +
                ((k as? java.awt.Container)?.let { texts(it) } ?: emptyList())
        }
        fun pending(c: java.awt.Container): Int = c.components.sumOf { k ->
            (if ((k as? javax.swing.JComponent)?.getClientProperty("magi.diagram.pending") == true) 1 else 0) +
                ((k as? java.awt.Container)?.let { pending(it) } ?: 0)
        }
        assertFalse(texts(view).toString(), texts(view).any { it.contains("flowchart") })
        assertEquals(1, pending(view))
        // The fence closes and prose follows: still a placeholder, and the prose is formatted.
        view.update("Intro\n\n```mermaid\nflowchart LR\n A --> B\n```\n\n**Done**")
        assertFalse(texts(view).toString(), texts(view).any { it.contains("flowchart") })
        assertEquals(1, pending(view))
        assertTrue(texts(view).toString(), texts(view).any { it.contains("Done") })
        // An ordinary fence still shows its code.
        view.update("```kotlin\nval x = 1")
        assertEquals(0, pending(view))
        assertTrue(texts(view).any { it.contains("val x = 1") })
    }

    /** Swapping row components moves focus; the row that receives it must not be scrolled into view. */
    fun testRearrangingRowsCannotScrollAFocusedRowIntoView() {
        val text = JTextArea("body")
        val viewport = TranscriptViewport { text }
        val content = JPanel().apply { add(text); preferredSize = Dimension(300, 4000) }
        viewport.setView(content)
        viewport.size = Dimension(300, 300)
        viewport.doLayout()
        viewport.viewPosition = Point(0, 3700)
        viewport.quietly { viewport.scrollRectToVisible(Rectangle(0, -3700, 10, 20)) }
        assertEquals("no reveal while rows are rearranged", 3700, viewport.viewPosition.y)
        viewport.scrollRectToVisible(Rectangle(0, -3700, 10, 20))
        assertEquals("nor from focus moved in the same tick", 3700, viewport.viewPosition.y)
        com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
        viewport.scrollRectToVisible(Rectangle(0, -3700, 10, 20))
        assertEquals("focused navigation is available again afterwards", 0, viewport.viewPosition.y)
    }
}
