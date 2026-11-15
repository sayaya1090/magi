package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import dev.sayaya.magi.ide.usecase.Row
import dev.sayaya.magi.ide.usecase.Who
import java.awt.Container

/**
 * A council is one line in the transcript — accepted or not — and its members' opinions open only when
 * that line is clicked. Asked 2026-11-15: "카운슬 결과는 완료/반대만 간략하게 한줄로 … 클릭하면 펼쳐서".
 */
class CouncilFoldViewTest : BasePlatformTestCase() {
    private fun texts(c: Container): List<String> = c.components.flatMap {
        listOfNotNull((it as? javax.swing.JLabel)?.text, (it as? javax.swing.text.JTextComponent)?.text) +
            (if (it is Container) texts(it) else emptyList())
    }

    fun testACouncilIsOneLineUntilItIsOpened() {
        val view = MagiToolWindow.View(project, sendSession = { "s1" }, sendConnection = { _, _, _ -> })
        try {
            val render = view.javaClass.getDeclaredMethod("renderRow", Row::class.java).apply { isAccessible = true }
            val flip = view.javaClass.getDeclaredMethod("flip", Row::class.java).apply { isAccessible = true }
            @Suppress("UNCHECKED_CAST")
            fun fold(rows: List<Row>) = view.javaClass.getDeclaredMethod("foldCouncils", List::class.java)
                .apply { isAccessible = true }.invoke(view, rows) as List<Row>
            val seat = Row(Who.Council, "Melchior said no", member = "Melchior", round = 1, decision = "continue", convene = 1, rowId = "v1")
            val closing = Row(Who.Council, "0 done / 3 continue", round = 1, decision = "continue", convene = 1, closing = true,
                why = "add the missing test", rowId = "d1")
            val user = Row(Who.User, "is it done", rowId = "u1")

            assertEquals(listOf(user, closing), fold(listOf(user, seat, closing)))
            val shut = texts(render.invoke(view, closing) as Container).joinToString("\n")
            assertTrue(shut, shut.contains(MagiBundle.msg("chat.council.held")))
            assertFalse("the opinions stay folded: $shut", shut.contains("add the missing test"))

            flip.invoke(view, closing)
            assertEquals(listOf(user, seat, closing), fold(listOf(user, seat, closing)))
            val open = texts(render.invoke(view, closing) as Container).joinToString("\n")
            assertTrue(open, open.contains("add the missing test"))

            val done = closing.copy(decision = "done", convene = 2, rowId = "d2")
            val ok = texts(render.invoke(view, done) as Container).joinToString("\n")
            assertTrue(ok, ok.contains(MagiBundle.msg("chat.council.accepted")))
        } finally { com.intellij.openapi.util.Disposer.dispose(view) }
    }
}
