package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.*
import org.junit.jupiter.api.Test

class AnswerDraftsTest {
    @Test fun `explicit enter cancel and reenter preserve both drafts`() {
        val s = AnswerDrafts()
        assertNull(s.bind("s", "q", "A")); assertNull(s.active)
        assertEquals("", s.enter("A")); s.edit("B")
        assertEquals("A", s.cancel("B")); assertEquals("B", s.enter("A"))
        assertNull(s.bind("s", "q", "B")); assertEquals("A", s.bind("s", "q2", "B"))
        s.bind("s", "q", "A"); assertEquals("B", s.enter("A"))
    }
    @Test fun `pending submission rejects duplicates and stale completion cannot unlock retry`() {
        val s = AnswerDrafts(); s.bind("s", "q", "A"); s.enter("A")
        val a = s.begin("B")!!; s.leaveAfterSubmit()
        assertNull(s.begin("again")); assertTrue(s.complete(a, false))
        val b = s.begin("C")!!
        assertFalse(s.complete(a, true)); assertTrue(s.busy())
        assertTrue(s.complete(b, true)); assertTrue(s.done()); assertNull(s.begin("D"))
    }
    @Test fun `late failure preserves newer edits and other session input`() {
        val s = AnswerDrafts(); s.bind("s1", "q", "A"); s.enter("A")
        val a = s.begin("B")!!; s.leaveAfterSubmit(); s.enter("A"); s.edit("C")
        assertTrue(s.complete(a, false)); assertEquals("A", s.cancel("C")); assertEquals("C", s.enter("A"))
        assertEquals("", s.bind("s2", "q", "C")); assertNull(s.active)
        s.edit("other"); assertEquals("A", s.bind("s1", "q", "other")); assertEquals("C", s.enter("A"))
    }
    @Test fun `expiry and disposal never submit or revive an active answer`() {
        val s = AnswerDrafts(); s.bind("s", "q", "A"); s.enter("A")
        val a = s.begin("B")!!
        assertEquals("A", s.bind("s", null, "B")); assertNull(s.active)
        s.close(); assertFalse(s.complete(a, true)); assertNull(s.enter("A"))
    }
    @Test fun `question left exposes draft in recovery and avoids duplicates on repeated bind`() {
        val s = AnswerDrafts()
        s.bind("s", "q1", "A", "Choose 1")
        assertEquals("", s.enter("A"))
        s.edit("B")
        val general = s.bind("s", "q2", "B", "Choose 2")
        assertEquals("A", general)
        val recs = s.recoveries
        assertEquals(1, recs.size)
        val rec = recs.single()
        assertEquals("s", rec.session)
        assertEquals("q1", rec.callId)
        assertEquals("Choose 1", rec.questionText)
        assertEquals("B", rec.text)
        assertEquals(1L, rec.version)
        assertEquals(AnswerDrafts.REASON_QUESTION_LEFT, rec.reason)

        s.bind("s", "q2", "A")
        assertEquals(1, s.recoveries.size)
        assertEquals(rec.id, s.recoveries.single().id)

        s.bind("s", "q1", "A")
        assertTrue(s.recoveries.isEmpty())
        assertEquals("B", s.enter("A"))
    }
    @Test fun `cancel preserves drafts without creating recovery and sessions are isolated`() {
        val s = AnswerDrafts()
        s.bind("s1", "q", "A")
        s.enter("A"); s.edit("B")
        assertEquals("A", s.cancel("B"))
        assertTrue(s.recoveries.isEmpty())
        assertEquals("B", s.enter("A"))

        // Different session with same callId
        s.bind("s2", "q", "B")
        assertEquals(1, s.recoveries.size)
        assertEquals("s1", s.recoveries.single().session)
        assertEquals("B", s.recoveries.single().text)

        s.enter("C"); s.edit("D")
        s.bind("s2", null, "D")
        val recs = s.recoveries
        assertEquals(2, recs.size)
        assertEquals("s2", recs.first().session)
        assertEquals("D", recs.first().text)
        assertEquals("s1", recs.last().session)
        assertEquals("B", recs.last().text)
    }
    @Test fun `done with newer edits exposed in recoveries and unchanged success leaves no recovery item`() {
        val s = AnswerDrafts()
        s.bind("s", "q", "A")
        s.enter("A"); s.edit("A")
        val a = s.begin("A")!!
        s.leaveAfterSubmit()
        s.enter("A"); s.edit("B")
        assertTrue(s.complete(a, true))
        assertTrue(s.done())
        val recs = s.recoveries
        assertEquals(1, recs.size)
        assertEquals("B", recs.single().text)
        assertTrue(recs.single().version > a.version)

        s.bind("s", "q2", "A")
        s.enter("A"); s.edit("X")
        val a2 = s.begin("X")!!
        s.leaveAfterSubmit()
        assertTrue(s.complete(a2, true))
        assertEquals(1, s.recoveries.size)
    }
    @Test fun `delete recovery removes item and prevents revival on late callback or redraw`() {
        val s = AnswerDrafts()
        s.bind("s", "q", "A")
        s.enter("A"); s.edit("B")
        val a = s.begin("B")!!
        s.leaveAfterSubmit()
        s.bind("s", null, "A")
        val rec = s.recoveries.single()
        assertEquals("B", rec.text)

        assertTrue(s.deleteRecovery(rec.id))
        assertTrue(s.recoveries.isEmpty())
        assertFalse(s.deleteRecovery(rec.id))

        assertTrue(s.complete(a, false))
        assertTrue(s.recoveries.isEmpty())

        s.bind("s", "q", "A")
        assertTrue(s.recoveries.isEmpty())

        assertEquals("", s.enter("A"))
        s.edit("C")
        s.bind("s", null, "C")
        val rec2 = s.recoveries.single()
        assertEquals("C", rec2.text)
    }
    @Test fun `whitespace and newline preserved verbatim and empty string ignored`() {
        val s = AnswerDrafts()
        s.bind("s", "q1", "A")
        s.enter("A"); s.edit("  line1\nline2  ")
        s.bind("s", "q2", "  line1\nline2  ")
        assertEquals("  line1\nline2  ", s.recoveries.single().text)

        s.enter("A"); s.edit("")
        s.bind("s", null, "")
        assertEquals(1, s.recoveries.size)
    }
    @Test fun `failed attempt and newer draft preserved separately on same question key`() {
        val s = AnswerDrafts()
        val sess = "s1"
        val q1 = "q1"
        val q2 = "q2"

        s.bind(sess, q1, "general")
        s.enter("general")
        val attemptA = s.begin("submitted A")!!
        s.leaveAfterSubmit()
        s.enter("general")
        s.edit("new B")

        // While still on q1, submission of A fails
        assertTrue(s.complete(attemptA, false))

        // Failed attempt A is accessible even while q1 is current
        val currentRecs = s.recoveries
        assertEquals(1, currentRecs.size)
        assertEquals("submitted A", currentRecs.single().text)
        assertEquals(AnswerDrafts.REASON_SUBMISSION_FAILED, currentRecs.single().reason)
        assertEquals(attemptA.version, currentRecs.single().version)

        // Leaving q1 for q2 exposes unsubmitted draft B as well
        s.bind(sess, q2, "new B")
        val bothRecs = s.recoveries
        assertEquals(2, bothRecs.size)
        val recB = bothRecs.first() // newest first
        val recA = bothRecs.last()
        assertEquals("new B", recB.text)
        assertEquals(AnswerDrafts.REASON_QUESTION_LEFT, recB.reason)
        assertEquals("submitted A", recA.text)
        assertEquals(AnswerDrafts.REASON_SUBMISSION_FAILED, recA.reason)

        // Independent deletion: deleting A does not remove B
        val idA = recA.id
        val idB = recB.id
        assertTrue(s.deleteRecovery(idA))
        assertEquals(1, s.recoveries.size)
        assertEquals("new B", s.recoveries.single().text)

        // Duplicate late complete does not revive A
        assertFalse(s.complete(attemptA, false))
        assertEquals(1, s.recoveries.size)

        // Deleting B leaves empty recoveries
        assertTrue(s.deleteRecovery(idB))
        assertTrue(s.recoveries.isEmpty())

        // Returning to q1 and typing C creates new recoverable generation
        s.bind(sess, q1, "general")
        assertEquals("", s.enter("general"))
        s.edit("draft C")
        s.bind(sess, q2, "draft C")
        assertEquals(1, s.recoveries.size)
        assertEquals("draft C", s.recoveries.single().text)
    }
    @Test fun `deleting newer draft B does not remove failed attempt A`() {
        val s = AnswerDrafts()
        s.bind("s", "q", "general")
        s.enter("general")
        val attemptA = s.begin("submitted A")!!
        s.leaveAfterSubmit()
        s.enter("general")
        s.edit("new B")
        s.complete(attemptA, false)
        s.bind("s", "q2", "new B")

        val recs = s.recoveries
        assertEquals(2, recs.size)
        val recB = recs.first()
        val recA = recs.last()

        assertTrue(s.deleteRecovery(recB.id))
        assertEquals(1, s.recoveries.size)
        assertEquals("submitted A", s.recoveries.single().text)
        assertEquals(AnswerDrafts.REASON_SUBMISSION_FAILED, s.recoveries.single().reason)
    }
}
