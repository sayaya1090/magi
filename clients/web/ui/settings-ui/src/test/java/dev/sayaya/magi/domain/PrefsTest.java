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

    /**
     * 스위치의 낱말은 이 화면의 것이 아니다 — 적는 것은 설정이고 읽는 것은 편집기·컴포저라
     * 규칙이 bridge에 산다. 그 자리를 여기서 재는 이유는 갈리면 조용히 어긋나기 때문이다.
     */
    @Test
    void anUnsetSwitchIsItsDefault() {
        assertTrue(dev.sayaya.magi.bridge.Prefs.means(null, true));
        assertFalse(dev.sayaya.magi.bridge.Prefs.means(null, false));
        assertFalse(dev.sayaya.magi.bridge.Prefs.means("off", true));
        assertTrue(dev.sayaya.magi.bridge.Prefs.means("on", false));
        assertEquals("off", dev.sayaya.magi.bridge.Prefs.word(false));
        assertEquals("on", dev.sayaya.magi.bridge.Prefs.word(true));
    }

    @Test
    void theFileDependsOnWhereYouStand() {
        assertEquals("settings.scope_global", Prefs.scopeKey(null));
        assertEquals("settings.scope_global", Prefs.scopeKey(""));
        assertEquals("settings.scope_project", Prefs.scopeKey("/tmp/a1.sock"));
    }
}
