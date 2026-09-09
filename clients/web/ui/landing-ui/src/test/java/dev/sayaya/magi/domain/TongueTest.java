package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Tongue;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;

/** 무슨 말로 그리는가 — 콘솔의 규칙(bridge/Labels.pick)과 같은 순서. */
class TongueTest {
    @Test
    void whatThePersonChoseComesFirst() {
        assertEquals(Tongue.EN, Tongue.pick("en", new String[]{"ko-KR", "ko"}));
        assertEquals(Tongue.KO, Tongue.pick("ko", new String[]{"en-US"}));
    }

    @Test
    void withoutAChoiceTheBrowsersOrderDecides() {
        assertEquals(Tongue.KO, Tongue.pick("", new String[]{"ko-KR", "en-US"}));
        assertEquals(Tongue.EN, Tongue.pick(null, new String[]{"en-GB", "ko"}));
    }

    /** 아는 말이 하나도 없으면 영어 — 빈 화면보다 낫다. */
    @Test
    void anUnknownWorldFallsBackToEnglish() {
        assertEquals(Tongue.EN, Tongue.pick(null, new String[]{"fr", "de"}));
        assertEquals(Tongue.EN, Tongue.pick(null, null));
        assertEquals(Tongue.EN, Tongue.pick("kr", new String[0]), "'kr'은 언어 코드가 아니다");
    }

    @Test
    void aStoredWordThatIsNotALanguageDoesNotDecide() {
        assertNull(Tongue.byCode("kr"));
        assertNull(Tongue.byCode(null));
        // 저장소에 헛것이 적혀 있어도 브라우저의 선호가 살아 있어야 한다.
        assertEquals(Tongue.KO, Tongue.pick("kr", new String[]{"ko"}));
    }

    @Test
    void theSwitchAlwaysPointsAtTheOtherOne() {
        assertEquals(Tongue.KO, Tongue.EN.other());
        assertEquals(Tongue.EN, Tongue.KO.other());
        assertEquals("한국어", Tongue.EN.other().tongueName());
    }
}
