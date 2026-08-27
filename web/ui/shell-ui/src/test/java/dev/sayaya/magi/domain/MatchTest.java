package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Match;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

/** 팔레트가 후보를 고르는 규칙 — 어떻게 맞았는가가 순서를 정한다. */
class MatchTest {
    @Test
    void whereItMatchedDecidesTheOrder() {
        assertEquals(Match.HEAD, Match.score("companions", "comp"));
        assertEquals(Match.INSIDE, Match.score("companions", "panion"));
        // 글자만 순서대로 흩어져 있어도 답이다 — 다만 가장 아래다.
        assertEquals(Match.SCATTERED, Match.score("companions", "cmp"));
        assertEquals(Match.NONE, Match.score("companions", "zzz"));
    }

    @Test
    void caseDoesNotMatterAndEmptyAsksNothing() {
        assertEquals(Match.HEAD, Match.score("Knowledge", "KNOW"));
        assertEquals(0, Match.score("Knowledge", ""));
    }
}
