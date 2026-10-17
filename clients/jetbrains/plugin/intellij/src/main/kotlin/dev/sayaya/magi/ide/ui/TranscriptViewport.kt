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
    override fun scrollRectToVisible(rect: Rectangle) {
        val focus = focusOwner()
        if (focus != null && view != null && SwingUtilities.isDescendingFrom(focus, view)) {
            super.scrollRectToVisible(rect)
        }
    }
}
