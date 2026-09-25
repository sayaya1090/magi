package dev.sayaya.magi.ide.ui

import dev.sayaya.magi.ide.usecase.AnswerDrafts
import org.junit.Assert.*
import org.junit.Test

class AnswerTransitionCoordinatorTest {

    @Test
    fun `test sync isolates mismatched session question and respects matching and null question`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)

        // 1. S1 수신 질문을 가진 채 S2로 sync: S1 callId가 S2에 bind되지 않음
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Option 1")
        val transMismatch = coordinator.sync(currentScope = "s2", received = q1, currentText = "my edit")
        assertNull(drafts.question)
        assertNull(drafts.active)
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transMismatch.effects,
        )

        // 2. 같은 callId의 다른 세션도 격리: S3로 sync해도 s1의 q1은 무시됨
        val transMismatch2 = coordinator.sync(currentScope = "s3", received = q1, currentText = "")
        assertNull(drafts.question)

        // 3. 정상 동일 세션 질문: s1으로 sync 시 q1 정상 바인딩
        // s3 -> s1 세션 변경으로 인한 bind 복원(RestoreText("")) 및 질문 변경(null -> q1)으로 InvalidateComposer 2회 발생
        val transMatch = coordinator.sync(currentScope = "s1", received = q1, currentText = "")
        assertEquals(AnswerDrafts.Key("s1", "q1"), drafts.question)
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText(""),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transMatch.effects,
        )

        // 4. null/권한 질문 대조군: 질문 해제 시 question이 null로 바뀜 (동일 세션 s1 유지)
        val transNull = coordinator.sync(currentScope = "s1", received = null, currentText = "")
        assertNull(drafts.question)
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transNull.effects,
        )
    }

    @Test
    fun `test bind effects order and duplicate invalidation preservation`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Q1")
        val q2 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q2", what = "Q2")

        // Setup: enter q1, write text "draft1", exit to general
        coordinator.sync("s1", q1, "")
        coordinator.enter("")
        drafts.edit("draft1")
        coordinator.cancel("draft1")

        // 1. When switching from q1 in active mode to q2 with changed session or left question:
        // bind returns non-null generalText, and question also changes (q1 -> q2).
        // Both conditions met: RestoreText -> InvalidateComposer (from restore) -> InvalidateComposer (from question change) -> PaintMode -> DrawRecovery
        coordinator.enter("") // active is q1
        val transBoth = coordinator.sync("s1", q2, "activeText")
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText(""),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transBoth.effects,
        )

        // 2. Only restore != null, but question unchanged:
        // First ensure question is null in s1
        coordinator.sync("s1", null, "")
        // Now switch session to s2 (both have question = null)
        val transRestoreOnly = coordinator.sync("s2", null, "textForS1")
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText(""),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transRestoreOnly.effects,
        )

        // 3. Question changed, but restore == null:
        // s2 currently has no question. Bind q_s2 while in general mode.
        val qS2 = AnswerTransitionCoordinator.QuestionContext(session = "s2", callId = "qs2", what = "QS2")
        val transQuestionOnly = coordinator.sync("s2", qS2, "")
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transQuestionOnly.effects,
        )

        // 4. Neither changed:
        val transNeither = coordinator.sync("s2", qS2, "")
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            transNeither.effects,
        )
    }

    @Test
    fun `test enter cancel roundtrip and submit success and rejections`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Choose")

        coordinator.sync("s1", q1, "generalDraft")

        // 1. Enter answer mode: restores question draft (initially empty)
        val enterTrans = coordinator.enter("generalDraft")
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText(""),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            enterTrans.effects,
        )
        assertEquals(AnswerDrafts.Key("s1", "q1"), drafts.active)

        // Edit question draft
        drafts.edit("answerDraft")

        // 2. Cancel answer mode: restores general draft ("generalDraft")
        val cancelTrans = coordinator.cancel("answerDraft")
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText("generalDraft"),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            cancelTrans.effects,
        )
        assertNull(drafts.active)

        // 3. Submit rejection on expected mismatch
        val rejectMismatch = coordinator.submit("answerDraft", AnswerDrafts.Key("s1", "differentQ"))
        assertNull(rejectMismatch.attempt)
        assertTrue(rejectMismatch.transition.effects.isEmpty())

        // 4. Submit rejection on null expected
        val rejectNull = coordinator.submit("answerDraft", null)
        assertNull(rejectNull.attempt)
        assertTrue(rejectNull.transition.effects.isEmpty())

        // 5. Submit rejection on blank text
        val rejectBlank = coordinator.submit("   ", AnswerDrafts.Key("s1", "q1"))
        assertNull(rejectBlank.attempt)
        assertTrue(rejectBlank.transition.effects.isEmpty())

        // 6. Submit success
        val submitSuccess = coordinator.submit("validAnswer", AnswerDrafts.Key("s1", "q1"))
        assertNotNull(submitSuccess.attempt)
        val attemptSuccess = submitSuccess.attempt!!
        assertEquals("validAnswer", attemptSuccess.text)
        assertEquals(AnswerDrafts.Key("s1", "q1"), attemptSuccess.key)
        assertNull(drafts.active) // leaveAfterSubmit called
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText("generalDraft"),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            submitSuccess.transition.effects,
        )

        // 7. Submit rejection on busy (attempt already pending)
        val rejectBusy = coordinator.submit("anotherAnswer", AnswerDrafts.Key("s1", "q1"))
        assertNull(rejectBusy.attempt)
        assertTrue(rejectBusy.transition.effects.isEmpty())
    }

    @Test
    fun `test late failure preserves edits and old result cannot unlock retry`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Choose")

        coordinator.sync("s1", q1, "G")
        coordinator.enter("G")
        drafts.edit("A")

        // Submit A: version increments from 1 (edit) to 2 (begin)
        val submitOutcome = coordinator.submit("A", AnswerDrafts.Key("s1", "q1"))
        val attemptA = submitOutcome.attempt!!
        assertEquals(2L, attemptA.version)
        assertEquals("A", attemptA.text)

        // Re-enter and edit to B: version increments from 2 to 3
        coordinator.enter("G")
        drafts.edit("B")
        assertEquals(3L, drafts.version(AnswerDrafts.Key("s1", "q1")))

        // A fails: error returned
        val failureOutcome = coordinator.complete(attemptA, "RPC error", currentScope = "s1", currentText = "B")
        assertTrue(failureOutcome.handled)
        // Newer edit "B" must be preserved: no RestoreText effect!
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.ReportError("RPC error"),
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            failureOutcome.transition.effects,
        )

        // Recovery has item for "A"
        val rec = drafts.recoveries.firstOrNull { it.callId == "q1" }
        assertNotNull(rec)
        assertEquals("A", rec!!.text)
        assertEquals(AnswerDrafts.REASON_SUBMISSION_FAILED, rec.reason)

        // Duplicate result for attemptA is rejected (handled = false)
        val duplicateOutcome = coordinator.complete(attemptA, "RPC error", currentScope = "s1", currentText = "B")
        assertFalse(duplicateOutcome.handled)
        assertTrue(duplicateOutcome.transition.effects.isEmpty())

        // Now B can be submitted
        val submitB = coordinator.submit("B", AnswerDrafts.Key("s1", "q1"))
        assertNotNull(submitB.attempt)
        val attemptB = submitB.attempt!!
        assertEquals("B", attemptB.text)
        assertEquals(4L, attemptB.version) // increments to 4 on begin
    }

    @Test
    fun `test cross session result delivery only updates recovery`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)

        // Session 1: question q1, submit attempt1
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Q1")
        coordinator.sync("s1", q1, "")
        coordinator.enter("")
        drafts.edit("ans1")
        val attempt1 = coordinator.submit("ans1", AnswerDrafts.Key("s1", "q1")).attempt!!

        // Switch to Session 2 with question q2
        val q2 = AnswerTransitionCoordinator.QuestionContext(session = "s2", callId = "q2", what = "Q2")
        coordinator.sync("s2", q2, "")
        coordinator.enter("")
        drafts.edit("ans2")

        // 1. attempt1 fails while on s2: only DrawRecovery is produced
        val crossFail = coordinator.complete(attempt1, "error", currentScope = "s2", currentText = "ans2")
        assertTrue(crossFail.handled)
        assertEquals(
            listOf(AnswerTransitionCoordinator.Effect.DrawRecovery),
            crossFail.transition.effects,
        )

        // S2 draft state is untouched
        assertEquals(AnswerDrafts.Key("s2", "q2"), drafts.active)

        // S1's failed draft is in recoveries
        assertTrue(drafts.recoveries.any { it.session == "s1" && it.callId == "q1" })
    }

    @Test
    fun `test current question success effects order and idempotent complete`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Choose")

        coordinator.sync("s1", q1, "generalText")
        coordinator.enter("generalText")
        drafts.edit("answerText")
        val attempt = coordinator.submit("answerText", AnswerDrafts.Key("s1", "q1")).attempt!!

        // Case A: User remained in general mode when result arrives
        // drafts.cancel("") returns null because active is null -> no RestoreText
        val outcomeInGeneral = coordinator.complete(attempt, error = null, currentScope = "s1", currentText = "generalText")
        assertTrue(outcomeInGeneral.handled)
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.RedrawPrompt,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            outcomeInGeneral.transition.effects,
        )
        assertTrue(drafts.done(AnswerDrafts.Key("s1", "q1")))

        // Completed attempt cannot be completed again
        val replay = coordinator.complete(attempt, error = null, currentScope = "s1", currentText = "generalText")
        assertFalse(replay.handled)
        assertTrue(replay.transition.effects.isEmpty())

        // Case B: User re-entered answer mode before success arrives
        val q2 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q2", what = "Choose 2")
        coordinator.sync("s1", q2, "general2")
        coordinator.enter("general2")
        drafts.edit("answer2")
        val attempt2 = coordinator.submit("answer2", AnswerDrafts.Key("s1", "q2")).attempt!!

        // User re-enters answer mode while attempt2 is pending
        coordinator.enter("general2")
        assertEquals(AnswerDrafts.Key("s1", "q2"), drafts.active)

        // Now attempt2 completes successfully -> drafts.cancel returns non-null generalText -> RestoreText included!
        val outcomeActive = coordinator.complete(attempt2, error = null, currentScope = "s1", currentText = "answer2")
        assertTrue(outcomeActive.handled)
        assertEquals(
            listOf(
                AnswerTransitionCoordinator.Effect.RestoreText("general2"),
                AnswerTransitionCoordinator.Effect.InvalidateComposer,
                AnswerTransitionCoordinator.Effect.RedrawPrompt,
                AnswerTransitionCoordinator.Effect.PaintMode,
                AnswerTransitionCoordinator.Effect.DrawRecovery,
            ),
            outcomeActive.transition.effects,
        )
        assertTrue(drafts.done(AnswerDrafts.Key("s1", "q2")))
    }

    @Test
    fun `test dispose suppresses all operations and does not close underlying drafts`() {
        val drafts = AnswerDrafts()
        val coordinator = AnswerTransitionCoordinator(drafts)
        val q1 = AnswerTransitionCoordinator.QuestionContext(session = "s1", callId = "q1", what = "Q1")

        coordinator.sync("s1", q1, "")
        assertFalse(coordinator.isDisposed)

        coordinator.dispose()
        assertTrue(coordinator.isDisposed)

        // Idempotent dispose
        coordinator.dispose()
        assertTrue(coordinator.isDisposed)

        // All operations return empty/null/handled=false after dispose
        val transSync = coordinator.sync("s1", q1, "text")
        assertTrue(transSync.effects.isEmpty())

        val transEnter = coordinator.enter("text")
        assertTrue(transEnter.effects.isEmpty())

        val transCancel = coordinator.cancel("text")
        assertTrue(transCancel.effects.isEmpty())

        val submitOutcome = coordinator.submit("text", AnswerDrafts.Key("s1", "q1"))
        assertNull(submitOutcome.attempt)
        assertTrue(submitOutcome.transition.effects.isEmpty())

        val fakeAttempt = AnswerDrafts.Attempt(1L, AnswerDrafts.Key("s1", "q1"), 1L, "text")
        val completeOutcome = coordinator.complete(fakeAttempt, null, "s1", "text")
        assertFalse(completeOutcome.handled)
        assertTrue(completeOutcome.transition.effects.isEmpty())

        // Crucial verification: coordinator.dispose() does NOT close drafts.
        // View retains ownership of AnswerDrafts lifecycle.
        assertFalse(drafts.done(AnswerDrafts.Key("s1", "q1")))
        // drafts is still usable by View: enter active mode and edit
        drafts.enter("")
        drafts.edit("still alive")
        assertEquals(1L, drafts.version(AnswerDrafts.Key("s1", "q1")))

        // View explicitly closes drafts
        drafts.close()
    }
}
