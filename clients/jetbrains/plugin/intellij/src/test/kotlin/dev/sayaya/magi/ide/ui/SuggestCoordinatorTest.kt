package dev.sayaya.magi.ide.ui

import org.junit.Assert.*
import org.junit.Test

class SuggestCoordinatorTest {

    @Test
    fun `test startMention returns valid ticket and respects dismissedToken and disposed`() {
        val coordinator = SuggestCoordinator()
        assertEquals(0L, coordinator.epoch)
        assertNull(coordinator.dismissedToken)
        assertFalse(coordinator.isDisposed)

        val ticket = coordinator.startMention("file1", "s1")
        assertNotNull(ticket)
        assertEquals(0L, ticket!!.epoch)
        assertEquals("s1", ticket.session)
        assertEquals("file1", ticket.token)

        // Dismiss ticket
        coordinator.dismissMention(ticket, "s1")
        assertEquals("file1", coordinator.dismissedToken)

        // Same token cannot be started again
        assertNull(coordinator.startMention("file1", "s1"))

        // Different token can be started
        val ticket2 = coordinator.startMention("file2", "s1")
        assertNotNull(ticket2)

        // Disposed coordinator cannot start
        coordinator.dispose()
        assertTrue(coordinator.isDisposed)
        assertNull(coordinator.startMention("file2", "s1"))
    }

    @Test
    fun `test startSuggestion returns valid ticket and respects enabled flag and disposed`() {
        val coordinator = SuggestCoordinator()

        // Disabled returns null
        assertNull(coordinator.startSuggestion("prefix", "s1", enabled = false))

        // Enabled returns ticket
        val ticket = coordinator.startSuggestion("prefix", "s1", enabled = true)
        assertNotNull(ticket)
        assertEquals(0L, ticket!!.epoch)
        assertEquals("s1", ticket.session)
        assertEquals("prefix", ticket.prefix)

        // Disposed returns null even if enabled
        coordinator.dispose()
        assertNull(coordinator.startSuggestion("prefix", "s1", enabled = true))
    }

    @Test
    fun `test A request then B edit rejects delivery and EDT presentation`() {
        val coordinator = SuggestCoordinator()
        val sTicket = coordinator.startSuggestion("hel", "s1", enabled = true)!!
        val mTicket = coordinator.startMention("foo", "s1")!!

        // User edits input (epoch incremented)
        coordinator.bumpEpoch()
        assertEquals(1L, coordinator.epoch)

        // Background deliver rejected due to stale epoch
        assertFalse("Suggestion deliver must be rejected when epoch changed", coordinator.canDeliverSuggestion(sTicket, "s1"))
        assertFalse("Mention deliver must be rejected when epoch changed", coordinator.canDeliverMention(mTicket, "s1"))

        // EDT presentation rejected due to stale epoch
        assertFalse("Suggestion EDT presentation must be rejected when epoch changed", coordinator.canPresentSuggestion(sTicket, "s1", "help"))
        assertFalse("Mention EDT presentation must be rejected when epoch changed", coordinator.canPresentMention(mTicket, "s1", "foobar"))
    }

    @Test
    fun `test text or token mismatch rejects EDT presentation even if epoch matches`() {
        val coordinator = SuggestCoordinator()
        val sTicket = coordinator.startSuggestion("prefixA", "s1", enabled = true)!!
        val mTicket = coordinator.startMention("tokenA", "s1")!!

        // Text mismatch on suggestion
        assertFalse(coordinator.canPresentSuggestion(sTicket, "s1", "prefixB"))
        assertTrue(coordinator.canPresentSuggestion(sTicket, "s1", "prefixA"))

        // Token mismatch on mention
        assertFalse(coordinator.canPresentMention(mTicket, "s1", "tokenB"))
        assertTrue(coordinator.canPresentMention(mTicket, "s1", "tokenA"))
    }

    @Test
    fun `test session switch rejects delivery presentation and choice acceptance`() {
        val coordinator = SuggestCoordinator()
        val sTicket = coordinator.startSuggestion("prefix", "s1", enabled = true)!!
        val mTicket = coordinator.startMention("token", "s1")!!

        // Session switched to s2
        assertFalse(coordinator.canDeliverSuggestion(sTicket, "s2"))
        assertFalse(coordinator.canDeliverMention(mTicket, "s2"))
        assertFalse(coordinator.canPresentSuggestion(sTicket, "s2", "prefix"))
        assertFalse(coordinator.canPresentMention(mTicket, "s2", "token"))
        assertFalse(coordinator.canAcceptMentionChoice(mTicket, "s2"))
        assertFalse(coordinator.acceptMentionChoice(mTicket, "s2"))

        // Still valid on original session s1
        assertTrue(coordinator.canDeliverSuggestion(sTicket, "s1"))
        assertTrue(coordinator.canDeliverMention(mTicket, "s1"))
        assertTrue(coordinator.canPresentSuggestion(sTicket, "s1", "prefix"))
        assertTrue(coordinator.canPresentMention(mTicket, "s1", "token"))
        assertTrue(coordinator.canAcceptMentionChoice(mTicket, "s1"))
    }

