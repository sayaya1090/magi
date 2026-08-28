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
}
