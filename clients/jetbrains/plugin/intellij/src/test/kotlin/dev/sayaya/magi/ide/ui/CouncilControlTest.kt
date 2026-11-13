package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase

class CouncilControlTest : BasePlatformTestCase() {
    fun testConfirmationCancellationAndFailureKeepDaemonValue() {
        var accepted = false
        var calls = 0
        var complete: (() -> Unit)? = null
        val control = CouncilControl({ accepted }) { session, enabled, done ->
            assertEquals("one", session)
            assertFalse(enabled)
            calls++
            complete = done
        }
        assertFalse(control.isEnabled)
        control.update("one", true)
        control.doClick()
        assertEquals(0, calls)
        assertTrue(control.isSelected)
        accepted = true
        control.doClick()
        control.doClick()
        assertEquals(1, calls)
        assertFalse(control.isEnabled)
        assertTrue(control.isSelected)
        control.update("one", null)
        complete!!()
        assertFalse(control.isEnabled)
        control.update("one", true)
        assertTrue(control.isEnabled)
        assertTrue(control.isSelected)
        control.update("one", false)
        assertFalse(control.isSelected)
    }

    fun testSessionChangeDuringConfirmationDoesNotSendToEitherSession() {
        lateinit var control: CouncilControl
        control = CouncilControl({ control.update("two", false); true }) { _, _, _ -> fail("stale target") }
        control.update("one", true)
        control.doClick()
        assertFalse(control.isSelected)
        control.update("two", null)
        assertFalse(control.isEnabled)
    }
}
