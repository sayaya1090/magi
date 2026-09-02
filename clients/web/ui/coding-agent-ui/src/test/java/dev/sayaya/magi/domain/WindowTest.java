package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Window;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** 전사 창의 셈 — 어디서부터 그릴 것인가. 브라우저 없이 재진다. */
class WindowTest {

    @Test
    void shortTranscriptsAreNotWindowedAtAll() {
        // 창보다 짧으면 창이 할 일이 없다. 이 자리가 대부분의 대화다.
        assertEquals(0, Window.nextFrom(10, 0, Window.CAP));
        assertEquals(0, Window.nextFrom(Window.CAP, 0, Window.CAP));
    }

    // 잘라내기는 청크로 한다. 한 행씩 밀면 행 재사용의 매치가 첫 행에서 깨져 매 프레임 창 전체를
    // 다시 짓고, 그것은 창이 없애려던 비용을 창이 만드는 꼴이다.
    @Test
    void itDoesNotMoveUntilTheSlackIsSpent() {
        // CAP를 막 넘긴 참 — 여유 안이므로 아직 움직이지 않는다.
        assertEquals(0, Window.nextFrom(Window.CAP + 1, 0, Window.CAP));
        assertEquals(0, Window.nextFrom(Window.CAP + Window.SLACK, 0, Window.CAP));
    }

    @Test
    void andThenItDropsAllTheWayBackToTheCap() {
        int total = Window.CAP + Window.SLACK + 1;
        int from = Window.nextFrom(total, 0, Window.CAP);
        assertEquals(total - Window.CAP, from);
        // 떨어진 직후에는 다시 여유가 생겼으므로 또 움직이지 않는다.
        assertEquals(from, Window.nextFrom(total, from, Window.CAP));
    }

    // 되찾기는 독자가 청한 것이라 즉시 들어준다 — 여유를 기다리지 않는다.
    @Test
    void reachingUpMovesTheWindowBackAtOnce() {
        int total = 1000;
        int from = Window.nextFrom(total, 0, Window.CAP);
        assertEquals(total - Window.CAP, from);

        int keep = Window.reach(Window.CAP);
        assertEquals(Window.CAP + Window.REACH, keep);
        assertEquals(total - keep, Window.nextFrom(total, from, keep));
    }

    /**
     * keep이 줄지 않는다는 것을 창의 움직임으로 잰다.
     *
     * 되찾은 뒤 전사가 계속 자라도, 그 자리는 앞으로 밀릴지언정 <b>되돌아가지 않는다</b>. keep을
     * 도로 CAP로 줄이면 다음 프레임의 잘라내기가 방금 되찾은 것을 가져가 창이 독자 밑에서 닫힌다.
     */
    @Test
    void whatWasReachedIsNotTakenBackAsTheTranscriptGrows() {
        int total = 1000, keep = Window.reach(Window.CAP);
        int from = Window.nextFrom(total, Window.nextFrom(total, 0, Window.CAP), keep);
        int shown = total - from;
        for (int grew = 1; grew <= 200; grew++) {
            int next = Window.nextFrom(total + grew, from, keep);
            assertTrue(next >= from, "창이 뒤로 물러섰다: " + from + " → " + next);
            assertTrue(total + grew - next >= shown - Window.SLACK,
                    "되찾은 만큼이 도로 사라졌다 (보이는 행 " + (total + grew - next) + ")");
            from = next;
        }
    }

    @Test
    void thereIsNothingLeftToReachAtTheHead() {
        assertTrue(Window.canReach(1));
        assertFalse(Window.canReach(0));
    }

    /**
     * 가정 높이는 스타일시트가 같은 이유로 주는 값과 같아야 한다.
     *
     * 첫 프레임은 그린 적 없는 행 수백 개를 잘라내므로 잴 것이 없고, 이 값으로 위쪽 상자를 세운다.
     * 두 곳이 다른 수를 쓰면 스크롤바가 처음부터 거짓말을 한다 — console.css의
     * contain-intrinsic-size:auto 3.5rem 이 그 짝이고 3.5rem = 56px다.
     */
    @Test
    void theGuessedHeightMatchesWhatTheStylesheetReserves() {
        assertEquals(56, Window.GUESS);
    }
}
