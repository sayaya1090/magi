package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBLabel
import java.awt.Container
import java.awt.FlowLayout
import javax.swing.JComponent
import javax.swing.JPanel

/**
 * Every row header ends the same way: [icons] [time], the time at the very right edge.
 *
 * A tool row's time once sat mid-row (2026-09-26): the actions panel (a FlowLayout, unbounded maximum
 * width) took half of the spare width from the horizontal BoxLayout. And the speaker rows put their
 * icons beside the body instead of in the header, so they dropped to a line under the time while tool
 * rows kept them on one line — two shapes in one transcript (2026-10-17, asked to align them and push
 * the time to an edge).
 */
class ToolRowHeadTest : BasePlatformTestCase() {
    private fun labels(c: Container): List<JBLabel> = c.components.flatMap {
        (if (it is JBLabel) listOf(it) else emptyList()) + (if (it is Container) labels(it) else emptyList())
    }

    private fun actions() = JPanel(FlowLayout(FlowLayout.RIGHT, 2, 0)).apply { add(JBLabel("[copy]")); add(JBLabel("[open]")) }

    private fun assertTimeAtRightEdgeAfterIcons(head: JComponent, actions: JPanel) {
        head.setSize(1200, 24)
        head.doLayout(); actions.doLayout()
        val time = labels(head).first { it.text == "22:11:12" }
        assertTrue("the time is not at the right edge: ends at ${time.x + time.width} of 1200",
            1200 - (time.x + time.width) in 0..2)
        val lastIcon = actions.getComponent(actions.componentCount - 1)
        val gap = time.x - (actions.x + lastIcon.x + lastIcon.width)
        assertTrue("the icons float ${gap}px away from the time", gap in 0..12)
    }

    fun `test a tool row ends with its icons then the time at the edge`() {
        val a = actions()
        assertTimeAtRightEdgeAfterIcons(Look.toolHead("edit", "✓", Look.success, "{\"path\":\"a.py\"}", "22:11:12", a), a)
    }

    fun `test a speaker row ends the same way`() {
        val a = actions()
        assertTimeAtRightEdgeAfterIcons(Look.rowHead("magi", Look.accent, emptyList(), "22:11:12", a), a)
    }
    fun `test all row variants share right padding and copy icon centers vertically`() {
        val panel = JPanel()
        val borders = listOf(Look.row(), Look.toolRow(), Look.pendingRow(), Look.pendingToolRow())
        val right = borders.first().getBorderInsets(panel).right
        borders.forEach { assertEquals(right, it.getBorderInsets(panel).right) }
        assertEquals(0.5f, Look.copyButton("copy") {}.alignmentY)
    }

}
