package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.Response
import kotlinx.coroutines.CompletableDeferred
import java.util.concurrent.atomic.AtomicInteger
import javax.swing.JButton
import javax.swing.JTextArea

class ProfileMaintenancePanelTest : BasePlatformTestCase() {
    private inline fun <reified T> field(panel: ProfileMaintenancePanel, name: String): T =
        ProfileMaintenancePanel::class.java.getDeclaredField(name).apply { isAccessible = true }.get(panel) as T
    private fun awaitUi(check: () -> Boolean) {
        val until = System.nanoTime() + 5_000_000_000L
        while (!check() && System.nanoTime() < until) { UIUtil.dispatchAllInvocationEvents(); Thread.sleep(5) }
        assertTrue("maintenance UI did not settle", check())
    }
    fun testReadDoesNotRefreshAndRequestFailureCanBeRetried() {
        val calls = java.util.Collections.synchronizedList(mutableListOf<Boolean>())
        val waiting = CompletableDeferred<Response>()
        val panel = ProfileMaintenancePanel(Workspace(project)) { refresh ->
            calls.add(refresh)
            when (calls.size) {
                1 -> Response(ok = true, out = """{"enabled":true,"pending":true,"pending_sources":{"source-chat":42}}""")
                2 -> waiting.await()
                else -> Response(ok = true, out = """{"enabled":true,"running":true}""")
            }
        }
        try {
            val refresh = field<JButton>(panel, "refresh")
            val reload = field<JButton>(panel, "reload")
            val text = field<JTextArea>(panel, "text")
            awaitUi { reload.isEnabled }
            assertEquals(listOf(false), calls.toList())
            assertTrue(text.text.contains("source-chat"))
            assertTrue(text.text.contains("42"))
            refresh.doClick(); refresh.doClick(); reload.doClick()
            awaitUi { calls.size == 2 }
            assertFalse(refresh.isEnabled); assertFalse(reload.isEnabled)
            waiting.complete(Response(ok = false, error = "no new evidence"))
            awaitUi { refresh.isEnabled }
            assertEquals("no new evidence", text.text)
            refresh.doClick()
            awaitUi { refresh.isEnabled }
            assertEquals(listOf(false, true, true), calls.toList())
            assertTrue(text.text.contains(MagiBundle.msg("maintenance.requested")))
        } finally { panel.dispose() }
    }
    fun testMalformedOrMissingResponseDoesNotPretendMaintenanceIsDisabled() {
        for (payload in listOf(null, "null", "{", "[]")) {
            val panel = ProfileMaintenancePanel(Workspace(project)) { Response(ok = true, out = payload) }
            try {
                awaitUi { field<JButton>(panel, "reload").isEnabled }
                assertEquals(MagiBundle.msg("knowledge.failed"), field<JTextArea>(panel, "text").text)
            } finally { panel.dispose() }
        }
    }
    fun testDisposeStopsTimerAndDiscardsPendingRead() {
        val pending = CompletableDeferred<Response>()
        val calls = AtomicInteger()
        val panel = ProfileMaintenancePanel(Workspace(project)) { calls.incrementAndGet(); pending.await() }
        awaitUi { calls.get() == 1 }
        panel.dispose()
        pending.complete(Response(ok = true, out = """{"enabled":true}"""))
        UIUtil.dispatchAllInvocationEvents()
        assertFalse(field<javax.swing.Timer>(panel, "timer").isRunning)
        assertEquals("", field<JTextArea>(panel, "text").text)
        field<JButton>(panel, "reload").doClick()
        assertEquals(1, calls.get())
    }
}
