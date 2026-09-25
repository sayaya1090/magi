package dev.sayaya.magi.ide.transport

import dev.sayaya.magi.ide.model.Wire
import dev.sayaya.magi.ide.usecase.Hand
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertThrows
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.longOrNull
import org.junit.jupiter.api.Test
import java.io.File
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
        var callCount = 0
        var shown: Pair<String, Int?>? = null
        var edit: List<String>? = null
        override fun show(path: String, line: Int?): String { callCount++; shown = path to line; return "opened $path" }
        override fun replace(path: String, old: String, new: String, all: Boolean): String {
            callCount++; edit = listOf(path, old, new, all.toString()); return "replaced in $path"
        }
        var askedFor: String? = null
        var asked = false
        override fun problems(path: String?): String { callCount++; askedFor = path; asked = true; return "no errors" }
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
                    requireBoolean(it.jsonObject["annotations"]?.jsonObject?.get("readOnlyHint"), "tool ${it.jsonObject["name"]} annotations.readOnlyHint")
            }
            assertEquals(true, ro["show"], "show 는 파일을 안 고친다고 말해야 한다")
            assertEquals(true, ro["problems"], "진단을 읽는 것은 아무것도 안 고친다")
            assertEquals(false, ro["apply_edit"], "apply_edit 는 고친다 — 그래야 기록에 changed 로 오른다")

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

    /**
     * ★ **빈 `old` 는 편집기에 닿기 전에 거절된다 — 닿으면 파일이 갈린다.**
     *
     * JVM 실측(2026-09-10): `"abc".replace("", "X")` 는 `XaXbXcX` 다 — 글자 **사이마다** 끼워
     * 넣는다. 그리고 `"abc".split("").size - 1` 은 2 이므로 빈 `old` 의 `hits` 는 「길이 - 1」이
     * 되어, `replaceAll` 이 참이면 다중-발견 가드도 **안 걸린다.** 그 조합이 파일을 통째로 갈아
     * 버리고, 도구는 「N 군데 바꿨다」고 **성공을 보고한다.**
     *
     * 스키마가 `old` 를 필수로 두지만 **빈 문자열은 필수를 통과한다.** 그래서 값의 검사가 따로
     * 있어야 한다. 짝인 VS Code 는 같은 자리에서 같은 말로 거절한다.
     */
    @Test
    fun `빈 old 는 편집기에 닿지 않는다`() {
        val ide = FakeIde()
        val a = Hand(ide).call("apply_edit", buildJsonObject {
            put("path", JsonPrimitive("a.kt")); put("old", JsonPrimitive(""))
            put("new", JsonPrimitive("X")); put("replaceAll", JsonPrimitive("true"))
        })
        assertTrue(a.error, "빈 old 가 오류로 돌아오지 않았다: ${a.text}")
        assertTrue("old is empty" in a.text, "짝과 다른 말로 거절한다: ${a.text}")
        assertNull(ide.edit, "빈 old 가 편집기까지 갔다 — 그 자리에서 파일이 갈린다")
    }

    /**
     * **빈 것만 막는다 — 공백만인 것은 아니다.**
     *
     * 변이가 잡았다: `isEmpty` 를 `isBlank` 으로 바꿔도 초록이었다. 공백 네 칸을 탭으로 바꾸는
     * 것은 **정당한 편집**이고, 그것까지 막으면 검사가 문을 닫아 버린다. 빈 것은 파일을 갈지만
     * 공백은 찾을 것이 있는 글자다.
     */
    @Test
    fun `공백만인 old 는 막지 않는다`() {
        val ide = FakeIde()
        val a = Hand(ide).call("apply_edit", buildJsonObject {
            put("path", JsonPrimitive("a.kt")); put("old", JsonPrimitive("    "))
            put("new", JsonPrimitive("\t")); put("replaceAll", JsonPrimitive("true"))
        })
        assertFalse(a.error, "들여쓰기를 바꾸는 편집을 막았다: ${a.text}")
        assertEquals(listOf("a.kt", "    ", "\t", "true"), ide.edit)
    }

    /** 그리고 멀쩡한 `old` 는 그대로 지나간다 — 검사가 문을 닫아 버리면 안 된다. */
    @Test
    fun `멀쩡한 old 는 편집기로 간다`() {
        val ide = FakeIde()
        val a = Hand(ide).call("apply_edit", buildJsonObject {
            put("path", JsonPrimitive("a.kt")); put("old", JsonPrimitive("y!!"))
            put("new", JsonPrimitive("y ?: z")); put("replaceAll", JsonPrimitive("false"))
        })
        assertFalse(a.error, a.text)
        assertEquals(listOf("a.kt", "y!!", "y ?: z", "false"), ide.edit)
    }

    data class CanonicalTool(
        val name: String,
        val readOnly: Boolean,
        val schema: JsonObject,
    )

    private val catalogueFixtureFile: File by lazy {
        val root = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .first { File(it, "clients/test-fixtures/ide_hand_catalogue.json").isFile }
        File(root, "clients/test-fixtures/ide_hand_catalogue.json").canonicalFile
    }

    private fun normalizeJsonElement(element: JsonElement, isRequired: Boolean = false): JsonElement {
        return when (element) {
            is JsonObject -> {
                val sortedMap = element.toSortedMap().mapValues { (k, v) ->
                    normalizeJsonElement(v, isRequired = (k == "required"))
                }
                JsonObject(sortedMap)
            }
            is JsonArray -> {
                if (isRequired) {
                    val sortedList = element.map { it.jsonPrimitive.content }.sorted().map { JsonPrimitive(it) }
                    JsonArray(sortedList)
                } else {
                    JsonArray(element.map { normalizeJsonElement(it, false) })
                }
            }
            else -> element
        }
    }

    private fun requireBoolean(element: JsonElement?, label: String): Boolean {
        check(element != null) { "$label: missing or null" }
        check(element !is JsonNull) { "$label: must not be JsonNull" }
        check(element is JsonPrimitive) { "$label: expected JsonPrimitive but was ${element::class.simpleName}" }
        check(!element.isString) { "$label: boolean must not be encoded as a JSON string" }
        return checkNotNull(element.booleanOrNull) { "$label: invalid boolean literal: $element" }
    }

    private fun normalizeTool(name: String, readOnly: Boolean, schema: JsonObject): CanonicalTool {
        return CanonicalTool(name, readOnly, normalizeJsonElement(schema) as JsonObject)
    }

    private fun parseCatalogue(jsonString: String): List<CanonicalTool> {
        val array = Wire.json.parseToJsonElement(jsonString).jsonArray
        return array.map { el ->
            val obj = el.jsonObject
            val name = obj["name"]!!.jsonPrimitive.content
            val ro = requireBoolean(obj["readOnly"], "tool $name readOnly")
            val schema = obj["schema"]!!.jsonObject
            normalizeTool(name, ro, schema)
        }.sortedBy { it.name }
    }

    private fun parseHttpTools(toolsArray: JsonArray): List<CanonicalTool> {
        return toolsArray.map { el ->
            val obj = el.jsonObject
            val name = obj["name"]!!.jsonPrimitive.content
            val annotations = obj["annotations"]?.jsonObject ?: error("tool $name missing annotations")
            val ro = requireBoolean(annotations["readOnlyHint"], "tool $name annotations.readOnlyHint")
            val schema = obj["inputSchema"]!!.jsonObject
            normalizeTool(name, ro, schema)
        }.sortedBy { it.name }
    }

    @Test
    fun `공유 카탈로그 fixture와 Hand tools가 손실 없이 일치한다`() {
        val expected = parseCatalogue(catalogueFixtureFile.readText())
        val actual = Hand(FakeIde()).tools().map { t ->
            normalizeTool(t.name, t.readOnly, t.schema)
        }.sortedBy { it.name }
        assertEquals(expected, actual)
    }

    @Test
    fun `HTTP tools list가 공유 카탈로그 fixture의 schema와 readOnlyHint를 보존한다`() {
        val expected = parseCatalogue(catalogueFixtureFile.readText())
        HandServer.start(Hand(FakeIde())).use { s ->
            val tools = rpc(s, "tools/list")["result"]!!.jsonObject["tools"]!!.jsonArray
            val actual = parseHttpTools(tools)
            assertEquals(expected, actual)
        }
    }

    @Test
    fun `카탈로그 비교는 누락 도구, 여분 속성, 잘못된 required, 뒤집힌 readOnly 시 실패한다`() {
        val expected = parseCatalogue(catalogueFixtureFile.readText())

        // 1. Missing tool
        val missing = expected.filter { it.name != "show" }
        assertNotEquals(expected, missing)

        // 2. Inverted readOnly
        val inverted = expected.map { if (it.name == "apply_edit") it.copy(readOnly = true) else it }
        assertNotEquals(expected, inverted)

        // 3. Wrong required
        val wrongReq = expected.map {
            if (it.name == "apply_edit") {
                val s = it.schema.toMutableMap()
                s["required"] = JsonArray(listOf(JsonPrimitive("path"), JsonPrimitive("old")))
                it.copy(schema = normalizeJsonElement(JsonObject(s)) as JsonObject)
            } else it
        }
        assertNotEquals(expected, wrongReq)

        // 4. Extra property
        val extraProp = expected.map {
            if (it.name == "show") {
                val props = it.schema["properties"]!!.jsonObject.toMutableMap()
                props["extra"] = buildJsonObject { put("type", "string") }
                val s = it.schema.toMutableMap()
                s["properties"] = JsonObject(props)
                it.copy(schema = normalizeJsonElement(JsonObject(s)) as JsonObject)
            } else it
        }
        assertNotEquals(expected, extraProp)

        // 5. schema.additionalProperties = false 추가 (§6.44.7)
        val extraAdditionalProps = expected.map {
            if (it.name == "show") {
                val s = it.schema.toMutableMap()
                s["additionalProperties"] = JsonPrimitive(false)
                it.copy(schema = normalizeJsonElement(JsonObject(s)) as JsonObject)
            } else it
        }
        assertNotEquals(expected, extraAdditionalProps)

        // 6. path.enum 추가 (§6.44.7)
        val pathEnum = expected.map {
            if (it.name == "show") {
                val props = it.schema["properties"]!!.jsonObject.toMutableMap()
                val pathObj = props["path"]!!.jsonObject.toMutableMap()
                pathObj["enum"] = JsonArray(listOf(JsonPrimitive("a.kt"), JsonPrimitive("b.kt")))
                props["path"] = JsonObject(pathObj)
                val s = it.schema.toMutableMap()
                s["properties"] = JsonObject(props)
                it.copy(schema = normalizeJsonElement(JsonObject(s)) as JsonObject)
            } else it
        }
        assertNotEquals(expected, pathEnum)

        // 7. line.minimum 추가 (§6.44.7)
        val lineMinimum = expected.map {
            if (it.name == "show") {
                val props = it.schema["properties"]!!.jsonObject.toMutableMap()
                val lineObj = props["line"]!!.jsonObject.toMutableMap()
                lineObj["minimum"] = JsonPrimitive(1)
                props["line"] = JsonObject(lineObj)
                val s = it.schema.toMutableMap()
                s["properties"] = JsonObject(props)
                it.copy(schema = normalizeJsonElement(JsonObject(s)) as JsonObject)
            } else it
        }
        assertNotEquals(expected, lineMinimum)

        // 8. apply_edit readOnlyHint / readOnly 누락 및 잘못된 타입 거절 (§6.44.7, §6.44.10)
        assertThrows(IllegalStateException::class.java) {
            val toolObj = buildJsonObject {
                put("name", "apply_edit")
                put("inputSchema", buildJsonObject {})
                put("annotations", buildJsonObject {}) // missing readOnlyHint
            }
            parseHttpTools(JsonArray(listOf(toolObj)))
        }
        assertThrows(IllegalStateException::class.java) {
            val toolObj = buildJsonObject {
                put("name", "apply_edit")
                put("schema", buildJsonObject {})
                // missing readOnly
            }
            parseCatalogue(JsonArray(listOf(toolObj)).toString())
        }

        // §6.44.10 readOnly 및 readOnlyHint boolean 타입 변이 검증 (문자열 "true"/"false", 숫자 0/1, null, 누락)
        val invalidPrimitives: List<JsonElement?> = listOf(
            JsonPrimitive("true"),
            JsonPrimitive("false"),
            JsonPrimitive(0),
            JsonPrimitive(1),
            JsonNull,
            null,
        )
        for (invalid in invalidPrimitives) {
            // 1) parseHttpTools
            assertThrows(IllegalStateException::class.java) {
                val toolObj = buildJsonObject {
                    put("name", "apply_edit")
                    put("inputSchema", buildJsonObject {})
                    put("annotations", buildJsonObject {
                        if (invalid != null) put("readOnlyHint", invalid)
                    })
                }
                parseHttpTools(JsonArray(listOf(toolObj)))
            }
            // 2) parseCatalogue
            assertThrows(IllegalStateException::class.java) {
                val toolObj = buildJsonObject {
                    put("name", "apply_edit")
                    put("schema", buildJsonObject {})
                    if (invalid != null) put("readOnly", invalid)
                }
                parseCatalogue(JsonArray(listOf(toolObj)).toString())
            }
        }

        val invalidNonPrimitives: List<JsonElement> = listOf(
            JsonArray(emptyList()),
            buildJsonObject {},
        )
        for (invalid in invalidNonPrimitives) {
            assertThrows(IllegalStateException::class.java) {
                val toolObj = buildJsonObject {
                    put("name", "apply_edit")
                    put("inputSchema", buildJsonObject {})
                    put("annotations", buildJsonObject {
                        put("readOnlyHint", invalid)
                    })
                }
                parseHttpTools(JsonArray(listOf(toolObj)))
            }
            assertThrows(IllegalStateException::class.java) {
                val toolObj = buildJsonObject {
                    put("name", "apply_edit")
                    put("schema", buildJsonObject {})
                    put("readOnly", invalid)
                }
                parseCatalogue(JsonArray(listOf(toolObj)).toString())
            }
        }

        // true / false 는 정상 통과
        for (validBool in listOf(true, false)) {
            val httpTool = buildJsonObject {
                put("name", "apply_edit")
                put("inputSchema", buildJsonObject {})
                put("annotations", buildJsonObject {
                    put("readOnlyHint", validBool)
                })
            }
            val parsedHttp = parseHttpTools(JsonArray(listOf(httpTool)))
            assertEquals(validBool, parsedHttp.first().readOnly)

            val catTool = buildJsonObject {
                put("name", "apply_edit")
                put("schema", buildJsonObject {})
                put("readOnly", validBool)
            }
            val parsedCat = parseCatalogue(JsonArray(listOf(catTool)).toString())
            assertEquals(validBool, parsedCat.first().readOnly)
        }
    }

    /**
     * §6.46: 오류 응답 시 요청 ID를 보존하고 미등록 method 및 잘못된 params 예외 시 FakeIde를 호출하지 않는다.
     */
    @Test
    fun `오류 응답 시 요청 ID를 보존하고 미등록 method 및 잘못된 params 예외 시 FakeIde를 호출하지 않는다`() {
        val ide = FakeIde()
        HandServer.start(Hand(ide)).use { s ->
            // 1. 미등록 method + 숫자 id=37: HTTP 200, 응답 id=37, error.code=-32603, error.message 존재, result 없음
            val (code1, body1) = post(s, """{"jsonrpc":"2.0","id":37,"method":"unknown_method","params":{}}""")
            assertEquals(200, code1)
            val json1 = Wire.json.parseToJsonElement(body1).jsonObject
            assertEquals(37L, json1["id"]?.jsonPrimitive?.longOrNull)
            assertFalse(json1["id"]!!.jsonPrimitive.isString)
            assertNotNull(json1["error"])
            assertEquals(-32603, json1["error"]!!.jsonObject["code"]?.jsonPrimitive?.intOrNull)
            assertTrue(json1["error"]!!.jsonObject["message"]?.jsonPrimitive?.content?.isNotEmpty() == true)
            assertNull(json1["result"])
            assertEquals(0, ide.callCount)

            // 2. tools/call의 params를 배열로 보내 파싱 이후 예외 발생: 응답 id를 보존하고 error 반환, FakeIde 호출 수 0
            val (code2, body2) = post(s, """{"jsonrpc":"2.0","id":99,"method":"tools/call","params":["not_an_object"]}""")
            assertEquals(200, code2)
            val json2 = Wire.json.parseToJsonElement(body2).jsonObject
            assertEquals(99L, json2["id"]?.jsonPrimitive?.longOrNull)
            assertNotNull(json2["error"])
            assertEquals(-32603, json2["error"]!!.jsonObject["code"]?.jsonPrimitive?.intOrNull)
            assertNull(json2["result"])
            assertEquals(0, ide.callCount)
        }
    }

    /**
     * §6.46: 문자열 ID(따옴표, 역슬래시, 한글 포함), 빈 문자열, 0, 음수 ID는 원래 JSON 값과 타입을 보존한다.
     */
    @Test
    fun `문자열, 0, 음수 ID는 원래 JSON 값과 타입을 보존한다`() {
        val ide = FakeIde()
        HandServer.start(Hand(ide)).use { s ->
            // 문자열 ID (따옴표, 역슬래시, 한글 포함)
            val rawStrId = "\"complex-\\\"id\\\"-\\\\-\\uac00\\ub098\""
            val (code1, body1) = post(s, """{"jsonrpc":"2.0","id":$rawStrId,"method":"tools/list"}""")
            assertEquals(200, code1)
            val json1 = Wire.json.parseToJsonElement(body1).jsonObject
            assertTrue(json1["id"]!!.jsonPrimitive.isString)
            assertEquals("complex-\"id\"-\\-\uac00\ub098", json1["id"]!!.jsonPrimitive.content)

            // 문자열 "37"은 숫자 37로 변환되지 않고 문자열 타입 유지 (성공 경로)
            val (codeStrSucc, bodyStrSucc) = post(s, """{"jsonrpc":"2.0","id":"37","method":"tools/list"}""")
            assertEquals(200, codeStrSucc)
            val jsonStrSucc = Wire.json.parseToJsonElement(bodyStrSucc).jsonObject
            assertTrue(jsonStrSucc["id"]!!.jsonPrimitive.isString)
            assertEquals("37", jsonStrSucc["id"]!!.jsonPrimitive.content)

            // 문자열 "37"은 오류 응답에서도 문자열 타입 유지
            val (codeStrErr, bodyStrErr) = post(s, """{"jsonrpc":"2.0","id":"37","method":"unknown"}""")
            assertEquals(200, codeStrErr)
            val jsonStrErr = Wire.json.parseToJsonElement(bodyStrErr).jsonObject
            assertTrue(jsonStrErr["id"]!!.jsonPrimitive.isString)
            assertEquals("37", jsonStrErr["id"]!!.jsonPrimitive.content)

            // 빈 문자열 ID
            val (codeEmpty, bodyEmpty) = post(s, """{"jsonrpc":"2.0","id":"","method":"unknown"}""")
            assertEquals(200, codeEmpty)
            val jsonEmpty = Wire.json.parseToJsonElement(bodyEmpty).jsonObject
            assertTrue(jsonEmpty["id"]!!.jsonPrimitive.isString)
            assertEquals("", jsonEmpty["id"]!!.jsonPrimitive.content)

            // 숫자 0 ID
            val (codeZero, bodyZero) = post(s, """{"jsonrpc":"2.0","id":0,"method":"unknown"}""")
            assertEquals(200, codeZero)
            val jsonZero = Wire.json.parseToJsonElement(bodyZero).jsonObject
            assertFalse(jsonZero["id"]!!.jsonPrimitive.isString)
            assertEquals(0L, jsonZero["id"]!!.jsonPrimitive.longOrNull)

            // 음수 ID
            val (codeNeg, bodyNeg) = post(s, """{"jsonrpc":"2.0","id":-42,"method":"unknown"}""")
            assertEquals(200, codeNeg)
            val jsonNeg = Wire.json.parseToJsonElement(bodyNeg).jsonObject
            assertFalse(jsonNeg["id"]!!.jsonPrimitive.isString)
            assertEquals(-42L, jsonNeg["id"]!!.jsonPrimitive.longOrNull)
        }
    }

    /**
     * §6.46: 잘린 JSON, 최상위 배열, 지원하지 않는 ID 타입은 error 응답의 id가 null이며 FakeIde 호출은 0회다.
     */
    @Test
    fun `잘린 JSON, 최상위 배열, 지원하지 않는 ID 타입은 error 응답의 id가 null이며 FakeIde 호출은 0회다`() {
        val ide = FakeIde()
        HandServer.start(Hand(ide)).use { s ->
            // 1. 잘린 JSON
            val (c1, b1) = post(s, """{"jsonrpc":"2.0","id":12,"method":""")
            assertEquals(200, c1)
            val j1 = Wire.json.parseToJsonElement(b1).jsonObject
            assertTrue(j1["id"] is JsonNull)
            assertNotNull(j1["error"])

            // 2. 최상위 배열
            val (c2, b2) = post(s, """[{"jsonrpc":"2.0","id":12,"method":"tools/list"}]""")
            assertEquals(200, c2)
            val j2 = Wire.json.parseToJsonElement(b2).jsonObject
            assertTrue(j2["id"] is JsonNull)
            assertNotNull(j2["error"])

            // 3. 지원하지 않는 ID: boolean, float, object, array
            for (unsupported in listOf("true", "false", "12.34", "{}", "[]")) {
                val (c, b) = post(s, """{"jsonrpc":"2.0","id":$unsupported,"method":"tools/list"}""")
                assertEquals(200, c)
                val j = Wire.json.parseToJsonElement(b).jsonObject
                assertTrue(j["id"] is JsonNull, "지원하지 않는 id $unsupported 는 id:null 로 응답해야 함: $b")
                assertNotNull(j["error"])
                assertEquals(-32603, j["error"]!!.jsonObject["code"]?.jsonPrimitive?.intOrNull)
            }

            // FakeIde 호출 0회 확인
            assertEquals(0, ide.callCount)
            assertNull(ide.shown)
            assertNull(ide.edit)
            assertFalse(ide.asked)
        }
    }
}


