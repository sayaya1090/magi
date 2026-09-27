package dev.sayaya.magi.ide.usecase

import java.io.Closeable
import org.junit.jupiter.api.Assertions.*
import org.junit.jupiter.api.Test

class HandRegistrationTest {
    @Test fun `close during attach cleans after attach finishes exactly once`() {
        val registration = HandRegistration()
        var closed = 0
        var detached = 0
        assertTrue(registration.install(Closeable { closed++ }))
        assertEquals("ok", registration.attach({ detached++ }) {
            registration.close()
            registration.close()
            assertEquals(1, closed)
            assertEquals(0, detached)
            "ok"
        })
        assertEquals(1, detached)
        registration.close()
        assertEquals(1, detached)
    }
    @Test fun `closed registration rejects server and never attaches`() {
        val registration = HandRegistration()
        registration.close()
        var closed = 0
        assertFalse(registration.install(Closeable { closed++ }))
        assertEquals(1, closed)
        assertNull(registration.attach({ error("detach") }) { error("attach") })
    }
    @Test fun `failed attach still cleans once on close`() {
        val registration = HandRegistration()
        var detached = 0
        registration.install(Closeable {})
        assertThrows(IllegalStateException::class.java) {
            registration.attach({ detached++ }) { error("lost response") }
        }
        registration.close()
        registration.close()
        assertEquals(1, detached)
    }
}
