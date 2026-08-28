package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.assertEquals
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

    @Test
    fun `그래서 도구 이름이 예측 가능하다`() {
        assertEquals("mcp__jetbrains__apply_edit", "mcp__${McpName.sanitize(McpName.VALUE)}__apply_edit")
    }

    /** 코어가 다듬는 규칙과 같은 답을 내야 한다(mcp/manager.go 의 sanitizeToolPart). */
    @Test
    fun `다듬기가 코어와 같다`() {
        assertEquals("ppt_one", McpName.sanitize("ppt.one"))
        assertEquals("ppt_one", McpName.sanitize("ppt_one"))   // 둘이 한 이름이 되는 그 자리
        assertEquals("a-b_C9", McpName.sanitize("a-b_C9"))
        assertEquals("__", McpName.sanitize("한글"))
    }

    /** 거절의 갈래가 다르면 사람이 할 일도 다르다. */
    @Test
    fun `두 거절을 갈래로 읽는다`() {
        assertEquals(McpName.Refusal.ALREADY_ATTACHED,
            McpName.refusalOf("""mcp: "jetbrains" is already attached; two servers cannot share one name"""))
        assertEquals(McpName.Refusal.COLLIDES_AFTER_SANITISE,
            McpName.refusalOf("""mcp: "jetbrains" collides with "jet.brains", which is already attached (both become "jetbrains" in tool names)"""))
        assertEquals(McpName.Refusal.OTHER, McpName.refusalOf("mcp: dial tcp: connection refused"))
        assertEquals(McpName.Refusal.OTHER, McpName.refusalOf(null))
    }
}
