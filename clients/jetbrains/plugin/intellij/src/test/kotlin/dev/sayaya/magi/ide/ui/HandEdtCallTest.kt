package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.transport.HandServer
import dev.sayaya.magi.ide.usecase.Hand
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.*
import org.junit.Test
import java.util.Collections
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

class HandEdtCallTest {

    @Test
    fun `test timeout before start does not execute work even when runnable runs later`() {
        val queue = Collections.synchronizedList(mutableListOf<Runnable>())
        val workCount = AtomicInteger(0)
        val edtCall = HandEdtCall(
            enqueue = { queue.add(it) },
            isEdt = { false },
            isDisposed = { false },
            timeoutMillis = 20,
        )

        val ex = assertThrows(IllegalStateException::class.java) {
            edtCall.run {
                workCount.incrementAndGet()
                "ok"
            }
        }
        assertTrue(ex.message!!.startsWith("IDE_HAND_NOT_STARTED: IDE task did not start before the deadline"))
        assertEquals(0, workCount.get())

        // 큐에 보관된 runnable을 타임아웃 이후 뒤늦게 실행해도 work는 0회여야 한다
        assertEquals(1, queue.size)
        queue.removeAt(0).run()
        assertEquals(0, workCount.get())
    }

    @Test
    fun `test timeout after start allows running work to complete exactly once`() {
        val workCount = AtomicInteger(0)
        val workStartedLatch = CountDownLatch(1)
        val workReleaseLatch = CountDownLatch(1)
        val threadPool = Executors.newSingleThreadExecutor()

        try {
            val edtCall = HandEdtCall(
                enqueue = { r -> threadPool.execute(r) },
                isEdt = { false },
                isDisposed = { false },
                timeoutMillis = 50,
            )

            val ex = assertThrows(IllegalStateException::class.java) {
                edtCall.run {
                    workStartedLatch.countDown()
                    workReleaseLatch.await()
                    workCount.incrementAndGet()
                    "finished"
                }
            }
            assertTrue(ex.message!!.startsWith("IDE_HAND_RESULT_UNKNOWN: stopped waiting for the IDE"))

            // 타임아웃 시점에는 latch가 대기 중이므로 work는 아직 완료 카운트되지 않음
            assertEquals(0, workCount.get())

            // 작업 해제 후 정확히 1회 완료
            workReleaseLatch.countDown()
            threadPool.shutdown()
            assertTrue(threadPool.awaitTermination(5, TimeUnit.SECONDS))
            assertEquals(1, workCount.get())
        } finally {
            workReleaseLatch.countDown()
            threadPool.shutdownNow()
        }
    }

    @Test
    fun `test completed result returned when work finishes before timeout`() {
        val threadPool = Executors.newSingleThreadExecutor()
        try {
            val edtCall = HandEdtCall(
                enqueue = { r -> threadPool.execute(r) },
                isEdt = { false },
                isDisposed = { false },
                timeoutMillis = 5_000,
            )
            val result = edtCall.run { "hello world" }
            assertEquals("hello world", result)
        } finally {
            threadPool.shutdownNow()
        }
    }

    @Test
    fun `test exception in work is propagated to caller`() {
        val threadPool = Executors.newSingleThreadExecutor()
        try {
            val edtCall = HandEdtCall(
                enqueue = { r -> threadPool.execute(r) },
                isEdt = { false },
                isDisposed = { false },
                timeoutMillis = 5_000,
            )
            val ex = assertThrows(IllegalArgumentException::class.java) {
                edtCall.run { throw IllegalArgumentException("custom error in work") }
            }
            assertEquals("custom error in work", ex.message)
        } finally {
            threadPool.shutdownNow()
        }
    }

    @Test
    fun `test interruption before start restores interrupt flag and throws NOT_STARTED`() {
        val queue = Collections.synchronizedList(mutableListOf<Runnable>())
        val workCount = AtomicInteger(0)
        val edtCall = HandEdtCall(
            enqueue = { queue.add(it) },
            isEdt = { false },
            isDisposed = { false },
            timeoutMillis = 60_000,
        )

        val callerThread = Thread.currentThread()
        val interrupter = Thread {
            while (callerThread.state != Thread.State.TIMED_WAITING) {
                Thread.yield()
            }
            callerThread.interrupt()
        }

        try {
            interrupter.start()
            val ex = assertThrows(IllegalStateException::class.java) {
                edtCall.run {
                    workCount.incrementAndGet()
                    "should not run"
                }
            }
            assertTrue(ex.message!!.startsWith("IDE_HAND_NOT_STARTED"))
            val wasInterrupted = Thread.interrupted()
            assertTrue(wasInterrupted)
            assertEquals(0, workCount.get())

            // 인터럽트 종료 후 큐에 남은 작업을 실행해도 work는 0회
            assertEquals(1, queue.size)
            queue.removeAt(0).run()
            assertEquals(0, workCount.get())
        } finally {
            Thread.interrupted() // 인터럽트 플래그 정리
            interrupter.join()
        }
    }

