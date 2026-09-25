package dev.sayaya.magi.ide.transport

import com.sun.net.httpserver.Authenticator
import com.sun.net.httpserver.Filter
import com.sun.net.httpserver.Headers
import com.sun.net.httpserver.HttpContext
import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpHandler
import com.sun.net.httpserver.HttpPrincipal
import com.sun.net.httpserver.HttpServer
import dev.sayaya.magi.ide.usecase.Hand
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertSame
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.HttpURLConnection
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.URI
import java.util.concurrent.Callable
import java.util.concurrent.CountDownLatch
import java.util.concurrent.CyclicBarrier
import java.util.concurrent.Executor
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.Future
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

/**
 * HandServer HTTP 서버 및 실행기(ExecutorService) 수명 주기 검증 (§6.45).
 *
 * 1. 실제 루프백 loopback tools/list 요청 후 close 시 executor.isShutdown 및 5초 내 awaitTermination 완료, 종료 후 재연결 실패.
 * 2. 생성 실패(executor 생성, context 등록, start) 시 각각 stop/shutdown 호출 횟수 및 예외/suppressed 체이닝 검증.
 * 3. 정상 및 동시 중복 close에서 stop/shutdown 각 1회 호출, stop/shutdown 실패 시에도 2회째 close 추가 실행 0회.
 * 4. 실행 중인 작업이 있어도 close는 지연 없이 즉시 복귀하며 worker는 interrupt되지 않고 작업 해제 시 정상 종료.
 * 5. 닫힌 서버 및 바디 읽기 중 닫힌 서버는 도구 실행을 0회로 차단하고, 이미 시작된 작업은 1회 완료 보장.
 * 6. 응답 전송 예외 발생 시 재전송 시도 없이 exchange가 안전하게 닫히며 인증/메서드/알림/호출 계약 유지.
 */
class HandServerLifecycleTest {

    private open class FakeHttpServer(
        val bindAddress: InetSocketAddress = InetSocketAddress(InetAddress.getLoopbackAddress(), 0)
    ) : HttpServer() {
        val stopCalls = AtomicInteger(0)
        var stopException: Throwable? = null
        var startException: Throwable? = null
        var createContextException: Throwable? = null
        var capturedHandler: HttpHandler? = null
        var exec: Executor? = null

        override fun bind(addr: InetSocketAddress?, backlog: Int) {}
        override fun start() {
            startException?.let { throw it }
        }
        override fun setExecutor(executor: Executor?) {
            this.exec = executor
        }
        override fun getExecutor(): Executor? = exec
        override fun stop(delay: Int) {
            stopCalls.incrementAndGet()
            stopException?.let { throw it }
        }
        override fun createContext(path: String?, handler: HttpHandler?): HttpContext {
            createContextException?.let { throw it }
            capturedHandler = handler
            return object : HttpContext() {
                override fun getHandler(): HttpHandler? = capturedHandler
                override fun setHandler(h: HttpHandler?) { capturedHandler = h }
                override fun getPath(): String = path.orEmpty()
                override fun getServer(): HttpServer = this@FakeHttpServer
                override fun getAttributes(): MutableMap<String, Any> = mutableMapOf()
                override fun getFilters(): MutableList<Filter> = mutableListOf()
                override fun getAuthenticator(): Authenticator? = null
                override fun setAuthenticator(auth: Authenticator?): Authenticator? = null
            }
        }
        override fun createContext(path: String?): HttpContext = createContext(path, null)
        override fun removeContext(path: String?) {}
        override fun removeContext(context: HttpContext?) {}
        override fun getAddress(): InetSocketAddress = bindAddress
    }

    private class RecordingExecutorService(
        private val delegate: ExecutorService = Executors.newFixedThreadPool(2)
    ) : ExecutorService by delegate {
        val shutdownCalls = AtomicInteger(0)
        var shutdownException: Throwable? = null

        override fun shutdown() {
            shutdownCalls.incrementAndGet()
            shutdownException?.let { throw it }
            delegate.shutdown()
        }
    }

