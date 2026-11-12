package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBLabel
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.ContextParts
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Rows
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import javax.swing.JComponent

/**
 * 툴 결과 접기 줄에 컨텍스트 구성이 붙으면, 실제 도구창 행이 그 줄 아래에 좌측바와 같은 띠([Look.GaugeBar])와
 * 조각별 범례(색 점 + 몫%)를 그린다. 구성이 없으면 띠를 안 세운다(모름을 0%로 그리지 않는다).
 */
class FoldGaugeViewTest : BasePlatformTestCase() {
    fun testAFoldedLineDrawsTheContextBarWithALegend() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            val rows = view.javaClass.getDeclaredField("shaper").apply { isAccessible = true }.get(view) as Rows
            val rowPanel = view.javaClass.getDeclaredMethod("rowPanel", Row::class.java).apply { isAccessible = true }
            rows.feed(LogEvent(seq = 1, type = "tool.progress", data = Json.parseToJsonElement(
                """{"callId":"compact","name":"compact","text":"freed the window by eliding 3 bulky tool result(s) — re-derivable"}""") as JsonObject))
            val bare = rows.list().last { it.folded }
            val none = rowPanel.invoke(view, bare) as JComponent
            assertTrue("no composition, no bar", UIUtil.findComponentsOfType(none, Look.GaugeBar::class.java).isEmpty())

            assertTrue(rows.pinFoldContext(Rows.Ctx(30000, 100000, 30.0, ContextParts(system = 4000, tools = 6000, talk = 8000, calls = 2000, results = 10000))))
            val line = rows.list().last { it.folded }
            val panel = rowPanel.invoke(view, line) as JComponent
            val bars = UIUtil.findComponentsOfType(panel, Look.GaugeBar::class.java)
            assertEquals("one bar under the folded line", 1, bars.size)
            assertEquals("a segment per part", 5, bars[0].segments.size)
            val legend = UIUtil.findComponentsOfType(panel, JBLabel::class.java).mapNotNull { it.text }.filter { it.endsWith("%") }
            assertEquals("a legend entry per part: $legend", 5, legend.size)
            assertTrue("results are the biggest share: $legend", legend.any { it.endsWith(" 33%") })
            println("PASS fold gauge: the folded line carries a 5-part bar and legend $legend")
        } finally { view.dispose() }
    }
}
