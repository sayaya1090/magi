package dev.sayaya.magi.ide.ui

/** Per-panel generation gate; no Swing or browser dependency. All access is on the EDT. */
internal class HeightReports {
    data class Request(val generation: Long, val width: Int)
    private var generation = 0L
    private var current: Request? = null
    private var sequence = -1L
    private var closed = false
    fun begin(width: Int): Request? {
        if (closed || width <= 0) return null
        return Request(++generation, width).also { current = it; sequence = -1 }
    }
    fun accept(generation: Long, width: Int, sequence: Long, height: Int, currentWidth: Int): Boolean {
        val request = current ?: return false
        if (closed || request.generation != generation || request.width != width || currentWidth != width || height <= 0 || sequence <= this.sequence) return false
        this.sequence = sequence
        return true
    }
    fun invalidate() { current = null; generation++; sequence = -1 }
    fun close() { invalidate(); closed = true }
}
