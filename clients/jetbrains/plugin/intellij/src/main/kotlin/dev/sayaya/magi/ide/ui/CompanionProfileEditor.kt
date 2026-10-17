package dev.sayaya.magi.ide.ui

import java.awt.BorderLayout
import java.awt.GridLayout
import java.util.UUID
import javax.swing.*
import javax.swing.event.DocumentEvent
import javax.swing.event.DocumentListener
import javax.swing.table.DefaultTableModel
import kotlinx.serialization.json.*

/** Metadata editor; policy authority never enters this form. */
internal object CompanionProfileEditor {
    fun create(raw: String): Pair<JTextArea, JComponent> {
        val root = Json.parseToJsonElement(raw).jsonObject
        val purpose = JTextField(root["purpose"]?.jsonPrimitive?.content.orEmpty(), 24)
        val responsibilities = JTextArea((root["responsibilities"] as? JsonArray)?.joinToString("\n") { it.jsonPrimitive.content }.orEmpty(), 3, 24)
        val tasks = ((root["tasks"] as? JsonArray)?.map { it.jsonObject } ?: emptyList()).toMutableList()
        val model = object : DefaultTableModel(arrayOf(MagiBundle.msg("profile.task"), MagiBundle.msg("profile.limits")), 0) {}
        tasks.forEach { task -> model.addRow(arrayOf(task["description"]?.jsonPrimitive?.content.orEmpty(), (task["constraints"] as? JsonArray)?.joinToString("; ") { it.jsonPrimitive.content }.orEmpty())) }
        val table = JTable(model).apply { putClientProperty("terminateEditOnFocusLost", true) }
        val value = JTextArea(raw)
        fun sync() {
            value.text = buildJsonObject {
                put("purpose", purpose.text)
                put("responsibilities", JsonArray(responsibilities.text.lines().map { it.trim() }.filter { it.isNotEmpty() }.map(::JsonPrimitive)))
                put("tasks", JsonArray((0 until model.rowCount).map { i -> buildJsonObject {
                    put("id", tasks[i]["id"] ?: JsonPrimitive("task-${UUID.randomUUID()}"))
                    put("description", model.getValueAt(i, 0)?.toString().orEmpty())
                    val original = tasks[i]["constraints"] as? JsonArray
                    val displayed = model.getValueAt(i, 1)?.toString().orEmpty()
                    put("constraints", if (original != null && original.joinToString("; ") { it.jsonPrimitive.content } == displayed) original else JsonArray(displayed.split(';').map { it.trim() }.filter { it.isNotEmpty() }.map(::JsonPrimitive)))
                } }))
            }.toString()
        }
        val listener = object : DocumentListener {
            override fun insertUpdate(e: DocumentEvent) = sync()
            override fun removeUpdate(e: DocumentEvent) = sync()
            override fun changedUpdate(e: DocumentEvent) = sync()
        }
        purpose.document.addDocumentListener(listener)
        responsibilities.document.addDocumentListener(listener)
        model.addTableModelListener { sync() }
        val buttons = JPanel().apply {
            add(JButton(MagiBundle.msg("profile.add")).apply { addActionListener {
                tasks.add(buildJsonObject { put("id", "task-${UUID.randomUUID()}") })
                model.addRow(arrayOf("", ""))
                table.editCellAt(model.rowCount - 1, 0)
            } })
            add(JButton(MagiBundle.msg("profile.remove")).apply { addActionListener {
                val row = table.selectedRow
                if (row >= 0) { if (table.isEditing) table.cellEditor.cancelCellEditing(); tasks.removeAt(row); model.removeRow(row) }
            } })
        }
        val top = JPanel(GridLayout(0, 1)).apply {
            add(JLabel(MagiBundle.msg("profile.purpose"))); add(purpose)
            add(JLabel(MagiBundle.msg("profile.responsibilities"))); add(JScrollPane(responsibilities))
        }
        val panel = JPanel(BorderLayout(0, 4)).apply {
            add(top, BorderLayout.NORTH)
            add(JScrollPane(table).apply { preferredSize = java.awt.Dimension(240, 150); minimumSize = java.awt.Dimension(0, 100) }, BorderLayout.CENTER)
            add(buttons, BorderLayout.SOUTH)
        }
        return value to panel
    }
}
