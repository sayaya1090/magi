package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.RecoveryState

/**
 * 대화창 맨 위의 복구 띠 — 지난 멈춤(재시작·중단)이 이 대화에 무엇을 남겼나.
 *
 * 데몬의 `recovery-state` 문이 정본이다. 이 층은 그 답을 띠의 모양으로 옮길 뿐 판정하지 않는다:
 * 무엇이 끊겼고 어느 호출이 실행됐을지 모르는지는 데몬이 로그로 정했다. 남은 것이 없거나
 * (`none`), 데몬이 이 칸을 모르거나(null), 모르는 상태가 오면 띠를 세우지 않는다 — 모르는 것을
 * 아는 척 그리지 않는다.
 *
 * 판정 버튼은 **아무것도 다시 돌리지 않는다.** 사람의 판정을 기록할 뿐이고, 띠는 데몬이 다시
 * 읽은 상태로 다시 선다.
 */
data class RecoveryStrip(
    val kind: Kind,
    /** 사람이 확인해야 하는 호출들. [Kind.NeedsCheck] 일 때만 차 있다. */
    val calls: List<Call>,
    /** 복구가 실패한 까닭 — 데몬이 말한 그대로. 없으면 빈 문자열. */
    val reason: String,
    val task: String = "",
    val lastCompleted: String = "",
) {
    enum class Kind { Resumable, NeedsCheck, Failed }

    /** 호출 하나. [deferred] 는 사람이 이미 「보류」로 미뤄 둔 것이다. */
    data class Call(val callId: String, val tool: String, val target: String, val deferred: Boolean, val details: String = "")

    companion object {
        /** 데몬 `resolve-call` 문이 받는 판정 단어. 다른 낱말은 문이 말로 거절한다. */
        val VERDICTS = listOf("ran", "not_ran", "rerun_ok", "deferred")

        fun of(state: RecoveryState?): RecoveryStrip? {
            state ?: return null
            val kind = when (state.kind) {
                "resumable" -> Kind.Resumable
                "needs_check" -> Kind.NeedsCheck
                "recovery_failed" -> Kind.Failed
                else -> return null
            }
            val calls = if (kind != Kind.NeedsCheck) emptyList() else state.uncertain.map {
                Call(it.callId, it.tool, it.target.orEmpty(), it.verdict == "deferred", it.details.orEmpty())
            }
            return RecoveryStrip(kind, calls, state.reason.orEmpty(), state.task.orEmpty(), state.lastCompleted.orEmpty())
        }
    }
}
