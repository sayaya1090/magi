package dev.sayaya.magi.ide.ui

import java.awt.Component
import java.awt.KeyboardFocusManager
import java.awt.Rectangle
import javax.swing.JViewport
import javax.swing.SwingUtilities

/** New read-only text widgets must not scroll their initial caret into view while composing. */
internal class TranscriptViewport(
    private val focusOwner: () -> Component? = { KeyboardFocusManager.getCurrentKeyboardFocusManager().focusOwner },
) : JViewport() {
    private var arranging = 0
    private var settling = 0
    /**
     * Runs [block] while row components are swapped or laid out. Detaching a focused row moves focus to
     * another row, whose reveal scrolled the view without any marker (IDE 21:10:35: -1395 then +2498).
     * Reveals are refused during the block and until the focus events it queued have run.
     */
    fun <T> quietly(block: () -> T): T {
        arranging++
        try { return block() } finally {
            arranging--
            settling++
            SwingUtilities.invokeLater { settling-- }
        }
    }

    override fun scrollRectToVisible(rect: Rectangle) {
        if (arranging > 0 || settling > 0) return
        val focus = focusOwner()
        if (focus != null && view != null && SwingUtilities.isDescendingFrom(focus, view)) {
            super.scrollRectToVisible(rect)
        }
    }
}
