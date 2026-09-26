package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.ui.components.JBLabel
import com.intellij.ui.components.JBScrollPane
import dev.sayaya.magi.ide.model.RosterRow
import java.awt.Dimension
import javax.swing.BoxLayout
import javax.swing.JButton
import javax.swing.JPanel

/**
 * The plan panel, as it stood on a real screen (2026-09-26): one long companion row widened the
 * whole panel, a horizontal scrollbar appeared, and the 「new conversation」 button on the right of
 * the controls row was pushed out of sight. And the row itself named that companion by its socket
 * file, `daemon-magi-6lw0yxf3.sock`.
 */
class PlanPanelLayoutTest : BasePlatformTestCase() {

    fun `test a long row does not widen the panel past its window`() {
        val long = JBLabel("x".repeat(400))
        val button = JButton("New")
        // The plan panel's own stacking: a column that tracks the viewport width (Look.column).
        val content = Look.column().apply {
            add(JPanel().apply { layout = BoxLayout(this, BoxLayout.Y_AXIS); add(long) })
            add(JPanel(java.awt.BorderLayout()).apply { add(button, java.awt.BorderLayout.EAST) })
        }
        val scroll = JBScrollPane(content).apply { size = Dimension(300, 400) }
        scroll.doLayout(); scroll.viewport.doLayout(); scroll.viewport.view.doLayout(); content.doLayout()
        (content.getComponent(1) as JPanel).doLayout()

        val viewWidth = scroll.viewport.view.width
        assertTrue("the content is wider than its window: $viewWidth > ${scroll.viewport.width}",
            viewWidth <= scroll.viewport.width)
        assertFalse("a horizontal scrollbar stands", scroll.horizontalScrollBar.isVisible)
        val right = button.x + button.width
        assertTrue("the right-hand button is outside the window: ends at $right of ${scroll.viewport.width}",
            right <= scroll.viewport.width)
    }

    /** The panel is stacked in that column — the property above means nothing if the window does not use it. */
    fun `test the plan window scrolls a width-tracking column`() {
        val src = java.io.File("src/main/kotlin/dev/sayaya/magi/ide/ui/PlanToolWindow.kt")
        assertTrue("the plan window source was not found at ${src.absolutePath}", src.isFile)
        val text = src.readText()
        val root = text.substringAfter("val root = ").substringBefore("toolWindow.contentManager.addContent")
        assertTrue("the plan window's root is not a width-tracking column: $root", root.trimStart().startsWith("Look.column()"))
        assertTrue("the plan window does not scroll that root", "JBScrollPane(root)" in text)
    }

    /** The two verbs stand on one row under the combos; they were scattered (one beside a combo, one alone). */
    fun `test the two control buttons share one row`() {
        val text = java.io.File("src/main/kotlin/dev/sayaya/magi/ide/ui/PlanToolWindow.kt").readText()
        val row = Regex("""add\(JBPanel<JBPanel<\*>>\(java\.awt\.FlowLayout[^\n]*\n(?:[^\n]*\n){1,3}?\s*add\(fresh\)\s*\n\s*add\(compact\)""")
        assertTrue("「새 채팅」 and 「채팅 요약」 are not in one row panel", row.containsMatchIn(text))
    }

    /**
     * The conversation and model lists are held as flows ([DaemonList]) that retry a failed read on
     * their own. The callback version dropped the failure (`onDaemon({})`) and left the combo empty for
     * good whenever the daemon was still starting; the model list also asked through the door that needs
     * a conversation, so a new, unspoken conversation left it blank.
     */
    fun `test both lists are flows that retry, and the model list needs no conversation`() {
        val text = java.io.File("src/main/kotlin/dev/sayaya/magi/ide/ui/PlanToolWindow.kt").readText()
        assertTrue("the conversation list is not a DaemonList", "val talks = DaemonList(lists)" in text)
        assertTrue("the model list is not a DaemonList", "val models = DaemonList(lists)" in text)
        val models = text.substringAfter("val models = DaemonList(lists)").substringBefore("fun off(")
        assertTrue("the model list asks through the door that needs a conversation", "askWithoutChat" in models)
        assertTrue("the window's scope is not cancelled with it", "Disposer.register(toolWindow.disposable) { lists.cancel() }" in text)
        assertTrue("opening the list does not re-read it", "popupMenuWillBecomeVisible(e: javax.swing.event.PopupMenuEvent?) = loadTalks()" in text)
    }

    /**
     * The 3-second poll is a flow: poll() only asks, and only the latest ask is painted (flatMapLatest),
     * so the hand-kept sequence check that discarded late answers is gone from the painting.
     */
    fun `test the poll is a flow that paints only the latest read`() {
        val text = java.io.File("src/main/kotlin/dev/sayaya/magi/ide/ui/PlanToolWindow.kt").readText()
        assertTrue("poll() does not just ask", Regex("""fun poll\(\) \{\s*polls\.tryEmit\(Unit\)\s*}""").containsMatchIn(text))
        assertTrue("the poll does not keep only the latest read", "polls.flatMapLatest" in text)
        val paint = text.substringAfter("fun paintPoll(p: PollRead)").substringBefore("fun ")
        assertFalse("painting still compares sequences by hand", "pollSeq" in paint)
        assertTrue("a failed read does not show the window as stale", "onFailure = { stale.isVisible = true }" in text)
    }

    fun `test a companion with no name is called by its workspace, not its socket file`() {
        val plan = PlanToolWindow()
        val row = plan.javaClass.getDeclaredMethod("fleetRow", RosterRow::class.java, Boolean::class.javaPrimitiveType, Boolean::class.javaPrimitiveType).let {
            it.isAccessible = true
            it.invoke(plan, RosterRow(socket = "/tmp/m/daemon-magi-6lw0yxf3.sock", workdir = "/Users/me/projects/billing"), false, false) as JBLabel
        }
        assertTrue("named by its socket: ${row.text}", row.text.startsWith("billing"))
        assertFalse("the workspace is said twice: ${row.text}", row.text.contains("(billing)"))
        assertTrue("the socket is still reachable from the tooltip", row.toolTipText.contains("daemon-magi-6lw0yxf3.sock"))

        val named = plan.javaClass.getDeclaredMethod("fleetRow", RosterRow::class.java, Boolean::class.javaPrimitiveType, Boolean::class.javaPrimitiveType).let {
            it.isAccessible = true
            it.invoke(plan, RosterRow(socket = "/tmp/s.sock", name = "api", workdir = "/Users/me/projects/billing"), false, false) as JBLabel
        }
        assertTrue("a named companion keeps its name, workspace beside it: ${named.text}",
            named.text.startsWith("api") && named.text.contains("(billing)"))
    }
}
