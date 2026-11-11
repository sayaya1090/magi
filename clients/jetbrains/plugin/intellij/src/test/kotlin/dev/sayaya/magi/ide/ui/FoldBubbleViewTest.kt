package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Rows
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import java.awt.BorderLayout
import javax.swing.JComponent
import javax.swing.JLabel
import javax.swing.JProgressBar
import javax.swing.SwingConstants
import javax.swing.SwingUtilities

/**
 * 압축 진행을 실제 도구창 위에서 구동한다: 압축이 시작되면 대화에 말풍선 행이 서고, 그 안에
 * 가운데 정렬한 진행 문구와 막대가 여백을 두고 그려진다. 하단에는 막대가 없다. 끝나면 그 행이
 * 「↯ 컨텍스트를 접었다」로 바뀐다.
 */
class FoldBubbleViewTest : BasePlatformTestCase() {
    fun testCompactionShowsABubbleWithCenteredProgressThenTheResult() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            UIUtil.dispatchAllInvocationEvents()
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view)
            view.javaClass.getDeclaredField("everBegan").apply { isAccessible = true }.setBoolean(view, true)
            val rows = field("shaper") as Rows
            val paint = view.javaClass.getDeclaredMethod("paintFold").apply { isAccessible = true }
            val rowPanel = view.javaClass.getDeclaredMethod("rowPanel", Row::class.java).apply { isAccessible = true }
            var seq = 0L
            fun feed(type: String, json: String) {
                rows.feed(LogEvent(seq = ++seq, type = type, data = Json.parseToJsonElement(json) as JsonObject))
                paint.invoke(view)
                UIUtil.dispatchAllInvocationEvents()
            }
            feed("tool.progress", """{"callId":"compact","name":"compact","text":"compacting","progress":{"done":1,"total":3,"chars":840,"elapsedMs":12500}}""")
            val folding = rows.list().last()
            assertEquals("a fold raises a row in the conversation", "compact", folding.callId)
            val panel = rowPanel.invoke(view, folding) as JComponent
            val bubble = field("foldBubble") as JComponent
            val label = field("foldLabel") as JLabel
            val bar = field("foldBar") as JProgressBar
            assertTrue("the row draws the progress bubble", SwingUtilities.isDescendingFrom(bubble, panel))
            assertTrue("the bar sits inside the bubble", SwingUtilities.isDescendingFrom(bar, bubble))
            assertTrue("the words sit inside the bubble", SwingUtilities.isDescendingFrom(label, bubble))
            assertEquals("the words are centred", SwingConstants.CENTER, label.horizontalAlignment)
            assertTrue("the words say which part of how many: ${label.text}", label.text.contains("2/3") && label.text.contains("840"))
            assertFalse(bar.isIndeterminate)
            assertEquals(333, bar.value)
            val inset = bubble.insets
            assertTrue("the bubble has a margin: $inset", inset.left >= 8 && inset.right >= 8 && inset.top >= 4 && inset.bottom >= 4)
            val root = field("root") as JComponent
            val south = (root.layout as BorderLayout).getLayoutComponent(BorderLayout.SOUTH) as JComponent
            assertFalse("no bar at the bottom any more", SwingUtilities.isDescendingFrom(bar, south))

            feed("compaction", """{"summary":"brief","tokensBefore":50868,"tokensAfter":19325}""")
            val last = rows.list().last()
            assertTrue("the bubble becomes the result: ${last.text}", last.text.startsWith("↯") && last.callId != "compact")
            println("PASS fold bubble: a row with centred progress words and a bar with margins, then the folded line in its place")
        } finally { view.dispose() }
    }
}
