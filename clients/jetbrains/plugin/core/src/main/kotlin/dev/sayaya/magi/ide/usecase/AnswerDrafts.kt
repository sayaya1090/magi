package dev.sayaya.magi.ide.usecase

/** View-local question drafts. RPC callbacks carry Attempt, not a mutable current question. */
class AnswerDrafts {
    data class Key(val session: String, val callId: String)
    data class Attempt(val id: Long, val key: Key, val version: Long, val text: String)
    data class Recovery(
        val id: String,
        val session: String,
        val callId: String,
        val questionText: String?,
        val version: Long,
        val text: String,
        val reason: String,
    ) {
        val key: Key get() = Key(session, callId)
        override fun toString(): String {
            val q = questionText?.take(20)?.let { " · $it" }.orEmpty()
            val preview = text.lineSequence().firstOrNull().orEmpty().take(40)
            return "${session.takeLast(8)} · ${callId.takeLast(8)}$q · $preview"
        }
    }
    private data class Draft(
        var text: String = "",
        var version: Long = 0,
        var pending: Attempt? = null,
        var done: Boolean = false,
        var questionText: String? = null,
    )
    private val drafts = mutableMapOf<Key, Draft>()
    private val general = mutableMapOf<String, String>()
    private val savedRecoveries = linkedMapOf<Key, Recovery>()
    private val deletedGenerations = mutableSetOf<Pair<Key, Long>>()
    private var session: String? = null
    private var serial = 0L
    private var recoverySerial = 0L
    private var closed = false
    var question: Key? = null; private set
    var active: Key? = null; private set

    companion object {
        const val REASON_SESSION_CHANGED = "다른 세션으로 이동"
        const val REASON_QUESTION_LEFT = "현재 대기 질문에서 벗어남"
    }

    val recoveries: List<Recovery>
        get() {
            if (closed) return emptyList()
            return savedRecoveries.values.filter { rec ->
                val d = drafts[rec.key]
                (rec.key != question || d?.done == true) &&
                    d != null &&
                    d.text.isNotEmpty() &&
                    !deletedGenerations.contains(Pair(rec.key, rec.version))
            }.reversed()
        }

    fun edit(text: String) {
        if (closed) return
        val key = active
        if (key != null) {
            val d = drafts.getOrPut(key) { Draft() }
            if (d.text != text) {
                d.text = text
                d.version++
            }
        } else session?.let { general[it] = text }
    }

    /** null means the composer does not need changing. Expiry keeps drafts until view disposal. */
    fun bind(scope: String?, callId: String?, text: String, what: String? = null): String? {
        if (closed) return null
        val next = if (scope != null && callId != null) Key(scope, callId) else null
        val beforeSession = session
        val beforeQuestion = question
        edit(text)
        session = scope
        question = next
        if (next != null) {
            val d = drafts.getOrPut(next) { Draft() }
            if (what != null) d.questionText = what
        }
        if (beforeSession == null && scope != null) general.putIfAbsent(scope, text)
        val changed = beforeSession != null && beforeSession != scope

        if (beforeQuestion != null && beforeQuestion != next) {
            val d = drafts[beforeQuestion]
            if (d != null && d.text.isNotEmpty()) {
                val reason = if (changed) REASON_SESSION_CHANGED else REASON_QUESTION_LEFT
                exposeRecovery(beforeQuestion, d, reason)
            }
        }

        if (active != null && (active != next || changed)) {
            active = null
            return scope?.let { general[it] }.orEmpty()
        }
        if (changed) return scope?.let { general[it] }.orEmpty()
        return null
    }

    private fun exposeRecovery(key: Key, draft: Draft, reason: String) {
        if (closed || draft.text.isEmpty()) return
        if (deletedGenerations.contains(Pair(key, draft.version))) return
        val existing = savedRecoveries[key]
        if (existing != null && existing.version == draft.version && existing.text == draft.text) {
            return
        }
        val id = "ans-rec-${++recoverySerial}"
        savedRecoveries[key] = Recovery(
            id = id,
            session = key.session,
            callId = key.callId,
            questionText = draft.questionText,
            version = draft.version,
            text = draft.text,
            reason = reason,
        )
    }

    fun deleteRecovery(id: String): Boolean {
        if (closed) return false
        val key = savedRecoveries.entries.firstOrNull { it.value.id == id }?.key ?: return false
        val rec = savedRecoveries.remove(key) ?: return false
        deletedGenerations.add(Pair(key, rec.version))
        val draft = drafts[key]
        if (draft != null && draft.version == rec.version) {
            draft.text = ""
        }
        return true
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
            if (draft.version == attempt.version) {
                draft.text = ""
                savedRecoveries.remove(attempt.key)
            } else {
                exposeRecovery(attempt.key, draft, REASON_QUESTION_LEFT)
            }
        } else {
            if (draft.done || attempt.key != question) {
                exposeRecovery(attempt.key, draft, REASON_QUESTION_LEFT)
            }
        }
        // On failure the draft is already retained; edits made since submission win.
        return true
    }

    fun close() {
        closed = true
        drafts.clear()
        general.clear()
        savedRecoveries.clear()
        deletedGenerations.clear()
        active = null
        question = null
    }
}
