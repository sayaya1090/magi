package dev.sayaya.magi.ide.transport

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import dev.sayaya.magi.ide.model.Wire
import dev.sayaya.magi.ide.usecase.Hand
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.add
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull
import kotlinx.serialization.json.put
import java.io.Closeable
import java.net.InetAddress
import java.net.InetSocketAddress
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/**
 * IDE 플랫폼 도구(Hand)를 데몬에 노출하는 루프백 HTTP MCP(Model Context Protocol) 서버.
 *
 * 1. 로컬 루프백 인터페이스(127.0.0.1)에만 바인딩한다. 동일 머신의 데몬 프로세스만 호출하면 되므로 외부 네트워크에 노출하지 않는다 (§6).
 *    루프백 포트는 동일 머신의 타 프로세스도 접근 가능하므로, 기동 시 난수 UUID 토큰을 생성하여 `X-Magi-Hand` 요청 헤더를 검증한다.
 * 2. SSE(Server-Sent Events) 대신 단일 HTTP JSON 응답 방식을 채택한다 (`http_transport.go`).
 *    IDE 도구 호출은 요청-응답 쌍으로 완결되며 역방향 스트리밍이 불필요하므로 불필요한 복잡성을 배제한다.
 * 3. 코어 클라이언트가 호출하는 4개 메서드만 구현한다 (`internal/adapter/mcp/client.go`):
 *    `initialize`, `notifications/initialized`, `tools/list`, `tools/call`.
 */
