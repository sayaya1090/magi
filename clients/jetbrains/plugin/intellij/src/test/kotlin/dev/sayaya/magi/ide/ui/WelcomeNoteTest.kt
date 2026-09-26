package dev.sayaya.magi.ide.ui

import com.intellij.testFramework.fixtures.BasePlatformTestCase

/**
 * The welcome hint is centred, and an italic face broke that on a real screen (2026-09-26): with
 * Korean text the paragraph measured the line in one face and drew it in the fallback face that has
 * the glyphs, so the hint sat left of the title above it (centre 716 of a 1600px window, not 800).
 * Upright, it centres. Headless fonts differ from a real screen, so this pins the cause, not the pixels.
 */
class WelcomeNoteTest : BasePlatformTestCase() {
    fun `test the welcome hint is not italic`() {
        val note = Look.welcomeNote("아래에 무엇을 할지 적으세요.")
        assertFalse("an italic hint is measured and drawn in different faces and drifts off centre", note.font.isItalic)
    }
}
