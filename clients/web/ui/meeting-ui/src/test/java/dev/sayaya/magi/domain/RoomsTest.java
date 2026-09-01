package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Rooms;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** 회의실의 순수 규칙 — 운영 meetWhere/turnRows/armConvene의 그 판단들. */
class RoomsTest {
    @Test
    void whatIsBlockingComesFirst() {
        // 바닥을 쥔 사람이 있으면 그것이 아무 일도 없는 이유다 — 라운드 숫자보다 먼저 말한다.
        assertEquals("meet.yours", Rooms.stageKey(false, true, false, false, 0, 2, 5));
        assertEquals("meet.collecting", Rooms.stageKey(false, false, true, false, 0, 2, 5));
        assertEquals("meet.round", Rooms.stageKey(false, false, false, false, 0, 2, 5));
    }

    @Test
    void twoWaysToBeFinishedMeanDifferentThings() {
        assertEquals("meet.closing", Rooms.stageKey(true, false, false, false, 0, 3, 5));
        assertEquals("meet.done", Rooms.stageKey(true, false, false, false, 2, 3, 5));
        assertEquals("meet.done_spent", Rooms.stageKey(true, false, false, true, 2, 5, 5));
    }

    @Test
    void noNumbersMeansNoSentence() {
        // 낡은 데몬은 키를 빼지 않고 0을 보낸다 — "0라운드 중 0"이 그렇게 나왔다.
        assertEquals("", Rooms.stageKey(false, false, false, false, 0, 0, 0));
    }

    @Test
    void aTurnIsWhatFollowsItsQuestion() {
        boolean[] rows = {true, false, false, true, false};   // user, …, user, …
        assertArrayEquals(new int[]{1, 3}, Rooms.turnSpan(rows, 0));
        assertArrayEquals(new int[]{4, 5}, Rooms.turnSpan(rows, 1));
        // 아직 오지 않은 차례는 빈 구간이다 — 없는 것을 그리지 않는다.
        assertArrayEquals(new int[]{0, 0}, Rooms.turnSpan(rows, 2));
    }

    @Test
    void aMeetingOfOneIsNotAMeeting() {
        assertFalse(Rooms.canConvene("which store?", 1));
        assertFalse(Rooms.canConvene("  ", 2));
        assertTrue(Rooms.canConvene("which store?", 2));
        assertEquals("meet.need_topic", Rooms.blockedKey("", 2, 3));
        assertEquals("meet.need_two", Rooms.blockedKey("q", 1, 3));
        // 부를 이가 둘도 없으면 고르기 전에 그것부터가 이유다.
        assertEquals("meet.need_two", Rooms.blockedKey("q", 0, 1));
    }

    @Test
    void tintsWrapAtSix() {
        assertEquals("sp0", Rooms.tint(0));
        assertEquals("sp5", Rooms.tint(5));
        assertEquals("sp0", Rooms.tint(6));
    }

    // 「지금 하는 것」 판이 누구 것을 그리는가. 준비와 회의 중은 다른 답이다: 준비는 전원이
    // 병렬이고, 열린 방은 바닥을 쥔 하나뿐이다.
    private static final String[] NAMES = {"alpha", "beta", "you"};
    private static final boolean[] PERSON = {false, false, true};

    @Test
    void everybodyStillGettingReadyIsWorking() {
        String[] now = Rooms.workingNow(NAMES, PERSON, new boolean[]{false, true, true},
                "", false, false);
        assertArrayEquals(new String[]{"alpha"}, now);
        assertArrayEquals(new String[]{"alpha", "beta"},
                Rooms.workingNow(NAMES, PERSON, new boolean[]{false, false, true}, "", false, false));
    }

    @Test
    void anOpenedRoomHasOneWorker() {
        assertArrayEquals(new String[]{"beta"},
                Rooms.workingNow(NAMES, PERSON, new boolean[]{true, true, true}, "beta", true, false));
        // 아무도 안 쥔 그 틈(턴 사이)에는 그릴 것이 없다.
        assertArrayEquals(new String[0],
                Rooms.workingNow(NAMES, PERSON, new boolean[]{true, true, true}, "", true, false));
    }

    @Test
    void thePersonHasNoRoomToShow() {
        // 사람이 타이핑하는 동안 방은 조용하다 — 사람에게는 읽을 방이 없다.
        assertArrayEquals(new String[0],
                Rooms.workingNow(NAMES, PERSON, new boolean[]{true, true, true}, "you", true, false));
    }

    @Test
    void aFinishedMeetingIsNobodyWorking() {
        assertArrayEquals(new String[0],
                Rooms.workingNow(NAMES, PERSON, new boolean[]{true, true, true}, "beta", true, true));
    }
}
