package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.usecase.AnswerDrafts

/**
 * 답변 모델 전이와 UI 적용 순서를 조정하는 코디네이터.
 *
 * Swing View와 AnswerDrafts 사이에서 상태 전이를 동기적으로 수행하고,
 * 전이에 따른 UI 효과 목록(Effect)을 순서대로 생성하여 반환합니다.
 */
internal class AnswerTransitionCoordinator(private val drafts: AnswerDrafts) {
    data class QuestionContext(val session: String?, val callId: String, val what: String?)

    sealed interface Effect {
        data class RestoreText(val text: String) : Effect
        data object InvalidateComposer : Effect
        data object PaintMode : Effect
        data object DrawRecovery : Effect
        data object RedrawPrompt : Effect
        data class ReportError(val error: String) : Effect
    }

    data class Transition(val effects: List<Effect> = emptyList())
    data class SubmitOutcome(val attempt: AnswerDrafts.Attempt?, val transition: Transition)
    data class CompletionOutcome(val handled: Boolean, val transition: Transition)

    @Volatile
    var isDisposed: Boolean = false
        private set

    fun sync(currentScope: String?, received: QuestionContext?, currentText: String): Transition {
        if (isDisposed) return Transition()
        val validCallId = if (received != null && received.session == currentScope) received.callId else null
        val validWhat = if (received != null && received.session == currentScope) received.what else null
        val prevQuestion = drafts.question
        val restore = drafts.bind(currentScope, validCallId, currentText, validWhat)
        val effects = mutableListOf<Effect>()
        if (restore != null) {
            effects.add(Effect.RestoreText(restore))
            effects.add(Effect.InvalidateComposer)
        }
        if (prevQuestion != drafts.question) {
            effects.add(Effect.InvalidateComposer)
        }
        effects.add(Effect.PaintMode)
        effects.add(Effect.DrawRecovery)
        return Transition(effects)
    }

    fun enter(currentText: String): Transition {
        if (isDisposed) return Transition()
        val restore = drafts.enter(currentText)
        val effects = mutableListOf<Effect>()
        if (restore != null) {
            effects.add(Effect.RestoreText(restore))
        }
        effects.add(Effect.InvalidateComposer)
        effects.add(Effect.PaintMode)
        effects.add(Effect.DrawRecovery)
        return Transition(effects)
    }

    fun cancel(currentText: String): Transition {
        if (isDisposed) return Transition()
        val restore = drafts.cancel(currentText)
        val effects = mutableListOf<Effect>()
        if (restore != null) {
            effects.add(Effect.RestoreText(restore))
        }
        effects.add(Effect.InvalidateComposer)
        effects.add(Effect.PaintMode)
        effects.add(Effect.DrawRecovery)
        return Transition(effects)
    }

    fun submit(text: String, expected: AnswerDrafts.Key?): SubmitOutcome {
        if (isDisposed) return SubmitOutcome(null, Transition())
        if (expected == null || drafts.question != expected) return SubmitOutcome(null, Transition())
        val attempt = drafts.begin(text) ?: return SubmitOutcome(null, Transition())
        drafts.leaveAfterSubmit()
        val effects = listOf(
            Effect.RestoreText(drafts.generalText()),
            Effect.InvalidateComposer,
            Effect.PaintMode,
            Effect.DrawRecovery,
        )
        return SubmitOutcome(attempt, Transition(effects))
    }

    fun complete(
        attempt: AnswerDrafts.Attempt,
        error: String?,
        currentScope: String?,
        currentText: String,
    ): CompletionOutcome {
        if (isDisposed) return CompletionOutcome(false, Transition())
        val ok = error == null
        if (!drafts.complete(attempt, ok)) return CompletionOutcome(false, Transition())
        val isCurrentContext = currentScope == attempt.key.session && drafts.question == attempt.key
        if (!isCurrentContext) {
            return CompletionOutcome(true, Transition(listOf(Effect.DrawRecovery)))
        }
        if (!ok) {
            val effects = listOf(
                Effect.ReportError(error),
                Effect.PaintMode,
                Effect.DrawRecovery,
            )
            return CompletionOutcome(true, Transition(effects))
        }
        val restore = drafts.cancel(currentText)
        val effects = mutableListOf<Effect>()
        if (restore != null) {
            effects.add(Effect.RestoreText(restore))
        }
        effects.add(Effect.InvalidateComposer)
        effects.add(Effect.RedrawPrompt)
        effects.add(Effect.PaintMode)
        effects.add(Effect.DrawRecovery)
        return CompletionOutcome(true, Transition(effects))
    }

    fun dispose() {
        isDisposed = true
    }
}
