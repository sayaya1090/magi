package dev.sayaya.magi.ide.ui

import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import dev.sayaya.magi.ide.model.Waiting
import java.awt.Container
import javax.swing.JComponent
import javax.swing.SwingUtilities

class PromptPlacementTest : BasePlatformTestCase() {
    private fun layoutTree(c: Container) {
        c.doLayout()
        c.components.filterIsInstance<Container>().forEach(::layoutTree)
    }
    fun testQuestionAndPermissionAppearAboveComposerBelowTranscript() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as JComponent
            val draw = view.javaClass.getDeclaredMethod("drawPrompt", Waiting::class.java, String::class.java).apply { isAccessible = true }
            val head = field("head")
            val input = field("input")
            val scroll = field("scroll")
            for ((width, height) in listOf(420 to 700, 1000 to 480)) {
                for (kind in listOf("question", "permission")) {
                    draw.invoke(view, Waiting("q", kind, "Choose how to proceed", options = if (kind == "question") listOf("A", "B") else emptyList()), "s1")
                    view.root.setSize(width, height)
                    layoutTree(view.root)
                    fun top(c: JComponent) = SwingUtilities.convertPoint(c, 0, 0, view.root).y
                    assertTrue(head.isVisible)
                    assertTrue("question must be below transcript", top(head) >= top(scroll) + scroll.height)
                    assertTrue("question must precede composer", top(head) + head.height <= top(input))
                    assertTrue("question and input should remain adjacent", top(input) - (top(head) + head.height) < 40)
                    assertTrue("input must stay inside the panel", top(input) + input.height <= height)
                }
                draw.invoke(view, null, "s1")
                layoutTree(view.root)
                assertFalse("answered questions must release their space", head.isVisible)
            }
        } finally { Disposer.dispose(view) }
    }
}
