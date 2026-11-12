package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.usecase.Rows
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import java.awt.BorderLayout
import java.awt.event.MouseEvent
import javax.swing.JComponent
import javax.swing.JLabel

/**
 * 대화 맨 위 계획 줄을 실제 도구창 위에서 구동한다: 계획이 오면 줄이 서고 목록은 접혀 있으며,
 * 줄을 누르면 펼쳐지고 다시 누르면 접히고, 계획이 비면 줄이 숨는다.
 */
class PlanStripViewTest : BasePlatformTestCase() {
    fun testPlanStripShowsTogglesAndHides() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view)
            view.javaClass.getDeclaredField("everBegan").apply { isAccessible = true }.setBoolean(view, true)
            val rows = field("shaper") as Rows
            val paint = view.javaClass.getDeclaredMethod("paintPlan").apply { isAccessible = true }
            var seq = 0L
            fun plan(vararg steps: Pair<String, String>) {
                rows.feed(LogEvent(seq = ++seq, type = "todos.changed", data = buildJsonObject {
                    put("todos", buildJsonArray {
                        steps.forEach { (c, s) -> add(buildJsonObject { put("content", c); put("status", s) }) }
                    })
                }))
                paint.invoke(view)
                UIUtil.dispatchAllInvocationEvents()
            }
            val strip = field("planStrip") as JComponent
            val head = field("planHead") as JLabel
            val list = field("planList") as JComponent
            val root = field("root") as JComponent
            assertSame("the strip sits at the top of the conversation", strip,
                (root.layout as BorderLayout).getLayoutComponent(BorderLayout.NORTH))
            assertFalse("no plan, no strip", strip.isVisible)

            plan("a" to "completed", "b" to "completed", "build it" to "in_progress", "d" to "pending")
            assertTrue("a plan raises the strip", strip.isVisible)
            // 대화와 한눈에 구분된다: 불투명 primaryContainer 바탕, 왼쪽 primary 띠, 굵은 글씨.
            assertTrue("the strip paints its own surface", strip.isOpaque)
            assertEquals("the strip stands on the primary container", Look.primaryContainer, strip.background)
            assertTrue("a primary stripe on the left: ${strip.insets}", strip.insets.left >= 4)
            assertTrue("the line is bold", head.font.isBold)
            assertTrue("the line names the step it is on: ${head.text}", head.text.contains("build it"))
            assertTrue("the line says which of how many: ${head.text}", head.text.contains("3") && head.text.contains("4"))
            assertFalse("the steps start folded", list.isVisible)
            assertEquals("folded, the line is centred", javax.swing.SwingConstants.CENTER, head.horizontalAlignment)
            assertEquals(4, list.componentCount)

            fun click() = head.dispatchEvent(MouseEvent(head, MouseEvent.MOUSE_CLICKED, System.currentTimeMillis(), 0, 2, 2, 1, false))
            click(); UIUtil.dispatchAllInvocationEvents()
            assertTrue("one click shows the steps", list.isVisible)
            assertEquals("open, the line lines up with the steps", javax.swing.SwingConstants.LEADING, head.horizontalAlignment)
            click(); UIUtil.dispatchAllInvocationEvents()
            assertFalse("a second click hides them", list.isVisible)
            assertEquals("folded again, centred again", javax.swing.SwingConstants.CENTER, head.horizontalAlignment)

            plan()
            assertFalse("an empty plan hides the strip", strip.isVisible)
            println("PASS plan strip: top of the conversation, click toggles the steps, empty plan hides it")
        } finally { view.dispose() }
    }
}
