package dev.sayaya.magi.ide.ui

/** A streaming HTML replacement may briefly report the old/empty DOM height. */
internal class StreamingHeight(var streaming: Boolean) {
    private var width = -1
    private var measured = 0

    fun accept(height: Int, currentWidth: Int): Int? {
        if (height <= 0 || currentWidth <= 0) return null
        // A real width change may legitimately reduce wrapped content height.
        if (currentWidth != width) {
            width = currentWidth
            measured = height
        } else {
            measured = if (streaming) maxOf(measured, height) else height
        }
        return measured
    }
}
