package dev.sayaya.magi.ide.usecase

/**
 * 훑어본 말을 **줄에 걸리는 것**과 나머지로 가른다.
 *
 * 코어 계약은 `<줄><TAB><지적>` 한 줄에 하나다(`internal/app/git.go` 의 LookOver — 콘솔이
 * 번호를 붙여 보내고 답이 그 모양을 지킨다). 그러나 그 모양을 **안 지킨 답**도 온다: 모델이
 * 문장 하나로 답하거나, 파일 전체에 대한 말이라 걸 줄이 없을 때다. 그때 지어낼 줄 번호는
 * 없으므로 그 말은 「줄 없는 말」로 남고, 화면은 그것만 편집기 위 띠에 세운다.
 *
 * `intellij` 모듈엔 시험이 없어 여기 둔다 — 재는 자리에 규칙을 두는 그 규칙 그대로.
 */
object LookNotes {

    data class Split(val anchored: List<Pair<Int, String>>, val loose: String)

    /**
     * 구분자는 **탭만이 아니다.** 계약은 탭이지만 모델이 공백이나 콜론으로 붙여 보내는 것을
     * 실측했다(라이브: `5broken link missing colon` — 탭이 없어 「줄 없는 말」로 밀려 띠에
     * 섰고, 사용자가 "똑같이 파란 박스로 뜬다"로 잡았다). 넓게 읽어도 안전한 이유가 있다:
     * 잘못 읽은 번호는 파일 줄 수 밖이면 거는 쪽이 못 걸고 그 말을 그대로 띠로 돌려보낸다 —
     * 관대하게 읽되 지어내지는 않는 자리다.
     */
    private val HEAD = Regex("^\\s*(\\d{1,6})\\s*[\\t:.)-]?\\s*(.*)$")

    fun split(out: String): Split {
        val anchored = mutableListOf<Pair<Int, String>>()
        val loose = mutableListOf<String>()
        for (raw in out.lineSequence()) {
            val line = raw.trim()
            if (line.isBlank()) continue
            val m = HEAD.find(line)
            val n = m?.groupValues?.get(1)?.toIntOrNull()
            val text = m?.groupValues?.get(2)?.trim().orEmpty()
            if (n != null && n > 0 && text.isNotEmpty()) anchored += n to text else loose += line
        }
        return Split(anchored, loose.joinToString("\n"))
    }
}
