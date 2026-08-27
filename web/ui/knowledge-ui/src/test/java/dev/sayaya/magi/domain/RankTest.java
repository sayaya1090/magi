package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Rank;
import org.junit.jupiter.api.Test;

import java.util.List;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;

/** 운영 rankByIDF과 같은 코퍼스, 같은 순서 — 드문 단어가 이긴다. */
class RankTest {
    @Test
    void rareSharedWordsWinOverPassingMentions() {
        List<String> docs = List.of(
                "prompt caching rule: always reuse the cache window",
                "notes about many things, cache mentioned in passing along with logs and tests",
                "wholly unrelated");
        assertArrayEquals(new int[]{0, 1}, Rank.order("cache", docs));
    }

    @Test
    void emptyOrShortQueryKeepsEveryRowInOrder() {
        List<String> docs = List.of("a", "b", "c");
        assertArrayEquals(new int[]{0, 1, 2}, Rank.order("", docs));
        assertArrayEquals(new int[]{0, 1, 2}, Rank.order("ab", docs));
    }

    @Test
    void moreMatchedTokensBreakTies() {
        List<String> docs = List.of("alpha beta", "alpha", "beta alpha extra");
        int[] got = Rank.order("alpha beta", docs);
        // 두 토큰을 다 가진 문서들이 앞, 하나뿐인 문서가 뒤.
        assertArrayEquals(new int[]{0, 2, 1}, got);
    }
}
