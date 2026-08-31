package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.ModelChoices;

import org.junit.jupiter.api.Test;

import java.util.Arrays;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;

/** 고르개가 켜 둘 값을 찾지 못하면 그 칸은 빈 칸이 된다 — 그 자리를 막는 계약. */
class ModelChoicesTest {
    @Test void theRunningModelIsAlwaysOfferable() {
        // 실측한 모양: 데몬은 클라우드 모델 위에 있었고, 그 백엔드의 목록은 로컬 것들이었다.
        List<String> got = ModelChoices.offer(Arrays.asList("qwen3-coder:30b", "gpt-oss:20b"), "gpt-oss:120b-cloud");
        assertEquals("gpt-oss:120b-cloud", got.get(0), "the model it is on must be selectable");
        assertEquals(3, got.size());
    }

    @Test void anAnsweredListKeepsItsOrderAndIsNotDuplicated() {
        List<String> got = ModelChoices.offer(Arrays.asList("a", "b", "c"), "b");
        assertEquals(Arrays.asList("a", "b", "c"), got);
    }

    @Test void nothingAnsweredStillOffersWhatItIsOn() {
        assertEquals(List.of("only"), ModelChoices.offer(List.of(), "only"));
        assertEquals(List.of(), ModelChoices.offer(List.of(), ""));
        assertEquals(List.of("x"), ModelChoices.offer(null, "x"));
    }
}