    @Test
    fun `test interruption after start restores interrupt flag and throws RESULT_UNKNOWN`() {
        val workStartedLatch = CountDownLatch(1)
        val workReleaseLatch = CountDownLatch(1)
        val workCount = AtomicInteger(0)
        val threadPool = Executors.newSingleThreadExecutor()

        val edtCall = HandEdtCall(
            enqueue = { r -> threadPool.execute(r) },
            isEdt = { false },
            isDisposed = { false },
            timeoutMillis = 60_000,
        )

        val callerThread = Thread.currentThread()
        val interrupter = Thread {
            workStartedLatch.await()
            while (callerThread.state != Thread.State.TIMED_WAITING) {
                Thread.yield()
            }
            callerThread.interrupt()
        }

        try {
            interrupter.start()
            val ex = assertThrows(IllegalStateException::class.java) {
                edtCall.run {
                    workStartedLatch.countDown()
                    workReleaseLatch.await()
                    workCount.incrementAndGet()
                    "done"
                }
            }
            assertTrue(ex.message!!.startsWith("IDE_HAND_RESULT_UNKNOWN"))
            val wasInterrupted = Thread.interrupted()
            assertTrue(wasInterrupted)
            assertEquals(0, workCount.get())

            workReleaseLatch.countDown()
            threadPool.shutdown()
            assertTrue(threadPool.awaitTermination(5, TimeUnit.SECONDS))
            assertEquals(1, workCount.get())
        } finally {
            Thread.interrupted() // 인터럽트 플래그 정리
            workReleaseLatch.countDown()
            interrupter.join()
            threadPool.shutdownNow()
        }
    }

    @Test
    fun `test disposed before enqueue throws immediately without enqueue`() {
        val enqueueCalled = AtomicBoolean(false)
        val edtCall = HandEdtCall(
            enqueue = { enqueueCalled.set(true) },
            isEdt = { false },
            isDisposed = { true },
            timeoutMillis = 5_000,
        )

        val ex = assertThrows(IllegalStateException::class.java) {
            edtCall.run { "ok" }
        }
        assertTrue(ex.message!!.contains("IDE_HAND_DISPOSED"))
        assertFalse(enqueueCalled.get())
    }

    @Test
    fun `test disposed while queued throws and work does not run`() {
        val queue = Collections.synchronizedList(mutableListOf<Runnable>())
        var disposed = false
        val workCount = AtomicInteger(0)
        val threadPool = Executors.newSingleThreadExecutor()

        try {
            val edtCall = HandEdtCall(
                enqueue = { queue.add(it) },
                isEdt = { false },
                isDisposed = { disposed },
                timeoutMillis = 5_000,
            )

            val future = threadPool.submit<String> {
                edtCall.run {
                    workCount.incrementAndGet()
                    "ok"
                }
            }

            while (queue.isEmpty()) {
                Thread.yield()
            }

            // 큐 대기 중 프로젝트가 dispose 됨
            disposed = true
            queue.removeAt(0).run()

            val ex = assertThrows(java.util.concurrent.ExecutionException::class.java) {
                future.get(5, TimeUnit.SECONDS)
            }
            assertTrue(ex.cause is IllegalStateException)
            assertTrue(ex.cause!!.message!!.contains("IDE_HAND_DISPOSED"))
            assertEquals(0, workCount.get())
        } finally {
            threadPool.shutdownNow()
        }
    }

    @Test
    fun `test direct execution on EDT runs synchronously without enqueue`() {
        val enqueueCalled = AtomicBoolean(false)
        val edtCall = HandEdtCall(
            enqueue = { enqueueCalled.set(true) },
            isEdt = { true },
            isDisposed = { false },
            timeoutMillis = 5_000,
        )

        val result = edtCall.run { "direct-edt" }
        assertEquals("direct-edt", result)
        assertFalse(enqueueCalled.get())
    }

