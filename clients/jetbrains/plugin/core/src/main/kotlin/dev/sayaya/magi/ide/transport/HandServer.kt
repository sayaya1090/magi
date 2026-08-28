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
 * 손을 내놓는 HTTP 서버 — MCP 를 말한다.
 *
 * **루프백에만 바인드한다.** 이 서버는 이 머신의 데몬 하나만 부르면 되고, 그 밖의 누구에게도
 * 열려 있을 이유가 없다. 소켓과 같은 규율이다(§6: 소켓은 0600 이라 호출자가 곧 머신 주인).
 * 다만 **소켓만큼 좁지는 않다** — 루프백 포트는 이 머신의 다른 프로세스도 닿는다. 그래서 붙일 때
 * 토큰을 헤더로 주고(`AttachMCP` 가 헤더를 받는다) 그것을 확인한다.
 *
 * **SSE 를 안 쓴다.** 코어의 HTTP 전송이 `application/json` 단일 응답과 `text/event-stream` 을
 * 둘 다 받는데(`http_transport.go`), 이쪽이 흘려보낼 것이 없으므로 단일 응답만 낸다. 안 쓰는 길을
 * 구현하면 안 도는 코드가 생기고, 그것은 첫 사용자가 쓸 때 처음 깨진다.
 *
 * 말하는 메서드는 **넷**이다: `initialize` · `notifications/initialized` · `tools/list` ·
 * `tools/call`. 코어 클라이언트가 그 넷만 부른다(`client.go`).
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
            // 토큰을 먼저 본다. 루프백이라도 이 머신의 다른 프로세스가 닿는다.
            if (ex.requestHeaders.getFirst("X-Magi-Hand") != token) return send(ex, 403, "")
            val body = ex.requestBody.readBytes().decodeToString()
            val req = Wire.json.parseToJsonElement(body).jsonObject
            val id = req["id"]
            val answer = dispatch(hand, req["method"]?.jsonPrimitive?.content.orEmpty(), req["params"])
            if (id == null || id is JsonNull) {
                // 알림에는 답이 없다. 몸을 실어 보내면 클라이언트가 짝 없는 응답을 읽는다.
                //
                // **204 다. 202 도 지금은 붙는다 — 바꿀 이유가 없어 두는 것이다.**
                //
                // 처음엔 202 를 냈다. MCP 명세가 받아들여진 알림에 지정하는 코드이고, 명세는 그것을
                // MUST 로 적는다. 그런데 당시 코어의 전송은 200 과 204 만 받아 202 를 에러로 읽었고,
                // 그래서 이 서버를 204 로 내렸다 — 명세가 아니라 상대에 맞춘 것이다.
                //
                // 그 판단의 전제는 틀렸다. 어긴 쪽은 명세를 따른 서버가 아니라 그것을 거절하던
                // 클라이언트였다. 코어가 고쳤고(`http_transport.go` 의 `notify` 가 이제
                // `StatusAccepted` 를 받는다), 202·200·204 셋 다 붙는다.
                //
                // 그래도 204 로 둔다. 셋 다 붙는 마당에 되돌리면 근거 없는 diff 만 하나 는다.
                //
                // 남길 것은 이것이다 — **이 건은 진짜 클라이언트로 붙여 보고서야 나왔다.** 제 시험이
                // 제가 옮긴 대화를 걸었다면 202 도 204 도 영영 통과했을 것이다.
                return send(ex, 204, "")
            }
            send(ex, 200, Wire.json.encodeToString(JsonElement.serializer(), buildJsonObject {
                put("jsonrpc", "2.0")
                put("id", id)
                put("result", answer)
            }))
        } catch (e: Exception) {
            // 프로토콜 오류는 HTTP 오류가 아니다 — 500 을 내면 클라이언트가 전송 고장으로 읽는다.
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
        // 모르는 메서드를 조용히 빈 결과로 답하지 않는다 — 부른 쪽이 성공으로 읽는다.
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
