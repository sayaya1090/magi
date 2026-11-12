package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import javax.swing.JComponent
import javax.swing.JPopupMenu
import javax.swing.JTextArea
import javax.swing.event.PopupMenuEvent

/**
 * 우클릭 메뉴가 열리면 Swing 이 포커스를 메뉴 쪽(JRootPane)으로 옮긴다. 닫을 때 원래 자리로 돌아오지 않으면
 * 입력창 포커스가 사라진 채 남는다(2026-11-12 보고). [FocusReturn] 은 열기 직전의 포커스 주인을 기억했다가
 * 닫힐 때 한 번 되돌린다. 대화 목록 메뉴와 입력창 메뉴 모두에 걸려 있어야 한다.
 */
class FocusReturnTest : BasePlatformTestCase() {
    private class Spy : JTextArea() {
        var asked = 0
        override fun requestFocusInWindow(): Boolean { asked++; return true }
    }

    fun testClosingTheMenuGivesFocusBack() {
        val spy = Spy()
        val menu = JPopupMenu()
        val back = FocusReturn { spy }
        val e = PopupMenuEvent(menu)
        back.popupMenuWillBecomeVisible(e)
        back.popupMenuWillBecomeInvisible(e)
        UIUtil.dispatchAllInvocationEvents()
        val picked = spy.asked
        back.popupMenuWillBecomeVisible(e)
        back.popupMenuCanceled(e)
        back.popupMenuWillBecomeInvisible(e)
        UIUtil.dispatchAllInvocationEvents()
        val cancelled = spy.asked - picked
        println("FOCUS-RETURN picked=$picked cancelled=$cancelled")
        assertEquals("closing by a pick returns focus once", 1, picked)
        assertEquals("closing by cancel returns focus once", 1, cancelled)
    }

    fun testBothTranscriptMenusReturnFocus() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view) as JComponent
            val found = listOf("input", "column").associateWith { n ->
                field(n).componentPopupMenu?.popupMenuListeners?.any { it is FocusReturn } == true
            }
            println("FOCUS-RETURN menus=$found")
            assertEquals("both menus give focus back: $found", listOf(true, true), found.values.toList())
        } finally { view.dispose() }
    }
}
