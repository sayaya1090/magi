package dev.sayaya.magi.ide.ui

/** Pure geometry. IDs belong to rows; measured heights survive insertion and removal. */
internal class TranscriptGeometry {
    data class Anchor(val id: String, val into: Int)
    private var ids = emptyList<String>()
    private var heights = IntArray(0)
    private var offsets = longArrayOf(0)
    val size get() = ids.size
    val total get() = pixel(offsets.last())
    fun index(id: String) = ids.indexOf(id)
    fun height(index: Int) = heights[index]
    fun offset(index: Int) = pixel(offsets[index])
    fun matches(next: List<String>) = ids == next
    /** [next] keeps every current row in place and only adds rows after them. */
    fun extendedBy(next: List<String>) = ids.isNotEmpty() && next.size > ids.size && next.subList(0, ids.size) == ids
    fun reset() { ids = emptyList(); heights = IntArray(0); offsets = longArrayOf(0) }
    fun anchor(y: Int): Anchor? = if (size == 0) null else at(y).let { Anchor(ids[it], y - offset(it)) }
    fun position(anchor: Anchor, clamp: Boolean = true): Int? {
        val i = ids.indexOf(anchor.id)
        if (i < 0) return null
        val into = if (clamp) anchor.into.coerceIn(0, heights[i] - 1) else anchor.into.coerceAtLeast(0)
        return pixel(offsets[i] + into)
    }
    /** A removed anchor moves to the next surviving row, then the previous one. */
    fun replace(next: List<String>, anchor: Anchor?): Anchor? {
        require(next.toSet().size == next.size) { "Duplicate transcript row ID" }
        if (next == ids) return anchor
        val old = ids.withIndex().associate { it.value to heights[it.index] }
        val surviving = next.toHashSet()
        val corrected = if (anchor == null || anchor.id in surviving) anchor else {
            val i = ids.indexOf(anchor.id)
            val replacement = ids.drop(i + 1).firstOrNull { it in surviving }
                ?: ids.take(i.coerceAtLeast(0)).lastOrNull { it in surviving }
            replacement?.let { Anchor(it, 0) }
        }
        ids = next.toList()
        heights = IntArray(next.size) { old[next[it]] ?: 100 }
        rebuild()
        return corrected
    }
    fun measure(index: Int, height: Int): Boolean {
        val h = height.coerceAtLeast(1)
        if (heights[index] == h) return false
        heights[index] = h
        return true
    }
    fun rebuild() {
        offsets = LongArray(size + 1)
        heights.indices.forEach { offsets[it + 1] = offsets[it] + heights[it] }
    }
    fun at(y: Int): Int {
        if (size == 0) return 0
        val found = offsets.binarySearch(y.coerceAtLeast(0).toLong())
        return (if (found >= 0) found else -found - 2).coerceIn(0, size - 1)
    }
    fun visible(y: Int, extent: Int): IntRange = if (size == 0) IntRange.EMPTY else at(y)..at(pixel(y.toLong() + extent.coerceAtLeast(0)))
    fun range(y: Int, extent: Int, direction: Int): IntRange {
        if (size <= 30) return 0 until size
        val screen = extent.coerceAtLeast(320).toLong()
        val before = if (direction < 0) 3 else if (direction > 0) 1 else 2
        val after = if (direction > 0) 3 else if (direction < 0) 1 else 2
        val visible = visible(y, extent)
        return maxOf(at(pixel(y - screen * before)), visible.first - 128, 0)..
            minOf(at(pixel(y + screen * (1 + after))), visible.last + 128, size - 1)
    }
    private fun pixel(value: Long) = value.coerceIn(0, Int.MAX_VALUE.toLong() - 1).toInt()
}