    private class FakeHttpExchange(
        private val method: String = "POST",
        headers: Map<String, String> = emptyMap(),
        private val requestBodyBytes: ByteArray = ByteArray(0),
        private val requestInputStream: InputStream? = null,
        val throwOnSendHeaders: Boolean = false,
        val throwOnResponseBody: Boolean = false,
    ) : HttpExchange() {
        private val reqHeaders = Headers().apply {
            headers.forEach { (k, v) -> add(k, v) }
        }
        private val respHeaders = Headers()
        private val respBody = ByteArrayOutputStream()
        private var code: Int = -1
        val closeCalls = AtomicInteger(0)
        val sendHeaderCalls = AtomicInteger(0)

        override fun getRequestHeaders(): Headers = reqHeaders
        override fun getResponseHeaders(): Headers = respHeaders
        override fun getRequestURI(): URI = URI.create("/mcp")
        override fun getRequestMethod(): String = method
        override fun getHttpContext(): HttpContext? = null
        override fun close() {
            closeCalls.incrementAndGet()
        }
        override fun getRequestBody(): InputStream = requestInputStream ?: ByteArrayInputStream(requestBodyBytes)
        override fun getResponseBody(): OutputStream {
            if (throwOnResponseBody) throw IOException("broken pipe on body write")
            return respBody
        }
        override fun sendResponseHeaders(rCode: Int, responseLength: Long) {
            sendHeaderCalls.incrementAndGet()
            if (throwOnSendHeaders) throw IOException("broken pipe on sendResponseHeaders")
            code = rCode
        }
        override fun getRemoteAddress(): InetSocketAddress = InetSocketAddress(InetAddress.getLoopbackAddress(), 12345)
        override fun getResponseCode(): Int = code
        override fun getLocalAddress(): InetSocketAddress = InetSocketAddress(InetAddress.getLoopbackAddress(), 8080)
        override fun getProtocol(): String = "HTTP/1.1"
        override fun getAttribute(name: String?): Any? = null
        override fun setAttribute(name: String?, value: Any?) {}
        override fun setStreams(i: InputStream?, o: OutputStream?) {}
        override fun getPrincipal(): HttpPrincipal? = null

        val responseBodyText: String get() = respBody.toByteArray().decodeToString()
    }

    private class TrackingIde(
        private val onCall: () -> Unit = {}
    ) : Hand.Ide {
        val calls = AtomicInteger(0)
        override fun show(path: String, line: Int?): String {
            calls.incrementAndGet()
            onCall()
            return "opened $path"
        }
        override fun replace(path: String, old: String, new: String, all: Boolean): String {
            calls.incrementAndGet()
            onCall()
            return "replaced $path"
        }
        override fun problems(path: String?): String {
            calls.incrementAndGet()
            onCall()
            return "no problems"
        }
    }

    private fun post(s: HandServer, json: String): Pair<Int, String> {
        val c = URI(s.url).toURL().openConnection() as HttpURLConnection
        c.connectTimeout = 3000
        c.readTimeout = 3000
        c.requestMethod = "POST"
        c.setRequestProperty("Content-Type", "application/json")
        c.setRequestProperty("X-Magi-Hand", s.token)
        c.doOutput = true
        c.outputStream.use { it.write(json.toByteArray()) }
        val code = c.responseCode
        val text = (if (code < 400) c.inputStream else c.errorStream)?.readBytes()?.decodeToString().orEmpty()
        return code to text
    }

