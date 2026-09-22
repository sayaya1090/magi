package dev.sayaya.magi.ide.usecase

/** View-local question drafts. RPC callbacks carry Attempt, not a mutable current question. */
class AnswerDrafts {
    data class Key(val session: String, val callId: String)
    data class Attempt(val id: Long, val key: Key, val version: Long, val text: String)
    private data class Draft(var text: String = "", var version: Long = 0, var pending: Attempt? = null, var done: Boolean = false)
    private val drafts = mutableMapOf<Key, Draft>()
    private val general = mutableMapOf<String, String>()
    private var session: String? = null
    private var serial = 0L
    private var closed = false
    var question: Key? = null; private set
    var active: Key? = null; private set
    fun edit(text: String) {
        if (closed) return
        val key = active
        if (key != null) drafts.getOrPut(key) { Draft() }.apply { this.text = text; version++ }
        else session?.let { general[it] = text }
    }
    /** null means the composer does not need changing. Expiry keeps drafts until view disposal. */
    fun bind(scope: String?, callId: String?, text: String): String? {
        if (closed) return null
        val next = if (scope != null && callId != null) Key(scope, callId) else null
        val before = session
        edit(text)
        session = scope
        question = next
        if (before == null && scope != null) general.putIfAbsent(scope, text)
        val changed = before != null && before != scope
        if (active != null && (active != next || changed)) {
            active = null
            return scope?.let { general[it] }.orEmpty()
        }
        if (changed) return scope?.let { general[it] }.orEmpty()
        return null
    }
    fun enter(text: String): String? {
        if (closed) return null
        val key = question ?: return null
        val draft = drafts.getOrPut(key) { Draft() }
        if (draft.done) return null
        edit(text)
        active = key
        return draft.text
    }
    fun cancel(text: String): String? {
        if (active == null || closed) return null
        edit(text); active = null
        return session?.let { general[it] }.orEmpty()
    }
    fun begin(text: String): Attempt? {
        if (closed || text.isBlank()) return null
        val key = question ?: return null
        val draft = drafts.getOrPut(key) { Draft() }
        if (draft.done || draft.pending != null) return null
        draft.text = text; draft.version++
        return Attempt(++serial, key, draft.version, text).also { draft.pending = it }
    }
    fun generalText(): String = session?.let { general[it] }.orEmpty()
    fun leaveAfterSubmit() { active = null }
    fun busy(key: Key? = question): Boolean = drafts[key]?.pending != null
    fun done(key: Key? = question): Boolean = drafts[key]?.done == true
    fun complete(attempt: Attempt, ok: Boolean): Boolean {
        if (closed) return false
        val draft = drafts[attempt.key] ?: return false
        if (draft.pending != attempt) return false
        draft.pending = null
        if (ok) {
            draft.done = true
            if (draft.version == attempt.version) draft.text = ""
        }
        // On failure the draft is already retained; edits made since submission win.
        return true
    }
    fun close() { closed = true; drafts.clear(); general.clear(); active = null; question = null }
}
