package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import com.intellij.openapi.util.Disposer
import dev.sayaya.magi.ide.model.FileRef
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Daemon
import dev.sayaya.magi.ide.usecase.SendDrafts
import javax.swing.JTextArea

/** Calls the real Enter handler; the connection boundary controls when its work and reply run. */
class SendDraftTest : BasePlatformTestCase() {
    private inner class Harness {
        var session = "s1"
        val pending = mutableListOf<Triple<String, (String) -> Unit, (Companion) -> Unit>>()
        val seen = mutableListOf<Request>()
        val view = MagiToolWindow.View(project,
            sendConnection = { sid, trouble, work -> pending.add(Triple(sid, trouble, work)) },
            sendSession = { session })
        @Suppress("UNCHECKED_CAST")
        fun <T> field(name: String): T = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T
        val input: JTextArea get() = field("input")
        val drafts: SendDrafts get() = field("sendDrafts")
        fun submit(text: String) {
            input.text = text
            input.actionMap.get("magi.send").actionPerformed(java.awt.event.ActionEvent(input, 0, "send"))
        }
        fun finish(index: Int = 0, ok: Boolean = true) {
            val (sid, _, work) = pending[index]
            work(Companion(object : Daemon {
                override fun exchange(request: Request): Response {
                    seen.add(request)
                    return if (request.method in listOf("submit", "steer")) Response(ok = ok, error = if (ok) null else "refused") else Response(ok = true)
                }
                override fun stream(request: Request, each: (Response) -> Boolean) {}
                override fun close() {}
            }, sid))
            UIUtil.dispatchAllInvocationEvents()
        }
        fun close() = Disposer.dispose(view)
    }
    private fun check(run: (Harness) -> Unit) {
        val h = Harness()
        try { run(h) } finally { h.close() }
    }
    fun testArrowRecallTraversesMultilineHistoryAndRestoresDraft() = check { h ->
        val recall = h.field<dev.sayaya.magi.ide.usecase.PromptRecall>("recall")
        recall.load(listOf("line one\nline two", "older prompt"))
        h.input.text = "unfinished draft"
        h.input.caretPosition = 0
        fun arrow(name: String) = h.input.actionMap.get(name).actionPerformed(java.awt.event.ActionEvent(h.input, 0, name))
        arrow("magi.recallOlder")
        assertEquals("line one\nline two", h.input.text)
        arrow("magi.recallOlder")
        assertEquals("older prompt", h.input.text)
        arrow("magi.recallNewer")
        assertEquals("line one\nline two", h.input.text)
        arrow("magi.recallNewer")
        assertEquals("unfinished draft", h.input.text)
    }

