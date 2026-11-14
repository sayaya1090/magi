package dev.sayaya.magi.ide.ui

/** Follow intent is independent from geometry. Time is injectable for deterministic tests. */
internal class ScrollPolicy(private val graceMs: Long, private val clock: () -> Long = System::currentTimeMillis) {
    var following = true
    var dragging = false
    var pinPending = false
    private var streaming = true
    private var quietSince = 0L
    fun streaming(on: Boolean) {
        if (on) streaming = true
        else if (streaming) { streaming = false; quietSince = clock() }
    }
    fun pulls() = pinPending || streaming || clock() - quietSince < graceMs
    fun headingToEnd(atEnd: Boolean) = following && !dragging && (pulls() || atEnd)
}
