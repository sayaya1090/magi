package dev.sayaya.magi.ide.usecase

/**
 * 대화창 맨 위 한 줄 — 계획(TodoWrite)의 몇 단계 중 몇 번째이고 지금 무엇을 하는가.
 *
 * 계획 창은 오른쪽에 따로 있어서, 대화를 보는 동안에는 지금 어디쯤인지가 안 보였다. 이 줄은
 * 그 사실을 대화 위에 세운다. 진행 중 표시(`in_progress`)가 있으면 그 단계가 지금이고, 없으면
 * 아직 안 끝난 첫 단계가 다음 할 일이다. 다 끝났으면 끝났다고 말한다. 계획이 없으면 null —
 * 줄 자체를 세우지 않는다.
 */
data class PlanStrip(
    /** 1부터 센 지금 단계. 다 끝났으면 [total]. */
    val step: Int,
    val total: Int,
    val done: Int,
    /** 지금(또는 다음) 단계의 내용. 다 끝났으면 빈 문자열. */
    val current: String,
    val finished: Boolean,
) {
    companion object {
        fun of(todos: List<Rows.Todo>?): PlanStrip? {
            if (todos.isNullOrEmpty()) return null
            val done = todos.count { it.status == "completed" }
            val at = todos.indexOfFirst { it.status == "in_progress" }
                .takeIf { it >= 0 } ?: todos.indexOfFirst { it.status != "completed" }
            if (at < 0) return PlanStrip(todos.size, todos.size, done, "", true)
            return PlanStrip(at + 1, todos.size, done, todos[at].content.trim(), false)
        }
    }
}
