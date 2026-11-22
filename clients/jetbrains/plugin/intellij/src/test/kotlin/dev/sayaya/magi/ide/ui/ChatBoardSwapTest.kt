package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import javax.swing.JLabel

/**
 * 채팅은 우측 한 자리에 선다. 「MAGI 현황」 판은 따로 창을 갖지 않고 같은 자리를 나눠 쓴다 —
 * 기본은 채팅, 제목 표시줄의 설정 단추를 누르면 판, 다시 누르면 채팅(사용자 결정, 2026-11-22).
 * 「지적 사항」 탭은 없앴다(쓰이지 않았다).
 */
class ChatBoardSwapTest : BasePlatformTestCase() {
    fun `test the chat shows first and the settings button swaps the same place with the board`() {
        val chat = JLabel("chat")
        val board = JLabel("board")
        val swap = ChatBoardSwap(chat, board)
        swap.component.setSize(400, 300)
        swap.component.doLayout()
        assertTrue("the chat is not shown first", chat.isVisible && !board.isVisible)
        assertFalse(swap.showingBoard)
        swap.flip()
        assertTrue("the board did not take the chat's place", board.isVisible && !chat.isVisible)
        assertTrue(swap.showingBoard)
        swap.flip()
        assertTrue("the chat did not come back", chat.isVisible && !board.isVisible)
    }

    fun `test the chat window sits on the right, holds both, and has no problems tab`() {
        val xml = java.io.File("src/main/resources/META-INF/plugin.xml").readText()
        val magi = Regex("<toolWindow\\s+id=\"magi\"[^>]*>").find(xml)?.value.orEmpty()
        assertTrue("the chat window is not anchored right: $magi", "anchor=\"right\"" in magi)
        assertFalse("the board still has a window of its own", "id=\"magi.plan\"" in xml)
        val src = java.io.File("src/main/kotlin/dev/sayaya/magi/ide/ui/MagiToolWindow.kt").readText()
        assertFalse("the problems tab is still made", "chat.tab.problems" in src || "problemsView" in src)
        assertTrue("the chat window does not hold the board in the same place", "ChatBoardSwap(" in src && "PlanToolWindow().build(" in src)
        assertTrue("no settings button swaps the place", "chat.board.toggle" in src && ".flip()" in src)
    }
}
