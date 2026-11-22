package dev.sayaya.magi.ide.usecase

import dev.sayaya.magi.ide.model.ProviderChoice
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/**
 * 정보 창의 「백엔드」 콤보는 프로필만이 아니라 지금 서빙 중인 프로바이더(codex·claudecode·
 * antigravity 셰임)도 고르게 한다 — 웹 콘솔과 TUI 의 `/providers` 가 고르게 하는 그것을, 데몬의
 * `providers` 문으로 받아서(사용자 요청, 2026-11-22).
 */
class BackendPickTest {
    private val claude = ProviderChoice(name = "claudecode", base = "http://127.0.0.1:53345/v1", models = listOf("opus"))
    private val codex = ProviderChoice(name = "codex", base = "http://127.0.0.1:55545/v1")

    @Test
    fun `프로필 뒤에 서빙 중인 프로바이더가 서고 같은 것은 한 번만 선다`() {
        assertEquals(
            listOf("fast", claude.base, codex.base),
            BackendPick.entries(listOf("fast"), listOf(claude, codex, claude)),
        )
    }

    @Test
    fun `프로바이더 항목은 주소가 아니라 이름으로 보인다`() {
        assertEquals("codex", BackendPick.label(codex.base, listOf(claude, codex), "default"))
        assertEquals("fast", BackendPick.label("fast", listOf(claude, codex), "default"))
        assertEquals("default", BackendPick.label("", listOf(claude, codex), "default"))
    }

    @Test
    fun `프로필이 없으면 지금 백엔드 주소와 같은 프로바이더가 골라져 있다`() {
        assertEquals("fast", BackendPick.current("fast", claude.base, listOf(claude)))
        assertEquals(claude.base, BackendPick.current("", "http://127.0.0.1:53345/v1/", listOf(claude, codex)))
        assertEquals("", BackendPick.current(null, "http://localhost:11434/v1", listOf(claude)))
    }

    @Test
    fun `주소 항목은 use-backend 로, 이름 항목은 use-profile 로 간다`() {
        assertTrue(BackendPick.isProvider(codex.base))
        assertTrue(BackendPick.isProvider("https://gw.example/v1"))
        assertFalse(BackendPick.isProvider("fast"))
        assertFalse(BackendPick.isProvider(""))
    }
}
