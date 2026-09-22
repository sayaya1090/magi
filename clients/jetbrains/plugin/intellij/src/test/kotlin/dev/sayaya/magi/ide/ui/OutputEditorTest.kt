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
    fun testSourceButtonOpensImmutableReadOnlyDocumentAndReusesOpenTab() {
        var session = "s1"
        val view = MagiToolWindow.View(project, sendSession = { session })
        val manager = FileEditorManager.getInstance(project)
        fun buttons(c: Container): List<JButton> = c.components.flatMap {
            (if (it is JButton) listOf(it) else emptyList()) + (if (it is Container) buttons(it) else emptyList())
        }
        fun source(row: Row): JButton? {
            val panel = view.javaClass.getDeclaredMethod("rowPanel", Row::class.java).apply { isAccessible = true }.invoke(view, row) as Container
            return buttons(panel).firstOrNull { it.text == MagiBundle.msg("chat.output.open") }
        }
        try {
            assertNull(source(Row(Who.Agent, "draft")))
            assertNull(source(Row(Who.Tool, "pending", callId = "a:b")))
            val row = Row(Who.Agent, "summary", outputText = "  full\nsource\n", outputSeq = 10)
            val button = source(row)!!
            session = "s2"
            button.doClick()
            val first = manager.openFiles.single()
            assertFalse(first.isWritable)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
            button.doClick(); assertEquals(1, manager.openFiles.size)
            source(row.copy(outputText = "other session"))!!.doClick()
            assertEquals(2, manager.openFiles.size)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
            source(Row(Who.Tool, "result", callId = "a:b", outputText = "", outputSeq = 11))!!.doClick()
            assertEquals(3, manager.openFiles.size)
            Disposer.dispose(view)
            assertEquals("  full\nsource\n", FileDocumentManager.getInstance().getDocument(first)!!.text)
        } finally {
            manager.openFiles.forEach { manager.closeFile(it) }
            if (!Disposer.isDisposed(view)) Disposer.dispose(view)
        }
    }
}
