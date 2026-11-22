package dev.sayaya.magi.ide.ui

import java.awt.CardLayout
import javax.swing.JComponent
import javax.swing.JPanel

/**
 * 우측 한 자리를 채팅과 「MAGI 현황」 판이 나눠 쓴다(사용자 결정, 2026-11-22: 「채팅은 우측이
 * 편하다 — 지금 우측의 현황판은 설정 단추로 전환」). 기본은 채팅이고, [flip] 이 둘을 맞바꾼다.
 *
 * 탭이 아니라 카드다: 탭이면 머리가 늘 한 줄을 차지하고, 단추와 탭 두 길로 같은 일을 하게 된다.
 * 판은 숨어 있는 동안에도 살아 있다 — 다시 보일 때 처음부터 그리지 않는다.
 */
class ChatBoardSwap(chat: JComponent, board: JComponent) {
    private val cards = CardLayout()
    val component: JPanel = JPanel(cards).apply {
        add(chat, CHAT)
        add(board, BOARD)
    }

    var showingBoard: Boolean = false
        private set

    init { cards.show(component, CHAT) }

    /** 채팅 ↔ 판. */
    fun flip() {
        showingBoard = !showingBoard
        cards.show(component, if (showingBoard) BOARD else CHAT)
    }

    private companion object {
        const val CHAT = "chat"
        const val BOARD = "board"
    }
}
