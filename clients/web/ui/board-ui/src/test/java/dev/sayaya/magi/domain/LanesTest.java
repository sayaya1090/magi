package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Lanes;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

/** 운영 loadBoard의 그 규칙들 — 걸친 밤과 무팀 레인이 핵심이다. */
class LanesTest {
    @Test
    void troubleFirstIsTheFleetOrder() {
        assertEquals(0, Lanes.rank("waiting"));
        assertEquals(1, Lanes.rank("working"));
        assertEquals(5, Lanes.rank("remote"));
        assertEquals(6, Lanes.rank(null));
    }

    @Test
    void aLaneIsTheTeamOrTheLoneName() {
        assertEquals("core", Lanes.laneOf("core", "build"));
        assertEquals("build", Lanes.laneOf("", "build"));
        assertEquals("build", Lanes.laneOf(null, "build"));
    }

    @Test
    void anOvernightRunBelongsToBothDays() {
        assertEquals(true, Lanes.onDay("2026-08-26", "2026-08-27", "2026-08-26"));
        assertEquals(true, Lanes.onDay("2026-08-26", "2026-08-27", "2026-08-27"));
        assertEquals(false, Lanes.onDay("2026-08-26", "2026-08-27", "2026-08-25"));
        // 아직 끝나지 않은 일은 열린 끝 — 오늘에 속한다.
        assertEquals(true, Lanes.onDay("2026-08-26", "", "2026-08-27"));
    }
}
