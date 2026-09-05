package dev.sayaya.magi.ide.model

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 모르는 물음을 아는 물음으로 넘겨짚지 않는다는 것을 못박는다.
 *
 * 이 시험이 지키는 것은 갈래 셋이 아니라 **셋째 갈래가 있다**는 사실이다. 이전 판은 갈래가 둘이라
 * `permission` 이 아닌 모든 것이 질문이었고, 그러면 코어(`protocol.go` 의 `Waiting.Event`, 반대로
 * `question` 이 아닌 모든 것이 권한)와 같은 물음을 서로 다르게 그린다.
 */
class AskTest {

    private fun w(kind: String, options: List<String>? = null) =
        Waiting(id = "c1", kind = kind, what = "무엇", options = options)

    @Test
    fun `권한은 세 갈래로 간다`() {
        assertEquals(Ask.Permission, w("permission").ask)
    }

    @Test
    fun `질문은 선택지 그대로 간다`() {
        assertEquals(Ask.Choose(listOf("A", "B")), w("question", listOf("A", "B")).ask)
    }

    @Test
    fun `모르는 종류는 질문으로 넘겨짚지 않고 거절한다`() {
        // 이전 판은 여기서 질문 가지로 떨어졌고, 선택지가 비어 있으면 단추 0개짜리 물음이 떴다.
        val ask = w("승인요청", listOf("A", "B")).ask
        assertTrue(ask is Ask.Undrawable, "모르는 종류인데 그렸다: $ask")
        assertTrue((ask as Ask.Undrawable).why.contains("승인요청"), "사유가 종류를 안 밝힌다: ${ask.why}")
    }

    @Test
    fun `선택지 없는 질문도 거절한다 — 딴 파일의 약속에 안 기댄다`() {
        // 이게 오늘 안 나는 이유는 이 파일에 없었다. `askuser.go` 가 2개 미만을 거절해서였다.
        assertTrue(w("question", emptyList()).ask is Ask.Undrawable)
        assertTrue(w("question", null).ask is Ask.Undrawable)
    }
}