    @Test
    fun `1 실제 executor를 보관하여 tools list 실행 후 종료와 awaitTermination을 확인한다`() {
        var capturedExecutor: ExecutorService? = null
        var server: HandServer? = null
        try {
            val ide = TrackingIde()
            server = HandServer.start(
                hand = Hand(ide),
                createExecutor = {
                    val pool = Executors.newFixedThreadPool(2)
                    capturedExecutor = pool
                    pool
                }
            )
            val exec = checkNotNull(capturedExecutor)
            val (code, body) = post(server, """{"jsonrpc":"2.0","id":1,"method":"tools/list"}""")
            assertEquals(200, code)
            assertTrue(body.contains("apply_edit"))

            // close 호출
            server.close()
            assertTrue(exec.isShutdown, "server.close() must shut down the executor")
            assertTrue(exec.awaitTermination(5, TimeUnit.SECONDS), "executor must terminate within 5 seconds")

            // 종료 후 새 HTTP 연결 실패 검증
            assertThrows(Exception::class.java) {
                post(server, """{"jsonrpc":"2.0","id":2,"method":"tools/list"}""")
            }
        } finally {
            server?.runCatching { close() }
            capturedExecutor?.runCatching {
                shutdownNow()
                awaitTermination(5, TimeUnit.SECONDS)
            }
        }
    }

    @Test
    fun `2 생성 중 executor 생성, context 등록, start 실패 시 각각 정리 횟수와 suppressed를 확인한다`() {
        val ide = TrackingIde()

        // 2a. createExecutor 실패
        val http1 = FakeHttpServer()
        val ex1 = assertThrows(RuntimeException::class.java) {
            HandServer.start(
                hand = Hand(ide),
                createHttp = { http1 },
                createExecutor = { throw RuntimeException("executor_fail") }
            )
        }
        assertEquals("executor_fail", ex1.message)
        assertEquals(1, http1.stopCalls.get(), "http.stop(0) must be called once when executor creation fails")

        // 2a-2. createExecutor 실패 + http.stop 도 예외
        val http1b = FakeHttpServer().apply { stopException = IOException("stop_fail") }
        val ex1b = assertThrows(RuntimeException::class.java) {
            HandServer.start(
                hand = Hand(ide),
                createHttp = { http1b },
                createExecutor = { throw RuntimeException("executor_fail") }
            )
        }
        assertEquals("executor_fail", ex1b.message)
        assertEquals(1, ex1b.suppressed.size)
        assertEquals("stop_fail", ex1b.suppressed[0].message)

        // 2b. createContext 실패
        val http2 = FakeHttpServer().apply { createContextException = RuntimeException("context_fail") }
        val rec2 = RecordingExecutorService()
        val ex2 = assertThrows(RuntimeException::class.java) {
            HandServer.start(
                hand = Hand(ide),
                createHttp = { http2 },
                createExecutor = { rec2 }
            )
        }
        assertEquals("context_fail", ex2.message)
        assertEquals(1, http2.stopCalls.get(), "http.stop(0) must be called once on context registration failure")
        assertEquals(1, rec2.shutdownCalls.get(), "executor.shutdown() must be called once on context registration failure")

        // 2b-2. createContext 실패 + stop/shutdown 모두 예외
        val http2b = FakeHttpServer().apply {
            createContextException = RuntimeException("context_fail")
            stopException = IOException("stop_fail")
        }
        val rec2b = RecordingExecutorService().apply {
            shutdownException = IOException("shutdown_fail")
        }
        val ex2b = assertThrows(RuntimeException::class.java) {
            HandServer.start(
                hand = Hand(ide),
                createHttp = { http2b },
                createExecutor = { rec2b }
            )
        }
        assertEquals("context_fail", ex2b.message)
        assertEquals(1, ex2b.suppressed.size)
        assertEquals("stop_fail", ex2b.suppressed[0].message)
        assertEquals(1, ex2b.suppressed[0].suppressed.size)
        assertEquals("shutdown_fail", ex2b.suppressed[0].suppressed[0].message)

        // 2c. start 실패
        val http3 = FakeHttpServer().apply { startException = RuntimeException("start_fail") }
        val rec3 = RecordingExecutorService()
        val ex3 = assertThrows(RuntimeException::class.java) {
            HandServer.start(
                hand = Hand(ide),
                createHttp = { http3 },
                createExecutor = { rec3 }
            )
        }
        assertEquals("start_fail", ex3.message)
        assertEquals(1, http3.stopCalls.get(), "http.stop(0) must be called once on start failure")
        assertEquals(1, rec3.shutdownCalls.get(), "executor.shutdown() must be called once on start failure")
    }

