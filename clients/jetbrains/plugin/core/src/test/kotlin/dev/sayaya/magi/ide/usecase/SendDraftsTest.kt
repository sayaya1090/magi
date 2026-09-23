package dev.sayaya.magi.ide.usecase

import org.junit.jupiter.api.Assertions.*
import org.junit.jupiter.api.Test

class SendDraftsTest {
    @Test fun `same edit cannot be submitted twice while pending and duplicate results do nothing`() {
        val s = SendDrafts()
        val a = s.begin("s1", "A", emptyList())!!
        assertNull(s.begin("s1", "A", emptyList()))
        assertTrue(s.complete(a, null, "s1")!!.clearInput)
        assertNull(s.complete(a, "late error", "s1"))
        assertTrue(s.failures.isEmpty())
    }
    @Test fun `edit revisions and session ownership both guard clearing`() {
        val s = SendDrafts()
        val a = s.begin("s1", "A", emptyList())!!
        s.edited(); s.edited()
        assertFalse(s.complete(a, null, "s1")!!.clearInput)
        val b = s.begin("s1", "A", emptyList())!!
        assertFalse(s.complete(b, null, "s2")!!.sameSession)
    }
    @Test fun `failure snapshots survive other successes until explicit recovery and close`() {
        val s = SendDrafts()
        val a = s.begin("s1", "A", emptyList())!!
        s.edited()
        val b = s.begin("s1", "B", emptyList())!!
        s.complete(a, "unknown", "s2")
        s.complete(b, null, "s1")
        assertEquals(a, s.failures.single().attempt)
        s.recovered(a.id)
        assertTrue(s.failures.isEmpty())
        s.close()
        assertNull(s.begin("s1", "C", emptyList()))
    }
    @Test fun `busy reflects active in-flight submissions per session and globally`() {
        val s = SendDrafts()
        assertFalse(s.busy())
        assertFalse(s.busy("s1"))
        val a = s.begin("s1", "A", emptyList())!!
        assertTrue(s.busy())
        assertTrue(s.busy("s1"))
        assertFalse(s.busy("s2"))
        s.complete(a, null, "s1")
        assertFalse(s.busy())
        assertFalse(s.busy("s1"))
    }
    @Test fun `inFlight distinguishes current revision lock from background pending sends`() {
        val s = SendDrafts()
        assertTrue(s.canSend("s1"))
        assertFalse(s.inFlight("s1"))
        val a = s.begin("s1", "A", emptyList())!!
        assertTrue(s.busy("s1"))
        assertTrue(s.inFlight("s1"))
        assertFalse(s.canSend("s1"))

        // User edits new text while A is pending:
        s.edited()
        assertTrue(s.busy("s1")) // A is still pending
        assertFalse(s.inFlight("s1")) // but current revision is NOT in-flight!
        assertTrue(s.canSend("s1")) // user is allowed to send B!

        val b = s.begin("s1", "B", emptyList())!!
        assertTrue(s.inFlight("s1"))
        assertFalse(s.canSend("s1"))

        // A completes while B is still pending:
        s.complete(a, null, "s1")
        assertTrue(s.busy("s1"))
        assertTrue(s.inFlight("s1"))

        // B completes:
        s.complete(b, null, "s1")
        assertFalse(s.busy("s1"))
        assertFalse(s.inFlight("s1"))
        assertTrue(s.canSend("s1"))
    }
}
