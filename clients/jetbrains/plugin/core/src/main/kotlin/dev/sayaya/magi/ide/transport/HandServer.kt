package dev.sayaya.magi.ide.transport

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import dev.sayaya.magi.ide.model.Wire
import dev.sayaya.magi.ide.usecase.Hand
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import java.io.Closeable
import java.net.InetAddress
import java.net.InetSocketAddress

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
class HandServer private constructor(
    private val http: HttpServer,
    val token: String,
) : Closeable {

    /** 데몬에게 줄 주소. 0 번 포트로 열고 **실제로 받은 포트**를 읽는다 — 짐작하지 않는다. */
    val url: String get() = "http://127.0.0.1:${http.address.port}/mcp"

    override fun close() = http.stop(0)

    companion object {
        /** 코어 클라이언트가 말하는 개정. 다르면 그쪽이 거절할 수 있으므로 그대로 맞춘다. */
        private const val PROTOCOL = "2025-06-18"

        fun start(hand: Hand): HandServer {
            val token = java.util.UUID.randomUUID().toString()
            val http = HttpServer.create(InetSocketAddress(InetAddress.getLoopbackAddress(), 0), 0)
            val server = HandServer(http, token)
            http.createContext("/mcp") { ex -> server.handle(hand, ex) }
            http.executor = java.util.concurrent.Executors.newFixedThreadPool(2)
            http.start()
            return server
        }
    }

    private fun handle(hand: Hand, ex: HttpExchange) {
        try {
            if (ex.requestMethod != "POST") return send(ex, 405, "")
            // 보안 검증: 루프백 포트 접근 프로세스의 인증 헤더 확인
            if (ex.requestHeaders.getFirst("X-Magi-Hand") != token) return send(ex, 403, "")
            val body = ex.requestBody.readBytes().decodeToString()
            val req = Wire.json.parseToJsonElement(body).jsonObject
            val id = req["id"]
            val answer = dispatch(hand, req["method"]?.jsonPrimitive?.content.orEmpty(), req["params"])
            if (id == null || id is JsonNull) {
                // 알림(Notification) 메시지는 응답 바디를 반환하지 않는다.
                // HTTP 상태 코드 204 유지 배경:
                // 과거 MCP 명세에 따라 202 Accepted를 반환했으나 초기 코어 클라이언트가 200/204만 허용하여 204로 조정한 이력이 있음.
                // 현재는 코어(`http_transport.go`의 `notify`)가 200, 202, 204를 모두 정상 수용하므로 안정성이 검증된 204 응답을 유지한다.
                return send(ex, 204, "")
            }
            send(ex, 200, Wire.json.encodeToString(JsonElement.serializer(), buildJsonObject {
                put("jsonrpc", "2.0")
                put("id", id)
                put("result", answer)
            }))
        } catch (e: Exception) {
            // JSON-RPC 프로토콜 에러는 HTTP 레벨(500)이 아닌 200 OK 내의 JSON-RPC 에러 객체로 응답하여
            // 클라이언트가 전송 계층 장애로 오인하지 않도록 처리한다.
            send(ex, 200, """{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":${
                Wire.json.encodeToString(kotlinx.serialization.serializer(), e.message ?: "internal error")
            }}}""")
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
        ex.close()
    }
}
