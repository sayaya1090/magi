package dev.sayaya.magi.ide.ui

import junit.framework.TestCase
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class TranscriptReconnectTest : TestCase() {
    fun testFailuresBackOffUntilOneAttachmentSucceeds() {
        val tasks = mutableListOf<Runnable>()
        val waits = mutableListOf<Int>()
        var attempts = 0
        var refreshed = 0
        val retry = TranscriptReconnect({ tasks.add(it) }, { ++attempts == 3 }, { refreshed++ }, { waits.add(it) })
        repeat(10) { retry.request() }
        assertEquals(1, tasks.size)
        tasks.single().run()
        assertEquals(listOf(1, 2, 3), waits)
        assertEquals(1, refreshed)
        retry.close()
    }

    fun testFailureDuringSuccessfulAttachIsNotLost() {
        val tasks = mutableListOf<Runnable>()
        var attempts = 0
        lateinit var retry: TranscriptReconnect
        retry = TranscriptReconnect({ tasks.add(it) }, {
            if (++attempts == 1) retry.request()
            true
        }, {}, {})
        retry.request()
        tasks.single().run()
        assertEquals(2, attempts)
        assertEquals(1, tasks.size)
        retry.close()
    }

    fun testCloseInterruptsBackoffAndNeverReconnects() {
        val waiting = CountDownLatch(1)
        val exited = CountDownLatch(1)
        val calls = AtomicInteger()
        val retry = TranscriptReconnect({ task -> Thread { try { task.run() } finally { exited.countDown() } }.start() },
            { calls.incrementAndGet(); true }, {}, { waiting.countDown(); Thread.sleep(60000) })
        retry.request()
        assertTrue(waiting.await(2, TimeUnit.SECONDS))
        retry.close()
        assertTrue(exited.await(2, TimeUnit.SECONDS))
        retry.request()
        assertEquals(0, calls.get())
    }

    fun testClosedQueuedWorkerDoesNotConnect() {
        val tasks = mutableListOf<Runnable>()
        var calls = 0
        val retry = TranscriptReconnect({ tasks.add(it) }, { calls++; true }, {}, {})
        retry.request()
        retry.close()
        tasks.single().run()
        assertEquals(0, calls)
    }
}
