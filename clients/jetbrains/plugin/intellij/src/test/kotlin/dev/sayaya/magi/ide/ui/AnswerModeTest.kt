package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import com.intellij.openapi.util.Disposer
import dev.sayaya.magi.ide.model.Request
import dev.sayaya.magi.ide.model.Response
import dev.sayaya.magi.ide.model.Waiting
import dev.sayaya.magi.ide.usecase.Companion
import dev.sayaya.magi.ide.usecase.Daemon
import javax.swing.JButton
import javax.swing.JPanel
import javax.swing.JTextArea

class AnswerModeTest : BasePlatformTestCase() {
    private inner class Harness {
        var session = "s1"
        val pending = mutableListOf<Triple<String, (String) -> Unit, (Companion) -> Unit>>()
        val seen = mutableListOf<Request>()
        val view = MagiToolWindow.View(project,
            sendConnection = { sid, error, work -> pending.add(Triple(sid, error, work)) }, sendSession = { session })
        @Suppress("UNCHECKED_CAST") fun <T> field(name: String): T =
            view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T
        val input: JTextArea get() = field("input")
        fun question(id: String? = "q", scope: String = session) {
            val w = id?.let { Waiting(id = it, kind = "question", what = "Choose", options = listOf("Alpha", "Beta")) }
            view.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java)
                .apply { isAccessible = true }.invoke(view, w, scope)
        }
        fun click(label: String) = field<JPanel>("buttons").components.filterIsInstance<JButton>().first { it.text == label }.doClick()
        fun direct() = click(MagiBundle.msg("chat.answer.direct"))
        fun key(action: String) = input.actionMap.get(action).actionPerformed(java.awt.event.ActionEvent(input, 0, action))
        fun finish(index: Int, ok: Boolean) {
            val (sid, _, work) = pending[index]
            work(Companion(object : Daemon {
                override fun exchange(request: Request): Response { seen.add(request); return Response(ok = ok, error = if (ok) null else "refused") }
                override fun stream(request: Request, each: (Response) -> Boolean) {}
                override fun close() {}
            }, sid))
            UIUtil.dispatchAllInvocationEvents()
        }
    }
    private fun check(body: (Harness) -> Unit) {
        val h = Harness()
        try { body(h) } finally { Disposer.dispose(h.view) }
    }
    fun testExplicitModeRoundTripAndRpcDestination() = check { h ->
        h.input.text = "A"; h.question()
        assertEquals("A", h.input.text); assertFalse(h.field<JPanel>("answerBar").isVisible)
        h.direct(); h.input.text = "B"; h.question()
        assertEquals("B", h.input.text)
        h.key("magi.cancelAnswer"); assertEquals("A", h.input.text)
        h.direct(); assertEquals("B", h.input.text)
        h.key("magi.send"); assertEquals("A", h.input.text)
        assertTrue(h.field<JButton>("sendButton").isEnabled)
        h.direct(); assertFalse(h.field<JButton>("sendButton").isEnabled)
        h.key("magi.send"); h.click("Alpha"); assertEquals(1, h.pending.size)
        h.finish(0, true)
        assertEquals("answer", h.seen.single().method)
        assertEquals("q", h.seen.single().callId)
        assertEquals("B", h.seen.single().answer)
        assertEquals("s1", h.seen.single().session)
        assertEquals("A", h.input.text)
        assertFalse(h.field<JPanel>("answerBar").isVisible)
    }
    fun testLateFailurePreservesEditsAndOldResultCannotUnlockRetry() = check { h ->
        h.input.text = "A"; h.question(); h.direct(); h.input.text = "B"; h.key("magi.send")
        h.direct(); h.input.text = "C"; h.finish(0, false)
        assertEquals("C", h.input.text)
        h.key("magi.send"); h.direct(); h.finish(0, true)
        assertFalse(h.field<JButton>("sendButton").isEnabled)
        assertEquals("C", h.input.text)
        h.finish(1, true); assertEquals("A", h.input.text)
    }
    fun testChoiceAndQuestionReplacementKeepGeneralDraft() = check { h ->
        h.input.text = "A"; h.question(); h.click("Beta")
        h.question("q2"); h.direct(); h.input.text = "new answer"
        h.finish(0, true)
        assertEquals("new answer", h.input.text)
        assertEquals("Beta", h.seen.single().answer)
        h.question(null); assertEquals("A", h.input.text)
        h.question("q2"); h.direct(); assertEquals("new answer", h.input.text)
    }
    fun testSessionChangeAndStalePromptCannotTargetNewSession() = check { h ->
        h.input.text = "A"; h.question(); h.direct(); h.input.text = "B"; h.key("magi.send")
        h.session = "s2"; h.question("q2"); h.input.text = "other"
        h.question("old", "s1"); h.finish(0, false)
        assertEquals("other", h.input.text)
        h.direct(); h.input.text = "reply2"; h.key("magi.send"); h.finish(1, true)
        assertEquals("q2", h.seen.last().callId); assertEquals("s2", h.seen.last().session)
    }
    fun testDetachedChoiceCannotAnswerReplacementQuestion() = check { h ->
        h.question("old")
        val old = h.field<JPanel>("buttons").components.filterIsInstance<JButton>().first { it.text == "Alpha" }
        h.question("new")
        old.doClick()
        assertTrue(h.pending.isEmpty())
        h.click("Beta"); h.finish(0, true)
        assertEquals("new", h.seen.single().callId)
    }
    fun testCompositionDoesNotSubmitOrCancel() = check { h ->
        h.input.text = "A"; h.question(); h.direct(); h.input.text = "한"
        val text = java.text.AttributedString("한").iterator
        val event = java.awt.event.InputMethodEvent(h.input, java.awt.event.InputMethodEvent.INPUT_METHOD_TEXT_CHANGED, text, 0, null, null)
        h.input.inputMethodListeners.forEach { it.inputMethodTextChanged(event) }
        h.key("magi.send"); h.key("magi.cancelAnswer")
        assertTrue(h.pending.isEmpty()); assertTrue(h.field<JPanel>("answerBar").isVisible)
    }
    fun testDisposeDoesNotReviveAnswerMode() = check { h ->
        h.question(); h.direct(); h.input.text = "B"; h.key("magi.send")
        Disposer.dispose(h.view); h.finish(0, false)
        assertTrue(h.seen.isEmpty())
    }
}
