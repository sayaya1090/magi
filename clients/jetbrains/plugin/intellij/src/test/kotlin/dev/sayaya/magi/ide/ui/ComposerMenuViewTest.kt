package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import javax.swing.JMenuItem
import javax.swing.event.PopupMenuEvent
import javax.swing.text.JTextComponent

/**
 * 하단 입력창에서 우클릭하면 편집 메뉴(잘라내기·복사·붙여넣기·모두 선택)가 열린다. 입력창은 맨 `JBTextArea` 라
 * Swing 이 기본 메뉴를 주지 않아 우클릭해도 아무것도 안 열렸다(2026-11-12 보고). 실제 도구창의 `input` 을
 * 꺼내 메뉴가 달렸는지, 선택 유무에 따라 항목이 켜지고 꺼지는지, 모두 선택이 동작하는지 본다.
 */
class ComposerMenuViewTest : BasePlatformTestCase() {
    fun testRightClickOnTheComposerOpensAnEditMenu() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val input = view.javaClass.getDeclaredField("input").apply { isAccessible = true }.get(view) as JTextComponent
            val menu = input.componentPopupMenu
            println("COMPOSER-MENU menu=${menu != null}")
            assertNotNull("the composer carries a context menu", menu)
            val items = menu.components.filterIsInstance<JMenuItem>()
            val keys = listOf("composer.menu.cut", "composer.menu.copy", "composer.menu.paste", "composer.menu.selectAll")
            assertEquals(keys.map { MagiBundle.msg(it) }, items.map { it.text })
            fun open() = menu.popupMenuListeners.forEach { it.popupMenuWillBecomeVisible(PopupMenuEvent(menu)) }
            input.text = "hello world"
            input.select(0, 0)
            open()
            val off = items.map { it.isEnabled }
            items[3].doClick()
            val picked = input.selectedText
            open()
            val on = items.map { it.isEnabled }
            println("COMPOSER-MENU off=$off picked=$picked on=$on")
            assertEquals("nothing selected: cut and copy are off", listOf(false, false, true, true), off)
            assertEquals("select all picks the whole text", "hello world", picked)
            assertEquals("with a selection every item is on", listOf(true, true, true, true), on)
        } finally {
            view.dispose()
        }
    }
}
