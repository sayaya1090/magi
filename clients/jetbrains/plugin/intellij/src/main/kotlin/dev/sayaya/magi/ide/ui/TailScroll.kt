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
internal class TailScroll(private val pane: JScrollPane, private val graceMs: Long = 1500L) : AutoCloseable {
    /**
     * Following the end is for an answer still arriving. After the turn a diagram node or radio pick
     * grows or shrinks a row (the notes under the diagram), and the view used to be pulled to the new
     * end under the pointer (sandbox: following=true, height 720->847, tail 3282->3447). Rows still
     * measured just after the turn are covered by [graceMs]; an explicit pull always goes once.
     */
    private var streaming = true
    private var quietSince = 0L
    private var pinPending = false
    private fun pulls() = pinPending || streaming || System.currentTimeMillis() - quietSince < graceMs

    /** Whether a turn is open; height changes only pull to the end while it is (and for [graceMs] after). */
    fun streaming(on: Boolean) {
        if (on) streaming = true
        else if (streaming) { streaming = false; quietSince = System.currentTimeMillis() }
    }
    // Every switch is recorded: a view thrown to the end has a follow 0->1 just before it.
    private var following = true
        set(v) {
            if (field != v) ScrollTrace.note("follow", if (field) 1 else 0, if (v) 1 else 0,
                "value=${pane.verticalScrollBar.value} max=${pane.verticalScrollBar.maximum} ext=${pane.verticalScrollBar.visibleAmount}")
            field = v
        }
    private var queued = false
    private var closed = false
    private var dragging = false
    private var gesture = 0
    private val bar get() = pane.verticalScrollBar

    /** The view is heading for the end, so the rows to build are the ones there, not the ones it passes. */
    val followingEnd get() = following && !dragging
    private fun nearEnd() = bar.value.toLong() + bar.visibleAmount >= bar.maximum.toLong() - 48
    private var traced = 0
    private val changed = ChangeListener {
        if (bar.value != traced) {
            ScrollTrace.note("model", traced, bar.value, "max=${bar.maximum} ext=${bar.visibleAmount} following=$following")
            traced = bar.value
        }
        contentChanged()
    }
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
        if (closed || !following || dragging || queued || !pulls()) return
        queued = true
        SwingUtilities.invokeLater {
            queued = false
            if (!closed && following && !dragging && pulls()) {
                val end = (bar.maximum - bar.visibleAmount).coerceAtLeast(bar.minimum)
                if (bar.value != end) { ScrollTrace.note("tail", bar.value, end); bar.value = end }
                pinPending = false
            }
        }
    }

    fun restore(position: Int, follow: Boolean) {
        gesture++; dragging = false; following = follow
        if (follow) { pinPending = true; contentChanged() } else bar.value = position.coerceAtLeast(0)
    }

    fun followEnd() { ScrollTrace.note("follow-end", bar.value, bar.value); gesture++; following = true; pinPending = true; contentChanged() }

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

        /**
         * How far the page itself will move for this wheel event once its script runs (JCEF hands the page
         * 10 px a line), so a browser that has not loaded yet scrolls no faster than one that has.
         */
        fun wheelPixels(e: java.awt.event.MouseWheelEvent): Double = e.preciseWheelRotation * e.scrollAmount * 10

        /** CEF consumes native wheel events; its JS bridge must forward user intent as well as position. */
        fun forwardWheel(pane: JScrollPane, delta: Double) {
            if (!delta.isFinite() || delta == 0.0) return
            (pane.getClientProperty(PROPERTY) as? TailScroll)?.userNavigation(delta > 0)
            val bar = pane.verticalScrollBar
            // Record where the bar actually goes: a wheel at the end used to be logged as a 370px jump
            // although the clamped bar never moved (idea.log 16:21:55).
            val next = (bar.value.toLong() + kotlin.math.round(delta).toLong()).coerceIn(bar.minimum.toLong(),
                (bar.maximum - bar.visibleAmount).coerceAtLeast(bar.minimum).toLong()).toInt()
            ScrollTrace.note("cef-wheel", bar.value, next)
            bar.value = next
        }
    }
}
