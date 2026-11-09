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
    private fun field(view: Any, name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view)
    private fun ids(column: JComponent): Set<String> = column.components.filterIsInstance<JComponent>()
        .mapNotNull { (it.getClientProperty("magi.row.snapshot") as? dev.sayaya.magi.ide.usecase.Row)?.msgId }.toSet()
    private fun said(n: Int, text: String = "message $n") = LogEvent(seq = n.toLong() + 1, type = "part.appended", data = buildJsonObject {
        put("messageId", "history-$n")
        put("part", buildJsonObject { put("kind", "text"); put("text", text) })
    })
    private fun beginReplay(view: Any) = view.javaClass.getDeclaredMethod("beginReplay").apply { isAccessible = true }.invoke(view)

    fun testReplayDrawsOnceAtTheEndAndOnlyTheEnd() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val column = field(view, "column") as JComponent
            beginReplay(view)
            repeat(600) { view.sink.frame(said(it)) }
            UIUtil.dispatchAllInvocationEvents()
            assertTrue("a replay must not draw rows it is passing through", ids(column).isEmpty())
            view.sink.caughtUp()
            UIUtil.dispatchAllInvocationEvents()
            val drawn = ids(column)
            assertTrue("the end of the replay must draw the latest message", "history-599" in drawn)
            assertFalse("the first draw must not build the start of the log", "history-0" in drawn)
        } finally { view.dispose() }
    }

    fun testReplayFromADaemonThatNeverSaysCaughtUpStillDraws() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val column = field(view, "column") as JComponent
            beginReplay(view)
            view.sink.frame(said(0))
            UIUtil.dispatchAllInvocationEvents()
            assertTrue("still replaying", ids(column).isEmpty())
            val deadline = System.currentTimeMillis() + 10_000
            while (ids(column).isEmpty() && System.currentTimeMillis() < deadline) {
                UIUtil.dispatchAllInvocationEvents()
                Thread.sleep(20)
            }
            assertTrue("an old daemon must not leave the window empty", "history-0" in ids(column))
        } finally { view.dispose() }
    }

    fun testFollowingDrawsTheEndOnTheFirstPass() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val rows = field(view, "shaper") as Rows
            val column = field(view, "column") as JComponent
            repeat(600) { rows.feed(said(it)) }
            view.javaClass.getDeclaredMethod("redrawLog").apply { isAccessible = true }.invoke(view)
            UIUtil.dispatchAllInvocationEvents()
            assertTrue("history-599" in ids(column))
            assertFalse("history-0" in ids(column))
        } finally { view.dispose() }
    }

    fun testARichAnswerOutOfRangeKeepsItsRenderer() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val rows = field(view, "shaper") as Rows
            val column = field(view, "column") as JComponent
            repeat(600) { rows.feed(if (it == 595) said(it, "| a |\n|---|\n| b |") else said(it)) }
            val pane = field(view, "scroll") as JScrollPane
            pane.size = Dimension(480, 320)
            val redraw = view.javaClass.getDeclaredMethod("redrawLog").apply { isAccessible = true }
            fun settle() { repeat(6) {
                redraw.invoke(view)
                pane.doLayout()
                column.setSize(pane.viewport.width, column.preferredSize.height)
                column.doLayout()
                pane.viewport.doLayout()
                UIUtil.dispatchAllInvocationEvents()
            } }
            settle()
            val answers = field(view, "richAnswers") as RichAnswer
            val eligible = RichAnswer::class.java.getDeclaredField("eligible").apply { isAccessible = true }
            val atTheEnd = eligible.get(answers) as Set<*>
            assertEquals("the report at the end is rendered", 1, atTheEnd.size)
            TailScroll.forwardWheel(pane, -10000000.0)
            settle()
            assertFalse("the report is out of range now", "history-595" in ids(column))
            assertEquals("leaving the range must not drop the browser", atTheEnd, eligible.get(answers))
        } finally { view.dispose() }
    }

    fun testRichRecencyKeepsWhatWasSeenLastAndPrefersTheScreen() {
        val recent = RichRecency(2)
        assertEquals(setOf("a"), recent.see(listOf("a"), emptyList()))
        assertEquals("out of range is not out of memory", setOf("a", "b"), recent.see(listOf("b"), emptyList()))
        assertEquals("the oldest goes first", setOf("b", "c"), recent.see(listOf("c"), emptyList()))
        assertEquals("the screen beats the margin", setOf("x", "z"), recent.see(listOf("x", "y", "z"), listOf("x")))
    }

    fun testARebuiltBrowserStartsAtItsMeasuredHeight() {
        val answers = RichAnswer()
        val estimate = answers.initialHeight("k", "one line")
        answers.remember("k", estimate + 1234)
        assertEquals(estimate + 1234, answers.initialHeight("k", "one line"))
        assertEquals(estimate, answers.initialHeight("other", "one line"))
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
