package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.RailModes;
import org.junit.jupiter.api.Test;

import static dev.sayaya.magi.client.domain.RailModes.State.COLLAPSE;
import static dev.sayaya.magi.client.domain.RailModes.State.EXPAND;
import static dev.sayaya.magi.client.domain.RailModes.State.HIDE;
import static org.junit.jupiter.api.Assertions.assertEquals;

/** handbook DrawerModeTest의 magi 번역 — 두 기둥은 상호 배타로 기둥 자리를 나눈다. */
class RailModesTest {
    @Test
    void collapsedWithoutToolsIsTheMenuColumn() {
        assertEquals(COLLAPSE, RailModes.menu(false, 0, false));
        assertEquals(HIDE, RailModes.tool(false, 0, false, false));
        // 도구 하나는 레일이 필요 없다 — 자동 선택과 같다.
        assertEquals(COLLAPSE, RailModes.menu(false, 1, false));
        assertEquals(HIDE, RailModes.tool(false, 1, false, false));
    }

    @Test
    void collapsedWithToolsSwapsTheColumn() {
        assertEquals(HIDE, RailModes.menu(false, 2, false));
        assertEquals(COLLAPSE, RailModes.tool(false, 2, false, false));
    }

    @Test
    void hoverPeeksTheCollapsedToolRail() {
        assertEquals(EXPAND, RailModes.tool(false, 2, true, false));
        assertEquals(HIDE, RailModes.menu(false, 2, false));
    }

    @Test
    void dismissBringsTheMenuBackWithoutLosingSelection() {
        assertEquals(COLLAPSE, RailModes.menu(false, 2, true));
        assertEquals(HIDE, RailModes.tool(false, 2, false, true));
    }

    @Test
    void openDrawerShowsMenuLabelsAndToolsBesideThem() {
        assertEquals(EXPAND, RailModes.menu(true, 2, false));
        assertEquals(EXPAND, RailModes.tool(true, 2, false, false));
        assertEquals(EXPAND, RailModes.menu(true, 0, false));
        assertEquals(HIDE, RailModes.tool(true, 0, false, false));
        // 열림은 닫힘(←)을 무시한다 — ←는 접힌 기둥의 것이다.
        assertEquals(EXPAND, RailModes.tool(true, 2, false, true));
    }

    @Test
    void theTwoColumnsNeverCollapseTogether() {
        for (boolean open : new boolean[]{false, true})
            for (int tools : new int[]{0, 1, 2, 5})
                for (boolean hover : new boolean[]{false, true})
                    for (boolean dismissed : new boolean[]{false, true}) {
                        RailModes.State m = RailModes.menu(open, tools, dismissed);
                        RailModes.State t = RailModes.tool(open, tools, hover, dismissed);
                        // 접힌 기둥은 한 번에 하나의 것이다.
                        boolean bothColumns = m == COLLAPSE && t == COLLAPSE;
                        assertEquals(false, bothColumns);
                    }
    }
}
