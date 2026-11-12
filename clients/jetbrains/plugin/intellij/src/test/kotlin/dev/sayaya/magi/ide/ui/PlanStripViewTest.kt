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
            // 대화와 한눈에 구분된다: 불투명 primaryContainer 바탕, 대화 말풍선과 같은 둥근 1px 테두리(네 변), 굵은 글씨.
            assertEquals("the strip stands on the primary container", Look.primaryContainer, strip.background)
            val edge = strip.border as? Look.BubbleBorder
            assertNotNull("the strip is outlined like a message bubble: ${strip.border}", edge)
            assertEquals("the outline has the bubble colour", Look.bubbleEdge(Look.primary), edge!!.edge)
            assertEquals("the outline is as faint as a bubble's", 65, edge.edge.alpha)
            val i = strip.insets
            assertTrue("the outline runs on all four sides: $i", i.top > 0 && i.left > 0 && i.right > 0 && i.bottom > 0)
            // 실제로 그려 본다: 네 변 각 가운데에서 선이 칠한 픽셀 수 - 말풍선과 같은 1px 이고, 네 변 모두에 선다.
            val img = java.awt.image.BufferedImage(200, 40, java.awt.image.BufferedImage.TYPE_INT_ARGB)
            val g = img.createGraphics()
            try { strip.border.paintBorder(strip, g, 0, 0, 200, 40) } finally { g.dispose() }
            fun inked(x: Int, y: Int) = (img.getRGB(x, y) ushr 24) > 0
            val sides = mapOf(
                "left" to (0 until 8).count { inked(it, 20) },
                "right" to (192 until 200).count { inked(it, 20) },
                "top" to (0 until 8).count { inked(100, it) },
                "bottom" to (32 until 40).count { inked(100, it) },
            )
            println("PLAN-STRIP-OUTLINE px per side: $sides insets: $i")
            assertEquals("a 1px outline on every side: $sides", mapOf("left" to 1, "right" to 1, "top" to 1, "bottom" to 1), sides)
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
