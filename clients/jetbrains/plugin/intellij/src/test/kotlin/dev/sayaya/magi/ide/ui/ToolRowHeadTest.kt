package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBLabel
import java.awt.Container
import java.awt.FlowLayout
import javax.swing.JPanel

/**
 * A tool row's time sat in the middle of the row on a real screen (2026-09-26) while every other row's
 * time stood at the right edge: the actions panel (a FlowLayout, unbounded maximum width) took half of
 * the spare width from the horizontal BoxLayout, the glue took the other half, and the time floated
 * between them. The time belongs just left of the row's icons.
 */
class ToolRowHeadTest : BasePlatformTestCase() {
    private fun labels(c: Container): List<JBLabel> = c.components.flatMap {
        (if (it is JBLabel) listOf(it) else emptyList()) + (if (it is Container) labels(it) else emptyList())
    }

    fun `test the time stands next to the row's icons, not mid-row`() {
        val actions = JPanel(FlowLayout(FlowLayout.RIGHT, 2, 0)).apply { add(JBLabel("[copy]")); add(JBLabel("[open]")) }
        val head = Look.toolHead("edit", "✓", Look.success, "{\"path\":\"a.py\"}", "22:11:12", actions)
        head.setSize(1200, 24)
        head.doLayout(); actions.doLayout()
        val time = labels(head).first { it.text == "22:11:12" }
        // Measured to the first icon, not to the actions panel: a stretched panel starts right after the
        // time and pushes its icons to its own right edge, which is exactly the defect.
        val firstIcon = actions.getComponent(0)
        val gap = (actions.x + firstIcon.x) - (time.x + time.width)
        assertTrue("the time floats ${gap}px away from the row's icons", gap in 0..12)
    }
}
