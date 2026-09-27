package dev.sayaya.magi.ide.usecase

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

@Serializable
data class DraftReference(val path: String, val start: Long, val end: Long)
@Serializable
data class ArchivedDraft(
    val id: String, val companionKey: String, val sessionId: String, val creationTaskId: String,
    val callId: String, val kind: String, val status: String, val text: String, val title: String,
    val reason: String, val version: Long, val attemptId: String, val seq: Long,
    val createdAt: Long, val attempts: Long, val refs: List<DraftReference>,
)
@Serializable
data class DeletedDraft(val id: String, val version: Long)
@Serializable
data class DraftArchive(
    val schemaVersion: Int, val workspace: String, val owner: String, val revision: Long,
    val entries: List<ArchivedDraft>, val deleted: List<DeletedDraft>,
) {
    companion object {
        private fun counter(n: Long) = n in 0..9007199254740991L
        /** Invalid files are rejected as a whole; the host must preserve the original file. */
        fun parse(text: String, workspace: String): DraftArchive {
            val a = Json.decodeFromString<DraftArchive>(text)
            require(a.schemaVersion == 1 && a.workspace.isNotBlank() && a.workspace == workspace)
            require(a.owner.isNotBlank() && counter(a.revision))
            require(a.entries.map { it.id }.distinct().size == a.entries.size)
            for (e in a.entries) {
                require(e.id.isNotBlank() && e.companionKey.isNotBlank())
                require(e.sessionId.isNotBlank() || e.creationTaskId.isNotBlank())
                require(e.kind in setOf("general", "question", "attempt", "recovery"))
                require(e.status in setOf("draft", "pending", "unknown", "failed"))
                require(e.kind != "question" || e.callId.isNotBlank())
                require((e.kind == "attempt") == e.attemptId.isNotBlank())
                require(e.status !in setOf("pending", "unknown") || e.kind == "attempt")
                require(e.kind != "attempt" || e.status != "draft")
                require(e.text.isNotEmpty() || e.refs.isNotEmpty())
                require(listOf(e.version, e.seq, e.createdAt, e.attempts).all(::counter))
                require(e.refs.all { it.path.isNotBlank() && counter(it.start) && counter(it.end) && it.end >= it.start })
            }
            require(a.deleted.all { it.id.isNotBlank() && counter(it.version) })
            return a
        }
    }

    /** Return recovery candidates, never transport locks or automatic submissions. */
    fun restore(): List<ArchivedDraft> = entries
        .filter { e -> deleted.none { it.id == e.id && it.version >= e.version } }
        .map { e -> e.copy(status = if (e.status == "pending") "unknown" else e.status, refs = e.refs.toList()) }
}
