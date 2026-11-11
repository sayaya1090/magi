package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.Progress
import kotlin.math.roundToInt

/**
 * 압축 막대가 그릴 것 — 코어가 센 값에서만 만든다.
 *
 * 조각이 여럿이면 끝난 조각의 비율이 막대를 채운다([fraction]). 조각이 하나뿐이면(대부분이다)
 * 얼마나 남았는지 아무도 모른다 — 모델이 요약을 얼마나 길게 쓸지 미리 알 수 없다. 그때는
 * 비율을 지어내지 않고 [fraction] 을 null 로 두어 움직이는 막대(비결정형)로 그린다. 옆의
 * 글자 수와 경과 초가 「멈춘 게 아니라 도는 중」을 말한다.
 */
data class FoldBar(
    /** 0.0–1.0, 또는 null(비결정형). */
    val fraction: Double?,
    /** 「2/3」 — 몇 번째 조각을 몇 개 중에 쓰는 중인가. 모르면 빈 문자열. */
    val part: String,
    val chars: Int,
    val seconds: Long,
) {
    /** 상태 표시줄처럼 글자만 쓰는 자리의 막대. 비결정형이면 도는 표시 하나. */
    fun glyph(cells: Int = 8): String = fraction?.let {
        val n = (it * cells).roundToInt().coerceIn(0, cells)
        FULL.repeat(n) + EMPTY.repeat(cells - n)
    } ?: SPIN

    companion object {
        const val FULL = "\u25B0"
        const val EMPTY = "\u25B1"
        const val SPIN = "\u27F3"

        fun of(p: Progress): FoldBar {
            val total = p.total.coerceAtLeast(0)
            val done = p.done.coerceIn(0, total)
            return FoldBar(
                fraction = if (total > 1) done.toDouble() / total else null,
                part = if (total > 0) "${minOf(done + 1, total)}/$total" else "",
                chars = p.chars.coerceAtLeast(0),
                seconds = (p.elapsedMs / 1000).coerceAtLeast(0),
            )
        }
    }
}
