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

    fun testManagementStatusRendersWhileDialogIsModal() {
        val modal = Any()
        var rendered = false
        com.intellij.openapi.application.impl.LaterInvocator.enterModal(modal)
        try {
            com.intellij.openapi.application.ApplicationManager.getApplication().executeOnPooledThread {
                managementUiLater { rendered = true }
            }.get(5, java.util.concurrent.TimeUnit.SECONDS)
            com.intellij.util.ui.UIUtil.dispatchAllInvocationEvents()
            assertTrue("management rendering was deferred until modal dialog closes", rendered)
        } finally { com.intellij.openapi.application.impl.LaterInvocator.leaveModal(modal) }
    }

    fun testDelegatedReviewPreservesCustomReportOrderAndEscapesUntrustedText() {
        val waiting = dev.sayaya.magi.ide.model.Waiting(
            id = "call", kind = "question", what = "<script>question</script>",
            report = listOf(
                dev.sayaya.magi.ide.model.Ground("custom-evidence", "<b>evidence</b>"),
                dev.sayaya.magi.ide.model.Ground("custom-risks", "risks"),
                dev.sayaya.magi.ide.model.Ground("custom-choice", "recommendation"),
            ),
        )
        val html = delegatedReviewHtml(dev.sayaya.magi.ide.model.PendingPrompt("source-session", "requester", waiting))
        assertTrue(html.contains("source-session"))
        assertTrue(html.contains("&lt;script&gt;"))
        assertFalse(html.contains("<script>"))
        assertTrue(html.indexOf("custom-evidence") < html.indexOf("custom-risks"))
        assertTrue(html.indexOf("custom-risks") < html.indexOf("custom-choice"))
        assertTrue(html.contains("&lt;b&gt;evidence&lt;/b&gt;"))
        assertFalse(html.contains("tried:"))
    }

    fun testInlineMaintenanceFitsNarrowPanelAndKeepsStatusHeightBounded() {
        val panel = ProfileMaintenancePanel(Workspace(project))
        try {
            for (width in listOf(240, 420, 900, 240)) {
                panel.setSize(width, panel.preferredSize.height)
                panel.doLayout()
                val scroll = panel.components.filterIsInstance<JBScrollPane>().single()
                assertEquals(javax.swing.ScrollPaneConstants.HORIZONTAL_SCROLLBAR_NEVER, scroll.horizontalScrollBarPolicy)
                assertTrue(scroll.x >= com.intellij.util.ui.JBUI.scale(12))
                assertTrue(scroll.x + scroll.width <= width-com.intellij.util.ui.JBUI.scale(12))
                assertTrue(scroll.height > 0)
                val actions = panel.components.filterIsInstance<JPanel>().first { it.layout is java.awt.GridLayout }
                actions.doLayout()
                val buttons = actions.components.filterIsInstance<JButton>()
                assertEquals(2, buttons.size)
                assertTrue(buttons.all { it.x >= 0 && it.x+it.width <= actions.width })
                assertTrue(buttons[1].y-buttons[0].y-buttons[0].height >= com.intellij.util.ui.JBUI.scale(8))
                assertTrue(panel.maximumSize.height <= com.intellij.util.ui.JBUI.scale(280))
            }
        } finally { panel.dispose() }
    }

    fun testInsetHeadingDoesNotDoubleIndentItsLabel() {
        val heading = Look.sectionHeading("Description", inset = false)
        val label = (heading as java.awt.Container).components.filterIsInstance<JBLabel>().single()
        assertEquals(0, label.insets.left)
    }

    fun testScheduleActionsFitNarrowPanelsAndSupportKeyboard() {
        var added = 0
        var reloaded = 0
        val row = ScheduleActions.row(true, true, { added++ }, { reloaded++ })
        val buttons = row.components.filterIsInstance<JButton>()
        assertEquals(2, buttons.size)
        for (width in listOf(240, 420, 900, 240)) {
            row.setSize(width, row.preferredSize.height)
            row.doLayout()
            for (button in buttons) {
                assertTrue("button extends beyond $width", button.x >= 0 && button.x + button.width <= width)
                assertTrue("button has visible height", button.height > 0)
                assertTrue("button needs an accessible name", !button.accessibleContext.accessibleName.isNullOrBlank())
                assertNotNull(button.toolTipText)
                assertTrue(button.isFocusable)
            }
        }
        buttons[0].doClick(0)
        buttons[1].doClick(0)
        assertEquals(1, added)
        assertEquals(1, reloaded)
        buttons[1].isEnabled = false
        buttons[1].doClick(0)
        assertEquals(1, reloaded)
        assertTrue("reload action should explain itself", buttons[1].text.isNotBlank())
        assertEquals(0, ScheduleActions.row(false, false, {}, {}).componentCount)
    }


    fun `test settings shrink after expansion with long profiles`() {
        val config = MagiConfigurable(project)
        val scroll = com.intellij.openapi.options.ex.ConfigurableCardPanel
            .createConfigurableComponent(config) as javax.swing.JScrollPane
        val content = scroll.viewport.view
        fun layout(c: java.awt.Container) {
            c.doLayout()
            c.components.filterIsInstance<java.awt.Container>().forEach(::layout)
        }
        for (width in listOf(480, 1100, 480, 800, 480)) {
            scroll.size = Dimension(width, 650)
            repeat(3) { layout(scroll) }
            assertEquals("settings must follow viewport after resize to $width",
                scroll.viewport.extentSize.width, content.width)
            assertFalse("settings must not need horizontal scrolling at $width",
                scroll.horizontalScrollBar.isVisible)
        }
    }

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

    fun testContextHeadingKeepsCompressionActionBesideGraphTitle() {
        val action = JButton(MagiBundle.msg("plan.compact"))
        val heading = Look.sectionHeading(MagiBundle.msg("plan.usage"), action)
        for (width in listOf(280, 420, 800)) {
            heading.setSize(width, 40)
            heading.doLayout()
            assertTrue("action outside narrow panel", action.x >= 0 && action.x + action.width <= width)
            assertTrue("action must remain on the title row", action.height > 0 && action.y + action.height <= 40)
            assertEquals("context heading must retain the actual action", heading, action.parent)
        }
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
        assertTrue("a named companion keeps its name: ${named.text}", named.text.startsWith("api"))
        assertTrue("the workspace of a named companion is in its tooltip: ${named.toolTipText}",
            named.toolTipText.contains("billing"))
        assertFalse("the long details went back onto the row: ${named.text}", named.text.contains("billing"))
    }
}
