package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.FileRef

/** EDT-owned submission snapshots. Editing back to the same text is still a new revision. */
class SendDrafts {
    data class Attempt(val id: Long, val session: String, val revision: Long, val text: String, val refs: List<FileRef>)
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
    fun edited() { revision++ }
    fun begin(session: String, text: String, refs: List<FileRef>): Attempt? {
        if (closed || pending.values.any { it.session == session && it.revision == revision }) return null
        return Attempt(++serial, session, revision, text, refs.toList()).also { pending[it.id] = it }
    }
    fun complete(attempt: Attempt, error: String?, currentSession: String?): Completion? {
        if (closed || pending.remove(attempt.id) != attempt) return null
        if (error != null) failed[attempt.id] = Failure(attempt, error)
        val same = currentSession == attempt.session
        return Completion(same, error == null && same && revision == attempt.revision)
    }
    fun recovered(id: Long) { failed.remove(id) }
    fun close() { closed = true; pending.clear(); failed.clear() }
}
