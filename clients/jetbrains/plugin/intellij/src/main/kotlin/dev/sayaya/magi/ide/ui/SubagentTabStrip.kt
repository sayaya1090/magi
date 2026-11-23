package dev.sayaya.magi.ide.ui

import com.intellij.openapi.Disposable
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Disposer
import com.intellij.openapi.util.Key
import com.intellij.openapi.wm.ToolWindow
import com.intellij.ui.content.Content
import com.intellij.ui.content.ContentFactory
import com.intellij.ui.content.ContentManagerEvent
import com.intellij.ui.content.ContentManagerListener
import dev.sayaya.magi.ide.model.ChildJob
import dev.sayaya.magi.ide.usecase.SubagentTabs
import javax.swing.SwingUtilities
import javax.swing.Timer

/**
 * 우측 자리의 탭 줄(사용자 결정, 2026-11-23). 첫 탭([main])의 이름은 지금 대화의 제목이고, 그 옆에
 * 도는 서브에이전트가 하나씩 탭으로 붙는다. 규칙은 [SubagentTabs] 가 정한다 — 끝난 탭은 보고 있지
 * 않으면 닫히고, 보고 있으면 남았다가 다른 탭으로 옮기면 닫힌다. 사람이 손으로 닫은 탭은 그
 * 서브에이전트가 도는 동안 다시 붙이지 않는다.
 */
internal class SubagentTabStrip(
    private val project: Project,
    private val toolWindow: ToolWindow,
    private val main: Content,
) : Disposable {
    private val key = Key.create<String>("magi.subagent.tab")
    private var tabs: List<SubagentTabs.Tab> = emptyList()
    private var lastJobs: List<ChildJob> = emptyList()
    private val dismissed = HashSet<String>()
    private var title: String? = null
    private var titleFor: String? = null
    private var polls = 0
    @Volatile private var asking = false
    private val timer = Timer(3_000) { poll() }

    private val listener = object : ContentManagerListener {
            override fun selectionChanged(event: ContentManagerEvent) = apply()
            override fun contentRemoved(event: ContentManagerEvent) {
                // 사람이 닫은 탭: 이 서브에이전트가 도는 동안 다시 세우지 않는다.
                val id = event.content.getUserData(key) ?: return
                if (tabs.any { it.id == id }) {
                    dismissed += id
                    tabs = tabs.filterNot { it.id == id }
                }
            }
        }

    fun start() {
        toolWindow.contentManager.addContentManagerListener(listener)
        timer.start()
        poll()
    }

    private fun poll() {
        if (asking || !toolWindow.isVisible) return
        asking = true
        val wantTitle = polls++ % 10 == 0
        Workspace(project).onDaemonWithoutChat({ asking = false }) { comp ->
            try {
                val jobs = comp.jobs().let { if (it.ok) it.jobs?.children.orEmpty() else emptyList() }
                val sid = comp.session
                val t = if (wantTitle || sid != titleFor) {
                    comp.sessions().sessions.orEmpty().firstOrNull { it.id == sid }?.title
                } else title
                SwingUtilities.invokeLater {
                    title = t
                    titleFor = sid
                    lastJobs = jobs
                    apply()
                }
            } finally {
                asking = false
            }
        }
    }

    /** EDT. 마지막으로 받은 목록과 지금 고른 탭으로 탭 줄을 맞춘다. */
    private fun apply() {
        if (Disposer.isDisposed(this)) return
        val cm = toolWindow.contentManager
        main.displayName = SubagentTabs.sessionLabel(title, MagiBundle.msg("chat.tab.chat"))
        dismissed.retainAll(lastJobs.filter { it.running }.map { it.id }.toSet())
        val selected = cm.selectedContent?.getUserData(key)
        val live = lastJobs.filterNot { it.id in dismissed }
        val next = SubagentTabs.next(tabs, live, selected)
        val keep = next.map { it.id }.toSet()
        tabs = next
        for (c in cm.contents.toList()) {
            val id = c.getUserData(key) ?: continue
            if (id !in keep) cm.removeContent(c, true)
        }
        for (t in next) {
            val label = (if (t.running) "⛐ " else "⛒ ") + t.label
            val have = cm.contents.firstOrNull { it.getUserData(key) == t.id }
            if (have != null) {
                have.displayName = label
                continue
            }
            val view = MagiToolWindow.View(project, pinned = t.id)
            val content = ContentFactory.getInstance().createContent(view.root, label, false)
            content.isCloseable = true
            content.putUserData(key, t.id)
            Disposer.register(content, view)
            cm.addContent(content)
            view.refresh()
        }
    }

    override fun dispose() {
        timer.stop()
        runCatching { toolWindow.contentManager.removeContentManagerListener(listener) }
    }
}
