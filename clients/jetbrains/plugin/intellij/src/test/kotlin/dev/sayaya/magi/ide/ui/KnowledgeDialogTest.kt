package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ui.UIUtil
import dev.sayaya.magi.ide.model.*
import kotlinx.coroutines.CompletableDeferred
import java.io.Closeable
import java.util.concurrent.atomic.AtomicInteger
import javax.swing.*

class KnowledgeDialogTest : BasePlatformTestCase() {
    private inline fun <reified T> field(dialog: KnowledgeDialog, name: String): T =
        KnowledgeDialog::class.java.getDeclaredField(name).apply { isAccessible = true }.get(dialog) as T
    private fun awaitUi(check: () -> Boolean) {
        val until = System.nanoTime() + 5_000_000_000L
        while (!check() && System.nanoTime() < until) {
            UIUtil.dispatchAllInvocationEvents(); Thread.sleep(5)
        }
        assertTrue("management UI did not settle", check())
    }
    private fun inventory(revision: String = "one") = Response(ok = true,
        knowledge = listOf(KnowledgeItem(name = "memory", revision = revision, managed = true)),
        slots = listOf(PluginSlot("slot", "plugin", "knowledge", "Status",
            actions = listOf(SlotAction("refresh", "Refresh plugin")))))

    fun testSlotActionReloadsInventoryAndFailureKeepsRowsButDisablesStaleActions() {
        val calls = java.util.Collections.synchronizedList(mutableListOf<String>())
        var failRead = false
        val dialog = KnowledgeDialog(project, Workspace(project), fetch = { _, action, plugin, slot ->
            calls.add(action)
            if (action.isNotEmpty()) {
                assertEquals("plugin", plugin); assertEquals("slot", slot)
                Response(ok = true)
            } else if (failRead) Response(ok = false, error = "offline") else inventory()
        }, observe = { null })
        try {
            val pin = field<JButton>(dialog, "pin")
            val reload = field<JButton>(dialog, "reload")
            val entries = field<DefaultListModel<String>>(dialog, "entries")
            val slots = field<JPanel>(dialog, "slotButtons")
            awaitUi { pin.isEnabled }
            (slots.getComponent(0) as JButton).doClick()
            awaitUi { calls.size == 3 && reload.isEnabled }
            assertEquals(listOf("", "refresh", ""), calls.toList())
            assertEquals(1, entries.size())
            assertTrue(pin.isEnabled)
            failRead = true
            reload.doClick()
            awaitUi { reload.isEnabled }
            assertEquals("offline", field<JLabel>(dialog, "notice").text)
            assertEquals(1, entries.size())
            assertFalse(pin.isEnabled)
            assertFalse(slots.getComponent(0).isEnabled)
            failRead = false
            reload.doClick()
            awaitUi { pin.isEnabled }
        } finally { dialog.close(0) }
    }
    fun testConflictReloadsNewRevisionAndWatchDuringReadQueuesOneReload() {
        val first = CompletableDeferred<Response>()
        val reads = AtomicInteger()
        val mutations = java.util.Collections.synchronizedList(mutableListOf<String>())
        var changed: () -> Unit = {}
        var closed = false
        val dialog = KnowledgeDialog(project, Workspace(project), fetch = { item, action, _, _ ->
            if (action.isNotEmpty()) {
                mutations.add(item!!.revision)
                Response(ok = false, error = "revision conflict")
            } else if (reads.incrementAndGet() == 1) first.await() else inventory("two")
        }, observe = { callback -> changed = callback; Closeable { closed = true } })
        try {
            changed(); changed(); UIUtil.dispatchAllInvocationEvents()
            first.complete(inventory())
            val pin = field<JButton>(dialog, "pin")
            awaitUi { reads.get() == 2 && pin.isEnabled }
            pin.doClick()
            awaitUi { pin.isEnabled }
            assertEquals(listOf("two"), mutations.toList())
            assertEquals(3, reads.get())
            assertEquals("revision conflict", field<JLabel>(dialog, "notice").text)
        } finally { dialog.close(0) }
        assertTrue(closed)
        changed(); UIUtil.dispatchAllInvocationEvents()
        assertEquals(3, reads.get())
    }
    fun testCloseCancelsPendingReadAndDoesNotPaintLateResponse() {
        val pending = CompletableDeferred<Response>()
        val started = AtomicInteger()
        val dialog = KnowledgeDialog(project, Workspace(project), fetch = { _, _, _, _ ->
            started.incrementAndGet(); pending.await()
        }, observe = { null })
        val entries = field<DefaultListModel<String>>(dialog, "entries")
        awaitUi { started.get() == 1 }
        dialog.close(0)
        pending.complete(inventory()); UIUtil.dispatchAllInvocationEvents()
        assertEquals(0, entries.size())
    }
}