    fun testLateSuccessPreservesNewDraft() = check { h ->
        h.submit("A"); h.input.text = "B"; h.finish()
        assertEquals("B", h.input.text)
    }
    fun testEditingBackToSameTextIsNotOldDraft() = check { h ->
        h.submit("A"); h.input.text = "B"; h.input.text = "A"; h.finish()
        assertEquals("A", h.input.text)
    }
    fun testUnchangedSuccessClearsInput() = check { h ->
        h.submit("A"); h.finish(); assertEquals("", h.input.text)
    }
    fun testFailureKeepsNewDraftAndRecoverableSubmission() = check { h ->
        h.submit(" A "); h.input.text = "B"; h.finish(ok = false)
        assertEquals("B", h.input.text)
        val attempt = h.drafts.failures.single().attempt
        assertEquals(" A ", attempt.text)
        val restore = h.view.javaClass.getDeclaredMethod("recoverSend", SendDrafts.Attempt::class.java).apply { isAccessible = true }
        restore.invoke(h.view, attempt)
        assertEquals("B", h.input.text)
        h.input.text = ""
        restore.invoke(h.view, attempt)
        assertEquals(" A ", h.input.text)
        assertTrue(h.drafts.failures.isEmpty())
        assertEquals(1, h.seen.count { it.method == "submit" }) // restoration never sends
    }
    fun testReverseCompletionsAndOldFailureSurviveNewSuccess() = check { h ->
        h.submit("A"); h.submit("B"); h.finish(0, false); h.finish(1)
        assertEquals("", h.input.text)
        assertEquals("A", h.drafts.failures.single().attempt.text)
        h.submit("C"); h.submit("D"); h.finish(3); h.input.text = "E"; h.finish(2)
        assertEquals("E", h.input.text)
    }
    fun testSessionIsCapturedBeforeWorkerRuns() = check { h ->
        h.submit("A"); h.session = "s2"; h.input.text = "B"; h.finish()
        assertEquals("s1", h.seen.last().session)
        assertEquals("B", h.input.text)
    }
    fun testDisposedViewRejectsQueuedWork() {
        val h = Harness(); h.submit("A"); h.close(); h.finish()
        assertTrue(h.seen.isEmpty())
    }
    fun testDisposeAfterRpcBeforeEdtCompletionKeepsInput() = check { h ->
        h.submit("A")
        val (sid, _, work) = h.pending.single()
        work(Companion(object : Daemon {
            override fun exchange(request: Request) = Response(ok = true)
            override fun stream(request: Request, each: (Response) -> Boolean) {}
            override fun close() {}
        }, sid))
        h.close()
        UIUtil.dispatchAllInvocationEvents()
        assertEquals("A", h.input.text)
    }
    fun testTransportFailureAndNewAttachmentsArePreserved() = check { h ->
        val a = FileRef("a.txt"); val b = FileRef("b.txt")
        h.view.attach(a); UIUtil.dispatchAllInvocationEvents()
        h.submit("A"); h.view.attach(b); UIUtil.dispatchAllInvocationEvents()
        h.finish()
        assertEquals(listOf(b), h.field<List<FileRef>>("refs").toList())
        h.submit("B"); h.input.text = "C"
        h.pending[1].second("connection lost; outcome unknown")
        UIUtil.dispatchAllInvocationEvents()
        assertEquals("C", h.input.text)
        assertEquals(listOf(b), h.drafts.failures.single().attempt.refs)
    }

    fun testDiagramNodeChipsGoWithTheMessageAndCanBeRemoved() = check { h ->
        h.view.attachNode("Start"); h.view.attachNode("Next"); h.view.attachNode("Start")
        UIUtil.dispatchAllInvocationEvents()
        assertEquals(listOf("Start", "Next"), h.field<List<String>>("nodes").toList())
        val chips: java.awt.Container = h.field("chips")
        assertTrue(chips.isVisible)
        chips.components.filterIsInstance<javax.swing.JButton>().single { it.getClientProperty("node") == "Next" }.doClick()
        UIUtil.dispatchAllInvocationEvents()
        assertEquals(listOf("Start"), h.field<List<String>>("nodes").toList())
        h.submit("")
        h.finish()
        assertEquals(MagiBundle.msg("chat.node.explain", "Start"), h.seen.last { it.method in listOf("submit", "steer") }.text)
        assertTrue(h.field<List<String>>("nodes").isEmpty())
        h.view.attachNode("B"); UIUtil.dispatchAllInvocationEvents()
        h.submit("why?")
        h.finish(1)
        assertEquals("why?\n\n" + MagiBundle.msg("chat.node.about", "B"), h.seen.last { it.method in listOf("submit", "steer") }.text)
        assertTrue(h.field<List<String>>("nodes").isEmpty())
    }
    /** A node chip had 0px above and below its label but 6px beside it, and 2px between rows but 6px between chips. */
    fun testANodeChipHasTheSameSpaceOnAllFourSides() = check { h ->
        h.view.attachNode("Start"); UIUtil.dispatchAllInvocationEvents()
        val chips: java.awt.Container = h.field("chips")
        val chip = chips.components.filterIsInstance<javax.swing.JButton>().single { it.getClientProperty("node") == "Start" }
        val inner = chip.insets
        val flow = chips.layout as java.awt.FlowLayout
        println("CHIP insets=$inner margin=${chip.margin} hgap=${flow.hgap} vgap=${flow.vgap}")
        assertEquals("inside: top and bottom match left and right", listOf(inner.left, inner.left, inner.left), listOf(inner.top, inner.bottom, inner.right))
        assertEquals("outside: the gap between rows matches the gap between chips", flow.hgap, flow.vgap)
        // The chips do not stand out of the input row: the first chip starts where the input starts
        // (12px) and the row ends where the input ends (8px).
        assertEquals("chips start at the input's left edge", 12, chips.insets.left + flow.hgap)
        assertEquals("chips end at the input's right edge", 8, chips.insets.right + flow.hgap)
    }

