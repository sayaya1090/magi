package dev.sayaya.magi.ide.ui

import com.intellij.ui.components.JBPanel
import java.awt.Dimension
import javax.swing.Scrollable
import javax.swing.SwingConstants

/** Only attached rows participate in Swing layout; other rows retain measured heights. */
internal class VirtualTranscript(private val changed: () -> Unit) : JBPanel<JBPanel<*>>(null), Scrollable {
    private val geometry = TranscriptGeometry()
    private var first = 0
    private var attachedIndices: List<Int>? = null
    private var measuring = false
    private var emptyHeight = 0
    private data class Measurement(val width: Int, val preferred: Dimension)
    private val measured = java.util.WeakHashMap<java.awt.Component, Measurement>()
    private var pendingAnchor: TranscriptGeometry.Anchor? = null
    private var anchorGeneration = 0L
    // The reading point of a correction that is queued but not applied yet. Until the coordinator
    // commits it the viewport still shows the old position, which no longer names the same content.
    private var awaited: TranscriptGeometry.Anchor? = null
    private var awaitedGeneration = 0L
    private var retained = IntRange.EMPTY
    private val viewport get() = parent as? javax.swing.JViewport
    private val coordinator get() = (viewport?.parent as? javax.swing.JScrollPane)?.let(TranscriptCoordinator::of)
    private data class Held(val anchor: TranscriptGeometry.Anchor, val generation: Long, val at: Long)
    private var held: Held? = null
    private companion object { const val HOLD_MS = 700L; const val MEASURE_PASSES = 3 }

    fun reset() { measured.clear(); emptyHeight = 0; geometry.reset(); pendingAnchor = null; awaited = null; held = null; retained = IntRange.EMPTY; coordinator?.cancel() }
    fun anchor(): TranscriptGeometry.Anchor? = geometry.anchor(viewport?.viewPosition?.y ?: 0)
    /** What the person is reading: a restore in progress, a correction still queued, or what the viewport shows. */
    private fun reading(): TranscriptGeometry.Anchor? {
        val generation = coordinator?.generation ?: 0
        return pendingAnchor?.takeIf { anchorGeneration == generation }
            ?: awaited?.takeIf { awaitedGeneration == generation && coordinator?.awaiting == true }
            ?: anchor()
    }
    fun restore(anchor: TranscriptGeometry.Anchor): Boolean {
        val target = geometry.position(anchor, false) ?: return false
        pendingAnchor = anchor
        anchorGeneration = coordinator?.generation ?: 0
        coordinator?.request(target, "restore")
        return true
    }
    fun restorationIndex(): Int? = (pendingAnchor ?: held?.anchor)?.let { geometry.index(it.id).takeIf { i -> i >= 0 } }
    fun rekey(next: List<String>) {
        if (geometry.matches(next)) return
        // Rows appended after the existing ones leave the indices of the retained range valid. Output
        // below a view that is not at the end must not re-attach and re-measure rows above it (IDE
        // 21:10:35: a row entered at the 100px guess while scrolling up, -1395 then +2498). Following
        // the end still moves the range: its y passes the guard and the range is rebuilt there.
        val appended = geometry.extendedBy(next)
        pendingAnchor = geometry.replace(next, reading())
        anchorGeneration = coordinator?.generation ?: 0
        if (!appended) retained = IntRange.EMPTY
    }
    // Compatibility for callers without row data (standalone layout fixtures).
    private fun sized(count: Int) {
        if (geometry.size != count) rekey(List(count) { "row:$it" })
    }
    fun range(count: Int, y: Int, extent: Int): IntRange {
        sized(count)
        val wanted = geometry.range(y, extent, coordinator?.direction ?: 0)
        // Keep the attached range until a screen edge approaches its boundary.
        val screen = extent.coerceAtLeast(320)
        val guard = geometry.visible((y - screen).coerceAtLeast(0), screen * 3)
        if (!retained.isEmpty() && guard.first >= retained.first && guard.last <= retained.last) return retained
        retained = wanted
        return wanted
    }
    fun visible(count: Int, y: Int, extent: Int): IntRange { sized(count); return geometry.visible(y, extent) }
    fun endPosition(count: Int, extent: Int): Int { sized(count); return (geometry.total - extent).coerceAtLeast(0) }
    fun start(index: Int) { first = index; attachedIndices = null }
    fun attach(indices: List<Int>) { attachedIndices = indices.toList() }
    private fun index(local: Int) = attachedIndices?.getOrNull(local) ?: (first + local)

