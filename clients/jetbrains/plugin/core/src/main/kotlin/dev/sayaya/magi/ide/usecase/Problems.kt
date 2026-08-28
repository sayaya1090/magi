package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.LogEvent
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonPrimitive

/**
 * 컴패니언이 겪은 문제를 전사에서 골라낸다.
 *
 * **IDE 가 아는 것을 다시 말하지 않는다.** IntelliJ 는 자기 인스펙션과 자기 빌드를 이미 그린다.
 * 여기 값은 그 목록에 없는 것들이다 — **컴패니언 자신의 실행**에서 나온 것, 그리고 사람이 안 돌린
 * 시각의 것. 그래서 항목마다 **언제·어느 호출**인지가 같이 간다(설계 문서 §5-4: 낡은 문제 목록은
 * 없느니만 못하다).
 *
 * ## 왜 거터에 안 다는가
 *
 * §5-4 는 거터를 말하지만 재보니 두 원천 중 어느 쪽도 거터에 맞지 않는다.
 *
 * - **카운슬의 반대 의견에는 파일이 없다.** `CouncilVerdictData` 는 `round`·`member`·`lens`·
 *   `decision`·`rationale`·`cite` 를 싣지 경로를 안 싣는다. 걸 자리가 없다.
 * - **빌드·테스트 출력은 산문이다.** 툴체인마다 모양이 달라 일반 파서는 끝이 없고, **틀린 마커는
 *   없는 마커보다 나쁘다** — §5-4 자신이 그렇게 적는다.
 *
 * 그래서 여기는 **앵커만** 읽는다. 못 읽으면 항목이 클릭이 안 될 뿐 사라지지도, 틀린 줄을 가리키지도
 * 않는다. 실패 모양이 "안 눌린다"이지 "엉뚱한 데를 가리킨다"가 아닌 것이 이 선택의 값이다.
 */
object Problems {

    /**
     * 한 건.
     *
     * [advisory] 가 이 목록의 핵심 구분이다. 코어의 `ToolResult.Advisory` 주석이 사유를 적는다 —
     * 파일을 **쓴 뒤** 언어 서버가 한 지적은 `IsError` 를 달지만 그 일은 **일어났다.** 둘을 같은
     * ✗ 로 그렸더니 디스크에 파일이 있는데 화면이 실패라고 했다(코어가 실측으로 적어 둔 사고).
     */
    data class Problem(
        val seq: Long,
        val at: String?,
        val tool: String?,
        val advisory: Boolean,
        val text: String,
        /** `경로:줄[:칸]` 을 읽어냈으면 그것. 못 읽었으면 null — 그래도 항목은 남는다. */
        val where: Where?,
    )

    data class Where(val path: String, val line: Int, val column: Int?)

    /**
     * `path:line:col:` 만 읽는다. magi 의 진단은 이 모양을 **한 자리에서만** 만든다
     * (`lspdiagnose.go` 의 `Fprintf`), 그래서 이건 임의의 컴파일러 출력을 해석하는 것이 아니라
     * 알려진 한 함수의 역이다. 빌드 출력이 우연히 같은 모양이면 덤으로 걸리고, 아니면 안 걸린다.
     */
    private val anchor = Regex("""(?m)^\s*([^\s:][^:\n]*\.[A-Za-z0-9]+):(\d+)(?::(\d+))?:""")

    /** 이 이벤트가 문제인가. 아니면 null. */
    fun of(e: LogEvent): Problem? {
        if (e.type != "part.appended") return null
        val part = e.data?.jsonObject?.get("part")?.jsonObject ?: return null
        val res = part["toolResult"]?.jsonObject ?: return null
        val isError = res["isError"]?.jsonPrimitive?.content == "true"
        if (!isError) return null
        // 값을 읽지 표현을 읽지 않는다. `content` 는 코어에서 `json.RawMessage` 라 보통 JSON
        // 문자열인데, `toString()` 하면 따옴표와 이스케이프까지 딸려 와 `\n` 이 진짜 줄바꿈이
        // 아니게 된다 — 그러면 줄머리를 찾는 앵커가 영영 안 맞는다. 시험이 이것을 잡았다.
        val raw = res["content"]
        val text = ((raw as? JsonPrimitive)?.takeIf { it.isString }?.content) ?: raw?.toString().orEmpty()
        val m = anchor.find(text)
        return Problem(
            seq = e.seq,
            at = e.ts,
            tool = part["toolCall"]?.jsonObject?.get("name")?.jsonPrimitive?.content,
            advisory = res["advisory"]?.jsonPrimitive?.content == "true",
            text = text,
            where = m?.let {
                Where(it.groupValues[1], it.groupValues[2].toInt(), it.groupValues[3].toIntOrNull())
            },
        )
    }

    /**
     * 카운슬이 반대한 것. 파일이 없으므로 [Problem] 이 아니라 따로 낸다 — 같은 목록에 섞으면
     * "클릭하면 어디로 가지"라는 질문이 답 없이 생긴다.
     */
    data class Dissent(val seq: Long, val at: String?, val member: String, val why: String)

    fun dissentOf(e: LogEvent): Dissent? {
        if (e.type != "council.verdict") return null
        val d = e.data?.jsonObject ?: return null
        if (d["decision"]?.jsonPrimitive?.content != "continue") return null // 반대만
        return Dissent(
            seq = e.seq,
            at = e.ts,
            member = d["member"]?.jsonPrimitive?.content.orEmpty(),
            why = (d["feedback"] ?: d["rationale"])?.jsonPrimitive?.content.orEmpty(),
        )
    }
}
