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
import kotlinx.serialization.json.JsonArray
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
        var askedFor: String? = null
        var asked = false
        override fun problems(path: String?): String { askedFor = path; asked = true; return "no errors" }
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

    /**
     * **설명이 안 말하는 선택 인자는 모델에게 없는 기능이다.**
     *
     * 도구 설명은 모델이 읽는 **계약**이다. 필수 인자는 스키마가 강제하니 피할 수 없지만, 선택
     * 인자는 **말해 주지 않으면 보이지 않는다.** 하나가 그랬다(2026-09-10, 두 클라이언트의 손을
     * 칸 단위로 대조): `apply_edit.replaceAll` 은 편의 플래그가 아니라 **애매한 편집이 거절되느냐**를
     * 가르는데(`old` 가 여러 번 나오면 거절하고 "narrow it, or pass replaceAll" 이라 답한다),
     * 설명이 그 이름을 안 댔다 — 모르는 모델은 **실패한 뒤에야** 안다.
     */
    @Test
    fun `선택 인자는 그것을 내놓는 도구의 설명이 말한다`() {
        val tools = Hand(FakeIde()).tools()
        assertTrue(tools.size >= 3, "도구를 ${tools.size}개만 찾았다 — 이 시험이 손을 못 보고 있다")
        for (t in tools) {
            val props = t.schema["properties"]?.jsonObject?.keys.orEmpty()
            assertTrue(props.isNotEmpty(), "${t.name} 의 인자를 하나도 못 읽었다 — 훑기가 죽었다")
            val required = (t.schema["required"] as? kotlinx.serialization.json.JsonArray).orEmpty()
                .mapNotNull { (it as? kotlinx.serialization.json.JsonPrimitive)?.content }.toSet()
            for (arg in props - required) {
                assertTrue(arg in t.description,
                    "${t.name} 이 선택 인자 `$arg` 를 받으면서 설명이 그것을 안 말한다 — " +
                        "스키마가 내놓는 것을 글이 감추면 모델은 안 쓴다")
            }
        }
        assertTrue("replaceAll" in tools.first { it.name == "apply_edit" }.description,
            "애매한 편집이 거절되느냐를 가르는 플래그를 설명이 안 말한다")
    }

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
            assertEquals(
                setOf("show", "apply_edit", "problems"),
                tools.map { it.jsonObject["name"]!!.jsonPrimitive.content }.toSet(),
            )
            // 스키마가 있어야 한다 — 없으면 모델이 인자를 지어낸다
            assertTrue(tools.all { it.jsonObject["inputSchema"] != null })
            // **읽기만 하는지 말해야 한다.** 코어가 이 선언으로 두 가지를 정한다: 창이 닫힐 때
            // 무엇을 먼저 덜어낼지, 그리고 파일을 지목하는 이 도구가 **고치는가 보는가**. 안 실으면
            // 프로토콜 기본값(쓰기)으로 잡혀 `show` 가 「이 턴이 그 파일을 고쳤다」로 기록에 오른다.
            val ro = tools.associate {
                it.jsonObject["name"]!!.jsonPrimitive.content to
                    it.jsonObject["annotations"]?.jsonObject?.get("readOnlyHint")?.jsonPrimitive?.content
            }
            assertEquals("true", ro["show"], "show 는 파일을 안 고친다고 말해야 한다")
            assertEquals("true", ro["problems"], "진단을 읽는 것은 아무것도 안 고친다")
            assertEquals("false", ro["apply_edit"], "apply_edit 는 고친다 — 그래야 기록에 changed 로 오른다")

            // 진단은 **없던 도구**다. 경로를 안 주면 열린 파일 전부라는 것까지 계약이다.
            rpc(s, "tools/call", """{"name":"problems","arguments":{}}""")
            assertTrue(ide.asked, "problems 가 IDE 에 안 물었다")
            assertEquals(null, ide.askedFor, "경로를 안 줬으면 열린 파일 전부여야 한다")

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
