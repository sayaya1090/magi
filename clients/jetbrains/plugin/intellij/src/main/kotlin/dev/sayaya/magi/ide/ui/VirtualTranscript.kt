package dev.sayaya.magi.ide.ui

import com.intellij.ui.components.JBPanel
import java.awt.Dimension
import javax.swing.Scrollable
import javax.swing.SwingConstants

/** Only attached rows participate in Swing layout; other rows retain measured heights. */
internal class VirtualTranscript(private val changed: () -> Unit) : JBPanel<JBPanel<*>>(null), Scrollable {
    private var heights = IntArray(0)
    private var offsets = intArrayOf(0)
    private var first = 0
    private var measuring = false

    fun reset() { heights = IntArray(0); offsets = intArrayOf(0); keys = emptyList(); held = null }

    private companion object { const val HOLD_MS = 700L }

    private var keys: List<String> = emptyList()

    /**
     * A row measured shorter than the depth the reader was at, kept for a moment. The IDE preview
     * re-patches an answer and its height is reported short and then whole again (idea.log:
     * 1348->304->477); the view was cut to the short row and never came back. If the row grows past
     * that depth again within [HOLD_MS] the reader returns to it; after that a shrink is real.
     */
    private class Held(val key: String?, val row: Int, val into: Int, val at: Long)
    private var held: Held? = null

    /**
     * Rows say who they are before the range is asked for. Heights used to be kept by row number only,
     * so a row leaving or arriving above the reader slid every height below it by one row and the
     * anchor, also a row number, jumped by the difference (idea.log: 26->988, 988->26, ~900px). Heights
     * now follow the row, and the viewport stays on the row it showed at the same depth.
     */
    fun rekey(next: List<String>) {
        val seen = HashMap<String, Int>()
        val named = next.map { k -> val n = (seen[k] ?: 0) + 1; seen[k] = n; if (n == 1) k else "$k#$n" }
        if (named == keys) return
        if (keys.isEmpty() || keys.size != heights.size) { keys = named; return }
        val viewport = parent as? javax.swing.JViewport
        val beforeY = viewport?.viewPosition?.y ?: 0
        val top = at(beforeY)
        val topKey = keys[top]
        val into = beforeY - offsets[top]
        val old = HashMap<String, Int>(keys.size * 2)
        keys.forEachIndexed { i, k -> old[k] = heights[i] }
        heights = IntArray(named.size) { old[named[it]] ?: 100 }
        keys = named
        rebuild()
        val moved = named.indexOf(topKey)
        if (viewport != null && moved >= 0) {
            val corrected = (offsets[moved].toLong() + into).coerceIn(0, Int.MAX_VALUE.toLong()).toInt()
            if (corrected != beforeY) {
                ScrollTrace.note("rekey", beforeY, corrected, "row=$top->$moved")
                setSize(width, offsets.last())
                viewport.viewPosition = java.awt.Point(viewport.viewPosition.x, corrected)
            }
        }
    }

    private fun sized(count: Int) {
        if (count != heights.size) {
            heights = IntArray(count) { heights.getOrElse(it) { 100 } }
            rebuild()
        }
    }

    private fun at(position: Int): Int {
        val found = offsets.binarySearch(position.coerceAtLeast(0))
        return (if (found >= 0) found else -found - 2).coerceIn(0, heights.lastIndex)
    }

    fun range(count: Int, y: Int, extent: Int): IntRange {
        sized(count)
        if (count == 0) return IntRange.EMPTY
        if (count <= 30) return 0 until count
        val margin = extent.coerceAtLeast(320)
        return at(y - margin)..at(y + extent.coerceAtLeast(320) + margin)
    }

    /** The rows the viewport itself shows at [y], without the margin [range] adds. */
    fun visible(count: Int, y: Int, extent: Int): IntRange {
        sized(count)
        return if (count == 0) IntRange.EMPTY else at(y)..at(y + extent)
    }

    /** Where the viewport will stand once it reaches the end of [count] rows. */
    fun endPosition(count: Int, extent: Int): Int {
        sized(count)
        return (offsets.last() - extent).coerceAtLeast(0)
    }

    fun start(index: Int) { first = index }

    private fun rebuild() {
        offsets = IntArray(heights.size + 1)
        heights.indices.forEach { i -> offsets[i + 1] =
            (offsets[i].toLong() + heights[i]).coerceAtMost(Int.MAX_VALUE.toLong() - 1).toInt() }
    }

