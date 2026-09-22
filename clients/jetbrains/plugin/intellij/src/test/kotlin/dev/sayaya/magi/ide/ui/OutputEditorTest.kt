package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.util.Disposer
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Who
import javax.swing.JButton
import java.awt.Container

class OutputEditorTest : BasePlatformTestCase() {
    private fun buttons(c: Container): List<JButton> = c.components.flatMap {
        (if (it is JButton) listOf(it) else emptyList()) + (if (it is Container) buttons(it) else emptyList())
    }
    private fun source(view: MagiToolWindow.View, row: Row): JButton? {
        val panel = view.javaClass.getDeclaredMethod("rowPanel", Row::class.java).apply { isAccessible = true }.invoke(view, row) as Container
        return buttons(panel).firstOrNull { it.text == MagiBundle.msg("chat.output.open") }
    }
    @Suppress("UNCHECKED_CAST")
    private fun <T> field(view: MagiToolWindow.View, name: String): T =
        view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as T

    fun testOpenFailureKeepsGeneralAndAnswerDraftsAndAttachments() {
        for (answering in listOf(false, true)) {
            var attempts = 0
            val view = MagiToolWindow.View(project, sendSession = { "s1" }, outputOpener = {
                attempts++; throw IllegalStateException("open fixture failed")
            })
            try {
                val input = field<javax.swing.JTextArea>(view, "input")
                input.text = "general draft"
                view.attach(dev.sayaya.magi.ide.model.FileRef("keep.kt"))
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                if (answering) {
                    val q = dev.sayaya.magi.ide.model.Waiting("q", "question", "Choose", options = listOf("A", "B"))
                    view.javaClass.getDeclaredMethod("drawPrompt", dev.sayaya.magi.ide.model.Waiting::class.java, String::class.java)
                        .apply { isAccessible = true }.invoke(view, q, "s1")
                    buttons(field<Container>(view, "buttons")).first { it.text == MagiBundle.msg("chat.answer.direct") }.doClick()
                    input.text = "answer draft"
                }
                source(view, Row(Who.Agent, "summary", outputText = "full", outputSeq = 20))!!.doClick()
                com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
                assertEquals(1, attempts)
                assertTrue(field<javax.swing.JLabel>(view, "notice").text.contains("open fixture failed"))
                assertEquals(if (answering) "answer draft" else "general draft", input.text)
                assertEquals(answering, field<javax.swing.JPanel>(view, "answerBar").isVisible)
                assertEquals(listOf(dev.sayaya.magi.ide.model.FileRef("keep.kt")), field<List<*>>(view, "refs"))
                if (answering) {
                    input.actionMap.get("magi.cancelAnswer").actionPerformed(java.awt.event.ActionEvent(input, 0, "cancel"))
                    assertEquals("general draft", input.text)
                }
                assertTrue(FileEditorManager.getInstance(project).openFiles.isEmpty())
            } finally { Disposer.dispose(view) }
        }
    }

    fun testClosedSourceReopensAndNewResultHasIndependentDocument() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" })
        val manager = FileEditorManager.getInstance(project)
        try {
            for ((index, text) in listOf("", "{\n  \"value\": 1\n}", "line\n".repeat(10000)).withIndex()) {
                val row = Row(Who.Tool, "summary", callId = "same:call", outputText = text, outputSeq = 30L + index, outputJson = index == 1)
                val button = source(view, row)!!
                button.doClick()
                val first = manager.openFiles.single()
                assertEquals(text, FileDocumentManager.getInstance().getDocument(first)!!.text)
                assertFalse(first.isWritable)
                val type = first.fileType
                manager.closeFile(first)
                button.doClick()
                val reopened = manager.openFiles.single()
                assertNotSame(first, reopened)
                assertEquals(text, FileDocumentManager.getInstance().getDocument(reopened)!!.text)
                assertEquals(type, reopened.fileType)
                assertFalse(reopened.isWritable)
                source(view, row.copy(outputSeq = 100L + index, outputText = "new result"))!!.doClick()
                assertEquals(2, manager.openFiles.size)
                assertEquals(text, FileDocumentManager.getInstance().getDocument(reopened)!!.text)
                manager.openFiles.forEach { manager.closeFile(it) }
            }
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            Disposer.dispose(view)
        }
    }

    fun testSourceButtonOpensImmutableReadOnlyDocumentAndReusesOpenTab() {
        var session = "s1"
        val view = MagiToolWindow.View(project, sendSession = { session })
        val manager = FileEditorManager.getInstance(project)
        try {
            assertNull(source(view, Row(Who.Agent, "draft")))
            assertNull(source(view, Row(Who.Tool, "pending", callId = "a:b")))
            val row = Row(Who.Agent, "summary", outputText = "  full\nsource\n", outputSeq = 10)
            val button = source(view, row)!!
            session = "s2"
            button.doClick()
            val first = manager.openFiles.single()
            assertFalse(first.isWritable)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
            button.doClick(); assertEquals(1, manager.openFiles.size)
            source(view, row.copy(outputText = "other session"))!!.doClick()
            assertEquals(2, manager.openFiles.size)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
            source(view, Row(Who.Tool, "result", callId = "a:b", outputText = "", outputSeq = 11))!!.doClick()
            assertEquals(3, manager.openFiles.size)
            Disposer.dispose(view)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            if (!Disposer.isDisposed(view)) Disposer.dispose(view)
        }
    }
}
