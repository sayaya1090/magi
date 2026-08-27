package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Prefs;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** 취향의 순수 규칙 — 값이 셋인 테마와, 저장된 적 없는 스위치. */
class PrefsTest {
    @Test
    void themeCyclesThroughThree() {
        assertEquals("light", Prefs.nextTheme("system"));
        assertEquals("dark", Prefs.nextTheme("light"));
        // 두 상태였다면 여기서 "기계를 따름"으로 돌아올 길이 없다.
        assertEquals("system", Prefs.nextTheme("dark"));
        assertEquals("light", Prefs.nextTheme("무엇인지 모를 값"));
    }

    @Test
    void followingTheMachineIsWritingNothing() {
        assertNull(Prefs.themeAttribute("system"));
        assertEquals("light", Prefs.themeAttribute("light"));
        assertEquals("dark", Prefs.themeAttribute("dark"));
    }

    @Test
    void anUnsetSwitchIsItsDefault() {
        assertTrue(Prefs.on(null, true));
        assertFalse(Prefs.on(null, false));
        assertFalse(Prefs.on("off", true));
        assertTrue(Prefs.on("on", false));
    }

    @Test
    void theFileDependsOnWhereYouStand() {
        assertEquals("settings.scope_global", Prefs.scopeKey(null));
        assertEquals("settings.scope_global", Prefs.scopeKey(""));
        assertEquals("settings.scope_project", Prefs.scopeKey("/tmp/a1.sock"));
    }
}
