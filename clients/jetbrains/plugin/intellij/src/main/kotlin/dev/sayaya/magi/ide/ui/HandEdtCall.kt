package dev.sayaya.magi.ide.ui

import java.util.concurrent.TimeUnit
import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

/**
 * MCP 요청 스레드에서 IntelliJ EDT로 작업을 디스패치하고 결과를 동기적으로 수신하는 제어기 (§6.44.3).
 *
 * 작업 수명을 [State]로 추적하여 타임아웃이나 대기 스레드 인터럽트 발생 시 시작 전 작업과 진행 중 작업을 구분하고,
 * 대기 종료를 성공으로 위장하지 않고 적절한 예외(IDE_HAND_NOT_STARTED / IDE_HAND_RESULT_UNKNOWN)로 실패 처리한다.
 */
internal class HandEdtCall(
    private val enqueue: (Runnable) -> Unit,
    private val isEdt: () -> Boolean,
    private val isDisposed: () -> Boolean,
    private val timeoutMillis: Long = 20_000,
) {
    private sealed interface State {
        object Queued : State
        object Running : State
        data class Completed(val result: Result<String>) : State
        object AbandonedBeforeStart : State
    }

    private class CallRecord {
        val lock = ReentrantLock()
        val condition = lock.newCondition()
        var state: State = State.Queued
    }

    fun run(work: () -> String): String {
        if (isDisposed()) {
            throw IllegalStateException("IDE_HAND_DISPOSED: project is already disposed")
        }
        if (isEdt()) {
            if (isDisposed()) {
                throw IllegalStateException("IDE_HAND_DISPOSED: project is already disposed")
            }
            return work()
        }

        val record = CallRecord()
        val runnable = Runnable {
            val shouldRun = record.lock.withLock {
                if (isDisposed()) {
                    if (record.state is State.Queued) {
                        record.state = State.Completed(
                            Result.failure(IllegalStateException("IDE_HAND_DISPOSED: project was disposed before task started"))
                        )
                        record.condition.signalAll()
                    }
                    return@Runnable
                }
                when (record.state) {
                    is State.Queued -> {
                        record.state = State.Running
                        true
                    }
                    is State.AbandonedBeforeStart -> false
                    is State.Running, is State.Completed -> false
                }
            }

            if (!shouldRun) return@Runnable

            val result = runCatching { work() }

            record.lock.withLock {
                record.state = State.Completed(result)
                record.condition.signalAll()
            }
        }

        try {
            enqueue(runnable)
        } catch (e: Throwable) {
            record.lock.withLock {
                record.state = State.AbandonedBeforeStart
            }
            throw IllegalStateException("IDE_HAND_NOT_STARTED: failed to enqueue task to IDE", e)
        }

        record.lock.lock()
        try {
            var remainingNanos = TimeUnit.MILLISECONDS.toNanos(timeoutMillis)
            while (record.state !is State.Completed && record.state !is State.AbandonedBeforeStart) {
                if (remainingNanos <= 0L) {
                    break
                }
                try {
                    remainingNanos = record.condition.awaitNanos(remainingNanos)
                } catch (ie: InterruptedException) {
                    Thread.currentThread().interrupt()
                    return handleInterruptedOrTimeout(record)
                }
            }
            return handleInterruptedOrTimeout(record)
        } finally {
            record.lock.unlock()
        }
    }

    private fun handleInterruptedOrTimeout(record: CallRecord): String {
        return when (val s = record.state) {
            is State.Completed -> s.result.getOrThrow()
            is State.Queued -> {
                record.state = State.AbandonedBeforeStart
                throw IllegalStateException("IDE_HAND_NOT_STARTED: IDE task did not start before the deadline; no editor operation was run.")
            }
            is State.Running -> {
                throw IllegalStateException("IDE_HAND_RESULT_UNKNOWN: stopped waiting for the IDE; the operation may still complete. Inspect the editor buffer before retrying.")
            }
            is State.AbandonedBeforeStart -> {
                throw IllegalStateException("IDE_HAND_NOT_STARTED: IDE task did not start before the deadline; no editor operation was run.")
            }
        }
    }
}
