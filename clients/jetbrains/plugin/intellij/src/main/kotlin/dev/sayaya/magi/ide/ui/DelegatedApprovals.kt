package dev.sayaya.magi.ide.ui

import com.intellij.notification.NotificationAction
import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.components.Service
import com.intellij.openapi.Disposable
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.wm.ToolWindowManager
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.PendingPrompt
import dev.sayaya.magi.ide.usecase.Markup
import kotlinx.coroutines.*
import java.awt.BorderLayout
import javax.swing.*

/** Workspace-wide discovery; review remains next to the main composer. */
@Service(Service.Level.PROJECT)
internal class DelegatedApprovals(private val project: Project) : Disposable {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val workspace = Workspace(project)
    private val notices = mutableMapOf<String, com.intellij.notification.Notification>()
    private val views = mutableSetOf<Review>()
    private var pending = emptyList<PendingPrompt>()
    private var selected: String? = null
    private fun key(p: PendingPrompt) = p.session + ":" + p.prompt.id

    fun start() {
        scope.launch {
            while (isActive) {
                val response = runCatching { workspace.askPolling { it.jobs() } }.getOrNull()
                if (response?.ok == true) {
                    val next = response.jobs?.approvals.orEmpty()
                    ApplicationManager.getApplication().invokeLater { if (!project.isDisposed) update(next) }
                }
                delay(3000)
            }
        }
    }

    private fun update(next: List<PendingPrompt>) {
        pending = next
        val ids = next.map(::key).toSet()
        notices.keys.toList().filterNot { it in ids }.forEach { notices.remove(it)?.expire() }
        for (p in next) {
            if (key(p) in notices) continue
            val notice = NotificationGroupManager.getInstance().getNotificationGroup("magi")
                .createNotification(MagiBundle.msg("approval.pending", p.requester, p.prompt.what), NotificationType.WARNING)
            notice.addAction(NotificationAction.createSimple(MagiBundle.msg("approval.review")) { review(p) })
            notices[key(p)] = notice
            notice.notify(project)
        }
        views.forEach { it.draw() }
    }

    fun review(p: PendingPrompt) {
        selected = key(p)
        ToolWindowManager.getInstance(project).getToolWindow("magi")?.activate {
            views.forEach { it.draw(); it.requestFocusInWindow() }
        }
    }

    fun panel(parent: Disposable): JComponent = Review().also { view ->
        views.add(view)
        Disposer.register(parent, Disposable { views.remove(view) })
        view.draw()
    }

    private fun respond(p: PendingPrompt, decision: String?, answer: String?, done: (String?) -> Unit) {
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
                val error = if (response?.ok == true) null else result.exceptionOrNull()?.message ?: response?.error ?: MagiBundle.msg("approval.failed")
                if (error == null) update(pending.filterNot { key(it) == key(p) })
                done(error)
            }
        }
    }

    private inner class Review : JBPanel<Review>(BorderLayout()) {
        private val pick = JComboBox<String>()
        private val text = JEditorPane("text/html", "").apply { isEditable = false; isOpaque = false }
        private val actions = JPanel(java.awt.GridLayout(0, 1, 0, JBUI.scale(6))).apply { isOpaque = false }
        private val answer = JTextField()
        private val error = JBLabel().apply { foreground = Look.error }
        private var painting = false
        private var busy = false
        private var drawn: PendingPrompt? = null
        init {
            isOpaque = false
            border = JBUI.Borders.empty(8, 12)
            preferredSize = JBUI.size(280, 260)
            maximumSize = java.awt.Dimension(Int.MAX_VALUE, JBUI.scale(320))
            minimumSize = java.awt.Dimension(0, JBUI.scale(180))
            add(pick, BorderLayout.NORTH)
            add(JBScrollPane(text).apply { horizontalScrollBarPolicy = ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER }, BorderLayout.CENTER)
            add(JPanel(BorderLayout()).apply { isOpaque = false; border = JBUI.Borders.emptyTop(8); add(actions, BorderLayout.CENTER); add(error, BorderLayout.SOUTH) }, BorderLayout.SOUTH)
            pick.addActionListener { if (!painting) { pending.getOrNull(pick.selectedIndex)?.let { selected = key(it); drawn = null; draw() } } }
        }
        fun draw() {
            isVisible = pending.isNotEmpty()
            if (pending.isEmpty()) { drawn = null; return }
            val index = pending.indexOfFirst { key(it) == selected }.takeIf { it >= 0 } ?: 0
            val p = pending[index]
            val unchanged = drawn == p
            painting = true
            pick.removeAllItems()
            pending.forEach { pick.addItem(it.requester + " · " + it.prompt.what) }
            pick.selectedIndex = index
            painting = false
            if (unchanged) return // Update the queue selector, preserving the current form.
            val sameRequest = drawn?.let { key(it) == key(p) } == true
            drawn = p
            text.text = delegatedReviewHtml(p)
            text.caretPosition = 0
            actions.removeAll()
            error.text = ""
            fun button(label: String, decision: String? = null, value: (() -> String)? = null) {
                actions.add(JButton(label).apply {
                    isEnabled = !busy
                    addActionListener {
                        if (busy) return@addActionListener
                        busy = true; actions.components.forEach { it.isEnabled = false }; pick.isEnabled = false
                        respond(p, decision, value?.invoke()) { why ->
                            busy = false; pick.isEnabled = true
                            if (why != null) {
                                error.text = why
                                actions.components.forEach { it.isEnabled = true }
                            } else { drawn = null; draw() }
                        }
                    }
                })
            }
            when (p.prompt.kind) {
                "permission" -> { button(MagiBundle.msg("approval.allow"), "allow"); button(MagiBundle.msg("approval.reject"), "deny") }
                "question" -> {
                    p.prompt.options.orEmpty().forEach { option -> button(option, value = { option }) }
                    if (!sameRequest) answer.text = ""; answer.toolTipText = p.prompt.what
                    actions.add(answer)
                    button(MagiBundle.msg("approval.answer"), value = { answer.text })
                }
            }
            revalidate(); repaint()
        }
    }

    override fun dispose() { scope.cancel(); notices.values.forEach { it.expire() }; notices.clear(); views.clear() }
}

/** Preserve the originating companion's ordered decision-report sections verbatim. */
internal fun delegatedReviewHtml(p: PendingPrompt): String = buildString {
    fun escape(s: String) = Markup.text(s).replace("\n", "<br/>")
    append("<html><b>" + escape(p.requester) + "</b><br/>" + escape(p.session))
    append("<h3>" + escape(p.prompt.what) + "</h3>")
    p.prompt.reason?.let { append("<p>" + escape(it) + "</p>") }
    p.prompt.args?.let { append("<p><tt>" + escape(it.toString()) + "</tt></p>") }
    p.prompt.report.orEmpty().forEach { ground -> append("<p><b>" + escape(ground.key) + ":</b><br/>" + escape(ground.text) + "</p>") }
    p.prompt.diff?.let { append("<pre>" + escape(it) + "</pre>") }
    append("</html>")
}
