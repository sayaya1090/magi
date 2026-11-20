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

    fun testAnAnswerWithFencedCodeGetsItsHtmlOffTheDispatchThread() {
        // A browser rebuilt on the way back into range made its HTML on the EDT; in IDE 2026.2 the fenced-code
        // highlighter asserts it is not on the EDT and throws, and the answer fell back to its source
        // (idea.log 2026-11-20 07:39:32). The 2026.1 test platform does not assert, so the test checks where
        // the HTML is made instead of waiting for the throw.
        assertTrue("the test runs on the EDT", javax.swing.SwingUtilities.isEventDispatchThread())
        val md = "```java\nint answer = 42;\n```\n\n```mermaid\nflowchart LR\n  A --> B\n```\n"
        var madeOnEdt: Boolean? = null
        var html: String? = null
        RichAnswer.markdownHtml(project, md, testRootDisposable, generate = { vf ->
            madeOnEdt = javax.swing.SwingUtilities.isEventDispatchThread()
            org.intellij.plugins.markdown.ui.preview.html.MarkdownUtil.generateMarkdownHtml(vf, md, project)
        }) { h, _ ->
            assertTrue("the HTML is handed back on the EDT", javax.swing.SwingUtilities.isEventDispatchThread())
            html = h
        }
        val until = System.currentTimeMillis() + 20_000
        while (html == null && System.currentTimeMillis() < until) {
            UIUtil.dispatchAllInvocationEvents()
            Thread.sleep(20)
        }
        assertEquals("the HTML must be made off the EDT", false, madeOnEdt)
        assertNotNull("the HTML arrives", html)
        assertTrue("the fenced code is in it", html!!.contains("answer"))
    }

    fun testARowThatGotNoBrowserIsNotRememberedAsRich() {
        // panel() returned null once and the light renderer took the row; the row cache kept "rich" and reused
        // the light panel on every later paint, so the diagram stayed source (2026-11-20).
        val light = com.intellij.ui.components.JBPanel<com.intellij.ui.components.JBPanel<*>>()
        assertFalse("a light row is not rich", RichAnswer.paintedAsRich(true, light))
        val row = com.intellij.ui.components.JBPanel<com.intellij.ui.components.JBPanel<*>>()
        val holder = com.intellij.ui.components.JBPanel<com.intellij.ui.components.JBPanel<*>>().apply { putClientProperty("magi.document", "t") }
        row.add(holder)
        assertTrue("a row holding a browser is rich", RichAnswer.paintedAsRich(true, row))
        assertFalse("nothing is rich that was not wanted", RichAnswer.paintedAsRich(false, row))
    }

    fun testAnAnswerThatKeepsFailingStopsAskingForABrowser() {
        val answers = RichAnswer()
        assertTrue(answers.mayTry("k"))
        repeat(RichAnswer.TRIES - 1) { answers.failed("k") }
        assertTrue("still tried below the limit", answers.mayTry("k"))
        answers.failed("k")
        assertFalse("not tried again at the limit", answers.mayTry("k"))
        assertTrue("another answer is unaffected", answers.mayTry("other"))
    }

    fun testHtmlThatCannotBeMadeFallsBackToTheSourceNotAnEmptyBox() {
        var html: String? = null
        RichAnswer.markdownHtml(project, "a < b```", testRootDisposable, generate = { throw IllegalStateException("highlighter") }) { h, _ -> html = h }
        val until = System.currentTimeMillis() + 20_000
        while (html == null && System.currentTimeMillis() < until) {
            UIUtil.dispatchAllInvocationEvents()
            Thread.sleep(20)
        }
        assertNotNull("a failure still hands something back", html)
        assertTrue("it is the escaped source: $html", html!!.contains("a &lt; b"))
    }

    fun testAToolsStagesShowUnderItsToolRowAndChangeInPlace() {
        // A tool reports its steps (tool.progress stages); the tool row carries one strip that keeps a
        // node per step and only repaints what changed, so a new report does not redraw the strip.
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val rows = field(view, "shaper") as Rows
            val column = field(view, "column") as JComponent
            var seq = 0L
            fun ev(type: String, data: String) = LogEvent(seq = ++seq, type = type, actor = null,
                ts = "2026-11-20T10:00:00Z", data = kotlinx.serialization.json.Json.parseToJsonElement(data))
            val redraw = view.javaClass.getDeclaredMethod("redrawLog").apply { isAccessible = true }
            fun nodes(c: java.awt.Component): List<javax.swing.JLabel> =
                listOfNotNull((c as? javax.swing.JLabel)?.takeIf { it.getClientProperty(ProgressStages.ID) != null }) +
                    ((c as? java.awt.Container)?.components?.flatMap { nodes(it) } ?: emptyList())
            rows.feed(ev("part.appended", """{"part":{"kind":"tool-call","toolCall":{"name":"long_job","callId":"run1"}}}"""))
            rows.feed(ev("tool.progress", """{"callId":"run1","name":"long_job","stages":[{"id":"s1","label":"list","kind":"tool","state":"running"},{"id":"s2","label":"check","state":"waiting"}]}"""))
            redraw.invoke(view); UIUtil.dispatchAllInvocationEvents()
            val first = nodes(column)
            assertEquals("every step is there from the first report", listOf("s1", "s2"), first.map { it.getClientProperty(ProgressStages.ID) })
            assertEquals(listOf("running", "waiting"), first.map { it.getClientProperty(ProgressStages.STATE) })
            rows.feed(ev("tool.progress", """{"callId":"run1","name":"long_job","stages":[{"id":"s1","label":"list","kind":"tool","state":"done"},{"id":"s2","label":"check","kind":"subagent","state":"running","done":1,"total":3,"running":2}]}"""))
            redraw.invoke(view); UIUtil.dispatchAllInvocationEvents()
            val second = nodes(column)
            assertEquals(listOf("done", "running"), second.map { it.getClientProperty(ProgressStages.STATE) })
            assertTrue("each step keeps its node; only what changed is repainted", first.size == 2 && first.zip(second).all { (a, b) -> a === b })
            assertTrue(second[1].text, second[1].text.contains("1/3"))
            assertEquals("subagent", second[1].getClientProperty(ProgressStages.KIND))
            assertNotNull("a step with a kind carries its mark", second[1].icon)
        } finally { view.dispose() }
    }

    fun testProgressStagesSitOnOneCentreAndAnIconTakesItsStepsColour() {
        // A step with a kind mark and a step without one sit on one line: every node is as tall as the
        // others, so their words share one vertical centre, and the mark is drawn in the step's own
        // colour, so two steps in the same state look the same.
        // A mark taller than the words, with ink: the IDE's own icons are blank stand-ins in this headless run.
        val mark = object : javax.swing.Icon {
            override fun paintIcon(c: java.awt.Component?, g: java.awt.Graphics, x: Int, y: Int) { g.color = java.awt.Color(0x12, 0x34, 0x56); g.fillRect(x + 4, y + 4, 16, 16) }
            override fun getIconWidth() = 24
            override fun getIconHeight() = 24
        }
        val strip = ProgressStages { kind -> if (kind == null) null else mark }
        strip.update(listOf(
            dev.sayaya.magi.ide.model.ProgressStage(id = "a", label = "list", kind = "tool", state = "done", done = 2, total = 2),
            dev.sayaya.magi.ide.model.ProgressStage(id = "b", label = "prep", state = "done"),
            dev.sayaya.magi.ide.model.ProgressStage(id = "c", label = "look", kind = "subagent", state = "running", done = 1, total = 3, running = 2),
            dev.sayaya.magi.ide.model.ProgressStage(id = "d", label = "check", state = "running"),
        ))
        strip.size = strip.preferredSize
        strip.doLayout()
        val labels = strip.components.filterIsInstance<javax.swing.JLabel>()
        assertEquals("start, four steps, their arrows, the arrow to the end and the end", 11, labels.size)
        val heights = labels.map { it.height }.toSet()
        assertEquals("every node is as tall as the others: " + labels.map { it.text + "=" + it.height }, 1, heights.size)
        val mids = labels.map { l ->
            val vr = java.awt.Rectangle(0, 0, l.width, l.height); val ir = java.awt.Rectangle(); val tr = java.awt.Rectangle()
            javax.swing.SwingUtilities.layoutCompoundLabel(l, l.getFontMetrics(l.font), l.text, l.icon, l.verticalAlignment,
                l.horizontalAlignment, l.verticalTextPosition, l.horizontalTextPosition, vr, ir, tr, l.iconTextGap)
            (l.y + tr.y) * 2 + tr.height
        }
        assertEquals("the words share one vertical centre: " + labels.map { it.text }.zip(mids), 1, mids.toSet().size)
        val marked = labels.filter { it.getClientProperty(ProgressStages.ID) != null && it.icon != null }
        assertEquals("the two steps with a kind carry a mark", 2, marked.size)
        for (l in marked) {
            val ic = l.icon
            val img = java.awt.image.BufferedImage(ic.iconWidth, ic.iconHeight, java.awt.image.BufferedImage.TYPE_INT_ARGB)
            val g = img.createGraphics(); ic.paintIcon(l, g, 0, 0); g.dispose()
            val want = l.foreground.rgb and 0xFFFFFF
            var inked = 0; var off = 0
            for (x in 0 until img.width) for (y in 0 until img.height) {
                val px = img.getRGB(x, y)
                if ((px ushr 24) < 160) continue
                inked++
                val d = maxOf(Math.abs(((px shr 16) and 255) - ((want shr 16) and 255)), Math.abs(((px shr 8) and 255) - ((want shr 8) and 255)), Math.abs((px and 255) - (want and 255)))
                if (d > 8) off++
            }
            assertTrue("the mark of " + l.text + " has ink", inked > 0)
            assertEquals("the mark of " + l.text + " is drawn in its step's colour (" + inked + " inked pixels)", 0, off)
        }
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