    @Test
    fun `test dismissMention ignores stale epoch or session`() {
        val coordinator = SuggestCoordinator()
        val ticket = coordinator.startMention("token", "s1")!!

        // Dismiss with mismatched session does not set dismissedToken
        coordinator.dismissMention(ticket, "s2")
        assertNull(coordinator.dismissedToken)

        // User typed before popup closed
        coordinator.bumpEpoch()
        coordinator.dismissMention(ticket, "s1")
        assertNull(coordinator.dismissedToken)

        // Matching dismiss succeeds
        val ticket2 = coordinator.startMention("token2", "s1")!!
        coordinator.dismissMention(ticket2, "s1")
        assertEquals("token2", coordinator.dismissedToken)
    }

    @Test
    fun `test stale popup selection callback rejected without side effects`() {
        val coordinator = SuggestCoordinator()
        val ticket = coordinator.startMention("token", "s1")!!

        // Set an existing dismissed token
        val dummyTicket = coordinator.startMention("old", "s1")!!
        coordinator.dismissMention(dummyTicket, "s1")
        assertEquals("old", coordinator.dismissedToken)

        // User typed (stale epoch)
        coordinator.bumpEpoch()

        // Old choice callback fired
        assertFalse(coordinator.canAcceptMentionChoice(ticket, "s1"))
        val accepted = coordinator.acceptMentionChoice(ticket, "s1")
        assertFalse(accepted)
        // dismissedToken must not be cleared by rejected choice
        assertEquals("old", coordinator.dismissedToken)
    }

    @Test
    fun `test valid candidate selection clears dismissedToken and accepts choice`() {
        val coordinator = SuggestCoordinator()
        val ticket = coordinator.startMention("token", "s1")!!

        // Dismiss previous token
        coordinator.dismissMention(ticket, "s1")
        assertEquals("token", coordinator.dismissedToken)

        // New ticket
        coordinator.bumpEpoch()
        val ticket2 = coordinator.startMention("token2", "s1")!!

        assertTrue(coordinator.canAcceptMentionChoice(ticket2, "s1"))
        assertTrue(coordinator.acceptMentionChoice(ticket2, "s1"))
        assertNull("Dismissed token must be cleared after successful choice", coordinator.dismissedToken)
    }

    @Test
    fun `test dispose rejects in-flight delivery presentation and selection`() {
        val coordinator = SuggestCoordinator()
        val sTicket = coordinator.startSuggestion("prefix", "s1", enabled = true)!!
        val mTicket = coordinator.startMention("token", "s1")!!

        coordinator.dispose()
        assertTrue(coordinator.isDisposed)
        assertEquals(1L, coordinator.epoch)

        assertFalse(coordinator.canDeliverSuggestion(sTicket, "s1"))
        assertFalse(coordinator.canDeliverMention(mTicket, "s1"))
        assertFalse(coordinator.canPresentSuggestion(sTicket, "s1", "prefix"))
        assertFalse(coordinator.canPresentMention(mTicket, "s1", "token"))
        assertFalse(coordinator.canAcceptMentionChoice(mTicket, "s1"))
        assertFalse(coordinator.acceptMentionChoice(mTicket, "s1"))
    }

    @Test
    fun `test extractAtToken parsing rules`() {
        assertEquals("foo", SuggestCoordinator.extractAtToken("@foo"))
        assertEquals("bar", SuggestCoordinator.extractAtToken("hello @bar"))
        assertEquals("multiline", SuggestCoordinator.extractAtToken("line 1\n@multiline"))
        assertNull("No whitespace before @", SuggestCoordinator.extractAtToken("user@domain"))
        assertNull("Too short (< 2 chars)", SuggestCoordinator.extractAtToken("@a"))
        assertNull("Bare at sign", SuggestCoordinator.extractAtToken("@"))
        assertNull("Whitespace in tail", SuggestCoordinator.extractAtToken("@foo bar"))
        assertNull("No @ in text", SuggestCoordinator.extractAtToken("just regular text"))
    }

    @Test
    fun `test escapeGlob escapes all special glob meta characters`() {
        assertEquals("normal", SuggestCoordinator.escapeGlob("normal"))
        assertEquals("foo\\*bar", SuggestCoordinator.escapeGlob("foo*bar"))
        assertEquals("foo\\?bar", SuggestCoordinator.escapeGlob("foo?bar"))
        assertEquals("foo\\[bar\\]", SuggestCoordinator.escapeGlob("foo[bar]"))
        assertEquals("foo\\\\bar", SuggestCoordinator.escapeGlob("foo\\bar"))
        assertEquals("\\[\\*\\]\\?\\\\", SuggestCoordinator.escapeGlob("[*]?\\"))
    }
}
