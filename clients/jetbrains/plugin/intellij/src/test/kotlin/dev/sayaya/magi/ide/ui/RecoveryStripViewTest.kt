package dev.sayaya.magi.ide.ui

import com.intellij.openapi.util.Disposer
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.RecoveryState
import dev.sayaya.magi.ide.model.UncertainCall
import java.awt.BorderLayout
import java.awt.Container
import javax.swing.AbstractButton
import javax.swing.JComponent
import javax.swing.JLabel

/**
 * 재시작이나 중단이 남긴 복구 상태를 실제 도구창 위에서 띠로 세운다: 확인이 필요한 호출마다 도구와
 * 대상과 판정 버튼 넷이 서고, 판정·다시 시도는 따르는 대화로 데몬에 간다. 남은 것이 없으면 숨는다.
 */
class RecoveryStripViewTest : BasePlatformTestCase() {
    fun testRecoveryStripShowsWhatARestartLeftAndAsksTheDaemon() {
        val asked = mutableListOf<String>()
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { sid, _, _ -> asked += sid })
        try {
            UIUtil.dispatchAllInvocationEvents()
            fun field(name: String) = view.javaClass.getDeclaredField(name).apply { isAccessible = true }.get(view)
            view.javaClass.getDeclaredField("followedSid").apply { isAccessible = true }.set(view, "s1")
            val paint = view.javaClass.getDeclaredMethod("paintRecovery", RecoveryState::class.java).apply { isAccessible = true }
            fun show(s: RecoveryState?) { paint.invoke(view, s); UIUtil.dispatchAllInvocationEvents() }
            val strip = field("recoveryStrip") as JComponent
            val root = field("root") as JComponent
            fun all(c: Container): List<java.awt.Component> = c.components.flatMap { listOf(it) + if (it is Container) all(it) else emptyList() }
            fun buttons() = all(strip).filterIsInstance<AbstractButton>().filter { it.isVisible }
            fun words() = all(strip).filterIsInstance<JLabel>().filter { it.isVisible }.joinToString("\n") { it.text.orEmpty() }

            assertSame("the plan strip keeps the top of the conversation", field("planStrip"),
                (root.layout as BorderLayout).getLayoutComponent(BorderLayout.NORTH))
            val holder = strip.parent as JComponent
            assertSame("the band sits above the conversation", strip, (holder.layout as BorderLayout).getLayoutComponent(BorderLayout.NORTH))
            assertSame("the conversation stays under the band", field("scroll"), (holder.layout as BorderLayout).getLayoutComponent(BorderLayout.CENTER))
            assertFalse("nothing left, no band", strip.isVisible)

            show(RecoveryState(kind = "needs_check", uncertain = listOf(
                UncertainCall(callId = "c_w", tool = "write", target = "notes.md"),
                UncertainCall(callId = "c_bash", tool = "bash", target = "make deploy", verdict = "deferred"),
            )))
            assertTrue("calls to check raise the band", strip.isVisible)
            val said = words()
            assertTrue(said, said.contains("write") && said.contains("notes.md") && said.contains("make deploy"))
            val verdicts = buttons().filter { it.getClientProperty("verdict") != null }
            assertEquals("four verdicts for each call", 8, verdicts.size)
            assertEquals(listOf("ran", "not_ran", "rerun_ok", "deferred"), verdicts.take(4).map { it.getClientProperty("verdict") })
            assertEquals("c_w", verdicts[1].getClientProperty("call"))
            verdicts[1].doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertEquals("a verdict goes to the daemon for the conversation on screen", listOf("s1"), asked)

            show(RecoveryState(kind = "resumable"))
            assertTrue(strip.isVisible)
            assertTrue(words(), words().contains(MagiBundle.msg("recovery.resumable")))
            assertTrue("a cut turn asks for nothing but continue", buttons().none { it.getClientProperty("verdict") != null })

            show(RecoveryState(kind = "recovery_failed", reason = "disk full"))
            assertTrue(strip.isVisible)
            assertTrue("the daemon's reason is shown as said: ${words()}", words().contains("disk full"))
            val retry = buttons().single { it.getClientProperty("retry") == true }
            retry.doClick()
            UIUtil.dispatchAllInvocationEvents()
            assertEquals(listOf("s1", "s1"), asked)

            show(RecoveryState(kind = "none"))
            assertFalse("none hides the band", strip.isVisible)
            show(RecoveryState(kind = "needs_check", uncertain = listOf(UncertainCall(callId = "c_x", tool = "bash"))))
            show(null)
            assertFalse("an old daemon says nothing, so no band", strip.isVisible)
        } finally { Disposer.dispose(view) }
    }
}