class HandServer internal constructor(
    private val http: HttpServer,
    private val executor: ExecutorService,
    val token: String,
) : Closeable {

    private val closed = AtomicBoolean(false)
    private val admissionLock = Any()

    /** 데몬에게 줄 주소. 0 번 포트로 열고 **실제로 받은 포트**를 읽는다 — 짐작하지 않는다. */
    val url: String get() = "http://127.0.0.1:${http.address.port}/mcp"

    override fun close() {
        val shouldClean = synchronized(admissionLock) {
            closed.compareAndSet(false, true)
        }
        if (!shouldClean) return
        var primary: Throwable? = null
        try {
            http.stop(0)
        } catch (t: Throwable) {
            primary = t
        }
        try {
            executor.shutdown()
        } catch (t: Throwable) {
            if (primary == null) {
                primary = t
            } else if (primary !== t) {
                primary.addSuppressed(t)
            }
        }
        if (primary != null) {
            throw primary
        }
    }

    companion object {
        /** 코어 클라이언트가 말하는 개정. 다르면 그쪽이 거절할 수 있으므로 그대로 맞춘다. */
        private const val PROTOCOL = "2025-06-18"

        fun start(hand: Hand): HandServer = start(
            hand = hand,
            createHttp = { HttpServer.create(InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0) },
            createExecutor = { Executors.newFixedThreadPool(2) },
        )

        internal fun start(
            hand: Hand,
            createHttp: () -> HttpServer = { HttpServer.create(InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0) },
            createExecutor: () -> ExecutorService = { Executors.newFixedThreadPool(2) },
        ): HandServer {
            val token = java.util.UUID.randomUUID().toString()
            val http = createHttp()
            var executor: ExecutorService? = null
            try {
                executor = createExecutor()
                val server = HandServer(http, executor, token)
                http.createContext("/mcp") { ex -> server.handle(hand, ex) }
                http.executor = executor
                http.start()
                return server
            } catch (t: Throwable) {
                var cleanupEx: Throwable? = null
                try {
                    http.stop(0)
                } catch (stopErr: Throwable) {
                    cleanupEx = stopErr
                }
                if (executor != null) {
                    try {
                        executor.shutdown()
                    } catch (shutdownErr: Throwable) {
                        if (cleanupEx == null) {
                            cleanupEx = shutdownErr
                        } else if (cleanupEx !== shutdownErr) {
                            cleanupEx.addSuppressed(shutdownErr)
                        }
                    }
                }
                if (cleanupEx != null && cleanupEx !== t) {
                    t.addSuppressed(cleanupEx)
                }
                throw t
            }
        }
    }

    private data class ResponseData(val code: Int, val body: String)

    internal fun handle(hand: Hand, ex: HttpExchange) {
        try {
            if (closed.get()) return
            var responseId: JsonElement = JsonNull
            val responseData: ResponseData? = try {
                if (ex.requestMethod != "POST") {
                    ResponseData(405, "")
                } else if (ex.requestHeaders.getFirst("X-Magi-Hand") != token) {
                    ResponseData(403, "")
                } else {
                    val body = ex.requestBody.readBytes().decodeToString()
                    if (closed.get()) {
                        null
                    } else {
                        val req = Wire.json.parseToJsonElement(body).jsonObject
                        val rawId = req["id"]
                        if (rawId == null || rawId is JsonNull) {
                            responseId = JsonNull
                        } else if (rawId is JsonPrimitive) {
                            if (rawId.isString) {
                                responseId = rawId
                            } else if (rawId.booleanOrNull == null && rawId.longOrNull != null) {
                                responseId = rawId
                            } else {
                                throw IllegalArgumentException("unsupported id: $rawId")
                            }
                        } else {
                            throw IllegalArgumentException("unsupported id: $rawId")
                        }

                        val permitted = synchronized(admissionLock) {
                            !closed.get()
                        }
                        if (!permitted) {
                            null
                        } else {
                            val answer = dispatch(hand, req["method"]?.jsonPrimitive?.content.orEmpty(), req["params"])
                            if (responseId is JsonNull) {
                                ResponseData(204, "")
                            } else {
                                val json = Wire.json.encodeToString(JsonElement.serializer(), buildJsonObject {
                                    put("jsonrpc", "2.0")
                                    put("id", responseId)
                                    put("result", answer)
                                })
                                ResponseData(200, json)
                            }
                        }
                    }
                }
            } catch (e: Exception) {
                val json = Wire.json.encodeToString(JsonElement.serializer(), buildJsonObject {
                    put("jsonrpc", "2.0")
                    put("id", responseId)
                    put("error", buildJsonObject {
                        put("code", -32603)
                        put("message", e.message ?: "internal error")
                    })
                })
                ResponseData(200, json)
            }

            if (responseData != null) {
                runCatching {
                    send(ex, responseData.code, responseData.body)
                }
            }
        } finally {
            try {
                ex.close()
            } catch (_: Throwable) {}
        }
    }

    private fun dispatch(hand: Hand, method: String, params: JsonElement?): JsonObject = when (method) {
        "initialize" -> buildJsonObject {
            put("protocolVersion", PROTOCOL)
            put("capabilities", buildJsonObject { put("tools", buildJsonObject {}) })
            put("serverInfo", buildJsonObject { put("name", "magi-jetbrains"); put("version", "0.1.0") })
        }
        "notifications/initialized" -> buildJsonObject {}
        "tools/list" -> buildJsonObject {
            put("tools", buildJsonArray {
                hand.tools().forEach { t ->
                    add(buildJsonObject {
                        put("name", t.name); put("description", t.description); put("inputSchema", t.schema)
                        // 코어가 참조하는 도구 속성 선언. 생략 시 기본값(쓰기 작업)으로 간주되어
                        // 단순 조회(show) 작업조차 변경 턴으로 기록되는 부작용을 방지하기 위해 readOnlyHint를 명시한다.
                        put("annotations", buildJsonObject { put("readOnlyHint", t.readOnly) })
                    })
                }
            })
        }
        "tools/call" -> {
            val p = params?.jsonObject
            val a = hand.call(
                p?.get("name")?.jsonPrimitive?.content.orEmpty(),
                p?.get("arguments") as? JsonObject ?: buildJsonObject {},
            )
            buildJsonObject {
                put("content", buildJsonArray {
                    add(buildJsonObject { put("type", "text"); put("text", a.text) })
                })
                put("isError", a.error)
            }
        }
        // 지원하지 않는 메서드 호출 시 예외를 발생시켜 JSON-RPC 프로토콜 에러로 응답한다 (무응답 성공 방지).
        else -> throw IllegalArgumentException("this server does not speak \"$method\"")
    }

    private fun send(ex: HttpExchange, code: Int, body: String) {
        val bytes = body.toByteArray()
        ex.responseHeaders.add("Content-Type", "application/json")
        ex.sendResponseHeaders(code, if (bytes.isEmpty()) -1 else bytes.size.toLong())
        if (bytes.isNotEmpty()) ex.responseBody.use { it.write(bytes) }
    }
}
