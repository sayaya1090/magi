package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.ChildJob

/**
 * 우측 자리의 탭 줄 규칙(사용자 결정, 2026-11-23). 첫 탭은 세션 제목이고, 그 옆에 **도는** 서브에이전트가
 * 하나씩 붙는다. 서브에이전트가 끝나면(또는 목록에서 사라지면) 그 탭은 닫히되, 지금 보고 있는 탭이면
 * 남겼다가 다른 탭으로 옮긴 다음 갱신에서 닫힌다 — 읽던 결과와 실패 사유를 눈앞에서 잃지 않게.
 * 웹·VS Code 도 같은 규칙을 쓴다.
 */
object SubagentTabs {
    data class Tab(val id: String, val label: String, val running: Boolean)

    private const val TAB_MAX = 24
    private const val SESSION_MAX = 32

    /** 지금 열린 [open] 과 데몬의 [children], 사람이 보고 있는 탭 [selected] 로 다음 탭 줄을 만든다. */
    fun next(open: List<Tab>, children: List<ChildJob>, selected: String?): List<Tab> {
        val running = children.filter { it.running && it.id.isNotBlank() }.associateBy { it.id }
        val out = ArrayList<Tab>()
        for (t in open) {
            val now = running[t.id]
            when {
                now != null -> out += Tab(t.id, labelOf(now), true)
                t.id == selected -> out += t.copy(running = false)
            }
        }
        for ((id, job) in running) if (out.none { it.id == id }) out += Tab(id, labelOf(job), true)
        return out
    }

    /** 서브에이전트 탭 이름: 과제의 첫 줄, 없으면 id 끝 여섯 자. */
    fun labelOf(job: ChildJob): String =
        clip(firstLine(job.task), TAB_MAX) ?: ("·" + job.id.takeLast(6))

    /** 세션 탭 이름: 대화 제목의 첫 줄, 길면 자르고, 없으면 [fallback]. */
    fun sessionLabel(title: String?, fallback: String): String = clip(firstLine(title), SESSION_MAX) ?: fallback

    private fun firstLine(s: String?): String? =
        s?.lineSequence()?.map { it.trim() }?.firstOrNull { it.isNotEmpty() }

    private fun clip(s: String?, max: Int): String? {
        if (s.isNullOrBlank()) return null
        return if (s.length > max) s.take(max - 1) + "…" else s
    }
}
