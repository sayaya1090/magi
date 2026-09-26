package dev.sayaya.magi.ide.usecase

/**
 * 컨텍스트 띠 — 얼마나 찼고 **무엇으로** 찼나. 모양은 Office 작업창의 띠(clients/excel/addin/src/ui/screen.js 의
 * `contextMeter`)와 같게, 수는 코어의 규칙대로 그린다.
 *
 * 총량만 보여 주면 사람은 대화를 줄이러 간다. 이 하네스에서 대화는 대개 작은 쪽이고, 매 요청에 실려 가는 도구
 * 목록이 큰 쪽이다 — 그래서 조각을 색으로 나눠 보인다.
 *
 * **눈금은 모델의 창이다.** 칠해진 길이는 측정된 총량(`used`)이 창에서 차지하는 몫이고, 안 찬 자리는 빈 채로 둔다
 * — 「얼마나 남았나」가 보이게. 창을 모르면 띠를 다 칠한다(그때 가득 찬 띠가 「모른다」의 모양이다).
 *
 * **조각은 몫으로만 말한다.** 코어가 규칙과 이유를 적어 두었다(`internal/app/context_state.go`): 조각은 chars/4
 * 어림이라 측정된 `used` 와 더해지지 않는다 — "honest as proportions and dishonest as totals, which is why the
 * screen draws them as a share of their own sum and says the reading is an estimate". 그래서 칠해진 구간 안을
 * 조각의 몫으로 나누고, 범례도 토큰 수가 아니라 몫(%)으로 적는다.
 */
object ContextGauge {
    /** 조각의 순서는 Office 와 같다: 시스템 · 도구 목록 · 대화 · 호출 · 결과. */
    enum class Part { System, Tools, Talk, Calls, Results }

    /**
     * 띠의 한 칸. [share] 는 조각들 제 합에 대한 몫(%, 어림), [fraction] 은 띠 전체 길이에 대한 몫이다
     * (= 조각의 몫 × 칠해진 길이).
     */
    data class Segment(val part: Part, val share: Int, val fraction: Double)

    data class Gauge(
        val used: Int,
        /** 0 이면 창을 모른다. */
        val window: Int,
        /** 창에 대한 백분율. 창을 모르면 null. */
        val percent: Int?,
        val segments: List<Segment>,
        /** 몇 번 접혔나. */
        val compactions: Int,
        /** 마지막 접기가 남겨 둔 주제들. */
        val topics: List<String>,
    )

    /** 아무것도 모르면 null — 모름을 0% 로 그리지 않는다. */
    fun of(ctx: Rows.Ctx?): Gauge? {
        if (ctx == null || (ctx.tokens <= 0 && ctx.window <= 0)) return null
        val p = ctx.parts
        val tokens = listOf(
            Part.System to (p?.system ?: 0),
            Part.Tools to (p?.tools ?: 0),
            Part.Talk to (p?.talk ?: 0),
            Part.Calls to (p?.calls ?: 0),
            Part.Results to (p?.results ?: 0),
        )
        val sum = tokens.sumOf { it.second }
        // 칠해진 길이: 측정된 총량의 창에 대한 몫. 창을 모르면 다 칠한다.
        val filled = if (ctx.window > 0) minOf(1.0, ctx.tokens.toDouble() / ctx.window) else 1.0
        val segments = if (sum > 0) {
            tokens.filter { it.second > 0 }.map { (part, n) ->
                Segment(part, Math.round(n * 100.0 / sum).toInt(), filled * n / sum)
            }
        } else emptyList()
        return Gauge(
            used = ctx.tokens,
            window = ctx.window,
            percent = if (ctx.window > 0) minOf(100, Math.round(ctx.tokens * 100.0 / ctx.window).toInt()) else null,
            segments = segments,
            compactions = ctx.compactions,
            topics = ctx.topics.orEmpty(),
        )
    }

    /** 토큰 수를 사람 눈금으로: 999 · 1.5k · 12k. Office 의 `kilo` 와 같다. */
    fun kilo(n: Int): String = when {
        n < 1000 -> n.toString()
        n < 10_000 -> String.format(java.util.Locale.ROOT, "%.1f", n / 1000.0).removeSuffix(".0") + "k"
        else -> "${Math.round(n / 1000.0)}k"
    }
}
