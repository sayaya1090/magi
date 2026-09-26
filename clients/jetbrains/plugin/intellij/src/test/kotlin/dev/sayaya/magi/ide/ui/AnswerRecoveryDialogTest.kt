package dev.sayaya.magi.ide.ui

import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import dev.sayaya.magi.ide.usecase.AnswerDrafts
import javax.swing.Action
import javax.swing.JComponent
import javax.swing.JLabel
import javax.swing.text.JTextComponent

/**
 * The recovery dialog, measured on its own now that it is a class and not an object literal inside
 * the tool window: what it shows, and that each button does its one thing to the item it was
 * opened for.
 */
class AnswerRecoveryDialogTest : BasePlatformTestCase() {
    private val item = AnswerDrafts.Recovery(
        id = "r1", session = "sess-1", callId = "call-9", questionText = "which file?",
        version = 1, text = "line one\nline two", reason = AnswerDrafts.REASON_QUESTION_LEFT,
    )

    private fun texts(c: java.awt.Component, out: MutableList<String> = ArrayList()): List<String> {
        when (c) {
            is JLabel -> out += c.text.orEmpty()
            is JTextComponent -> out += c.text.orEmpty()
        }
        if (c is java.awt.Container) c.components.forEach { texts(it, out) }
        return out
    }

    fun `test the dialog shows the item it was opened for, in words`() {
        val copied = ArrayList<AnswerDrafts.Recovery>()
        val dlg = AnswerRecoveryDialog(project, item, onCopy = { copied += it }, onDelete = {})
        Disposer.register(testRootDisposable, dlg.disposable)
        // Headless: no window, so read the panel the dialog builds rather than the one it shows.
        val panel = dlg.javaClass.getDeclaredMethod("createCenterPanel").let {
            it.isAccessible = true
            it.invoke(dlg) as JComponent
        }
        val shown = texts(panel)
        assertTrue("session: $shown", "sess-1" in shown)
        assertTrue("call id: $shown", "call-9" in shown)
        assertTrue("question: $shown", "which file?" in shown)
        assertTrue("the full text, untouched: $shown", "line one\nline two" in shown)
        // The reason code is translated, never shown as the raw code.
        assertTrue("reason in words: $shown", MagiBundle.msg("chat.answer.recovery.reason.question_left") in shown)
        assertFalse("raw reason code leaked: $shown", AnswerDrafts.REASON_QUESTION_LEFT in shown)
    }

    fun `test copy hands over the item and delete hands it over and closes`() {
        val copied = ArrayList<AnswerDrafts.Recovery>()
        val deleted = ArrayList<AnswerDrafts.Recovery>()
        val dlg = AnswerRecoveryDialog(project, item, onCopy = { copied += it }, onDelete = { deleted += it })
        Disposer.register(testRootDisposable, dlg.disposable)
        val actions = dlg.javaClass.getDeclaredMethod("createActions").let {
            it.isAccessible = true
            @Suppress("UNCHECKED_CAST")
            it.invoke(dlg) as Array<Action>
        }
        fun named(key: String) = actions.first { it.getValue(Action.NAME) == MagiBundle.msg(key) }

        named("chat.answer.recovery.copy").actionPerformed(null)
        assertEquals(listOf(item), copied)
        assertEquals(emptyList<AnswerDrafts.Recovery>(), deleted)
        assertFalse("copy must not close the dialog", dlg.isDisposed)

        named("chat.answer.recovery.delete").actionPerformed(null)
        assertEquals(listOf(item), deleted)
        assertTrue("delete closes the dialog", dlg.isDisposed)
    }
}
