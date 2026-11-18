package dev.sayaya.magi.ide.usecase

/**
 * 입력줄의 ↑/↓ — 전에 친 입력을 되불러온다.
 *
 * 목록은 데몬의 `prompts` 문이 준다: 이 워크스페이스의 **모든 대화**에서 사람이 친 것, 최신순, 같은
 * 글은 한 번. 그래서 대화를 바꾸거나 창을 다시 열어도 남는다(2026-10-17 사용자 요청 — 이 창엔
 * 아예 없었고, 터미널의 것은 보던 대화와 함께 사라졌다).
 *
 * 쓰던 글은 버리지 않는다: 첫 ↑ 에 지금 입력을 [draft] 로 맡겨 두고, 맨 아래까지 ↓ 하면 돌려준다.
 * 보낸 글은 [remember] 로 바로 맨 앞에 선다 — 데몬에게 다시 묻기 전에도 방금 친 것이 ↑ 한 번이다.
 */
class PromptRecall {
    private var history: List<String> = emptyList() // 최신이 앞
    private var at = -1 // -1 = 쓰던 글
    private var draft = ""

    /** 데몬이 준 목록으로 갈아 끼운다. 둘러보던 중이면 자리를 잃지 않게 갈아 끼우지 않는다. */
    fun load(newestFirst: List<String>) {
        if (at >= 0) return
        history = newestFirst.filter { it.isNotBlank() }.distinct()
    }

    /**
     * ↑: 하나 더 오래된 것. 더 없으면 null(입력을 안 바꾼다).
     *
     * 둘러보던 자리는 보내기([remember])만 풀었다. 답변 보내기·보내기 복원·프리필·지우고 다시
     * 쓰기는 입력만 바꾸고 자리를 남겨, 끝까지 둘러본 뒤에는 ↑ 가 영영 null 이었다. 지금 입력이
     * 불러온 글이 아니면 그 자리는 이미 끝난 것으로 보고 처음부터 다시 둘러본다.
     */
    fun older(current: String): String? {
        if (at >= 0 && history.getOrNull(at) != current) at = -1
        if (at + 1 >= history.size) return null
        if (at == -1) draft = current
        at++
        return history[at]
    }

    /** ↓: 하나 더 새로운 것, 맨 아래면 쓰던 글. 둘러보는 중이 아니면 null. */
    fun newer(): String? {
        if (at < 0) return null
        at--
        return if (at == -1) draft else history[at]
    }

    val browsing: Boolean get() = at >= 0

    /** 진단용: 둘러보는 자리(-1 = 쓰던 글)와 목록 크기. */
    val position: Int get() = at
    val size: Int get() = history.size

    /** Repeated arrows traverse recalled multiline entries; edits restore normal caret navigation. */
    fun isRecalled(text: String): Boolean = at >= 0 && history.getOrNull(at) == text

    /** 보냈다 — 그 글을 맨 앞에 세우고 둘러보기를 끝낸다. */
    fun remember(sent: String) {
        val t = sent.trim()
        if (t.isNotEmpty()) history = listOf(t) + history.filter { it != t }
        at = -1
        draft = ""
    }
}
