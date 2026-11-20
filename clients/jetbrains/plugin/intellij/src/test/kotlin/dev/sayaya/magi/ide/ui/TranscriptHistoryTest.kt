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

    private fun stageStrip(): ProgressStages {
        // A mark with ink and of an odd size: the IDE's own icons are blank stand-ins in this headless run.
        val mark = object : javax.swing.Icon {
            override fun paintIcon(c: java.awt.Component?, g: java.awt.Graphics, x: Int, y: Int) { g.color = java.awt.Color(0x12, 0x34, 0x56); g.fillRect(x + 4, y + 4, 16, 16) }
            override fun getIconWidth() = 24
            override fun getIconHeight() = 24
        }
        return ProgressStages { mark }
    }

    private fun step(id: String, kind: String?, state: String, done: Int = 0, total: Int = 0) =
        dev.sayaya.magi.ide.model.ProgressStage(id = id, label = id, kind = kind, state = state, done = done, total = total)

    private fun role(strip: ProgressStages, r: String) =
        strip.components.filterIsInstance<javax.swing.JLabel>().filter { it.getClientProperty(ProgressStages.ROLE) == r }

    fun testProgressStagesDrawAnIconOverEachLabel() {
        // Each node — start, every step, end — is a large mark with its label under it; the marks share one
        // top and the labels one line; an arrow sits on the marks' centre; a mark is drawn in its node's colour.
        val strip = stageStrip()
        strip.update(listOf(step("list", "tool", "done", 2, 2), step("prep", null, "done"), step("look", "subagent", "running", 1, 3), step("check", null, "waiting")))
        strip.size = strip.preferredSize
        strip.doLayout()
        val nodes = role(strip, "start") + role(strip, "step") + role(strip, "end")
        assertEquals("start, four steps and end", 6, nodes.size)
        val size = com.intellij.util.ui.JBUI.scale(ProgressStages.ICON)
        fun rects(l: javax.swing.JLabel): Pair<java.awt.Rectangle, java.awt.Rectangle> {
            val ins = l.insets
            val vr = java.awt.Rectangle(ins.left, ins.top, l.width - ins.left - ins.right, l.height - ins.top - ins.bottom)
            val ir = java.awt.Rectangle(); val tr = java.awt.Rectangle()
            javax.swing.SwingUtilities.layoutCompoundLabel(l, l.getFontMetrics(l.font), l.text, l.icon, l.verticalAlignment,
                l.horizontalAlignment, l.verticalTextPosition, l.horizontalTextPosition, vr, ir, tr, l.iconTextGap)
            return ir to tr
        }
        for (n in nodes) {
            assertNotNull("every node has a mark: " + n.text, n.icon)
            assertEquals("the mark is " + size + "px: " + n.text, size, n.icon.iconHeight)
            val (ir, tr) = rects(n)
            assertTrue("the label sits under the mark: " + n.text, tr.y >= ir.y + ir.height - 1)
        }
        assertEquals("the marks share one top: " + nodes.map { it.text }, 1, nodes.map { it.y + rects(it).first.y }.toSet().size)
        assertEquals("the labels share one line: " + nodes.map { it.text }, 1, nodes.map { it.y + rects(it).second.y }.toSet().size)
        val markMid = nodes.first().let { it.y + rects(it).first.let { r -> r.y + r.height / 2 } }
        val arrows = role(strip, "arrow")
        assertEquals("an arrow before each step and one to the end", 5, arrows.size)
        for (a in arrows) {
            assertNull("an arrow has no mark", a.icon)
            val tr = rects(a).second
            assertTrue("the arrow sits on the marks' centre (" + (a.y + tr.y + tr.height / 2) + " vs " + markMid + ")", Math.abs(a.y + tr.y + tr.height / 2 - markMid) <= 1)
        }
        for (n in nodes) {
            val ic = n.icon
            val img = java.awt.image.BufferedImage(ic.iconWidth, ic.iconHeight, java.awt.image.BufferedImage.TYPE_INT_ARGB)
            val g = img.createGraphics(); ic.paintIcon(n, g, 0, 0); g.dispose()
            val want = n.foreground.rgb and 0xFFFFFF
            var inked = 0; var off = 0
            for (x in 0 until img.width) for (y in 0 until img.height) {
                val px = img.getRGB(x, y)
                if ((px ushr 24) < 160) continue
                inked++
                val d = maxOf(Math.abs(((px shr 16) and 255) - ((want shr 16) and 255)), Math.abs(((px shr 8) and 255) - ((want shr 8) and 255)), Math.abs((px and 255) - (want and 255)))
                if (d > 8) off++
            }
            assertTrue("the mark of " + n.text + " has ink", inked > 0)
            assertEquals("the mark of " + n.text + " is drawn in its node's colour", 0, off)
        }
    }

    fun testProgressStagesStartAndEndFollowTheRun() {
        // Start and end take the same states as steps: start runs while the run does and is done after it;
        // end waits until every step is done, and shows a failure.
        val strip = stageStrip()
        fun state(r: String) = role(strip, r).single().getClientProperty(ProgressStages.STATE)
        strip.update(listOf(step("a", null, "running"), step("b", null, "waiting")))
        assertEquals("running", state("start")); assertEquals("waiting", state("end"))
        strip.update(listOf(step("a", null, "done"), step("b", null, "done")))
        assertEquals("done", state("start")); assertEquals("done", state("end"))
        assertEquals("start and end are drawn like a done step", Look.body, role(strip, "end").single().foreground)
        strip.update(listOf(step("a", null, "done"), step("b", null, "failed")))
        assertEquals("done", state("start")); assertEquals("failed", state("end"))
    }

    fun testARenderThatFinishesLateDoesNotPutOlderTextBack() {
        // The first render of an answer and a later update are made off the EDT; whichever finishes
        // last must not win. HTML made from text the panel no longer shows is dropped.
        val vf = com.intellij.testFramework.LightVirtualFile("a.md", "")
        val applied = mutableListOf<String>()
        var shown: String? = null
        val first = RichAnswer.current({ shown }, "old") { h, _ -> applied += h }
        val update = RichAnswer.current({ shown }, "new") { h, _ -> applied += h }
        shown = "new"
        update("NEW", vf)
        first("OLD", vf)
        assertEquals("the late first render must not cover the update", listOf("NEW"), applied)
        val fresh = mutableListOf<String>()
        RichAnswer.current({ null }, "only") { h, _ -> fresh += h }("ONLY", vf)
        assertEquals("a render with nothing shown yet is applied", listOf("ONLY"), fresh)
    }

    fun testProgressStagesFollowTheOrderBackAfterItChanges() {
        // The strip lays its steps out in the order reported. Steps that swap and then swap back must
        // be shown in the order of the last report, each still the same node.
        val strip = ProgressStages()
        fun step(id: String) = dev.sayaya.magi.ide.model.ProgressStage(id = id, label = id, state = "running")
        fun shown() = strip.components.filterIsInstance<javax.swing.JLabel>().mapNotNull { it.getClientProperty(ProgressStages.ID) as String? }
        fun node(id: String) = strip.components.filterIsInstance<javax.swing.JLabel>().first { it.getClientProperty(ProgressStages.ID) == id }
        strip.update(listOf(step("a"), step("b")))
        val a = node("a")
        assertEquals(listOf("a", "b"), shown())
        strip.update(listOf(step("b"), step("a")))
        assertEquals(listOf("b", "a"), shown())
        strip.update(listOf(step("a"), step("b")))
        assertEquals("back in the order of the last report", listOf("a", "b"), shown())
        assertSame("each step keeps its node", a, node("a"))
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
