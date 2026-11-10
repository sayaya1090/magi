package dev.sayaya.magi.ide.ui

/** A streaming HTML replacement may briefly report the old/empty DOM height. */
internal class StreamingHeight(
    var streaming: Boolean,
    private val clock: () -> Long = System::currentTimeMillis,
) {
    private var width = -1
    private var measured = 0
    private var last = -1
    private var beforeLast = -1
    private var lastAt = 0L

    fun accept(height: Int, currentWidth: Int): Int? {
        if (height <= 0 || currentWidth <= 0) return null
        val now = clock()
        // A real width change may legitimately reduce wrapped content height.
        if (currentWidth != width) {
            width = currentWidth
            measured = height
            beforeLast = -1
        } else {
            measured = when {
                streaming -> maxOf(measured, height)
                // A settled page reflowing between two heights at one width would shake the
                // following view up and down at the rate of its reports: hold the taller one.
                height == beforeLast && height != last && now - lastAt < FLIP_MS -> maxOf(height, last)
                else -> height
            }
        }
        beforeLast = last
        last = height
        lastAt = now
        return measured
    }

    private companion object {
        /** A return to the height before last within this long is a reflow flip, not a change. */
        const val FLIP_MS = 1000L
    }
}
