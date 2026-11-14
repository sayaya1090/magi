package dev.sayaya.magi.ide.ui

import javax.swing.JScrollPane
import javax.swing.SwingUtilities

/** Only this object writes programmatic positions. Native user movement remains Swing's job. */
internal class TranscriptCoordinator private constructor(private val pane: JScrollPane) {
    private data class Move(val position: Int?, val generation: Long, val reason: String)
    private var pending: Move? = null
    private var queued = false
    var generation = 0L
        private set
    private var movedAt = 0L
    private var lastDirection = 0
    val direction get() = if (System.nanoTime() - movedAt < 250_000_000L) lastDirection else 0
    fun cancel(direction: Int = 0) { generation++; pending = null; lastDirection = direction; movedAt = System.nanoTime() }
    /** A geometry correction is queued and still valid: the viewport has not caught up with the layout yet. */
    val awaiting get() = pending?.let { it.generation == generation && it.reason in CORRECTIONS } == true
    fun request(position: Int?, reason: String) {
        // Following the tail wins over geometry correction in the same transaction. A later correction
        // replaces an earlier one: two rows growing in one tick queued two, and keeping the first left
        // the view off by the second growth (850 instead of 920).
        if (pending == null || position == null || reason == "restore" || pending?.reason in CORRECTIONS)
            pending = Move(position, generation, reason)
        if (queued) return
        queued = true
        SwingUtilities.invokeLater {
            queued = false
            val move = pending ?: return@invokeLater
            pending = null
            if (move.generation != generation) return@invokeLater
            // Commit the published content size before clamping the new position.
            pane.doLayout()
            pane.viewport.doLayout()
            val bar = pane.verticalScrollBar
            val extent = pane.viewport.extentSize.height
            val maximum = pane.viewport.viewSize.height.coerceAtLeast(extent)
            val end = (maximum - extent).coerceAtLeast(0)
            val next = (move.position ?: end).coerceIn(bar.minimum, end)
            if (bar.value != next || bar.maximum != maximum || bar.visibleAmount != extent) {
                ScrollTrace.note(move.reason, bar.value, next)
                bar.setValues(next, extent, 0, maximum)
            }
        }
    }
    companion object {
        private const val KEY = "magi.transcript.coordinator"
        private val CORRECTIONS = setOf("anchor", "held")
        fun of(pane: JScrollPane): TranscriptCoordinator =
            (pane.getClientProperty(KEY) as? TranscriptCoordinator) ?: TranscriptCoordinator(pane).also { pane.putClientProperty(KEY, it) }
    }
}
