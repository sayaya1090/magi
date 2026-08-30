package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Updates;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * 갱신 버튼의 기억 — 운영 updateControl.state가 모듈 스코프에 들고 있던 그 규칙들.
 * 어느 것도 DOM을 필요로 하지 않는다.
 */
class UpdatesTest {
    private static final String SOCK = "/tmp/a1.sock";

    @Test void standsOnlyOnBehindBuilds() {
        Updates u = new Updates();
        assertTrue(u.button(SOCK, true));
        assertFalse(u.button(SOCK, false), "최신인 것을 최신으로 만드는 버튼은 서지 않는다");
        assertEquals("", u.line(SOCK));
    }

    @Test void locksWhileWorking() {
        Updates u = new Updates();
        u.began(SOCK, "checking…");
        assertTrue(u.busy(SOCK));
        assertEquals("checking…", u.line(SOCK));
        assertFalse(u.button(SOCK, true), "받는 중에는 버튼이 아니라 하는 말이 선다");
    }

    @Test void whatTheDaemonSaidReplacesTheButton() {
        Updates u = new Updates();
        u.began(SOCK, "checking…");
        u.ended(SOCK, "  already at v0.28.0  ", "update failed");
        assertFalse(u.busy(SOCK));
        assertEquals("already at v0.28.0", u.line(SOCK), "앞뒤 공백만 턴다 — 말은 데몬의 것이다");
        assertFalse(u.button(SOCK, true), "사유가 있는 답에 버튼을 다시 세우면 같은 사유를 다시 받으란 말이 된다");
    }

    @Test void aBrokenWireKeepsTheButton() {
        Updates u = new Updates();
        u.began(SOCK, "checking…");
        u.ended(SOCK, "", "update failed");
        assertEquals("update failed", u.line(SOCK));
        assertTrue(u.button(SOCK, true), "아무도 아무 말도 안 했으면 다시 눌러 볼 만하다");
        assertFalse(u.button(SOCK, false), "그래도 뒤처졌을 때만");
    }

    @Test void leavingForgetsFinishedOnly() {
        Updates u = new Updates();
        u.began("/tmp/a1.sock", "checking…");
        u.began("/tmp/a2.sock", "checking…");
        u.ended("/tmp/a2.sock", "updated, restarting", "update failed");

        u.forgetFinished();

        assertTrue(u.busy("/tmp/a1.sock"), "받는 중인 것은 남는다 — 돌아와 두 번 보내지 않게");
        assertEquals("", u.line("/tmp/a2.sock"), "끝난 줄은 할 말을 다 했다");
    }

    @Test void socketsDoNotShareAnAnswer() {
        Updates u = new Updates();
        u.began("/tmp/a1.sock", "checking…");
        assertFalse(u.busy("/tmp/a2.sock"));
        assertTrue(u.button("/tmp/a2.sock", true));
    }

    @Test void aNamelessSocketIsStillOne() {
        Updates u = new Updates();
        u.began(null, "checking…");
        assertTrue(u.busy(null));
        assertTrue(u.busy(""));
    }
}
