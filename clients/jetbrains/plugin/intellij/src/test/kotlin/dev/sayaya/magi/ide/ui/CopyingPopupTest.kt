package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBPanel
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Who
import java.awt.BorderLayout
import javax.swing.JPanel
import javax.swing.JTextArea

/**
 * 대화창의 우클릭 메뉴(선택 복사·모두 복사·모두 선택)는 대화 목록(`column`)에 달린다. 그런데 목록은 거의 전부
 * 말풍선 행과 그 안의 글 상자로 덮여 있고, Swing 은 우클릭한 바로 그 컴포넌트에서 메뉴를 찾는다 — 자식이
 * `inheritsPopupMenu` 를 켜지 않으면 말풍선 위에서는 아무것도 안 열린다(2026-11-12 보고). [Copying.install] 이
 * 행과 그 자식 전부에 리스너를 다는 자리에서 메뉴도 물려받게 한다.
 */
class CopyingPopupTest : BasePlatformTestCase() {
    fun testRightClickOnABubbleFindsTheTranscriptMenu() {
        val column = JPanel()
        val rows = listOf(Row(Who.Agent, "hello"))
        val copying = Copying()
        copying.popup(column) { rows }
        val menu = column.componentPopupMenu
        assertNotNull("the transcript carries the menu", menu)
        val text = JTextArea("hello")
        val inner = JPanel().apply { add(text) }
        val panel = JBPanel<JBPanel<*>>(BorderLayout()).apply { add(inner, BorderLayout.CENTER) }
        column.add(panel)
        copying.install(panel, rows[0]) { rows }
        val found = listOf(panel, inner, text).associate { it.javaClass.simpleName to (it.componentPopupMenu === menu) }
        println("ROW-MENU found: $found")
        assertEquals("a right click anywhere on a bubble opens the transcript menu: $found", listOf(true, true, true), found.values.toList())
    }
}
