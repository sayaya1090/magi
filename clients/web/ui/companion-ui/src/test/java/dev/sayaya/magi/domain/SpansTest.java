package dev.sayaya.magi.domain;

import dev.sayaya.magi.component.Spans;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

/** 아직 말이 되는 가장 큰 단위 하나 — "4s"에 맞춘 열이 "4 seconds"를 못 담는다는 규칙. */
class SpansTest {
    @Test void picksTheLargestUnitThatStillSpeaks() {
        assertEquals("4s", Spans.dur(4));
        assertEquals("2m", Spans.dur(90));       // 반올림: 90s → 2m
        assertEquals("3h", Spans.dur(3 * 3600 + 600));
        assertEquals("2d", Spans.dur(2 * 86400));
    }
}
