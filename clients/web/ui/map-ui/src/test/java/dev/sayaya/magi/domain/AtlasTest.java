package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Atlas;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

class AtlasTest {
    @Test
    void trustOrdersOwnFirst() {
        assertEquals(0, Atlas.trustRank("own"));
        assertEquals(1, Atlas.trustRank("admitted"));
        assertEquals(2, Atlas.trustRank("unknown"));
        assertEquals(3, Atlas.trustRank(""));
    }

    @Test
    void accountIsTheHalfBeforeTheAt() {
        assertEquals("you", Atlas.accountOf("you@buildbox"));
        assertEquals("solo", Atlas.accountOf("solo"));
        assertEquals("", Atlas.accountOf(null));
    }

    @Test
    void unreachableBeatsBusy() {
        assertEquals("down", Atlas.edgeClass(false, "working"));
        assertEquals("flight", Atlas.edgeClass(true, "working"));
        assertEquals("ok", Atlas.edgeClass(true, "done"));
    }
}
