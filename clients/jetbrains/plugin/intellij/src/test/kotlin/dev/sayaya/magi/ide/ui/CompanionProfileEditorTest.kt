package dev.sayaya.magi.ide.ui

import java.awt.Container
import javax.swing.*
import kotlinx.serialization.json.*
import com.intellij.testFramework.fixtures.BasePlatformTestCase

class CompanionProfileEditorTest : BasePlatformTestCase() {
    private fun descendants(c: java.awt.Component): List<java.awt.Component> = listOf(c) + if (c is Container) c.components.flatMap(::descendants) else emptyList()
    fun testEditsAndDeletesTasksWithoutChangingStableIds() {
        run {
            val (value, panel) = CompanionProfileEditor.create("""{"purpose":"original","responsibilities":["sources"],"tasks":[{"id":"build","description":"build project","constraints":["local only"]}]}""")
            val components = descendants(panel)
            val purpose = components.filterIsInstance<JTextField>().first()
            purpose.text = "changed"
            val table = components.filterIsInstance<JTable>().single()
            table.model.setValueAt("run tests", 0, 0)
            var result = Json.parseToJsonElement(value.text).jsonObject
            assertEquals("changed", result["purpose"]!!.jsonPrimitive.content)
            assertEquals("build", result["tasks"]!!.jsonArray[0].jsonObject["id"]!!.jsonPrimitive.content)
            assertEquals("local only", result["tasks"]!!.jsonArray[0].jsonObject["constraints"]!!.jsonArray[0].jsonPrimitive.content)
            val buttons = components.filterIsInstance<JButton>().filter { it.text != null && it.text.isNotEmpty() }
            buttons.first { it.text == MagiBundle.msg("profile.add") }.doClick()
            assertEquals(2, table.rowCount)
            table.setRowSelectionInterval(1, 1)
            buttons.first { it.text == MagiBundle.msg("profile.remove") }.doClick()
            result = Json.parseToJsonElement(value.text).jsonObject
            assertEquals(1, result["tasks"]!!.jsonArray.size)
        }
    }
}
