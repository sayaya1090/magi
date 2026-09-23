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
}
