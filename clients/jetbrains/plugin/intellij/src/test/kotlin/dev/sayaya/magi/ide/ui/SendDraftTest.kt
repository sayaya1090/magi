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
}
