package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationAction
import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.components.Service
import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import dev.sayaya.magi.ide.model.PendingPrompt
import kotlinx.coroutines.*

/** Independent of the selected chat and of whether the status toolwindow was opened. */
@Service(Service.Level.PROJECT)
internal class DelegatedApprovals(private val project: Project) : Disposable {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val workspace = Workspace(project)
    private val notices = mutableMapOf<String, com.intellij.notification.Notification>()
    private fun key(p: PendingPrompt) = p.session + ":" + p.prompt.id

    fun start() {
        scope.launch {
            while (isActive) {
                val response = runCatching { workspace.askPolling { it.jobs() } }.getOrNull()
                if (response?.ok == true) {
                    val pending = response.jobs?.approvals.orEmpty()
                    ApplicationManager.getApplication().invokeLater {
                        if (!project.isDisposed) update(pending)
                    }
                }
                delay(3000)
            }
        }
    }

    private fun update(pending: List<PendingPrompt>) {
        val ids = pending.map(::key).toSet()
        notices.keys.toList().filterNot { it in ids }.forEach { notices.remove(it)?.expire() }
        for (p in pending) {
            if (key(p) in notices) continue
            val notice = NotificationGroupManager.getInstance().getNotificationGroup("magi")
                .createNotification(MagiBundle.msg("approval.pending", p.requester, p.prompt.what), NotificationType.WARNING)
            notice.addAction(NotificationAction.createSimple(MagiBundle.msg("approval.review")) { review(p) })
            notices[key(p)] = notice
            notice.notify(project)
        }
    }

    fun review(p: PendingPrompt) {
        val prompt = p.prompt
        val details = listOfNotNull(p.requester, p.session, prompt.what, prompt.reason, prompt.args?.toString(), prompt.diff, prompt.report?.toString()).joinToString("\n\n")
        var decision: String? = null
        var answer: String? = null
        if (prompt.kind == "permission") {
            val picked = Messages.showDialog(project, details, MagiBundle.msg("approval.title"),
                arrayOf(MagiBundle.msg("approval.allow"), MagiBundle.msg("approval.reject"), MagiBundle.msg("approval.later")), 2, Messages.getWarningIcon())
            decision = when (picked) { 0 -> "allow"; 1 -> "deny"; else -> return }
        } else if (prompt.kind == "question") {
            val options = prompt.options.orEmpty()
            answer = if (options.isNotEmpty()) {
                val picked = Messages.showDialog(project, details, MagiBundle.msg("approval.title"),
                    (options + MagiBundle.msg("approval.later")).toTypedArray(), options.size, Messages.getQuestionIcon())
                options.getOrNull(picked) ?: return
            } else Messages.showInputDialog(project, details, MagiBundle.msg("approval.title"), Messages.getQuestionIcon()) ?: return
        } else return
        scope.launch {
            val result = runCatching { workspace.askWithoutChat { companion ->
                val fresh = companion.jobs()
                check(fresh.ok) { fresh.error ?: MagiBundle.msg("approval.failed") }
                check(fresh.jobs?.approvals.orEmpty().any { key(it) == key(p) }) { MagiBundle.msg("approval.stale") }
                companion.respondDelegated(p, decision, answer)
            } }
            ApplicationManager.getApplication().invokeLater {
                if (project.isDisposed) return@invokeLater
                val response = result.getOrNull()
                if (response?.ok != true) Messages.showErrorDialog(project,
                    result.exceptionOrNull()?.message ?: response?.error ?: MagiBundle.msg("approval.failed"), MagiBundle.msg("approval.title"))
            }
        }
    }

    override fun dispose() { scope.cancel(); notices.values.forEach { it.expire() }; notices.clear() }
}
