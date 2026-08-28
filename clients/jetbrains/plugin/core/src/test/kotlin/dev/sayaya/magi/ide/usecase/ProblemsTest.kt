package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import dev.sayaya.magi.ide.model.Wire
import kotlinx.serialization.json.JsonElement
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 문제 골라내기의 계약.
 *
 * 시험의 입력은 **코어가 실제로 내는 모양**이어야 한다. 오늘 이 저장소에서 한 번 겪었다 — 코어가
 * 안 내는 문자열로 시험을 쓰면 매처를 조일 때 그것만 조용히 죽거나, 더 나쁘게는 조인 매처가 가짜에
 * 맞춰진다. 그래서 아래 JSON 은 `session.Part`·`ToolResult` 의 json 태그를 그대로 쓴다.
 */
class ProblemsTest {

    private fun json(s: String): JsonElement = Wire.json.parseToJsonElement(s)

    private fun result(content: String, isError: Boolean, advisory: Boolean = false, tool: String = "edit") =
        LogEvent(
            seq = 7, type = "part.appended", ts = "2026-08-28T10:00:00Z",
            data = json(
                """{"part":{"kind":"toolResult",
                    "toolCall":{"name":"$tool"},
                    "toolResult":{"content":${Wire.json.encodeToString(kotlinx.serialization.serializer(), content)},
                                  "isError":$isError,"advisory":$advisory}}}"""
            ),
        )

    @Test
    fun `성공한 호출은 문제가 아니다`() {
        assertNull(Problems.of(result("ok", isError = false)))
    }

    @Test
    fun `앵커를 읽으면 어디인지가 붙는다`() {
        val p = Problems.of(result("\n  internal/app/guard.go:441:9: error: undefined: foo", isError = true))
        assertNotNull(p)
        assertEquals("internal/app/guard.go", p!!.where?.path)
        assertEquals(441, p.where?.line)
        assertEquals(9, p.where?.column)
        // 화면은 `실패 <tool>  #<seq>  <ts>` 로 그린다(`MagiToolWindow`). 셋 다 로그 사건에서
        // **옮겨 싣는** 값이라 통째로 비워도 항목은 멀쩡히 뜬다 — `#0` 은 클릭할 자리를 잃은
        // 것이고, 빈 도구 이름은 무엇이 실패했는지를 잃은 것이다.
        assertEquals(7L, p.seq)
        assertEquals("2026-08-28T10:00:00Z", p.at)
        assertEquals("edit", p.tool)
    }

    @Test
    fun `앵커를 못 읽어도 항목은 남는다 — 사라지지도 엉뚱한 데를 가리키지도 않는다`() {
        // 이것이 이 설계의 값이다. 실패 모양이 "안 눌린다"이지 "틀린 줄"이 아니다.
        val p = Problems.of(result("build failed: something went wrong", isError = true))
        assertNotNull(p)
        assertNull(p!!.where)
        assertTrue(p.text.contains("build failed"))
    }

    @Test
    fun `한 일과 실패한 일을 가른다`() {
        // 코어가 실측으로 적어 둔 사고: 파일은 디스크에 있는데 두 화면 다 ✗ 라고 했다.
        assertTrue(Problems.of(result("x.go:1:1: warn: unused", isError = true, advisory = true))!!.advisory)
        assertFalse(Problems.of(result("boom", isError = true))!!.advisory)
    }

    @Test
    fun `카운슬 반대만 고른다`() {
        fun verdict(decision: String) = LogEvent(
            seq = 9, type = "council.verdict", ts = "2026-08-28T10:01:00Z",
            data = json("""{"round":1,"member":"casper","decision":"$decision","feedback":"검사가 없다"}"""),
        )
        assertNull(Problems.dissentOf(verdict("done")))
        val d = Problems.dissentOf(verdict("continue"))
        assertNotNull(d)
        assertEquals("casper", d!!.member)
        assertEquals("검사가 없다", d.why)
        // 반대도 같은 줄에 `#<seq>  <ts>` 를 달고 나간다.
        assertEquals(9L, d.seq)
        assertEquals("2026-08-28T10:01:00Z", d.at)
    }

    @Test
    fun `반대 의견은 문제 목록에 안 섞인다 — 클릭할 자리가 없어서다`() {
        val e = LogEvent(seq = 9, type = "council.verdict", data = json("""{"decision":"continue"}"""))
        assertNull(Problems.of(e))
    }
}
