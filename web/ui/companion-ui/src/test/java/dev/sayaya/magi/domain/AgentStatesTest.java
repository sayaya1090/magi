package dev.sayaya.magi.domain;

import dev.sayaya.magi.bridge.AgentStates;
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

    @Test void goneIsOneTile() {
        assertEquals("gone", AgentStates.groupOf("stopped"));
        assertEquals("gone", AgentStates.groupOf("abandoned"));
    }

    @Test void unknownReadsAsIdleNotAsCrash() {
        assertEquals(2, AgentStates.orderOf("someday-state"));
        assertEquals("idle", AgentStates.groupOf(null));
    }
}