    override fun doLayout() {
        if (heights.isEmpty()) {
            components.forEach { it.setBounds(0, 0, width, it.preferredSize.height) }
            return
        }
        if (measuring) return
        measuring = true
        var updated = false
        val viewport = parent as? javax.swing.JViewport
        val beforeY = viewport?.viewPosition?.y ?: 0
        val found = offsets.binarySearch(beforeY)
        val anchor = (if (found >= 0) found else -found - 2).coerceIn(0, heights.lastIndex)
        val anchorOffset = beforeY - offsets[anchor]
        try {
            updated = measure(0)
            if (updated) {
                rebuild()
                val now = System.currentTimeMillis()
                val back = held?.takeIf { now - it.at <= HOLD_MS }?.let { h ->
                    val row = h.key?.let { k -> keys.indexOf(k).takeIf { it >= 0 } } ?: h.row
                    if (row in heights.indices && heights[row] > h.into) offsets[row] + h.into else null
                }
                if (held != null && (back != null || now - held!!.at > HOLD_MS)) held = null
                if (viewport != null && back != null) {
                    if (back != beforeY) {
                        ScrollTrace.note("held", beforeY, back, "row=$anchor")
                        viewport.viewPosition = java.awt.Point(viewport.viewPosition.x, back)
                    }
                } else if (viewport != null) {
                    if (anchorOffset >= heights[anchor] && held == null)
                        held = Held(keys.getOrNull(anchor), anchor, anchorOffset, now)
                    val corrected = (offsets[anchor].toLong() + anchorOffset).coerceIn(0, Int.MAX_VALUE.toLong()).toInt()
                    if (corrected != beforeY) {
                        ScrollTrace.note("anchor", beforeY, corrected, "row=$anchor")
                        viewport.viewPosition = java.awt.Point(viewport.viewPosition.x, corrected)
                    }
                }
            }
            components.forEachIndexed { local, component ->
                val index = first + local
                if (index in heights.indices) component.setBounds(0, offsets[index], width, heights[index])
            }
        } finally { measuring = false }
        if (updated) { revalidate(); changed() }
    }

    /** Measures attached rows from [from] on; true when any height changed. */
    private fun measure(from: Int): Boolean {
        var updated = false
        components.forEachIndexed { local, component ->
            val index = first + local
            if (index !in heights.indices || index < from) return@forEachIndexed
            component.setSize(width.coerceAtLeast(1), heights[index])
            val h = component.preferredSize.height.coerceAtLeast(1)
            if (heights[index] != h) {
                ScrollTrace.note("height", heights[index], h, "row=$index ${component.javaClass.simpleName}")
                heights[index] = h; updated = true
            }
        }
        return updated
    }

    /**
     * The viewport sizes itself from this before [doLayout] runs, and the followed end is pulled
     * there at once. A row that has just landed still counts as the 100px guess at that point, so
     * the end jumped past it and fell back once it was measured — on every tool row of a turn.
     * Rows below the top row cannot move what the viewport shows, so they are measured first.
     */
    override fun getPreferredSize(): Dimension {
        if (isPreferredSizeSet) return super.getPreferredSize()
        if (heights.isEmpty()) return Dimension(width.coerceAtLeast(1), components.maxOfOrNull { it.preferredSize.height } ?: 0)
        if (!measuring && width > 0) {
            measuring = true
            val changed = try { measure(at((parent as? javax.swing.JViewport)?.viewPosition?.y ?: 0) + 1) } finally { measuring = false }
            if (changed) { rebuild(); changed() }
        }
        return Dimension(width.coerceAtLeast(1), offsets.last())
    }
    override fun getPreferredScrollableViewportSize() = Dimension(480, 320)
    override fun getScrollableTracksViewportWidth() = true
    override fun getScrollableTracksViewportHeight() = false
    override fun getScrollableUnitIncrement(r: java.awt.Rectangle, orientation: Int, direction: Int) = 20
    override fun getScrollableBlockIncrement(r: java.awt.Rectangle, orientation: Int, direction: Int) =
        if (orientation == SwingConstants.VERTICAL) r.height.coerceAtLeast(20) else r.width.coerceAtLeast(20)
}
