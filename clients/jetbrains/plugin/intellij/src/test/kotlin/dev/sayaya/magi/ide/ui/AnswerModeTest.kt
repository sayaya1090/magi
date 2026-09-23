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
        val suggestions = mutableListOf<(String?) -> Unit>()
        val files = mutableListOf<(List<String>) -> Unit>()
        val choices = mutableListOf<(String) -> Unit>()
        val recoveryViewerEvents = mutableListOf<Triple<dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery, () -> Unit, () -> Unit>>()
        val view = MagiToolWindow.View(project,
            sendConnection = { sid, error, work -> pending.add(Triple(sid, error, work)) }, sendSession = { session },
            suggestRequest = { _, done -> suggestions.add(done) },
            filesRequest = { _, done -> files.add(done) },
            fileChooser = { _, chosen -> choices.add(chosen) },
            answerRecoveryViewer = { item, onCopy, onDelete -> recoveryViewerEvents.add(Triple(item, onCopy, onDelete)) })
        @Suppress("UNCHECKED_CAST") fun <T> field(name: String): T =
            view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T
        val input: JTextArea get() = field("input")
        val answers: dev.sayaya.magi.ide.usecase.AnswerDrafts get() = field("answers")
        val answerRecoveryBtn: JButton get() = field("answerRecovery")
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
    fun testCompletionCallbacksAreBoundToComposerContext() {
        for (change in listOf("mode", "cancel", "submit", "answerSubmit", "question", "session", "sessionOnly", "dispose")) check { h ->
            h.question(); h.input.text = "same"
            if (change == "answerSubmit" || change == "cancel") { h.direct(); h.input.text = "same" }
            h.view.javaClass.getDeclaredMethod("askSuggestion").apply { isAccessible = true }.invoke(h.view)
            when (change) {
                "mode" -> { h.direct(); h.input.text = "same" }
                "submit", "answerSubmit" -> h.key("magi.send")
                "cancel" -> h.key("magi.cancelAnswer")
                "sessionOnly" -> { h.session = "s2" }
                "question" -> h.question("q2")
                "session" -> { h.session = "s2"; h.question("q2"); h.input.text = "same" }
                "dispose" -> Disposer.dispose(h.view)
            }
            h.suggestions.first().invoke("STALE")
            UIUtil.dispatchAllInvocationEvents()
            assertNull(change, h.field<String?>("suggestion"))
        }
    }
    fun testFileDeliveryAndSelectionAreBoundToComposerContext() {
        for (delivered in listOf(false, true)) {
            for (change in listOf("mode", "cancel", "submit", "answerSubmit", "question", "session", "sessionOnly", "dispose")) check { h ->
                h.question(); h.input.text = "@same"
                if (change == "answerSubmit" || change == "cancel") { h.direct(); h.input.text = "@same" }
                h.view.javaClass.getDeclaredMethod("askSuggestion").apply { isAccessible = true }.invoke(h.view)
                if (delivered) { h.files.first().invoke(listOf("file.kt")); UIUtil.dispatchAllInvocationEvents() }
                when (change) {
                    "mode" -> { h.direct(); h.input.text = "@same" }
                    "submit", "answerSubmit" -> h.key("magi.send")
                "cancel" -> h.key("magi.cancelAnswer")
                "sessionOnly" -> { h.session = "s2" }
                    "question" -> h.question("q2")
                    "session" -> { h.session = "s2"; h.question("q2"); h.input.text = "@same" }
                    "dispose" -> Disposer.dispose(h.view)
                }
                if (delivered) { h.choices.single().invoke("file.kt"); UIUtil.dispatchAllInvocationEvents() } else {
                    h.files.first().invoke(listOf("file.kt")); UIUtil.dispatchAllInvocationEvents()
                    assertTrue(change, h.choices.isEmpty())
                }
                assertEquals(change, "@same", h.input.text)
                assertTrue(change, h.field<List<*>>("refs").isEmpty())
            }
        }
    }
    fun testCurrentCompletionAndFileSelectionStillWork() = check { h ->
        h.question(); h.input.text = "same"
        h.view.javaClass.getDeclaredMethod("askSuggestion").apply { isAccessible = true }.invoke(h.view)
        h.suggestions.single().invoke(" suffix"); UIUtil.dispatchAllInvocationEvents()
        h.view.javaClass.getDeclaredMethod("acceptSuggestion").apply { isAccessible = true }.invoke(h.view)
        assertEquals("same suffix", h.input.text)
        h.input.text = "@same"
        h.view.javaClass.getDeclaredMethod("askSuggestion").apply { isAccessible = true }.invoke(h.view)
        h.files.single().invoke(listOf("file.kt")); UIUtil.dispatchAllInvocationEvents()
        h.choices.single().invoke("file.kt"); UIUtil.dispatchAllInvocationEvents()
        assertEquals("", h.input.text)
        assertEquals(1, h.field<List<*>>("refs").size)
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
    fun testAnswerRecoveryButtonVisibilityAndViewerInteraction() = check { h ->
        h.input.text = "A"; h.question("q1"); h.direct(); h.input.text = "B"
        h.question("q2")
        assertEquals("A", h.input.text)
        assertTrue(h.answerRecoveryBtn.isVisible)
        assertTrue(h.answerRecoveryBtn.text.contains("(1)"))
        assertEquals(1, h.answers.recoveries.size)
        val rec = h.answers.recoveries.single()
        assertEquals("B", rec.text)
        assertEquals("q1", rec.callId)
        assertEquals("Choose", rec.questionText)

        // Invoke showAnswerRecovery
        h.view.javaClass.getDeclaredMethod("showAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        assertEquals(1, h.recoveryViewerEvents.size)
        val (item, onCopy, onDelete) = h.recoveryViewerEvents.single()
        assertEquals("B", item.text)

        // Copy does not delete, does not alter input, does not send RPC
        onCopy()
        assertEquals(1, h.answers.recoveries.size)
        assertEquals("A", h.input.text)
        assertTrue(h.pending.isEmpty())
        val clipboardText = com.intellij.openapi.ide.CopyPasteManager.getInstance()
            .getContents<String>(java.awt.datatransfer.DataFlavor.stringFlavor)
        assertEquals("B", clipboardText)

        // Delete removes item and hides button
        onDelete()
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
    }
    fun testLateCallbackAndRedrawAfterDeleteDoesNotReviveDraft() = check { h ->
        h.input.text = "A"; h.question("q1"); h.direct(); h.input.text = "B"
        h.key("magi.send")
        assertEquals(1, h.pending.size)
        h.question(null)
        assertTrue(h.answerRecoveryBtn.isVisible)
        val rec = h.answers.recoveries.single()

        h.view.javaClass.getDeclaredMethod("deleteAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)

        // Late RPC failure does not revive deleted draft
        h.finish(0, false)
        assertTrue(h.answers.recoveries.isEmpty())

        // Redraw does not revive
        h.question("q1")
        h.question(null)
        assertTrue(h.answers.recoveries.isEmpty())

        // New answer draft on q1 can be created and recovered
        h.question("q1"); h.direct(); h.input.text = "C"
        h.question(null)
        assertEquals(1, h.answers.recoveries.size)
        assertEquals("C", h.answers.recoveries.single().text)
    }
    fun testDoneQuestionWithNewerEditsExposedInRecoveryView() = check { h ->
        h.question("q1"); h.direct(); h.input.text = "A"; h.key("magi.send")
        h.direct(); h.input.text = "B"
        h.finish(0, true)
        assertTrue(h.answerRecoveryBtn.isVisible)
        assertEquals(1, h.answers.recoveries.size)
        assertEquals("B", h.answers.recoveries.single().text)
    }
    fun testReenterWithoutEditsDoesNotCreateSpuriousRecovery() = check { h ->
        h.input.text = "general"
        h.question("q1")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")
        assertEquals(1, h.pending.size)

        // Re-enter answer mode without typing anything
        h.direct()
        assertEquals("A", h.input.text)

        // A succeeds
        h.finish(0, true)

        // Recoveries must be empty!
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
    }
    fun testCancelAndReenterRepetitionsDoNotCreateSpuriousRecovery() = check { h ->
        h.input.text = "general"
        h.question("q1")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")

        // Repeat cancel and direct re-entry multiple times without typing
        h.direct()
        h.key("magi.cancelAnswer")
        h.direct()
        h.key("magi.cancelAnswer")
        h.direct()

        // Redraw same question
        h.question("q1")

        // A succeeds
        h.finish(0, true)

        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
    }
    fun testReenterWithActualEditsPreservesDraftOnSuccess() = check { h ->
        h.input.text = "general"
        h.question("q1")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")

        // Re-enter and actually type B
        h.direct()
        h.input.text = "new B"
        h.finish(0, true)

        assertEquals(1, h.answers.recoveries.size)
        assertEquals("new B", h.answers.recoveries.single().text)
        assertTrue(h.answerRecoveryBtn.isVisible)
    }
    fun testReenterWithActualEditsPreservesBothOnFailure() = check { h ->
        h.input.text = "general"
        h.question("q1")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")

        // Re-enter and type B
        h.direct()
        h.input.text = "new B"
        h.finish(0, false)

        // Question is still current; failed attempt A is in recoveries
        val recs = h.answers.recoveries
        assertEquals(1, recs.size)
        assertEquals("A", recs.single().text)
        assertEquals(dev.sayaya.magi.ide.usecase.AnswerDrafts.REASON_SUBMISSION_FAILED, recs.single().reason)

        // In composer, user still has "new B"
        assertEquals("new B", h.input.text)

        // Leaving question exposes B as well
        h.question("q2")
        val both = h.answers.recoveries
        assertEquals(2, both.size)
        assertEquals("new B", both.first().text)
        assertEquals("A", both.last().text)
    }
    fun testRecoveryCallbacksBlockedAfterViewDisposed() = check { h ->
        h.question("q1"); h.direct(); h.input.text = "text1"
        h.question(null)
        val rec = h.answers.recoveries.single()
        h.view.javaClass.getDeclaredMethod("showAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        val (_, onCopy, onDelete) = h.recoveryViewerEvents.single()

        com.intellij.openapi.ide.CopyPasteManager.getInstance()
            .setContents(java.awt.datatransfer.StringSelection("SENTINEL"))

        Disposer.dispose(h.view)

        // Callbacks invoked after view disposal must do nothing
        onCopy()
        val clip = com.intellij.openapi.ide.CopyPasteManager.getInstance()
            .getContents<String>(java.awt.datatransfer.DataFlavor.stringFlavor)
        assertEquals("SENTINEL", clip)

        onDelete()
        // Must not resurrect or alter state
    }
    fun testDeleteRecoveryWhileActiveDoesNotResurrectOnRedrawOrQuestionSwitch() = check { h ->
        h.input.text = "general"
        h.question("q")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")
        h.finish(0, false)
        h.direct()
        assertEquals("A", h.input.text)
        assertTrue(h.answerRecoveryBtn.isVisible)
        val rec = h.answers.recoveries.single()
        assertEquals("A", rec.text)

        h.view.javaClass.getDeclaredMethod("showAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        val (item, _, onDelete) = h.recoveryViewerEvents.single()
        assertEquals("A", item.text)
        onDelete()

        // 삭제 직후: 복구 목록 비어있음, 버튼 숨김, 입력창의 "A" 유지, 일반 초안은 "general" 유지
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
        assertEquals("A", h.input.text)
        assertEquals("general", h.answers.generalText())

        // 같은 질문 재그림 반복
        h.question("q")
        h.question("q")
        h.question("q")

        // q2 전환
        h.question("q2")

        // 기대값: 복구 목록 0건, 버튼 숨김, 입력창은 general로 복귀
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
        assertEquals("general", h.input.text)
    }
    fun testDeleteRecoveryCancelAndReenterDoesNotResurrectOnQuestionSwitch() = check { h ->
        h.input.text = "general"
        h.question("q")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")
        h.finish(0, false)
        h.direct()
        val rec = h.answers.recoveries.single()

        h.view.javaClass.getDeclaredMethod("showAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        val (_, _, onDelete) = h.recoveryViewerEvents.single()
        onDelete()
        assertTrue(h.answers.recoveries.isEmpty())

        // Cancel answer mode -> restores general draft
        h.key("magi.cancelAnswer")
        assertEquals("general", h.input.text)
        assertFalse(h.field<JPanel>("answerBar").isVisible)

        // Re-enter direct answer mode -> restores "A" without version increment
        h.direct()
        assertEquals("A", h.input.text)
        assertTrue(h.field<JPanel>("answerBar").isVisible)

        // Question switch to q2 -> does not resurrect
        h.question("q2")
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
        assertEquals("general", h.input.text)
    }
    fun testDeleteRecoveryThenActualEditIsRecovered() = check { h ->
        h.input.text = "general"
        h.question("q")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")
        h.finish(0, false)
        h.direct()
        val rec = h.answers.recoveries.single()

        h.view.javaClass.getDeclaredMethod("showAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        val (_, _, onDelete) = h.recoveryViewerEvents.single()
        onDelete()
        assertTrue(h.answers.recoveries.isEmpty())

        // User actually types new B
        h.input.text = "new B"

        // Leaving question exposes new B
        h.question("q2")
        assertEquals(1, h.answers.recoveries.size)
        val recB = h.answers.recoveries.single()
        assertEquals("new B", recB.text)
        assertTrue(recB.version > rec.version)
        assertTrue(h.answerRecoveryBtn.isVisible)
        assertTrue(h.answerRecoveryBtn.text.contains("(1)"))
        assertEquals("general", h.input.text)
    }
    fun testDeleteRecoveryThenSessionSwitchDoesNotResurrect() = check { h ->
        h.input.text = "general"
        h.question("q")
        h.direct()
        h.input.text = "A"
        h.key("magi.send")
        h.finish(0, false)
        h.direct()
        val rec = h.answers.recoveries.single()

        h.view.javaClass.getDeclaredMethod("showAnswerRecovery", dev.sayaya.magi.ide.usecase.AnswerDrafts.Recovery::class.java)
            .apply { isAccessible = true }.invoke(h.view, rec)
        val (_, _, onDelete) = h.recoveryViewerEvents.single()
        onDelete()
        assertTrue(h.answers.recoveries.isEmpty())

        // Switch to session s2
        h.session = "s2"
        h.question("q2", "s2")
        assertTrue(h.answers.recoveries.isEmpty())
        assertFalse(h.answerRecoveryBtn.isVisible)
    }
}


