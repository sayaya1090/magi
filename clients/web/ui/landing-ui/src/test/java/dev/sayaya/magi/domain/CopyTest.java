package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Copy;
import dev.sayaya.magi.client.domain.Page;
import dev.sayaya.magi.client.domain.Tongue;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * 두 말이 같은 페이지를 말하는가.
 *
 * 이 시험이 있는 이유는 한쪽 말에만 문장을 더하는 실수가 <b>그 말을 읽는 사람에게만</b>
 * 보이기 때문이다. 영어로 열어 보고 초록이라 여기는 동안 한국어 페이지에는 키 문자열이
 * 서 있다(콘솔에서 실제로 있었던 모양: 설정 화면만 한국어였다).
 */
class CopyTest {
    @Test
    void everyKeyThePageAsksForIsAnsweredInBothTongues() {
        List<String> missing = new ArrayList<>();
        for (String key : Page.keys()) {
            for (Tongue tongue : Tongue.values()) {
                // 사전은 모르는 키를 키 그대로 돌려준다 — 그래서 '답한 것과 물은 것이 같다'가 곧 결번이다.
                if (key.equals(Copy.word(tongue, key))) missing.add(tongue.code() + ":" + key);
            }
        }
        assertEquals(List.of(), missing, "말이 빠진 자리");
    }

    /**
     * 그리고 그 반대 — 아무도 묻지 않는 말. 지운 절의 문장이 사전에 남으면 다음 사람이 그것을
     * 화면에서 찾다가 없다는 것을 알게 된다.
     */
    @Test
    void theDictionaryCarriesNothingThePageNeverAsksFor() {
        List<String> asked = Page.keys();
        List<String> orphans = new ArrayList<>();
        for (String key : Copy.keys()) if (!asked.contains(key)) orphans.add(key);
        assertEquals(List.of(), orphans, "아무도 묻지 않는 말");
    }

    @Test
    void thePageAsksEachKeyOnlyOnce() {
        List<String> asked = Page.keys();
        List<String> seen = new ArrayList<>();
        List<String> twice = new ArrayList<>();
        for (String key : asked) {
            if (seen.contains(key)) twice.add(key);
            else seen.add(key);
        }
        assertEquals(List.of(), twice, "모양이 두 번 묻는 키");
    }

    /** 기계가 낸 글은 번역하지 않는다 — 붙여 넣어 돌릴 수 있어야 하기 때문이다. */
    @Test
    void machineTextStaysOutOfTheDictionary() {
        assertTrue(Page.INSTALL.startsWith("curl -fsSL https://"), "설치 한 줄이 명령이 아니다");
        assertTrue(Page.TRANSCRIPT.length > 5, "전사가 비었다");
        for (String key : Copy.keys()) {
            assertTrue(!Copy.word(Tongue.EN, key).equals(Page.INSTALL), "명령이 사전에 들어왔다: " + key);
        }
    }
}
