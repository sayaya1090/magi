package dev.sayaya.magi.domain;

import dev.sayaya.magi.bridge.AgentStates;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.FleetProgress;
import dev.sayaya.magi.component.Spans;
import dev.sayaya.magi.client.domain.Versions;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** 눈의 순서와 요약 그룹 — 골칫거리 먼저, stopped와 abandoned는 하나의 gone. */
class AgentStatesTest {
    @Test void troubleSortsFirst() {
        assertTrue(AgentStates.orderOf("waiting") < AgentStates.orderOf("working"));
        assertTrue(AgentStates.orderOf("working") < AgentStates.orderOf("idle"));
        assertTrue(AgentStates.orderOf("idle") < AgentStates.orderOf("abandoned"));
        assertTrue(AgentStates.orderOf("stopped") < AgentStates.orderOf("remote"));
    }

    /** 압축 막대는 센 값만 그린다 — 조각이 하나면 남은 양을 모르므로 비율을 지어내지 않는다. */
    @Test void theFoldBarFillsByFinishedPartsAndNeverInventsAShare() {
        FleetAgent a = new FleetAgent();
        a.state = "working";
        assertEquals(-1, AgentStates.foldFraction(a), 0);
        assertEquals("", AgentStates.foldPart(a));
        a.progress = new FleetProgress();
        a.progress.done = 1;
        a.progress.total = 3;
        assertEquals(1.0 / 3, AgentStates.foldFraction(a), 1e-9);
        assertEquals("2/3", AgentStates.foldPart(a));
        a.progress.done = 0;
        a.progress.total = 1;
        assertEquals(-1, AgentStates.foldFraction(a), 0);
        assertEquals("1/1", AgentStates.foldPart(a));
    }

    /** 진행 말이 같아도 센 값이 바뀌면 열쇠가 바뀐다 — 그래야 대화 화면 막대가 따라 움직인다. */
    @Test void theFoldKeyMovesWithTheCountNotOnlyTheWords() {
        FleetAgent a = new FleetAgent();
        assertEquals("", AgentStates.foldKey(a));
        a.progress = new FleetProgress();
        a.progress.done = 0;
        a.progress.total = 2;
        a.progress.chars = 10;
        a.progress.elapsedMs = 1000;
        String first = AgentStates.foldKey(a);
        a.progress.chars = 30;
        assertTrue(!first.equals(AgentStates.foldKey(a)), "글자 수가 늘었는데 열쇠가 같다");
        assertEquals("0/2/30/1000", AgentStates.foldKey(a));
    }

    @Test void goneIsOneTile() {
        assertEquals("gone", AgentStates.groupOf("stopped"));
        assertEquals("gone", AgentStates.groupOf("abandoned"));
    }

    /**
     * 스텝 0 의 뜻은 상태가 정한다.
     *
     * <p>명단은 열린 턴이 있을 때만 이 수를 채운다. 그래서 같은 0 이 두 가지를 뜻하고, 화면은
     * 둘을 다르게 그려야 한다 — 도는 턴의 0 은 "아직 아무 도구도 안 불렀다"라는 소식이고, 쉬는
     * 컴패니언의 0 은 셀 것이 없다는 뜻이다.
     *
     * <p>여기서 재는 이유: 이 판단을 하는 화면이 둘(사실판·명단 표)인데 한쪽만 고쳐진 적이
     * 있다. 술어가 브리지에 있으면 그 갈라짐이 구조적으로 불가능하다.
     */
    @Test void aZeroMeansTwoThingsAndTheStateSaysWhich() {
        assertEquals("0", AgentStates.stepsText(agent("working", 0)));
        assertEquals("0", AgentStates.stepsText(agent("waiting", 0)));
        assertEquals("—", AgentStates.stepsText(agent("idle", 0)));
        assertEquals("—", AgentStates.stepsText(agent("stopped", 0)));
        // 센 것이 있으면 상태와 무관하게 그 수다: 멈춘 컴패니언이 몇 걸음에서 멈췄는지는
        // 그 컴패니언에 대해 남은 마지막 사실이다.
        assertEquals("7", AgentStates.stepsText(agent("stopped", 7)));
        assertEquals("—", AgentStates.stepsText(null));
    }

    private static FleetAgent agent(String state, int steps) {
        FleetAgent a = new FleetAgent();
        a.state = state;
        a.steps = steps;
        return a;
    }

    @Test void unknownReadsAsIdleNotAsCrash() {
        assertEquals(2, AgentStates.orderOf("someday-state"));
        assertEquals("idle", AgentStates.groupOf(null));
    }
}
