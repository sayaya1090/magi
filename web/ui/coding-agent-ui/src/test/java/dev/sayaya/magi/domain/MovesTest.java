package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Moves;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** 옮기고 보내기의 두 술어 — 운영 movingTo와 composerReach의 idle 판정. */
class MovesTest {
    @Test
    void theOrdinaryPageMovesNowhere() {
        assertEquals("", Moves.to(null, "s_now"));
    }

    @Test
    void theListIsNotAConversation() {
        // 빈 past는 목록이다 — 보낼 곳으로 고른 세션이 아직 없다.
        assertEquals("", Moves.to("", "s_now"));
    }

    @Test
    void theSessionOnScreenIsTheOneItIsIn() {
        assertEquals("", Moves.to("s_now", "s_now"));
    }

    @Test
    void anotherSessionHasToBeMovedInto() {
        assertEquals("s_old", Moves.to("s_old", "s_now"));
    }

    @Test
    void withoutARowTheMoveIsStillOffered() {
        // 명단을 못 읽은 화면은 "지금 세션"을 모른다 — 모르는 것을 같다고 치면 옮기지 않고
        // 보내게 되고, 그 말은 화면 밖 대화로 들어간다. 물어보는 쪽이 안전하다.
        assertEquals("s_old", Moves.to("s_old", null));
    }

    @Test
    void nothingToMoveIsNeverBlocked() {
        assertFalse(Moves.blocked("", "working"));
    }

    @Test
    void aCompanionStillTalkingCannotLeave() {
        assertTrue(Moves.blocked("s_old", "working"));
        assertTrue(Moves.blocked("s_old", "waiting"));
    }

    @Test
    void aStillCompanionCanMove() {
        assertFalse(Moves.blocked("s_old", "idle"));
        assertFalse(Moves.blocked("s_old", "stopped"));
        // 행이 없으면 도는 것을 본 적이 없다 — 막지 않는다(운영 `!a ||`).
        assertFalse(Moves.blocked("s_old", null));
    }
}
