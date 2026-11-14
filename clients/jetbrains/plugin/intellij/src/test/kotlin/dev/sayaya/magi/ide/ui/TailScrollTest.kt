package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import java.awt.Dimension
import java.awt.event.MouseEvent
import java.awt.event.MouseWheelEvent
import javax.swing.JPanel
import javax.swing.JScrollPane

class TailScrollTest : BasePlatformTestCase() {
    fun testRestorePreservesReadingPositionAcrossGrowth() {
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane)
        try {
            flush(pane)
            tail.restore(600, false)
            flush(pane)
            assertEquals(600, pane.verticalScrollBar.value)
            body.preferredSize = Dimension(300, 8000)
            flush(pane)
            assertEquals(600, pane.verticalScrollBar.value)
            tail.restore(0, true)
            flush(pane); assertAtEnd(pane)
        } finally { tail.close() }
    }
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

    fun testStreamingKeepsCompletedRowsAttachedAndReaderPositionStable() = checkStreamingRows("text")

    fun testThinkingKeepsItsRowAttachedAndReaderPositionStable() = checkStreamingRows("reasoning")

    private fun checkStreamingRows(kind: String) {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view)
            val rows = field("shaper") as dev.sayaya.magi.ide.usecase.Rows
            val pane = field("scroll") as JScrollPane
            val column = field("column") as javax.swing.JComponent
            val redraw = view.javaClass.getDeclaredMethod("redrawLog").apply { isAccessible = true }
            var seq = 0L
            fun feed(type: String, data: kotlinx.serialization.json.JsonElement) {
                rows.feed(dev.sayaya.magi.ide.model.LogEvent(seq = ++seq, type = type, data = data))
                redraw.invoke(view)
                UIUtil.dispatchAllInvocationEvents()
            }
            fun delta(text: String) = feed("part.delta", kotlinx.serialization.json.buildJsonObject {
                put("messageId", kotlinx.serialization.json.JsonPrimitive("stream"))
                put("kind", kotlinx.serialization.json.JsonPrimitive(kind))
                put("text", kotlinx.serialization.json.JsonPrimitive(text))
            })
            fun layout(c: java.awt.Container) { c.doLayout(); c.components.filterIsInstance<java.awt.Container>().forEach(::layout) }
            pane.size = Dimension(480, 320)
            fun settle() { repeat(3) {
                pane.doLayout()
                column.setSize(pane.viewport.width, column.preferredSize.height)
                layout(column); pane.viewport.doLayout(); UIUtil.dispatchAllInvocationEvents()
            } }
            for (n in 1..2) feed("part.appended", kotlinx.serialization.json.buildJsonObject {
                put("messageId", kotlinx.serialization.json.JsonPrimitive("old-$n"))
                put("part", kotlinx.serialization.json.buildJsonObject {
                    put("kind", kotlinx.serialization.json.JsonPrimitive("text"))
                    put("text", kotlinx.serialization.json.JsonPrimitive("finished answer $n. ".repeat(80)))
                })
            })
            delta("```kotlin\n" + "val x = 1\n".repeat(80))
            if (kind == "reasoning") {
                val closedDraft = column.getComponent(2)
                delta("more hidden reasoning\n")
                assertSame("closed thinking must stay attached", closedDraft, column.getComponent(2))
                assertNull((closedDraft as javax.swing.JComponent).getClientProperty("magi.draft.body"))
                val row = rows.list().last()
                view.javaClass.getDeclaredMethod("flip", dev.sayaya.magi.ide.usecase.Row::class.java)
                    .apply { isAccessible = true }.invoke(view, row)
                UIUtil.dispatchAllInvocationEvents()
            }
            settle()
            assertAtEnd(pane)
            repeat(3) { delta("still following\n"); settle(); assertAtEnd(pane) }
            val history = listOf(column.getComponent(0), column.getComponent(1))
            val bounds = history.map { java.awt.Rectangle(it.bounds) }
            val draft = column.getComponent(2) as javax.swing.JComponent
            val draftBody = draft.getClientProperty("magi.draft.body")
            assertTrue("streaming must not reload CEF", if (kind == "text") draftBody is StreamingMarkdown else draftBody is javax.swing.JTextArea)
            var detachedHistory = 0
            column.addContainerListener(object : java.awt.event.ContainerAdapter() {
                override fun componentRemoved(e: java.awt.event.ContainerEvent) { if (e.child in history) detachedHistory++ }
            })
            pane.dispatchEvent(MouseWheelEvent(pane, MouseEvent.MOUSE_WHEEL, 0, 0, 10, 10, 0, false, MouseWheelEvent.WHEEL_UNIT_SCROLL, 3, -1))
            settle()
            val reading = pane.viewport.viewPosition.y
            repeat(20) {
                delta("val line$it = $it\n".repeat(4))
                settle()
                assertSame(history[0], column.getComponent(0))
                assertSame(history[1], column.getComponent(1))
                assertEquals(bounds, history.map { java.awt.Rectangle(it.bounds) })
                assertSame(draft, column.getComponent(2))
                assertSame(draftBody, draft.getClientProperty("magi.draft.body"))
                assertEquals("reading position moved during streaming", reading, pane.viewport.viewPosition.y)
            }
            assertEquals(0, detachedHistory)
            assertTrue((draft.getClientProperty("magi.row.snapshot") as dev.sayaya.magi.ide.usecase.Row).text.contains("line19"))
            val other = MagiToolWindow.View(project, sendSession = { "s2" }, sendConnection = { _, _, _ -> })
            try {
                val owned = field("richAnswers") as RichAnswer
                val second = other.javaClass.getDeclaredField("richAnswers").apply { isAccessible = true }.get(other) as RichAnswer
                owned.keepOnly(listOf("first")); second.keepOnly(listOf("second"))
                val eligible = RichAnswer::class.java.getDeclaredField("eligible").apply { isAccessible = true }
                assertEquals(setOf("first"), eligible.get(owned))
                assertEquals(setOf("second"), eligible.get(second))
            } finally { com.intellij.openapi.util.Disposer.dispose(other) }
        } finally { com.intellij.openapi.util.Disposer.dispose(view) }
    }

    fun testWheelOverAnAnswerStillLoadingMovesAsFarAsOnceItHasLoaded() {
        // Measured on JCEF off-screen rendering: the page sees deltaY = 10 px x scrollAmount x rotation.
        val source = JPanel()
        fun wheel(amount: Int, precise: Double) = MouseWheelEvent(source, MouseEvent.MOUSE_WHEEL, 0L, 0, 0, 0, 0, 0, 0, false,
            MouseWheelEvent.WHEEL_UNIT_SCROLL, amount, precise.toInt(), precise)
        assertEquals(10.0, TailScroll.wheelPixels(wheel(1, 1.0)), 1e-9)
        assertEquals(30.0, TailScroll.wheelPixels(wheel(3, 1.0)), 1e-9)
        assertEquals(-1.5, TailScroll.wheelPixels(wheel(1, -0.15)), 1e-9)
    }

    /**
     * A diagram node or radio picked after the answer ended shows or hides the notes under the diagram,
     * and the row grows or shrinks. Sandbox repro: following=true, height 720->847, tail 3282->3447 -
     * the diagram under the pointer jumped 165px. Following the end is for an answer still arriving.
     */
    fun testGrowthAfterTheTurnEndedDoesNotPullTheViewToTheEnd() {
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane, graceMs = 0)
        try {
            flush(pane); assertAtEnd(pane)
            tail.streaming(false)
            val before = pane.verticalScrollBar.value
            body.preferredSize = Dimension(300, 4127)
            flush(pane)
            assertEquals("a height change after the turn leaves the reader where they are", before, pane.verticalScrollBar.value)
            tail.followEnd()
            flush(pane); assertAtEnd(pane)
        } finally { tail.close() }
    }

    fun testGrowthWhileTheTurnStreamsStillFollowsTheEnd() {
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane, graceMs = 0)
        try {
            flush(pane); assertAtEnd(pane)
            tail.streaming(true)
            body.preferredSize = Dimension(300, 4600)
            flush(pane); assertAtEnd(pane)
        } finally { tail.close() }
    }

    /** Review: after the turn a growing row left the view above the end, yet followingEnd stayed true and the latest button stayed off. */
    fun testAViewLeftAboveTheEndAfterTheTurnIsNotFollowingTheEnd() {
        val body = JPanel().apply { preferredSize = Dimension(300, 4000) }
        val pane = JScrollPane(body).apply { size = Dimension(400, 300) }
        val tail = TailScroll(pane, graceMs = 0)
        try {
            flush(pane); tail.streaming(false)
            body.preferredSize = Dimension(300, 4700); flush(pane)
            assertTrue(pane.verticalScrollBar.value < pane.verticalScrollBar.maximum - pane.verticalScrollBar.visibleAmount)
            assertFalse("the latest button must be offered away from the end", tail.followingEnd)
            tail.followEnd(); flush(pane); assertAtEnd(pane)
            assertTrue("at the end again it follows", tail.followingEnd)
        } finally { tail.close() }
    }
}