    @Test
    fun `test direct execution on EDT checks disposed`() {
        val edtCall = HandEdtCall(
            enqueue = { },
            isEdt = { true },
            isDisposed = { true },
            timeoutMillis = 5_000,
        )

        val ex = assertThrows(IllegalStateException::class.java) {
            edtCall.run { "direct-edt" }
        }
        assertTrue(ex.message!!.contains("IDE_HAND_DISPOSED"))
    }

    @Test
    fun `test enqueue rejection throws NOT_STARTED`() {
        val edtCall = HandEdtCall(
            enqueue = { throw java.util.concurrent.RejectedExecutionException("queue closed") },
            isEdt = { false },
            isDisposed = { false },
            timeoutMillis = 5_000,
        )

        val ex = assertThrows(IllegalStateException::class.java) {
            edtCall.run { "ok" }
        }
        assertTrue(ex.message!!.startsWith("IDE_HAND_NOT_STARTED"))
    }

    @Test
    fun `test Hand call and HandServer wrap NOT_STARTED as error true`() {
        val failingIde = object : Hand.Ide {
            override fun show(path: String, line: Int?): String =
                throw IllegalStateException("IDE_HAND_NOT_STARTED: IDE task did not start before the deadline; no editor operation was run.")
            override fun replace(path: String, old: String, new: String, all: Boolean): String =
                throw IllegalStateException("IDE_HAND_NOT_STARTED: IDE task did not start before the deadline; no editor operation was run.")
            override fun problems(path: String?): String =
                throw IllegalStateException("IDE_HAND_NOT_STARTED: IDE task did not start before the deadline; no editor operation was run.")
        }
        val hand = Hand(failingIde)
        val ans = hand.call("show", buildJsonObject { put("path", "foo.kt") })
        assertTrue(ans.error)
        assertTrue(ans.text.startsWith("IDE_HAND_NOT_STARTED"))

        val server = HandServer.start(hand)
        try {
            val conn = (java.net.URI(server.url).toURL().openConnection() as java.net.HttpURLConnection).apply {
                requestMethod = "POST"
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
                setRequestProperty("X-Magi-Hand", server.token)
                outputStream.use {
                    it.write("""{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"show","arguments":{"path":"foo.kt"}}}""".toByteArray())
                }
            }
            assertEquals(200, conn.responseCode)
            val body = conn.inputStream.bufferedReader().readText()
            assertTrue(body.contains(""""isError":true"""))
            assertTrue(body.contains("IDE_HAND_NOT_STARTED"))
        } finally {
            server.close()
        }
    }

    @Test
    fun `test Hand call and HandServer wrap RESULT_UNKNOWN as error true`() {
        val failingIde = object : Hand.Ide {
            override fun show(path: String, line: Int?): String =
                throw IllegalStateException("IDE_HAND_RESULT_UNKNOWN: stopped waiting for the IDE; the operation may still complete. Inspect the editor buffer before retrying.")
            override fun replace(path: String, old: String, new: String, all: Boolean): String =
                throw IllegalStateException("IDE_HAND_RESULT_UNKNOWN: stopped waiting for the IDE; the operation may still complete. Inspect the editor buffer before retrying.")
            override fun problems(path: String?): String =
                throw IllegalStateException("IDE_HAND_RESULT_UNKNOWN: stopped waiting for the IDE; the operation may still complete. Inspect the editor buffer before retrying.")
        }
        val hand = Hand(failingIde)
        val ans = hand.call("apply_edit", buildJsonObject {
            put("path", "foo.kt")
            put("old", "a")
            put("new", "b")
        })
        assertTrue(ans.error)
        assertTrue(ans.text.startsWith("IDE_HAND_RESULT_UNKNOWN"))

        val server = HandServer.start(hand)
        try {
            val conn = (java.net.URI(server.url).toURL().openConnection() as java.net.HttpURLConnection).apply {
                requestMethod = "POST"
                doOutput = true
                setRequestProperty("Content-Type", "application/json")
                setRequestProperty("X-Magi-Hand", server.token)
                outputStream.use {
                    it.write("""{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"apply_edit","arguments":{"path":"foo.kt","old":"a","new":"b"}}}""".toByteArray())
                }
            }
            assertEquals(200, conn.responseCode)
            val body = conn.inputStream.bufferedReader().readText()
            assertTrue(body.contains(""""isError":true"""))
            assertTrue(body.contains("IDE_HAND_RESULT_UNKNOWN"))
        } finally {
            server.close()
        }
    }
}
