package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import java.util.concurrent.atomic.AtomicInteger

/**
 * The list the plan window shows, as state. The callback version read once and dropped the failure,
 * so a daemon still starting left the combo empty for good (2026-09-26); these pin that a failed read
 * comes back on its own, a refresh reads again, a final answer is not retried, and closing stops it.
 */
class DaemonListTest : BasePlatformTestCase() {
    private fun scope() = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    fun `test a read that fails is retried until it lands`() = runBlocking {
        val scope = scope()
        val tries = AtomicInteger()
        val list = DaemonList(scope, retryAfterMs = { 10 }) {
            if (tries.incrementAndGet() < 3) throw IllegalStateException("daemon still starting")
            listOf("s1", "s2", "s3")
        }
        val ready = withTimeout(5_000) { list.state.first { it is DaemonList.State.Ready } }
        assertEquals(listOf("s1", "s2", "s3"), (ready as DaemonList.State.Ready).value)
        assertEquals("two failures, then the read that landed", 3, tries.get())
        scope.cancel()
    }

    fun `test refresh reads again`() = runBlocking {
        val scope = scope()
        val n = AtomicInteger()
        val list = DaemonList(scope, retryAfterMs = { 10 }) { n.incrementAndGet() }
        withTimeout(5_000) { list.state.first { it == DaemonList.State.Ready(1) } }
        list.refresh()
        withTimeout(5_000) { list.state.first { it == DaemonList.State.Ready(2) } }
        scope.cancel()
    }

    fun `test a final answer is a value, not retried`() = runBlocking {
        val scope = scope()
        val n = AtomicInteger()
        val list = DaemonList<String?>(scope, retryAfterMs = { 10 }) { n.incrementAndGet(); null } // "no such door"
        withTimeout(5_000) { list.state.first { it is DaemonList.State.Ready } }
        delay(100)
        assertEquals("a final answer was read again", 1, n.get())
        scope.cancel()
    }

    fun `test closing the window stops the retries`() = runBlocking {
        val scope = scope()
        val n = AtomicInteger()
        DaemonList(scope, retryAfterMs = { 10 }) { n.incrementAndGet(); throw IllegalStateException("down") }
        withTimeout(5_000) { while (n.get() < 2) delay(5) }
        scope.cancel()
        delay(50)
        val after = n.get()
        delay(150)
        assertEquals("reads went on after the scope was cancelled", after, n.get())
    }
}
