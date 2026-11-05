package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.awt.Container
import javax.swing.JButton
import javax.swing.JPanel

class TranscriptFocusTest : BasePlatformTestCase() {
    fun testTranscriptTraversalDoesNotSortTheWholeConversation() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            val column = view.javaClass.getDeclaredField("column").apply { isAccessible = true }.get(view) as JPanel
            val root = JPanel().apply {
                isFocusCycleRoot = true
                focusTraversalPolicy = javax.swing.LayoutFocusTraversalPolicy()
                add(column)
            }
            repeat(1000) { n -> column.add(JPanel().apply { add(JButton("copy $n")) }) }
            val traversalRoot: Container = if (column.isFocusCycleRoot || column.isFocusTraversalPolicyProvider) column else root
            val policy = traversalRoot.focusTraversalPolicy
            assertFalse("chat traversal must avoid coordinate sorting over all historical rows",
                policy is javax.swing.SortingFocusTraversalPolicy)
            assertFalse("Tab must remain able to leave the transcript", column.isFocusCycleRoot)
            assertTrue("transcript must supply its own traversal order", column.isFocusTraversalPolicyProvider)
        } finally { view.dispose() }
    }
    fun testTraversalVisitsRowsInOrderAndLeavesForComposer() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        val root = JPanel(java.awt.BorderLayout()).apply {
            isFocusCycleRoot = true
            focusTraversalPolicy = java.awt.ContainerOrderFocusTraversalPolicy()
            isFocusable = false
        }
        try {
            val column = view.javaClass.getDeclaredField("column").apply { isAccessible = true }.get(view) as JPanel
            column.isFocusable = false
            val first = JButton("first")
            val last = JButton("last")
            column.add(JPanel().apply { isFocusable = false; add(first) })
            column.add(JPanel().apply { isFocusable = false; add(last) })
            val composer = JButton("composer")
            root.add(column, java.awt.BorderLayout.CENTER)
            root.add(composer, java.awt.BorderLayout.SOUTH)
            root.addNotify()
            root.setSize(400, 500)
            fun layout(c: Container) { c.doLayout(); c.components.filterIsInstance<Container>().forEach(::layout) }
            layout(root)
            val policy = root.focusTraversalPolicy
            assertSame("forward traversal must visit the next row", last, policy.getComponentAfter(root, first))
            assertSame("Tab must leave the final row for the composer", composer, policy.getComponentAfter(root, last))
            assertSame("backward traversal must return to the final row", last, policy.getComponentBefore(root, composer))
            println("PASS transcript forward/backward traversal and composer exit")
        } finally {
            if (root.isDisplayable) root.removeNotify()
            view.dispose()
        }
    }

}
