package dev.sayaya.magi.ide.ui

import java.awt.KeyboardFocusManager
import java.awt.KeyEventDispatcher
import java.awt.event.KeyEvent
import java.awt.event.MouseAdapter
import java.awt.event.MouseEvent
import java.awt.event.MouseWheelListener
import javax.swing.JScrollPane
import javax.swing.SwingUtilities
import javax.swing.event.ChangeListener

/** Follow delayed row sizing until the person scrolls away. All state belongs to the EDT. */
internal class TailScroll(private val pane: JScrollPane) : AutoCloseable {
    private var following = true
    private var queued = false
    private var closed = false
    private var dragging = false
    private var gesture = 0
    private val bar get() = pane.verticalScrollBar
    private fun nearEnd() = bar.value.toLong() + bar.visibleAmount >= bar.maximum.toLong() - 48
    private val changed = ChangeListener { contentChanged() }
    private val wheel = MouseWheelListener { e ->
        if (!e.isShiftDown && e.preciseWheelRotation != 0.0) userNavigation(e.preciseWheelRotation > 0)
    }
    private val mouse = object : MouseAdapter() {
        override fun mousePressed(e: MouseEvent) { gesture++; dragging = true; following = false }
        override fun mouseReleased(e: MouseEvent) {
            dragging = false
            following = nearEnd()
            contentChanged()
        }
    }
    private val keys = KeyEventDispatcher { e ->
        if (e.id == KeyEvent.KEY_PRESSED && SwingUtilities.isDescendingFrom(e.component, pane)) {
            when (e.keyCode) {
                KeyEvent.VK_UP, KeyEvent.VK_PAGE_UP, KeyEvent.VK_HOME -> userNavigation(false)
                KeyEvent.VK_DOWN, KeyEvent.VK_PAGE_DOWN, KeyEvent.VK_END -> userNavigation(true)
            }
        }
        false
    }

    init {
        pane.putClientProperty(PROPERTY, this)
        bar.model.addChangeListener(changed)
        pane.viewport.addChangeListener(changed)
        pane.addMouseWheelListener(wheel)
        bar.addMouseListener(mouse)
        KeyboardFocusManager.getCurrentKeyboardFocusManager().addKeyEventDispatcher(keys)
        contentChanged()
    }

    private fun userNavigation(towardEnd: Boolean) {
        val stamp = ++gesture
        following = false // cancels a pin queued before this gesture
        if (towardEnd) SwingUtilities.invokeLater {
            // Default Swing movement happens after the input event. A later upward
            // gesture must invalidate this callback before it can re-enable following.
            if (!closed && stamp == gesture && !dragging) {
                following = nearEnd()
                contentChanged()
            }
        }
    }

    fun contentChanged() {
        if (closed || !following || dragging || queued) return
        queued = true
        SwingUtilities.invokeLater {
            queued = false
            if (!closed && following && !dragging) {
                val end = (bar.maximum - bar.visibleAmount).coerceAtLeast(bar.minimum)
                if (bar.value != end) bar.value = end
            }
        }
    }

    fun followEnd() { gesture++; following = true; contentChanged() }

    override fun close() {
        closed = true
        gesture++
        bar.model.removeChangeListener(changed)
        pane.viewport.removeChangeListener(changed)
        pane.removeMouseWheelListener(wheel)
        bar.removeMouseListener(mouse)
        KeyboardFocusManager.getCurrentKeyboardFocusManager().removeKeyEventDispatcher(keys)
        if (pane.getClientProperty(PROPERTY) === this) pane.putClientProperty(PROPERTY, null)
    }

    companion object {
        private const val PROPERTY = "magi.transcript.tail-scroll"

        /** CEF consumes native wheel events; its JS bridge must forward user intent as well as position. */
        fun forwardWheel(pane: JScrollPane, delta: Double) {
            if (!delta.isFinite() || delta == 0.0) return
            (pane.getClientProperty(PROPERTY) as? TailScroll)?.userNavigation(delta > 0)
            val bar = pane.verticalScrollBar
            val next = bar.value.toLong() + kotlin.math.round(delta).toLong()
            bar.value = next.coerceIn(bar.minimum.toLong(),
                (bar.maximum - bar.visibleAmount).coerceAtLeast(bar.minimum).toLong()).toInt()
        }
    }
}
