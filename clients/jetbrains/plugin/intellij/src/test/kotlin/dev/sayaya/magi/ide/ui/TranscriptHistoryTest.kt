package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.usecase.Rows
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import javax.swing.JScrollPane
import java.awt.Dimension
import javax.swing.JComponent

class TranscriptHistoryTest : BasePlatformTestCase() {
    fun testLongHistoryVirtualizesBothScrollDirections() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view)
            val rows = field("shaper") as Rows
            val column = field("column") as JComponent
            repeat(600) { n ->
                rows.feed(LogEvent(seq = n.toLong() + 1, type = "part.appended", data = buildJsonObject {
                    put("messageId", "history-$n")
                    put("part", buildJsonObject { put("kind", "text"); put("text", "message $n") })
                }))
            }
            view.javaClass.getDeclaredMethod("redrawLog").apply { isAccessible = true }.invoke(view)
            UIUtil.dispatchAllInvocationEvents()
            assertTrue("startup must not attach every historical row", column.componentCount <= 101)
            assertEquals("display limits must not discard history", 600, rows.list().size)
            val pane = field("scroll") as JScrollPane
            pane.size = Dimension(480, 320)
            fun settle() { repeat(6) {
                pane.doLayout()
                column.setSize(pane.viewport.width, column.preferredSize.height)
                column.doLayout()
                pane.viewport.doLayout()
                UIUtil.dispatchAllInvocationEvents()
            } }
            fun visibleIds(): Set<String> = column.components.filterIsInstance<JComponent>()
                .mapNotNull { (it.getClientProperty("magi.row.snapshot") as? dev.sayaya.magi.ide.usecase.Row)?.msgId }.toSet()
            settle()
            val recent = visibleIds()
            assertTrue("initial view must reach the latest message", "history-599" in recent)
            TailScroll.forwardWheel(pane, -10000000.0)
            settle()
            val first = visibleIds()
            assertTrue("scrolling to the top must render the first message", "history-0" in first)
            assertFalse("offscreen recent rows must be detached", "history-599" in first)
            assertTrue("scrolling must keep component count bounded", column.componentCount <= 101)
            TailScroll.forwardWheel(pane, 10000000.0)
            settle()
            assertTrue("scrolling back must restore the latest message", "history-599" in visibleIds())
            assertTrue("returning to bottom must not accumulate components", column.componentCount <= 101)
            assertEquals("virtualization must retain the complete model", 600, rows.list().size)
            val copying = field("copying") as Copying
            copying.all(rows.list())
            assertEquals("whole-chat selection includes unrendered history", 600, copying.selected(rows.list()).size)
            println("PASS bounded virtual rows, bidirectional scrolling and full-history selection")
        } finally { view.dispose() }
    }
    fun testQueuedRedrawDoesNotRebuildDisposedView() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        UIUtil.dispatchAllInvocationEvents()
        val column = view.javaClass.getDeclaredField("column").apply { isAccessible = true }.get(view) as JComponent
        view.javaClass.getDeclaredMethod("redrawLog").apply { isAccessible = true }.invoke(view)
        view.dispose()
        column.removeAll()
        UIUtil.dispatchAllInvocationEvents()
        assertEquals("queued redraw must not recreate disposed components", 0, column.componentCount)
        println("PASS disposed transcript ignores queued redraw")
    }

}
