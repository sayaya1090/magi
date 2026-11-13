package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.usecase.Backoff
import java.util.concurrent.FutureTask

/** One reconnect worker per transcript. A failure during attachment schedules another pass. */
internal class TranscriptReconnect(
    private val execute: (Runnable) -> Unit,
    private val attach: () -> Boolean,
    private val recovered: () -> Unit,
    private val pause: (Int) -> Unit = { Thread.sleep(Backoff.delayMs(it, Math.random())) },
) : AutoCloseable {
    private var closed = false
    private var requested = 0L
    private var worker: FutureTask<Unit>? = null

    @Synchronized fun request() {
        if (closed) return
        requested++
        if (worker != null) return
        lateinit var task: FutureTask<Unit>
        task = FutureTask<Unit> { runLoop(task) }
        worker = task
        try { execute(task) } catch (e: RuntimeException) { worker = null; throw e }
    }

    private fun runLoop(owner: FutureTask<Unit>) {
        var attempt = 1
        try {
            while (true) {
                val ticket = synchronized(this) { if (closed) return; requested }
                pause(attempt)
                if (synchronized(this) { closed }) return
                if (attach()) {
                    recovered()
                    synchronized(this) {
                        if (requested == ticket) { worker = null; return }
                    }
                    attempt = 1
                } else {
                    attempt = minOf(attempt + 1, 30)
                }
            }
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
        } finally {
            synchronized(this) { if (worker === owner) worker = null }
        }
    }

    @Synchronized override fun close() {
        closed = true
        worker?.cancel(true)
        worker = null
    }
}
