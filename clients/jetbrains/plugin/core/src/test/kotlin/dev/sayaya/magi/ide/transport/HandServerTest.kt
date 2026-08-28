package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.usecase.Hand
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.net.HttpURLConnection
import java.net.URI

/**
 * 손 서버가 **코어 MCP 클라이언트가 실제로 하는 대화**를 견디는지.
 *
 * 시험이 부르는 넷은 짐작이 아니라 `internal/adapter/mcp/client.go` 가 부르는 그 넷이고, 헤더도
 * `http_transport.go` 가 붙이는 그대로다. 그러지 않으면 시험은 통과하는데 붙지는 않는 서버가
 * 된다 — 오늘 이 저장소에서 그 부류를 여러 번 잡았다.
 *
 * 그리고 그것만으로는 모자라서 [HandInteropTest] 가 진짜 클라이언트를 부른다. 이 파일이 거는 것은
 * **내가 코어를 읽고 옮긴 대화**이고, 옮기면서 틀렸으면 그 틀린 것에 맞춰 통과한다.
 */
class HandServerTest {

    private class FakeIde : Hand.Ide {
        var shown: Pair<String, Int?>? = null
        var edit: List<String>? = null
        override fun show(path: String, line: Int?): String { shown = path to line; return "opened $path" }
        override fun replace(path: String, old: String, new: String, all: Boolean): String {
            edit = listOf(path, old, new, all.toString()); return "replaced in $path"
        }
    }

    private fun post(s: HandServer, body: String, token: String? = null): Pair<Int, String> {
        val c = URI(s.url).toURL().openConnection() as HttpURLConnection
        c.requestMethod = "POST"
        c.doOutput = true
        c.setRequestProperty("Content-Type", "application/json")
        c.setRequestProperty("Accept", "application/json, text/event-stream")
        c.setRequestProperty("X-Magi-Hand", token ?: s.token)
        c.outputStream.use { it.write(body.toByteArray()) }
        val code = c.responseCode
        val text = (if (code < 400) c.inputStream else c.errorStream)?.readBytes()?.decodeToString().orEmpty()
        return code to text
    }

    private fun rpc(s: HandServer, method: String, params: String = "{}", id: String = "1"): JsonObject =
        dev.sayaya.magi.ide.model.Wire.json
            .parseToJsonElement(post(s, """{"jsonrpc":"2.0","id":$id,"method":"$method","params":$params}""").second)
            .jsonObject

    @Test
    fun `코어가 부르는 넷을 다 견딘다`() {
        val ide = FakeIde()
        HandServer.start(Hand(ide)).use { s ->
            // 1. initialize — 클라이언트가 프로토콜 개정을 맞춰 본다
            val init = rpc(s, "initialize")["result"]!!.jsonObject
            assertEquals("2025-06-18", init["protocolVersion"]?.jsonPrimitive?.content)

            // 2. notifications/initialized — 알림이라 id 가 없고, 답이 없어야 한다
            val (code, body) = post(s, """{"jsonrpc":"2.0","method":"notifications/initialized"}""")
            assertEquals(204, code)  // 202 가 아니다 — 코어 전송이 202 를 거절한다
            assertTrue(body.isEmpty(), "알림에 몸을 실어 보내면 클라이언트가 짝 없는 응답을 읽는다")

            // 3. tools/list
            val tools = rpc(s, "tools/list")["result"]!!.jsonObject["tools"]!!.jsonArray
            assertEquals(setOf("show", "apply_edit"), tools.map { it.jsonObject["name"]!!.jsonPrimitive.content }.toSet())
            // 스키마가 있어야 한다 — 없으면 모델이 인자를 지어낸다
            assertTrue(tools.all { it.jsonObject["inputSchema"] != null })

            // 4. tools/call
            val r = rpc(s, "tools/call", """{"name":"show","arguments":{"path":"a.kt","line":"12"}}""")["result"]!!.jsonObject
            assertEquals("a.kt" to 12, ide.shown)
            assertEquals(false, r["isError"]?.jsonPrimitive?.content?.toBoolean())
            assertTrue(r["content"]!!.jsonArray[0].jsonObject["text"]!!.jsonPrimitive.content.contains("a.kt"))
        }
    }

    @Test
    fun `모르는 도구는 거절한다 — 조용히 성공하지 않는다`() {
        HandServer.start(Hand(FakeIde())).use { s ->
            val r = rpc(s, "tools/call", """{"name":"nope","arguments":{}}""")["result"]!!.jsonObject
            assertEquals(true, r["isError"]?.jsonPrimitive?.content?.toBoolean())
        }
    }

    @Test
    fun `인자가 빠지면 거절한다`() {
        HandServer.start(Hand(FakeIde())).use { s ->
            val r = rpc(s, "tools/call", """{"name":"apply_edit","arguments":{"path":"a.kt"}}""")["result"]!!.jsonObject
            assertEquals(true, r["isError"]?.jsonPrimitive?.content?.toBoolean())
        }
    }

    @Test
    fun `토큰이 없으면 안 받는다`() {
        HandServer.start(Hand(FakeIde())).use { s ->
            assertEquals(403, post(s, """{"jsonrpc":"2.0","id":1,"method":"tools/list"}""", token = "wrong").first)
        }
    }

    @Test
    fun `루프백에만 선다`() {
        HandServer.start(Hand(FakeIde())).use { s ->
            assertTrue(s.url.startsWith("http://127.0.0.1:"), "루프백이 아니면 이 머신 밖이 닿는다: ${s.url}")
        }
    }

    @Test
    fun `고치기는 준 글자와 「전부」 를 그대로 넘긴다`() {
        // **성공하는 `apply_edit` 을 지나가는 시험이 없었다.** 있던 것은 인자가 빠졌을 때의 거절
        // 하나뿐이라, 가짜는 넷을 다 적어 두는데 아무도 그 적힌 것을 안 봤다 — 그래서
        // `replaceAll` 판단을 뒤집어도 스위트가 안 죽었다(돌연변이로 재 봤다, 2026-08-29).
        //
        // 이 깃발이 정하는 것은 사람 파일에서 **한 군데를 고치느냐 전부를 고치느냐**다. 뒤집혀도
        // 도구는 성공을 돌려주고 모델은 시킨 대로 됐다고 읽는다. 틀린 것이 드러나는 자리는 사람이
        // 한참 뒤에 자기 파일을 볼 때다.
        val ide = FakeIde()
        HandServer.start(Hand(ide)).use { s ->
            rpc(s, "tools/call",
                """{"name":"apply_edit","arguments":{"path":"a.kt","old":"x","new":"y","replaceAll":true}}""")
            assertEquals(listOf("a.kt", "x", "y", "true"), ide.edit,
                "글자든 깃발이든 하나라도 어긋나면 사람 파일이 시킨 것과 다르게 고쳐진다")

            // 안 주면 한 군데다. 「안 준 것」이 「전부」로 읽히는 쪽이 제일 나쁜 갈래다.
            rpc(s, "tools/call", """{"name":"apply_edit","arguments":{"path":"b.kt","old":"x","new":"y"}}""")
            assertEquals(listOf("b.kt", "x", "y", "false"), ide.edit,
                "안 준 깃발은 거짓이다 — 기본이 「전부」면 한 군데만 고치라는 말을 할 수가 없다")
        }
    }
}
