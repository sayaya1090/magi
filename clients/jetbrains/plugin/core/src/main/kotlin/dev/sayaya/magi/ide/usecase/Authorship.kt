package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive

/**
 * 이 파일을 어느 턴이 건드렸고, 그 턴은 무엇을 하라는 요청이었나.
 *
 * IDE 의 blame 이 답하는 "누가·어느 커밋"과 **다른 질문**이다. 커밋 하나에 턴 여럿이 들어 있고,
 * 커밋 안 된 편집에는 blame 이 아예 답하지 않는다. 이건 magi 만 답할 수 있다.
 *
 * ## 줄 단위는 거의 못 답한다 — 그리고 그렇게 말한다
 *
 * §5-5 는 "이 **줄**을 어느 턴이 썼나"를 청하는데, 재보니 소리 나게 답할 수 있는 범위가 좁다.
 *
 * - `edit` 은 `at`/`to` 로 줄을 짚을 수 있다(도구 스키마가 그렇게 적는다). 그런데 **뒤에 온 편집이
 *   줄을 밀어낸다.** 같은 파일에 편집이 하나만 더 와도 앞의 `at` 은 지금 파일에서 다른 줄이다.
 * - `old`/`new` 형태로 온 편집에는 줄 번호가 아예 없다.
 * - `write` 는 파일을 통째로 쓴다.
 *
 * 그래서 **마지막 편집이 짚은 범위만** 소리가 난다 — 그 뒤에 아무것도 안 왔으니까. 그 밖은
 * **못 짚는다고 말한다.** §5-5 자신의 규칙이 그것이다: 답을 못 낼 때는 추측하지 않는다. 좁은 답을
 * 넓게 말하는 것이 여기서는 틀린 줄을 가리키는 것이고, 그건 안 하느니만 못하다.
 */
class Authorship {

    /** 한 턴이 이 파일에 한 일. */
    data class Touch(
        val seq: Long,
        val at: String?,
        val tool: String,
        /** 그 턴을 시작한 요청. 못 찾으면 null — 지어내지 않는다. */
        val asked: String?,
        /** 편집이 짚은 줄 범위. `at`/`to` 를 안 실었으면 null. */
        val lines: IntRange?,
    )

    private val touches = LinkedHashMap<String, MutableList<Touch>>()
    private var asking: String? = null

    /** 전사 프레임 하나를 먹인다. 순서대로 와야 한다 — 요청이 그 뒤 편집들의 주인이다. */
    fun feed(e: LogEvent) {
        when (e.type) {
            "prompt.submitted" -> asking = firstText(e)
            "part.appended" -> edit(e)
        }
    }

    private fun firstText(e: LogEvent): String? =
        e.data?.jsonObject?.get("parts")?.jsonArray
            ?.firstNotNullOfOrNull { (it.jsonObject["text"] as? JsonPrimitive)?.takeIf { p -> p.isString }?.content }
            ?.trim()?.takeIf { it.isNotBlank() }

    private fun edit(e: LogEvent) {
        val call = e.data?.jsonObject?.get("part")?.jsonObject?.get("toolCall")?.jsonObject ?: return
        val tool = call["name"]?.jsonPrimitive?.content ?: return
        if (tool !in setOf("edit", "write", "multiedit")) return
        val args = call["args"]?.jsonObject ?: return
        val path = args["path"]?.jsonPrimitive?.content?.takeIf { it.isNotBlank() } ?: return
        val from = args["at"]?.jsonPrimitive?.content?.toIntOrNull()
        val to = args["to"]?.jsonPrimitive?.content?.toIntOrNull() ?: from
        touches.getOrPut(path) { mutableListOf() } +=
            Touch(e.seq, e.ts, tool, asking, if (from != null && to != null) from..to else null)
    }

    /** 이 파일을 건드린 턴들, 온 차례대로. */
    fun of(path: String): List<Touch> = touches[path].orEmpty()

    /**
     * 이 줄을 짚을 수 있나.
     *
     * **마지막 편집이 그 줄을 범위로 짚었을 때만** 답한다. 그 뒤에 아무것도 안 왔으므로 줄이 안
     * 밀렸다는 것이 확실한 유일한 경우다. 그 밖은 null — 부르는 쪽이 "이 줄은 못 짚는다"를 말해야
     * 하고, 파일을 건드린 턴 목록은 [of] 로 여전히 줄 수 있다.
     */
    fun wrote(path: String, line: Int): Touch? =
        touches[path]?.lastOrNull()?.takeIf { it.lines?.contains(line) == true }
}