    @Test
    fun `3 정상 및 동시 중복 close에서 stop과 shutdown이 각 1회이며 실패 시에도 2회째 close는 실행하지 않는다`() {
        val ide = TrackingIde()

        // 3a. 정상 close 및 2회차 멱등성
        val http1 = FakeHttpServer()
        val rec1 = RecordingExecutorService()
        val server1 = HandServer.start(
            hand = Hand(ide),
            createHttp = { http1 },
            createExecutor = { rec1 }
        )
        server1.close()
        assertEquals(1, http1.stopCalls.get())
        assertEquals(1, rec1.shutdownCalls.get())

        // 2회차 close는 아무것도 하지 않음 (추가 실행 0회)
        server1.close()
        assertEquals(1, http1.stopCalls.get())
        assertEquals(1, rec1.shutdownCalls.get())

        // 3b. CyclicBarrier 기반 동시 close 경합 (정확히 1회만 실행)
        val http2 = FakeHttpServer()
        val rec2 = RecordingExecutorService()
        val server2 = HandServer.start(
            hand = Hand(ide),
            createHttp = { http2 },
            createExecutor = { rec2 }
        )
        val threadCount = 8
        val barrier = CyclicBarrier(threadCount)
        val pool = Executors.newFixedThreadPool(threadCount)
        val futures = (0 until threadCount).map {
            pool.submit(Callable {
                barrier.await()
                server2.close()
            })
        }
        futures.forEach { it.get(5, TimeUnit.SECONDS) }
        pool.shutdown()
        pool.awaitTermination(5, TimeUnit.SECONDS)

        assertEquals(1, http2.stopCalls.get(), "Concurrent close must call stop exactly once")
        assertEquals(1, rec2.shutdownCalls.get(), "Concurrent close must call shutdown exactly once")

        // 3c. stop이 예외를 던져도 shutdown 1회 보장
        val http3 = FakeHttpServer().apply { stopException = IOException("stop_error") }
        val rec3 = RecordingExecutorService()
        val server3 = HandServer.start(
            hand = Hand(ide),
            createHttp = { http3 },
            createExecutor = { rec3 }
        )
        val ex3 = assertThrows(IOException::class.java) { server3.close() }
        assertEquals("stop_error", ex3.message)
        assertEquals(1, http3.stopCalls.get())
        assertEquals(1, rec3.shutdownCalls.get())

        // 3d. 두 정리 모두 실패해도 두 번째 close는 추가 실행 0회
        val http4 = FakeHttpServer().apply { stopException = IOException("stop_error") }
        val rec4 = RecordingExecutorService().apply { shutdownException = IOException("shutdown_error") }
        val server4 = HandServer.start(
            hand = Hand(ide),
            createHttp = { http4 },
            createExecutor = { rec4 }
        )
        val ex4 = assertThrows(IOException::class.java) { server4.close() }
        assertEquals("stop_error", ex4.message)
        assertEquals(1, ex4.suppressed.size)
        assertEquals("shutdown_error", ex4.suppressed[0].message)

        // 두 번째 close는 이미 closed=true이므로 추가 실행 0회 및 예외 없음
        server4.close()
        assertEquals(1, http4.stopCalls.get())
        assertEquals(1, rec4.shutdownCalls.get())
    }

