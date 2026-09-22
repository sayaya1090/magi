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
}
