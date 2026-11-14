package dev.sayaya.magi.ide.ui

/**
 * Diagnostic: keeps the last two seconds of transcript scroll writes and row height changes on the EDT.
 * When the position reverses direction often in that window it dumps the window to idea.log once,
 * so a report of the view jumping up and down names who moved it and which row changed size.
 * A single large correction or forwarded page wheel is dumped too, a second later so the moves that
 * followed it (a smooth-scroll animation still heading for its old target) are in the record.
 */
internal object ScrollTrace {
    private val log = com.intellij.openapi.diagnostic.Logger.getInstance(ScrollTrace::class.java)
    private class Entry(val at: Long, val who: String, val from: Int, val to: Int, val note: String)
    private val ring = ArrayDeque<Entry>()
    private var lastDump = 0L
    private const val WINDOW_MS = 2000L
    private const val REVERSALS = 6

    /** Where a dump goes; idea.log unless a test listens. */
    internal var sink: ((String) -> Unit)? = null
    private fun write(text: String) = sink?.invoke(text) ?: log.info(text)

    private const val JUMP = 300
    /** Between dumps; 5s let a send just before hide the move a person reported. */
    private const val GAP_MS = 1500L
    private var pending: javax.swing.Timer? = null

    internal fun reset() { pending?.stop(); pending = null; ring.clear(); lastDump = 0L }

    fun note(who: String, from: Int, to: Int, note: String = "") {
        val now = System.currentTimeMillis()
        ring.addLast(Entry(now, who, from, to, note))
        while (ring.size > 200 || (ring.isNotEmpty() && now - ring.first().at > WINDOW_MS)) ring.removeFirst()
        // A large correction, a forwarded page wheel, a rekey move, or following switching on: dumped a
        // second later with the whole window, so moves under the jump size are in the record as well.
        // tail and model too: a diagram radio or node pick dropped the view to the bottom through a tail
        // pull, which was never a trigger, so that moment left no record.
        val jump = (who == "anchor" || who == "cef-wheel" || who == "rekey" || who == "tail" || who == "model") &&
            kotlin.math.abs(to - from) >= JUMP
        val followOn = who == "follow" && from == 0 && to == 1
        if ((jump || followOn) && pending == null && now - lastDump >= GAP_MS) {
            lastDump = now
            val title = if (followOn) "magi: transcript scroll following on $note" else "magi: transcript scroll jump $who $from->$to $note"
            pending = javax.swing.Timer(1000) { pending = null; dump(title) }.apply { isRepeats = false; start() }
            return
        }
        if (who != "model" || now - lastDump < GAP_MS) return
        val moves = ring.filter { it.who == "model" && it.to != it.from }.map { Integer.signum(it.to - it.from) }
        val reversals = moves.zipWithNext().count { (a, b) -> a != b }
        if (reversals < REVERSALS) return
        lastDump = now
        dump("magi: transcript scroll oscillation reversals=$reversals in ${now - ring.first().at}ms")
    }

    private fun dump(title: String) {
        val start = ring.firstOrNull()?.at ?: return
        write(title + "\n" + ring.joinToString("\n") { "  +${it.at - start}ms ${it.who} ${it.from}->${it.to} ${it.note}" })
    }
}