    @Test
    fun `4 실행 중인 작업이 있어도 close는 지연 없이 복귀하고 worker는 interrupt되지 않는다`() {
        val pool = Executors.newFixedThreadPool(2)
        val taskStartedLatch = CountDownLatch(1)
        val taskBlockLatch = CountDownLatch(1)
        val closeDoneLatch = CountDownLatch(1)
        val taskCompletedLatch = CountDownLatch(1)
        val workerInterrupted = AtomicBoolean(false)

        val http = FakeHttpServer()
        val server = HandServer.start(
            hand = Hand(TrackingIde()),
            createHttp = { http },
            createExecutor = { pool }
        )

        try {
            // 실행기 안에 블로킹된 작업 제출
            pool.submit {
                taskStartedLatch.countDown()
                try {
                    taskBlockLatch.await()
                } finally {
                    workerInterrupted.set(Thread.currentThread().isInterrupted)
                    taskCompletedLatch.countDown()
                }
            }

            assertTrue(taskStartedLatch.await(5, TimeUnit.SECONDS), "task must start running")

            // 별도 스레드에서 close 실행
            val closeThread = Thread {
                server.close()
                closeDoneLatch.countDown()
            }
            closeThread.start()

            // close 완료 신호가 작업 해제(taskBlockLatch) 전에 먼저 도착함을 검증
            assertTrue(closeDoneLatch.await(5, TimeUnit.SECONDS), "server.close() must return without waiting for tasks")
            closeThread.join(5000)

            // 작업 해제 전까지 worker가 interrupt되지 않았는지 확인
            assertFalse(workerInterrupted.get(), "worker thread must not be interrupted by close()")
        } finally {
            taskBlockLatch.countDown()
            taskCompletedLatch.await(5, TimeUnit.SECONDS)
            server.runCatching { close() }
            pool.shutdownNow()
            pool.awaitTermination(5, TimeUnit.SECONDS)
        }
    }

