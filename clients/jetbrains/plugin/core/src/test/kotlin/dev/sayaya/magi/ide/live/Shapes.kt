package dev.sayaya.magi.ide.live

import dev.sayaya.magi.ide.usecase.LookNotes

/**
 * 모델이 준 글자가 **꽂을 수 있는 모양인가**를 판정한다. 불평이 있으면 그 문장을, 없으면 null.
 *
 * 판정을 라이브 시험 안에 인라인으로 두면 **그 판정 자체를 아무도 안 잰다** — 모델이 있어야만
 * 도는 코드라 평소 CI 에서는 한 줄도 안 실행된다. 그래서 순수 함수로 빼고, 옆의 `ShapesTest` 가
 * **실제로 겪은 나쁜 모양들**을 먹여 판정이 우는지 확인한다. 재는 기계도 재야 한다.
 */
internal object Shapes {

    /** 커서 자리에 그대로 들어갈 글자인가. 접두를 주면 되풀이도 본다. */
    fun insertable(out: String, prefix: String = "", maxLines: Int = 8): String? {
        if (out.isBlank()) return "빈 답"
        if ("```" in out) return "코드 펜스가 들어 있다 — 그대로 커서에 꽂힌다"
        if (PREAMBLE.containsMatchIn(out.lineSequence().first())) return "머리말이 붙어 있다"
        val head = prefix.trimStart().takeIf { it.isNotBlank() }?.lineSequence()?.first()?.trim()
        if (head != null && head.length >= 6 && out.trimStart().startsWith(head)) {
            return "접두를 되풀이한다 — 같은 줄이 두 번 써진다"
        }
        val n = out.trimEnd().lineSequence().count()
        if (n > maxLines) return "한 자리 완성이 ${n}줄이다"
        return null
    }

    /** 입력줄 한 칸에 붙는 모양인가. */
    fun oneLine(out: String): String? {
        if (out.isBlank()) return "빈 답"
        if ("```" in out) return "코드 펜스가 들어 있다"
        val n = out.trimEnd().lineSequence().count()
        if (n > 2) return "여러 줄(${n})이라 입력줄에 안 맞는다"
        return null
    }

    /**
     * 훑어보기 답이 **줄에 붙는가.** 안 붙으면 인레이가 하나도 안 뜨고 전부 띠로 밀린다 —
     * 화면에서는 「할 말이 없다」와 구분이 안 간다.
     */
    fun anchored(out: String, fileLines: Int): String? {
        if (out.isBlank()) return "빈 답"
        val split = LookNotes.split(out)
        if (split.anchored.isEmpty()) return "지적이 하나도 줄에 안 붙었다"
        val off = split.anchored.map { it.first }.filter { it !in 1..fileLines }
        if (off.isNotEmpty()) return "파일은 ${fileLines}줄인데 없는 줄을 짚었다: $off"
        return null
    }

    /** 커밋 칸에 그대로 들어갈 모양인가. */
    fun commitShape(out: String): String? {
        if (out.isBlank()) return "빈 답"
        if ("```" in out) return "코드 펜스가 들어 있다"
        val first = out.lineSequence().first()
        if (PREAMBLE.containsMatchIn(first)) return "머리말이 붙어 있다"
        if (first.length > 100) return "첫 줄이 ${first.length}자다 — 제목으로 안 쓰인다"
        return null
    }

    /**
     * 모델이 잘 붙이는 머리말들. **실측으로 늘린다** — 여기 없는 모양을 만나면 그때 더한다.
     * 지어내서 넓히면 멀쩡한 완성을 불평하게 된다.
     */
    private val PREAMBLE = Regex(
        "^\\s*(here('s| is)\\b|sure[,!]|of course|다음은|아래는|물론|설명(하|드리)|" +
            "the (completion|answer|code) is\\b|이 코드는)",
        RegexOption.IGNORE_CASE,
    )
}
