package dev.sayaya.magi.ide.model

import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

/**
 * 「무엇을 허가하는가」가 화면에 닿는지를 잰다.
 *
 * 이 판정이 없을 때 창은 `args` 를 **한 번도 안 그렸고** `reason` 이 없으면 빈 줄을 그렸다.
 * 남는 것은 굵은 글씨 도구 이름과 허용·거부·항상 셋이었다.
 */
class SubjectTest {

    private fun waiting(args: kotlinx.serialization.json.JsonElement? = null, reason: String? = null) =
        Waiting(id = "c1", kind = "permission", what = "bash", args = args, reason = reason)

    @Test
    fun `인자가 오면 그것이 정해지는 것이다`() {
        // 도구 이름은 요청의 **설명**이다. 정해지는 것은 이쪽이다.
        assertEquals(
            Subject.Stated(args = "\"rm -rf /tmp/x\"", reason = null),
            waiting(args = JsonPrimitive("rm -rf /tmp/x")).subject,
        )
    }

    @Test
    fun `사유는 인자를 대신하지 않는다`() {
        // 둘을 안 합친다 — 하나는 정해지는 것 자체고 하나는 정책이 왜 섰는지다. 합쳐 두면
        // 화면이 어느 쪽을 그리는지 모르게 되고, 인자가 빠진 것도 안 보인다.
        val s = waiting(args = JsonPrimitive("rm"), reason = "쓰기 도구는 물어본다").subject
        assertEquals(Subject.Stated(args = "\"rm\"", reason = "쓰기 도구는 물어본다"), s)
    }

    @Test
    fun `아무것도 안 오면 그렇게 말한다`() {
        // 소켓에서 `args` 는 `omitempty` 라 진짜로 안 온다. 그때 조용하면 사람은 아는 것만
        // 보고 누른다.
        assertEquals(Subject.Unstated, waiting().subject)
        assertEquals(Subject.Unstated, waiting(args = JsonNull, reason = "  ").subject)
    }

    @Test
    fun `빈 Stated 는 만들 수 없다`() {
        // 「빈 요청」과 「말 안 해 준 요청」이 같은 이름을 쓰면 화면이 둘을 같게 그린다.
        assertThrows(IllegalArgumentException::class.java) { Subject.Stated(null, null) }
    }

    /**
     * **승인 화면이 base64 를 그렸다.** 코어가 이 칸을 `[]byte` 로 보내던 동안 encoding/json 이
     * 그것을 base64 문자열로 쌌다 — Go 끼리는 정상 왕복해서 아무도 안 아팠고, **Go 밖 클라이언트만**
     * 그 문자열을 받았다. VS Code 실물에서 쟀다(2026-09-20): 「이 편집을 허용하겠냐」는 카드에
     * 184자의 base64 가 있었고 사람은 바뀔 내용을 못 봤다.
     *
     * 생산자는 고쳤지만 **이미 기록된 로그에는 그 모양이 남는다.** 그래서 읽는 쪽이 둘 다 읽어야
     * 하고, 그 판정을 여기서 잰다. 화면의 `if` 로 두면 소스 글자로밖에 못 재기 때문이다.
     */
    @Test
    fun `인자는 지금 모양도 옛 base64 모양도 읽힌다`() {
        val args = """{"path":"Sample.kt","new":"반갑습니다"}"""
        val inline = kotlinx.serialization.json.Json.parseToJsonElement(args)
        assertEquals(inline.toString(), (waiting(args = inline).subject as Subject.Stated).args)

        val legacy = java.util.Base64.getEncoder().encodeToString(args.toByteArray())
        assertEquals(args, (waiting(args = JsonPrimitive(legacy)).subject as Subject.Stated).args,
            "옛 로그의 base64 가 안 풀렸다 — 고침이 과거를 못 읽게 만들면 그건 다른 결함이다")

        // JSON 도 base64 도 아닌 원문은 **여태 그리던 그대로**(따옴표째). 이 고침의 범위가 아니다.
        assertEquals("\"path=Sample.kt\"",
            (waiting(args = JsonPrimitive("path=Sample.kt")).subject as Subject.Stated).args)

        // ⚠ base64 로 풀리기는 하지만 JSON 이 아닌 것은 **건드리지 않는다** — 그렇게 생겼다는
        // 이유로 사람의 글자를 알아볼 수 없는 것으로 바꾸지 않는다.
        assertEquals("\"deadbeef\"",
            (waiting(args = JsonPrimitive("deadbeef")).subject as Subject.Stated).args)

        assertEquals(Subject.Unstated, waiting(args = JsonNull).subject)
    }
}