    @Test
    fun `5 닫힌 서버와 읽기 도중 닫힌 서버는 도구 실행을 차단하고 이미 시작된 작업은 완료를 허용한다`() {
        // 5a. 닫힌 서버의 handler를 fake exchange로 호출 시 hand.call 0회
        val ide5a = TrackingIde()
        val http5a = FakeHttpServer()
        val server5a = HandServer.start(
            hand = Hand(ide5a),
            createHttp = { http5a },
            createExecutor = { Executors.newFixedThreadPool(2) }
        )
        val handler5a = checkNotNull(http5a.capturedHandler)
        server5a.close()

        val reqJson = """{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"show","arguments":{"path":"a.kt"}}}"""
        val ex5a = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server5a.token),
            requestBodyBytes = reqJson.toByteArray()
        )
        handler5a.handle(ex5a)
        assertEquals(0, ide5a.calls.get(), "Closed server must not dispatch to hand.call")
        assertEquals(1, ex5a.closeCalls.get(), "Exchange must be closed in finally")

        // 5b. body 읽기를 latch로 막고 close 후 읽기를 풀어도 0회
        val ide5b = TrackingIde()
        val http5b = FakeHttpServer()
        val server5b = HandServer.start(
            hand = Hand(ide5b),
            createHttp = { http5b },
            createExecutor = { Executors.newFixedThreadPool(2) }
        )
        val handler5b = checkNotNull(http5b.capturedHandler)

        val readStartedLatch = CountDownLatch(1)
        val readUnblockLatch = CountDownLatch(1)
        val blockingInputStream = object : InputStream() {
            private val delegate = ByteArrayInputStream(reqJson.toByteArray())
            override fun read(): Int {
                readStartedLatch.countDown()
                readUnblockLatch.await()
                return delegate.read()
            }
            override fun read(b: ByteArray, off: Int, len: Int): Int {
                readStartedLatch.countDown()
                readUnblockLatch.await()
                return delegate.read(b, off, len)
            }
        }

        val ex5b = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server5b.token),
            requestInputStream = blockingInputStream
        )

        val handleThread = Thread {
            handler5b.handle(ex5b)
        }
        handleThread.start()

        try {
            assertTrue(readStartedLatch.await(5, TimeUnit.SECONDS), "Body reading must start")
            server5b.close()
        } finally {
            readUnblockLatch.countDown()
            handleThread.join(5000)
        }

        assertEquals(0, ide5b.calls.get(), "Request whose body finished reading after close must not dispatch")
        assertEquals(1, ex5b.closeCalls.get())

        // 5c. FakeIde 실행 시작을 확인한 후 close한 경우에는 작업 해제 뒤 1회 완료 허용
        val ideStartedLatch = CountDownLatch(1)
        val ideBlockLatch = CountDownLatch(1)
        val ide5c = TrackingIde {
            ideStartedLatch.countDown()
            ideBlockLatch.await()
        }
        val http5c = FakeHttpServer()
        val server5c = HandServer.start(
            hand = Hand(ide5c),
            createHttp = { http5c },
            createExecutor = { Executors.newFixedThreadPool(2) }
        )
        val handler5c = checkNotNull(http5c.capturedHandler)

        val ex5c = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server5c.token),
            requestBodyBytes = reqJson.toByteArray()
        )

        val callThread = Thread {
            handler5c.handle(ex5c)
        }
        callThread.start()

        try {
            assertTrue(ideStartedLatch.await(5, TimeUnit.SECONDS), "IDE work must start before close")
            server5c.close()
        } finally {
            ideBlockLatch.countDown()
            callThread.join(5000)
        }

        assertEquals(1, ide5c.calls.get(), "Work started before close must complete exactly once")
        assertEquals(1, ex5c.closeCalls.get())
    }

    @Test
    fun `6 응답 전송 예외 시 재전송 시도 없이 exchange가 닫히며 인증과 메서드 계약이 유지된다`() {
        val ide = TrackingIde()
        val http = FakeHttpServer()
        val server = HandServer.start(
            hand = Hand(ide),
            createHttp = { http },
            createExecutor = { Executors.newFixedThreadPool(2) }
        )
        val handler = checkNotNull(http.capturedHandler)

        // 6a. sendResponseHeaders 예외 발생 시 재전송 없이 exchange.close() 1회 호출
        val reqJson = """{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"show","arguments":{"path":"a.kt"}}}"""
        val exFail = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server.token),
            requestBodyBytes = reqJson.toByteArray(),
            throwOnSendHeaders = true
        )
        handler.handle(exFail)
        assertEquals(1, exFail.sendHeaderCalls.get(), "sendResponseHeaders must be attempted only once")
        assertEquals(1, exFail.closeCalls.get(), "exchange.close() must be called in outer finally")

        // 6b. 인증 실패 (403)
        val exAuth = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to "wrong_token"),
            requestBodyBytes = reqJson.toByteArray()
        )
        handler.handle(exAuth)
        assertEquals(403, exAuth.responseCode)
        assertEquals(1, exAuth.closeCalls.get())

        // 6c. 메서드 불일치 (405)
        val exMethod = FakeHttpExchange(
            method = "GET",
            headers = mapOf("X-Magi-Hand" to server.token),
            requestBodyBytes = reqJson.toByteArray()
        )
        handler.handle(exMethod)
        assertEquals(405, exMethod.responseCode)
        assertEquals(1, exMethod.closeCalls.get())

        // 6d. 알림 (204)
        val notifJson = """{"jsonrpc":"2.0","method":"notifications/initialized"}"""
        val exNotif = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server.token),
            requestBodyBytes = notifJson.toByteArray()
        )
        handler.handle(exNotif)
        assertEquals(204, exNotif.responseCode)
        assertEquals(1, exNotif.closeCalls.get())

        // 6e. 정상 tools/list (200)
        val listJson = """{"jsonrpc":"2.0","id":1,"method":"tools/list"}"""
        val exList = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server.token),
            requestBodyBytes = listJson.toByteArray()
        )
        handler.handle(exList)
        assertEquals(200, exList.responseCode)
        assertTrue(exList.responseBodyText.contains("show"))
        assertEquals(1, exList.closeCalls.get())

        // 6f. 정상 tools/call (200)
        val exCall = FakeHttpExchange(
            headers = mapOf("X-Magi-Hand" to server.token),
            requestBodyBytes = reqJson.toByteArray()
        )
        handler.handle(exCall)
        assertEquals(200, exCall.responseCode)
        assertTrue(exCall.responseBodyText.contains("isError"))
        assertEquals(1, exCall.closeCalls.get())

        server.close()
    }
}
