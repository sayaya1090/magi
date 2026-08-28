package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
import kotlinx.serialization.json.*
import org.junit.jupiter.api.Test

class McpNameTest {

    /**
     * 다듬어진 뒤에 달라지는 이름을 고르면, 사람이 도구 목록에서 보는 이름과 allow 룰에 적어야
     * 하는 이름이 갈린다. 이름을 바꾸려는 다음 사람이 여기서 걸린다.
     */
    @Test
    fun `이름은 자기 자신으로 다듬어진다`() {
        assertEquals(McpName.VALUE, McpName.sanitize(McpName.VALUE))
    }

    /**
     * 다듬기와 이름 조립이 코어와 같은 답을 내는지 — **기대값은 Go 가 뱉은 것**이다.
     *
     * 여기 값을 손으로 적었을 때 두 자리가 갈렸고(서로게이트 쌍, 빈 이름) 고치면서 하나 더
     * 틀렸다(`"..."` 를 `"x"` 로 적었는데 `___` 다 — 비허용 문자는 지워지는 게 아니라 바뀐다).
     * 손으로 적은 기대값은 적는 사람이 이미 아는 것만 덮는다.
     */
    @Test
    fun `다듬기와 이름 조립이 골든과 같다`() {
        val g = Json.parseToJsonElement(
            javaClass.getResourceAsStream("/mcpname-golden.json")!!.readBytes().decodeToString()
        ).jsonObject
        val why = g["regenerate"]!!.jsonPrimitive.content

        for (row in g["sanitizeToolPart"]!!.jsonArray) {
            val (input, want) = row.jsonArray.map { it.jsonPrimitive.content }
            assertEquals(want, McpName.sanitize(input), "sanitize(${'$'}input)\n${'$'}why")
        }
        for (row in g["namespacedToolName"]!!.jsonArray) {
            val (server, tool, want) = row.jsonArray.map { it.jsonPrimitive.content }
            assertEquals(want, McpName.toolName(server, tool), "toolName(${'$'}server, ${'$'}tool)\n${'$'}why")
        }
    }

    /** 우리 도구의 이름은 예측 가능해야 한다 — allow 룰이 그 문자열로 쓰인다. */
    @Test
    fun `우리 도구 이름`() {
        assertEquals("mcp__jetbrains__apply_edit", McpName.ours("apply_edit"))
    }

    /**
     * 거절의 갈래가 다르면 사람이 할 일도 다르다.
     *
     * **입력은 코어가 실제로 내는 문장이어야 한다.** 여기 한 번 `\"read\"` 로 적었는데 코틀린
     * raw string 은 이스케이프를 처리하지 않아 백슬래시가 글자로 들어갔다. 코어는 `%q` 라
     * `"read"` 를 낸다. `contains` 가 꼬리만 봐서 통과했지만, 그러면 이 케이스는 **코어가 낸 적
     * 없는 문자열**로 "코어 문장을 갈래로 읽는다"를 주장하게 된다. 매처를 조이는 날 옆 둘은 살고
     * 이것만 죽거나, 더 나쁘게는 조인 매처가 이 가짜에 맞춰진다.
     */
    @Test
    fun `두 거절을 갈래로 읽는다`() {
        assertEquals(McpName.Refusal.ALREADY_ATTACHED,
            McpName.refusalOf("""mcp: "jetbrains" is already attached; two servers cannot share one name"""))
        assertEquals(McpName.Refusal.COLLIDES_AFTER_SANITISE,
            McpName.refusalOf("""mcp: "jetbrains" collides with "jet.brains", which is already attached (both become "jetbrains" in tool names)"""))
        assertEquals(McpName.Refusal.NAME_TAKEN_BY_TOOL,
            McpName.refusalOf(""""read" is the name of a tool this companion already has"""))
        assertEquals(McpName.Refusal.OTHER, McpName.refusalOf("mcp: dial tcp: connection refused"))
        assertEquals(McpName.Refusal.OTHER, McpName.refusalOf(null))
    }
}
