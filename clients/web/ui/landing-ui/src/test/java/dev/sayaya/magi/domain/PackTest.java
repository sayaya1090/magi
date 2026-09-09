package dev.sayaya.magi.domain;

import dev.sayaya.magi.client.domain.Page;
import dev.sayaya.magi.client.domain.Tongue;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * 두 말이 같은 페이지를 말하는가 — <b>배포되는 그 파일</b>에 대고.
 *
 * 말이 자바 상수였을 때 이 시험은 컴파일된 지도를 읽었다. 지금은 브라우저가 받는 바로 그 json 을
 * 읽는다. 그래서 「자바는 맞는데 팩이 낡았다」가 성립하지 않는다.
 *
 * 이 시험이 있는 이유는 한쪽 말에만 문장을 더하거나 빼는 실수가 <b>그 말을 읽는 사람에게만</b>
 * 보이기 때문이다(콘솔에서 실측된 그 모양: 설정 화면만 한국어였다).
 */
class PackTest {
    private static final Path DIR = Path.of("src", "main", "webapp", "i18n");

    @Test
    void everyKeyThePageAsksForIsAnsweredInBothTongues() throws IOException {
        List<String> missing = new ArrayList<>();
        for (Tongue tongue : Tongue.values()) {
            Map<String, String> pack = pack(tongue);
            for (String key : Page.keys()) {
                String said = pack.get(key);
                if (said == null || said.isBlank()) missing.add(tongue.code() + ":" + key);
            }
        }
        assertEquals(List.of(), missing, "말이 빠진 자리");
    }

    /** 그 반대 — 지운 절의 문장이 팩에 남으면 다음 사람이 화면에서 그것을 찾다가 없다는 것을 알게 된다. */
    @Test
    void theTonguesCarryNothingThePageNeverAsksFor() throws IOException {
        List<String> asked = Page.keys();
        for (Tongue tongue : Tongue.values()) {
            List<String> orphans = new ArrayList<>();
            for (String key : pack(tongue).keySet()) if (!asked.contains(key)) orphans.add("「" + key + "」");
            assertEquals(List.of(), orphans, tongue.code() + " 팩에서 아무도 묻지 않는 말");
        }
    }

    /** 그리고 두 팩의 키가 서로 같은가 — 한쪽에만 있는 키는 위의 둘 중 하나로 잡히지만, 이 한 줄이 그 사실을 이름으로 말한다. */
    @Test
    void bothTonguesCarryTheSameKeys() throws IOException {
        assertEquals(pack(Tongue.EN).keySet(), pack(Tongue.KO).keySet());
    }

    @Test
    void thePageAsksEachKeyOnlyOnce() {
        List<String> seen = new ArrayList<>();
        List<String> twice = new ArrayList<>();
        for (String key : Page.keys()) {
            if (seen.contains(key)) twice.add(key);
            else seen.add(key);
        }
        assertEquals(List.of(), twice, "모양이 두 번 묻는 키");
    }

    /** 기계가 낸 글은 팩에 없다 — 번역하면 붙여 넣어 돌릴 수 없게 되기 때문이다. */
    @Test
    void machineTextStaysOutOfThePacks() throws IOException {
        assertTrue(Page.INSTALL.startsWith("curl -fsSL https://"), "설치 한 줄이 명령이 아니다");
        assertTrue(Page.TRANSCRIPT.length > 5, "전사가 비었다");
        for (Tongue tongue : Tongue.values()) {
            for (String said : pack(tongue).values()) {
                assertTrue(!said.equals(Page.INSTALL), "명령이 팩에 들어왔다: " + tongue.code());
            }
        }
    }

    /**
     * 팩은 <b>평평한 문자열 지도</b>다 — 그 계약을 여기서 읽는다. json 라이브러리를 test 의존성에
     * 더하지 않는 이유는 이 파일이 그 계약보다 복잡해지면 안 되기 때문이다: 중첩이 생기는 순간
     * 화면이 읽는 코드(FetchWords)와 이 시험이 서로 다른 것을 읽게 된다.
     */
    private static Map<String, String> pack(Tongue tongue) throws IOException {
        String body = Files.readString(DIR.resolve("landing." + tongue.code() + ".json"), StandardCharsets.UTF_8);
        Map<String, String> out = new LinkedHashMap<>();
        int i = body.indexOf('{');
        assertTrue(i >= 0, "팩이 객체가 아니다");
        while (true) {
            int k0 = body.indexOf('"', i + 1);
            if (k0 < 0) break;
            int k1 = end(body, k0);
            String key = unescape(body.substring(k0 + 1, k1));
            int colon = body.indexOf(':', k1);
            int v0 = body.indexOf('"', colon);
            int v1 = end(body, v0);
            out.put(key, unescape(body.substring(v0 + 1, v1)));
            i = v1;
        }
        return out;
    }

    /** 다음 닫는 따옴표 — 이스케이프된 것은 세지 않는다. */
    private static int end(String body, int open) {
        for (int i = open + 1; i < body.length(); i++) {
            char c = body.charAt(i);
            if (c == '\\') { i++; continue; }
            if (c == '"') return i;
        }
        throw new IllegalStateException("닫히지 않은 문자열: " + open);
    }

    private static String unescape(String raw) {
        return raw.replace("\\\"", "\"").replace("\\n", "\n").replace("\\\\", "\\");
    }
}
