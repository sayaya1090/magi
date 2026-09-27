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
        val archived: ArchivedDraft? = null,
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
    private val savedRecoveries = linkedMapOf<String, Recovery>()
    private val deletedGenerations = mutableSetOf<Pair<Key, Long>>()
    private var session: String? = null
    private var serial = 0L
    private var recoverySerial = 0L
    private val archiveDeletions = mutableMapOf<String, Long>()
    fun exportedDeletions() = archiveDeletions.map { DeletedDraft(it.key, it.value) }
    private var closed = false
    var question: Key? = null; private set
    var active: Key? = null; private set

    companion object {
        const val REASON_SESSION_CHANGED = "session_changed"
        const val REASON_QUESTION_LEFT = "question_left"
        const val REASON_SUBMISSION_FAILED = "submission_failed"
    }

    val recoveries: List<Recovery>
        get() {
            if (closed) return emptyList()
            return savedRecoveries.values.filter { rec ->
                val d = drafts[rec.key]
                rec.text.isNotEmpty() &&
                    !deletedGenerations.contains(Pair(rec.key, rec.version)) &&
                    (rec.reason == REASON_SUBMISSION_FAILED ||
                        rec.key != question ||
                        d?.done == true ||
                        (d != null && rec.version != d.version))
            }.reversed()
        }

    fun edit(text: String) {
        if (closed) return
        val key = active
        if (key != null) {
            val d = drafts.getOrPut(key) { Draft() }
            if (!d.done && d.text != text) {
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
                exposeRecovery(beforeQuestion, d.version, d.text, d.questionText, reason)
            }
        }

        if (active != null && (active != next || changed)) {
            active = null
            return scope?.let { general[it] }.orEmpty()
        }
        if (changed) return scope?.let { general[it] }.orEmpty()
        return null
    }

    private fun exposeRecovery(
        key: Key,
        version: Long,
        text: String,
        questionText: String?,
        reason: String
    ) {
        if (closed || text.isEmpty()) return
        if (deletedGenerations.contains(Pair(key, version))) return
        val existing = savedRecoveries.values.firstOrNull { it.key == key && it.version == version }
        if (existing != null) {
            if (reason == REASON_SUBMISSION_FAILED && existing.reason != REASON_SUBMISSION_FAILED) {
                savedRecoveries[existing.id] = existing.copy(reason = reason)
            }
            return
        }
        val duplicate = savedRecoveries.values.firstOrNull { it.key == key && it.text == text && it.reason == reason }
        if (duplicate != null) return
        val id = "ans-rec-${++recoverySerial}"
        savedRecoveries[id] = Recovery(
            id = id,
            session = key.session,
            callId = key.callId,
            questionText = questionText,
            version = version,
            text = text,
            reason = reason,
        )
    }

    fun deleteRecovery(id: String): Boolean {
        if (closed) return false
        val rec = savedRecoveries.remove(id) ?: return false
        rec.archived?.let {
            archiveDeletions[it.id] = maxOf(archiveDeletions[it.id] ?: -1, it.version)
            return true
        }
        deletedGenerations.add(Pair(rec.key, rec.version))
        val draft = drafts[rec.key]
        if (draft != null && draft.version == rec.version) {
            if (active != rec.key) {
                draft.text = ""
            }
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
        val d = drafts[active]
        if (d != null && !d.done) {
            edit(text)
        }
        active = null
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
    fun version(key: Key? = question): Long = drafts[key]?.version ?: 0L

    fun complete(attempt: Attempt, ok: Boolean): Boolean {
        if (closed) return false
        val draft = drafts[attempt.key] ?: return false
        if (draft.pending != attempt) return false
        draft.pending = null
        if (ok) {
            draft.done = true
            savedRecoveries.values.removeIf { it.key == attempt.key && it.version == attempt.version }
            if (draft.version == attempt.version) {
                draft.text = ""
            } else if (draft.text.isNotEmpty()) {
                exposeRecovery(attempt.key, draft.version, draft.text, draft.questionText, REASON_QUESTION_LEFT)
            }
        } else {
            exposeRecovery(attempt.key, attempt.version, attempt.text, draft.questionText, REASON_SUBMISSION_FAILED)
            if (attempt.key != question && draft.text.isNotEmpty() && draft.version != attempt.version) {
                exposeRecovery(attempt.key, draft.version, draft.text, draft.questionText, REASON_QUESTION_LEFT)
            }
        }
        return true
    }

    /** All sessions, including inactive drafts and submitted text distinct from later edits. */
    fun exportEntries(owner: String, companion: String): List<ArchivedDraft> {
        val out = mutableListOf<ArchivedDraft>()
        fun add(kind: String, scope: String, call: String, text: String, version: Long = 0,
                status: String = "draft", attempt: String = "", title: String = "", reason: String = "", suffix: String = "") {
            if (text.isEmpty()) return
            val id = kotlinx.serialization.json.JsonArray(listOf(owner, kind, scope, call, suffix)
                .map { kotlinx.serialization.json.JsonPrimitive(it) }).toString()
            out += ArchivedDraft(id, companion, scope, "", call, kind, status, text, title,
                reason, version, attempt, 0, 0, 0, emptyList())
        }
        general.forEach { (scope, text) -> add("general", scope, "", text) }
        drafts.forEach { (key, draft) ->
            if (!deletedGenerations.contains(key to draft.version))
                add("question", key.session, key.callId, draft.text, draft.version, title = draft.questionText.orEmpty())
            draft.pending?.let { a ->
                if (!deletedGenerations.contains(key to a.version))
                    add("attempt", key.session, key.callId, a.text, a.version, "pending", "$owner:${a.id}",
                        draft.questionText.orEmpty(), suffix = a.id.toString())
            }
        }
        savedRecoveries.values.forEach { r ->
            if (!deletedGenerations.contains(r.key to r.version)) {
                val archived = r.archived
                if (archived != null) out += archived
                else add("recovery", r.session, r.callId, r.text, r.version, "failed",
                    title = r.questionText.orEmpty(), reason = r.reason, suffix = r.id)
            }
        }
        return out
    }

    /** Import as recovery records only; never bind the active question or restore a busy lock. */
    fun importArchive(archive: DraftArchive, workspace: String) {
        val valid = DraftArchive.parse(kotlinx.serialization.json.Json.encodeToString(DraftArchive.serializer(), archive), workspace)
        valid.deleted.forEach { archiveDeletions[it.id] = maxOf(archiveDeletions[it.id] ?: -1, it.version) }
        savedRecoveries.values.removeIf { r -> r.archived?.let { (archiveDeletions[it.id] ?: -1) >= it.version } ?: false }
        for (e in valid.restore()) {
            if ((archiveDeletions[e.id] ?: -1) >= e.version) continue
            if (savedRecoveries.containsKey(e.id)) continue
            savedRecoveries[e.id] = Recovery(e.id, e.sessionId, e.callId, e.title, e.version, e.text,
                if (e.status == "unknown") "result_unknown" else e.reason.ifEmpty { "restored_draft" }, e)
        }
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
