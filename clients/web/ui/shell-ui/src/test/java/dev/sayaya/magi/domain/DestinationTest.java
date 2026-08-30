package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Destination;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertSame;

/** 주소가 목적지가 되는 자리 — 그리고 이름이 갈린 옛 주소가 어디에 닿는가. */
class DestinationTest {
    @Test
    void everyDoorAnswersToItsOwnAddress() {
        for (Destination d : Destination.all()) assertSame(d, Destination.byId(d.id), d.id);
    }

    @Test
    void renamedAddressesStillLandWhereTheyPointed() {
        // 운영이 이 둘을 지식으로 접었다. 폴백은 이것을 잡아 주지 못한다 — 모르는 이름이
        // 플릿으로 가는 것과, 알던 이름이 옮겨 간 곳으로 가는 것은 다른 일이다.
        assertEquals("skills", Destination.byId("mcp").id);
        assertEquals("skills", Destination.byId("interventions").id);
    }

    @Test
    void anythingElseIsStillTheFirstDoor() {
        // 잘못 친 주소는 빈 화면이 아니라 첫 문이다 — 옛 주소를 접느라 이 규칙을 잃지 않는다.
        assertSame(Destination.byId("fleet"), Destination.byId("nonesuch"));
        assertSame(Destination.byId("fleet"), Destination.byId(""));
    }
}