    override fun doLayout() {
        if (measuring) return
        if (geometry.size == 0) {
            val next = components.maxOfOrNull { it.preferredSize.height } ?: 0
            components.forEach { it.setBounds(0, 0, width, it.preferredSize.height) }
            if (emptyHeight != next) { emptyHeight = next; revalidate(); changed() }
            return
        }
        measuring = true
        val before = reading()
        pendingAnchor = null
        var updated = false
        try {
            updated = measure(0)
            if (updated) geometry.rebuild()
            val now = System.currentTimeMillis()
            val generation = coordinator?.generation ?: 0
            held = held?.takeIf { it.generation == generation && now - it.at <= HOLD_MS && geometry.position(it.anchor) != null }
            val back = held?.takeIf { geometry.position(it.anchor) == geometry.position(it.anchor, false) }?.anchor
            if (back != null) held = null
            val target = back ?: before
            if (target != null) {
                if (held == null && geometry.position(target) != geometry.position(target, false))
                    held = Held(target, generation, now)
                // Temporary reflow retains depth; the coordinator clamps to the content bounds.
                val y = geometry.position(target, false)
                // While an earlier correction is queued it is replaced even when the target equals the
                // current position (a row that grew and shrank back in one tick).
                if (y != null && (y != viewport?.viewPosition?.y || coordinator?.awaiting == true)) {
                    coordinator?.request(y, if (back != null) "held" else "anchor")
                    awaited = target; awaitedGeneration = generation
                } else awaited = null
            }
            components.forEachIndexed { local, component ->
                val i = index(local)
                if (i < geometry.size) component.setBounds(0, geometry.offset(i), width, geometry.height(i))
            }
            // Publish size before the queued correction sees the scrollbar's bounds.
            if (height != geometry.total) setSize(width, geometry.total)
        } finally { measuring = false }
        if (updated) { revalidate(); changed() }
    }

    /** Empties the layout caches under [c] without invalidating the column it sits in. */
    private fun forgetLayout(c: java.awt.Component) {
        if (c !is java.awt.Container) return
        (c.layout as? java.awt.LayoutManager2)?.invalidateLayout(c)
        c.components.forEach { forgetLayout(it) }
    }

    private fun layOut(c: java.awt.Component) {
        if (c !is java.awt.Container) return
        c.doLayout()
        c.components.forEach { layOut(it) }
    }

    /** Measures attached rows from [from] on; true when any height changed. */
    private fun measure(from: Int): Boolean {
        var updated = false
        components.forEachIndexed { local, component ->
            val index = index(local)
            if (index !in 0 until geometry.size || index < from) return@forEachIndexed
            // Lay the row out at this width before asking its height, with the layout caches emptied, and
            // again at the height it reports until the two agree. A wrapping text area that was never laid
            // out is 0px wide and reports one line (a System row flipped 36<->60), BoxLayout keeps its
            // children's sizes until invalidated, and in the IDE a council verdict body reported the size
            // it was last given: the row flipped 153<->281 and settled on the third pass (sandbox).
            val previous = measured[component]
            if (component.isValid && previous?.width == width && previous.preferred == component.preferredSize) return@forEachIndexed
            var given = geometry.height(index)
            var h = given
            val samples = linkedSetOf(given)
            var settled = false
            for (pass in 0 until MEASURE_PASSES) {
                component.setSize(width.coerceAtLeast(1), given)
                forgetLayout(component)
                layOut(component)
                h = component.preferredSize.height.coerceAtLeast(1)
                if (h == given) { settled = true; break }
                if (!samples.add(h)) break
                given = h
            }
            if (!settled) {
                // A cycle must not alternate the committed height on every subsequent layout.
                h = samples.max()
                if (geometry.height(index) != h) ScrollTrace.note("unsettled", geometry.height(index), h, "row=$index samples=$samples")
            }
            measured[component] = Measurement(width, Dimension(component.preferredSize))
            if (geometry.height(index) != h) {
                ScrollTrace.note("height", geometry.height(index), h, "row=$index ${component.javaClass.simpleName}")
                geometry.measure(index, h); updated = true
            }
        }
        return updated
    }

    /** Queries never measure, mutate geometry, or schedule work. */
    override fun getPreferredSize(): Dimension = if (isPreferredSizeSet) super.getPreferredSize()
        else Dimension(width.coerceAtLeast(1), if (geometry.size == 0) emptyHeight else geometry.total)
    override fun getPreferredScrollableViewportSize() = Dimension(480, 320)
    override fun getScrollableTracksViewportWidth() = true
    override fun getScrollableTracksViewportHeight() = false
    override fun getScrollableUnitIncrement(r: java.awt.Rectangle, orientation: Int, direction: Int) = 20
    override fun getScrollableBlockIncrement(r: java.awt.Rectangle, orientation: Int, direction: Int) =
        if (orientation == SwingConstants.VERTICAL) r.height.coerceAtLeast(20) else r.width.coerceAtLeast(20)
}
