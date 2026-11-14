package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.FileRef

/** EDT-owned submission snapshots. Editing back to the same text is still a new revision. */
class SendDrafts {
    data class Attempt(val id: Long, val session: String, val revision: Long, val text: String, val refs: List<FileRef>, val nodes: List<String> = emptyList())
    data class Failure(val attempt: Attempt, val reason: String) {
        override fun toString() = "${attempt.session.takeLast(8)} · ${attempt.text.lineSequence().firstOrNull().orEmpty().take(60)}"
    }
    data class Completion(val sameSession: Boolean, val clearInput: Boolean)
    private var revision = 0L
    private var serial = 0L
    private var closed = false
    private val pending = mutableMapOf<Long, Attempt>()
    private val failed = linkedMapOf<Long, Failure>()
    val failures: List<Failure> get() = failed.values.toList()
    fun busy(session: String? = null): Boolean = if (session == null) pending.isNotEmpty() else pending.values.any { it.session == session }
    fun inFlight(session: String? = null): Boolean = if (session == null) pending.isNotEmpty() else pending.values.any { it.session == session && it.revision == revision }
    fun canSend(session: String? = null): Boolean = !closed && (session == null || pending.values.none { it.session == session && it.revision == revision })
    fun edited() { revision++ }
    fun begin(session: String, text: String, refs: List<FileRef>, nodes: List<String> = emptyList()): Attempt? {
        if (closed || pending.values.any { it.session == session && it.revision == revision }) return null
        return Attempt(++serial, session, revision, text, refs.toList(), nodes.toList()).also { pending[it.id] = it }
    }
    fun complete(attempt: Attempt, error: String?, currentSession: String?): Completion? {
        if (closed || pending.remove(attempt.id) != attempt) return null
        if (error != null) failed[attempt.id] = Failure(attempt, error)
        val same = currentSession == attempt.session
        return Completion(same, error == null && same && revision == attempt.revision)
    }
    fun recovered(id: Long) { failed.remove(id) }
    fun close() { closed = true; pending.clear(); failed.clear() }

    companion object {
        /**
         * The message a send carries when diagram nodes are chipped in the composer: the typed text, then
         * the node names on their own line. With nothing typed the names alone become an explain request,
         * so a chip can be sent without writing a question. Only names go, not the diagram source.
         */
        fun withNodes(text: String, nodes: List<String>, about: (String) -> String, explain: (String) -> String): String {
            val typed = text.trim()
            if (nodes.isEmpty()) return typed
            val names = nodes.joinToString(", ")
            return if (typed.isEmpty()) explain(names) else typed + "\n\n" + about(names)
        }
    }
}
