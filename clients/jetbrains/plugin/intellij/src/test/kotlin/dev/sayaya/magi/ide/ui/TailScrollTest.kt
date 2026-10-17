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

    fun testStreamingKeepsCompletedRowsAttachedAndReaderPositionStable() {
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
                put("kind", kotlinx.serialization.json.JsonPrimitive("text"))
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
            settle()
            assertAtEnd(pane)
            repeat(3) { delta("still following\n"); settle(); assertAtEnd(pane) }
            val history = listOf(column.getComponent(0), column.getComponent(1))
            val bounds = history.map { java.awt.Rectangle(it.bounds) }
            val draft = column.getComponent(2) as javax.swing.JComponent
            val draftBody = draft.getClientProperty("magi.draft.body")
            assertTrue("streaming must not reload CEF", draftBody is javax.swing.JTextArea)
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

}
