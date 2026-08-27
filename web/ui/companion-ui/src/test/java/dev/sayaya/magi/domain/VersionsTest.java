package dev.sayaya.magi.domain;

import dev.sayaya.magi.bridge.AgentStates;
import dev.sayaya.magi.client.domain.Spans;
import dev.sayaya.magi.client.domain.Versions;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** verCmp 이식의 계약: 숫자 코어 우선, describe 접미는 동률만 깬다. */
class VersionsTest {
    @Test void ordersNumericCore() {
        assertTrue(Versions.compare("v1.2.0", "v1.10.0") < 0);   // 문자열 비교였다면 반대
        assertTrue(Versions.compare("v2.0.0", "v1.99.9") > 0);
        assertEquals(0, Versions.compare("v1.2.3", "1.2.3"));    // v 접두는 장식
    }

    @Test void describeSuffixBreaksTiesOnly() {
        assertTrue(Versions.compare("v1.2.3-14-gabc", "v1.2.3") > 0); // 태그를 지난 빌드
        assertTrue(Versions.compare("v1.2.3-14-gabc", "v1.2.4") < 0); // 접미가 코어를 못 이긴다
    }

    @Test void missingPartsCountAsZero() {
        assertEquals(0, Versions.compare("v1.2", "v1.2.0"));
        assertTrue(Versions.compare("", "v0.0.1") < 0);
    }
}