    fun testDiagramNodeChipsSurviveAFailedSend() = check { h ->
        h.view.attachNode("X"); UIUtil.dispatchAllInvocationEvents()
        h.submit("q")
        h.finish(ok = false)
        assertEquals(listOf("X"), h.field<List<String>>("nodes").toList())
        assertEquals(listOf("X"), h.drafts.failures.single().attempt.nodes)
    }
    fun testAnswerPageNodeMessagesBecomeComposerChips() = check { h ->
        // The page script the answer browser runs: it opts the viewer into the composer and has one
        // slot where the JBCefJSQuery call goes.
        assertTrue(RichAnswer.nodeHook.contains("window.magiDiagramComposer = true"))
        assertEquals(1, RichAnswer.nodeHook.split("MAGI_SEND").size - 1)
        assertEquals("Start", RichAnswer.nodeLabel("node: Start "))
        assertNull(RichAnswer.nodeLabel("node:  "))
        assertNull(RichAnswer.nodeLabel("wheel:12"))
        // What the query handler hands on is what the tool window wired: a chip, once per node.
        val answers: Any = h.field("richAnswers")
        @Suppress("UNCHECKED_CAST")
        val onNode = answers.javaClass.getDeclaredField("onNode").apply { isAccessible = true }.get(answers) as (String) -> Unit
        onNode(RichAnswer.nodeLabel("node:Start")!!); onNode(RichAnswer.nodeLabel("node:Start")!!)
        UIUtil.dispatchAllInvocationEvents()
        assertEquals(listOf("Start"), h.field<List<String>>("nodes").toList())
        val chips: java.awt.Container = h.field("chips")
        assertEquals(1, chips.components.count { (it as? javax.swing.JButton)?.getClientProperty("node") == "Start" })
    }
    fun testTheDaemonsThemeReachesTheAnswerBrowsers() = check { h ->
        val wire = dev.sayaya.magi.ide.model.Wire.json.decodeFromString(Response.serializer(),
            """{"ok":true,"theme":{"dark":{"primary":"#123456"},"light":{"primary":"#ABCDEF"}}}""")
        assertEquals("#123456", wire.theme?.dark?.get("primary"))
        val comp = Companion(object : Daemon {
            override fun exchange(request: Request): Response = wire
            override fun stream(request: Request, each: (Response) -> Boolean) {}
            override fun close() {}
        }, "s1")
        val theme = comp.facts().theme
        assertEquals("#ABCDEF", theme?.light?.get("primary"))
        val script = RichAnswer.themeScript(RichAnswer.themeJson(theme))
        assertTrue(script, script.contains("window.magiDiagramTheme") && script.contains("setTheme") && script.contains("#ABCDEF"))
        val answers: RichAnswer = h.field("richAnswers")
        h.view.useTheme(theme)
        assertTrue(answers.themeJson.orEmpty().contains("#123456"))
    }
    fun testTheViewerScriptTurnsTheComposerOnBeforeTheViewerLoads() {
        // The measuring script can fail to run in a panel (idea.log: no height answer); the composer flag
        // must not depend on it, or the panel shows a question field nobody can type into.
        val script = RichAnswer.viewerScript(null)
        val flag = script.indexOf("window.magiDiagramComposer = true")
        assertTrue(script, flag >= 0 && flag < script.indexOf("window.magiMermaid"))
    }
    fun testClickDiagnosticsShipWithOneSendSlot() {
        // Diagnostic for radios that stop responding after a node is selected: the page reports each
        // pointerdown/click/change in a diagram through one query slot.
        assertTrue(RichAnswer.clickDiag.contains("pointerdown"))
        assertEquals(1, RichAnswer.clickDiag.split("MAGI_SEND").size - 1)
    }
}

