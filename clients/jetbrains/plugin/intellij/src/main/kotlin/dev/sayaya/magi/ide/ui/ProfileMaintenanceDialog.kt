package dev.sayaya.magi.ide.ui

import com.intellij.openapi.application.ApplicationManager
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
import java.awt.FlowLayout
import javax.swing.JButton
import javax.swing.JComponent
import javax.swing.JTextArea

/** A read-only view of the bounded internal session; no transcript selection. */
internal class ProfileMaintenanceDialog(project: Project, private val workspace: Workspace) : DialogWrapper(project) {
    private val text = JTextArea().apply { isEditable = false; lineWrap = true; wrapStyleWord = true; border = JBUI.Borders.empty(8) }
    private val refresh = JButton(MagiBundle.msg("maintenance.refresh"))
    private val reload = JButton(MagiBundle.msg("knowledge.reload"))
    private var alive = true
    private val requests = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    init {
        title = MagiBundle.msg("maintenance.title")
        init()
        refresh.addActionListener { load(true) }
        reload.addActionListener { load() }
        load()
    }
    private fun load(request: Boolean = false) {
        refresh.isEnabled = false; reload.isEnabled = false
        requests.launch {
            val result = runCatching { workspace.askWithoutChat { it.profileMaintenance(request) } }
            ApplicationManager.getApplication().invokeLater {
                if (!alive) return@invokeLater
                refresh.isEnabled = true; reload.isEnabled = true
                val response = result.getOrNull()
                if (response?.ok != true) {
                    text.text = response?.error ?: result.exceptionOrNull()?.message ?: MagiBundle.msg("knowledge.failed")
                } else {
                    text.text = runCatching {
                        val state = Json.parseToJsonElement(response.out ?: "{}").jsonObject
                        fun value(key: String) = (state[key] as? JsonPrimitive)?.contentOrNull.orEmpty()
                        fun date(key: String) = value(key).takeUnless { it.isEmpty() || it.startsWith("0001-") } ?: MagiBundle.msg("knowledge.never")
                        buildString {
                            if (request) appendLine(MagiBundle.msg("maintenance.requested"))
                            appendLine(value("id"))
                            appendLine(MagiBundle.msg(when { value("enabled") != "true" -> "maintenance.disabled"; value("running") == "true" -> "maintenance.running"; value("pending") == "true" -> "maintenance.pending"; else -> "maintenance.idle" }))
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
                }
                text.caretPosition = 0
            }
        }
    }
    override fun createCenterPanel(): JComponent = JBPanel<JBPanel<*>>(BorderLayout()).apply {
        preferredSize = JBUI.size(640, 400)
        add(JBScrollPane(text), BorderLayout.CENTER)
        add(JBPanel<JBPanel<*>>(FlowLayout(FlowLayout.LEFT)).apply { add(refresh); add(reload) }, BorderLayout.SOUTH)
    }
    override fun createActions() = arrayOf(cancelAction)
    override fun dispose() { alive = false; requests.cancel(); super.dispose() }
}
