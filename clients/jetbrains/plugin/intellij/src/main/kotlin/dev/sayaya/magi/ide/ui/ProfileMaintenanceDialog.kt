package dev.sayaya.magi.ide.ui

import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.DialogWrapper
import com.intellij.ui.components.JBPanel
import com.intellij.ui.components.JBScrollPane
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import kotlinx.coroutines.cancel
import com.intellij.util.ui.JBUI
import kotlinx.serialization.json.*
import java.awt.BorderLayout
import javax.swing.JButton
import javax.swing.JComponent
import javax.swing.JTextArea

/** A read-only view of the bounded internal session; no transcript selection. */
internal class ProfileMaintenanceDialog(project: Project, workspace: Workspace) : DialogWrapper(project) {
    private val content = ProfileMaintenancePanel(workspace)
    init { title = MagiBundle.msg("maintenance.title"); init() }
    override fun createCenterPanel(): JComponent = content
    override fun createActions() = arrayOf(cancelAction)
    override fun dispose() { content.dispose(); super.dispose() }
}

/** Inline status with bounded scrolling; opening a dialog is optional. */
internal class ProfileMaintenancePanel(private val workspace: Workspace) : JBPanel<ProfileMaintenancePanel>(BorderLayout()), com.intellij.openapi.Disposable {
    private val text = JTextArea().apply { isEditable = false; lineWrap = true; wrapStyleWord = true; border = JBUI.Borders.empty(8) }
    private val refresh = JButton(MagiBundle.msg("maintenance.refresh")).apply { toolTipText = MagiBundle.msg("maintenance.refresh.hint") }
    private val reload = JButton(MagiBundle.msg("knowledge.reload"))
    private var alive = true
    private val requests = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val timer = javax.swing.Timer(5000) { if (isShowing && reload.isEnabled) load() }
    init {
        isOpaque = false
        border = JBUI.Borders.empty(8, 12, 12, 12)
        minimumSize = java.awt.Dimension(0, JBUI.scale(180))
        preferredSize = JBUI.size(280, 240)
        maximumSize = java.awt.Dimension(Int.MAX_VALUE, JBUI.scale(280))
        text.isOpaque = false
        val scroll = JBScrollPane(text).apply {
            horizontalScrollBarPolicy = javax.swing.ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER
        }
        add(Look.sectionHeading(MagiBundle.msg("maintenance.title"), inset = false), BorderLayout.NORTH)
        add(scroll, BorderLayout.CENTER)
        add(JBPanel<JBPanel<*>>(java.awt.GridLayout(2, 1, 0, JBUI.scale(8))).apply {
            isOpaque = false
            border = JBUI.Borders.emptyTop(8)
            add(refresh); add(reload)
        }, BorderLayout.SOUTH)
        refresh.addActionListener { load(true) }
        reload.addActionListener { load() }
        load()
        timer.start()
    }
    private fun load(request: Boolean = false) {
        refresh.isEnabled = false; reload.isEnabled = false
        requests.launch {
            val result = runCatching { workspace.askWithoutChat { it.profileMaintenance(request) } }
            managementUiLater {
                if (!alive) return@managementUiLater
                refresh.isEnabled = true; reload.isEnabled = true
                val response = result.getOrNull()
                if (response?.ok != true) {
                    text.text = response?.error ?: result.exceptionOrNull()?.message ?: MagiBundle.msg("knowledge.failed")
                } else {
                    val content = runCatching {
                        val state = Json.parseToJsonElement(response.out ?: "{}").jsonObject
                        fun value(key: String) = (state[key] as? JsonPrimitive)?.contentOrNull.orEmpty()
                        fun date(key: String) = value(key).takeUnless { it.isEmpty() || it.startsWith("0001-") } ?: MagiBundle.msg("knowledge.never")
                        buildString {
                            if (request) appendLine(MagiBundle.msg("maintenance.requested"))
                            appendLine(MagiBundle.msg(when { value("enabled") != "true" -> "maintenance.disabled"; value("running") == "true" -> "maintenance.running"; value("pending") == "true" -> "maintenance.pending"; else -> "maintenance.idle" }))
                            (state["pending_sources"] as? JsonObject)?.forEach { (source, through) ->
                                appendLine(MagiBundle.msg("maintenance.source", source, through.jsonPrimitive.content))
                            }
                            appendLine(MagiBundle.msg("maintenance.attempt", date("last_attempt")))
                            appendLine(MagiBundle.msg("maintenance.success", date("last_success")))
                            if (value("last_error").isNotEmpty()) appendLine(value("last_error"))
                            (state["history"] as? JsonArray)?.reversed()?.forEach { element ->
                                val e = element.jsonObject
                                appendLine()
                                val status = (e["status"] as? JsonPrimitive)?.content.orEmpty()
                                appendLine("${e["at"]?.jsonPrimitive?.content.orEmpty()} · ${MagiBundle.msg(when(status) { "published" -> "maintenance.published"; "failed" -> "maintenance.failed"; else -> "maintenance.unchanged" })}")
                                for (key in listOf("added", "updated")) (e[key] as? JsonArray)?.forEach { appendLine("- ${it.jsonPrimitive.content}") }
                                (e["error"] as? JsonPrimitive)?.contentOrNull?.let { appendLine(it) }
                            }
                        }
                    }.getOrElse { MagiBundle.msg("knowledge.failed") }
                    if (text.text != content) {
                        val caret = text.caretPosition
                        text.text = content
                        text.caretPosition = minOf(caret, text.document.length)
                    }
                }
            }
        }
    }
    override fun dispose() { alive = false; timer.stop(); requests.cancel() }
}
