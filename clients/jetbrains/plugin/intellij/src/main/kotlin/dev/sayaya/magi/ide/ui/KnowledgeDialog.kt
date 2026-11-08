package dev.sayaya.magi.ide.ui

import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.DialogWrapper
import com.intellij.openapi.ui.Messages
import com.intellij.ui.components.JBList
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.cancel
import com.intellij.util.ui.JBUI
import dev.sayaya.magi.ide.model.KnowledgeItem
import java.awt.BorderLayout
import java.awt.FlowLayout
import javax.swing.DefaultListModel
import javax.swing.JButton
import javax.swing.JComponent
import javax.swing.ListSelectionModel

/** Native project-only controls. Requests use the displayed revision and reload on conflict. */
internal class KnowledgeDialog(project: Project, private val workspace: Workspace) : DialogWrapper(project) {
    private val entries = DefaultListModel<String>()
    private val list = JBList(entries).apply { selectionMode = ListSelectionModel.SINGLE_SELECTION }
    private val details = JBLabel(" ")
    private val slotStatus = JBLabel(" ")
    private val notice = JBLabel(" ")
    private val pin = JButton()
    private val archive = JButton()
    private val register = JButton(MagiBundle.msg("knowledge.import"))
    private val reload = JButton(MagiBundle.msg("knowledge.reload"))
    private val slotButtons = JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT))
    private var items = emptyList<KnowledgeItem>()
    private var busy = false
    private var alive = true
    private var watch: java.io.Closeable? = null
    private val requests = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    init {
        title = MagiBundle.msg("knowledge.title")
        init()
        list.addListSelectionListener { updateSelection() }
        pin.addActionListener { change(if (selected()?.pinned == true) "unpin" else "pin") }
        archive.addActionListener {
            val item = selected() ?: return@addActionListener
            if (!item.archived && Messages.showYesNoDialog(contentPanel,
                    MagiBundle.msg("knowledge.archive.confirm"), title, Messages.getQuestionIcon()) != Messages.YES) return@addActionListener
            change(if (item.archived) "restore" else "archive")
        }
        register.addActionListener {
            if (Messages.showYesNoDialog(contentPanel, MagiBundle.msg("knowledge.import.confirm"), title, Messages.getQuestionIcon()) == Messages.YES) change("import")
        }
        reload.addActionListener { request() }
        request()
        // 다이얼로그가 열린 동안 슬롯 변경 알림이 오면 슬롯 요약을 다시 읽는다.
        watch = workspace.slotWatch {
            requests.launch {
                if (!busy && alive) {
                    val res = runCatching { workspace.askWithoutChat { it.knowledge() } }.getOrNull()
                    if (res?.ok == true && alive) {
                        managementUiLater {
                            if (!alive || busy) return@managementUiLater
                            val slots = res.slots.orEmpty()
                            if (slots.isNotEmpty()) {
                                val statusSummaries = slots.mapNotNull { s ->
                                    val itemsText = s.items?.joinToString(" · ") { "${it.label}: ${it.value}" } ?: ""
                                    val badgeText = s.status?.let { " [${it.label}${if (it.badge > 0) " (${it.badge})" else ""}]" } ?: ""
                                    "${s.title}: $itemsText$badgeText"
                                }
                                slotStatus.text = statusSummaries.joinToString("  |  ")
                            } else {
                                slotStatus.text = " "
                            }
                        }
                    }
                }
            }
        }
    }
    private fun selected(): KnowledgeItem? = items.getOrNull(list.selectedIndex)
    private fun updateSelection() {
        val item = selected()
        pin.text = MagiBundle.msg(if (item?.pinned == true) "knowledge.unpin" else "knowledge.pin")
        archive.text = MagiBundle.msg(if (item?.archived == true) "knowledge.restore" else "knowledge.archive")
        pin.isEnabled = !busy && item?.managed == true
        archive.isEnabled = !busy && item?.managed == true
        register.isEnabled = !busy && item?.importable == true
        reload.isEnabled = !busy
        slotButtons.components.forEach { it.isEnabled = !busy }
        details.text = when {
            item == null -> " "
            !item.managed -> MagiBundle.msg("knowledge.legacy")
            else -> MagiBundle.msg("knowledge.history", item.applicationCount,
                item.lastApplied.takeUnless { it.isEmpty() || it.startsWith("0001-") } ?: MagiBundle.msg("knowledge.never"))
        }
        details.toolTipText = details.text
    }
    private fun change(action: String) { selected()?.let { request(it, action) } }
    private fun request(item: KnowledgeItem? = null, action: String = "", plugin: String = "", slot: String = "") {
        if (busy || !alive) return
        busy = true
        notice.text = MagiBundle.msg("knowledge.loading")
        updateSelection()
        requests.launch {
            val result = runCatching { workspace.askWithoutChat { it.knowledge(item, action, plugin, slot) } }
            // Always reload after mutation, including stale revision or lost acknowledgement.
            val inventory = if (item != null) runCatching { workspace.askWithoutChat { it.knowledge() } } else result
            managementUiLater {
                if (!alive) return@managementUiLater
                busy = false
                val response = inventory.getOrNull()
                val changed = result.getOrNull()
                notice.text = when {
                    changed?.ok != true -> changed?.error ?: result.exceptionOrNull()?.message ?: MagiBundle.msg("knowledge.failed")
                    response?.ok != true -> response?.error ?: MagiBundle.msg("knowledge.failed")
                    response.knowledge.isNullOrEmpty() -> MagiBundle.msg("knowledge.empty")
                    else -> " "
                }
                val selectedName = item?.name ?: selected()?.name
                val slots = response?.slots.orEmpty()
                if (slots.isNotEmpty()) {
                    val statusSummaries = slots.mapNotNull { s ->
                        val itemsText = s.items?.joinToString(" · ") { "${it.label}: ${it.value}" } ?: ""
                        val badgeText = s.status?.let { " [${it.label}${if (it.badge > 0) " (${it.badge})" else ""}]" } ?: ""
                        "${s.title}: $itemsText$badgeText"
                    }
                    slotStatus.text = statusSummaries.joinToString("  |  ")
                } else {
                    slotStatus.text = " "
                }

                // Dynamically update slot action buttons
                slotButtons.removeAll()
                slots.forEach { slot ->
                    slot.actions?.forEach { act ->
                        val btn = JButton(act.label).apply {
                            toolTipText = act.description
                            isEnabled = !busy
                            addActionListener { request(action = act.id, plugin = slot.plugin, slot = slot.id) }
                        }
                        slotButtons.add(btn)
                    }
                }
                slotButtons.revalidate()
                slotButtons.repaint()

                items = if (response?.ok == true) response.knowledge.orEmpty() else emptyList()
                entries.clear()
                items.forEach { row ->
                    val states = listOfNotNull(
                        MagiBundle.msg(if (row.kind == "memory") "knowledge.memory" else "knowledge.skill"),
                        MagiBundle.msg(when { row.archived -> "knowledge.archived"; row.archiveCandidate -> "knowledge.candidate"; row.cold -> "knowledge.cold"; else -> "knowledge.active" }),
                        if (row.pinned) MagiBundle.msg("knowledge.pinned") else null,
                        if (row.inUse) MagiBundle.msg("knowledge.protected") else null)
                    entries.addElement("${row.description.ifEmpty { row.name }} · ${states.joinToString(" · ")}")
                }
                list.selectedIndex = items.indexOfFirst { it.name == selectedName }.takeIf { it >= 0 } ?: if (items.isEmpty()) -1 else 0
                updateSelection()
            }
        }
    }
    override fun createCenterPanel(): JComponent = JBPanel<JBPanel<*>>(BorderLayout(JBUI.scale(8), JBUI.scale(8))).apply {
        preferredSize = JBUI.size(720, 420)
        add(JBScrollPane(list), BorderLayout.CENTER)
        add(JBPanel<JBPanel<*>>(BorderLayout()).apply {
            add(JBPanel<JBPanel<*>>(BorderLayout()).apply {
                add(details, BorderLayout.NORTH)
                add(slotStatus, BorderLayout.SOUTH)
            }, BorderLayout.NORTH)
            add(JBPanel<JBPanel<*>>(BorderLayout()).apply {
                add(JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT)).apply {
                    add(pin); add(archive); add(register); add(reload)
                }, BorderLayout.NORTH)
                add(slotButtons, BorderLayout.CENTER)
            }, BorderLayout.CENTER)
            add(notice, BorderLayout.SOUTH)
        }, BorderLayout.SOUTH)
    }
    override fun createActions() = arrayOf(cancelAction)
    override fun dispose() { alive = false; watch?.close(); requests.cancel(); super.dispose() }
}
