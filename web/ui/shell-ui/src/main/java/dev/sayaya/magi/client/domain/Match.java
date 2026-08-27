package dev.sayaya.magi.client.domain;

/**
 * 팔레트가 후보를 고르는 규칙 — 순수하다.
 *
 * 점수는 <b>어떻게 맞았는가</b>이지 얼마나 맞았는가가 아니다: 앞에서 맞은 것이 가장 좋고,
 * 가운데서 맞은 것이 다음이고, 글자만 순서대로 흩어져 있는 것이 마지막이다. 사람이 타이핑을
 * 멈추는 지점은 대개 이름의 앞이라서, 그 순서가 곧 사람이 기대하는 순서다.
 */
public final class Match {
    public static final int HEAD = 3, INSIDE = 2, SCATTERED = 1, NONE = -1;

    private Match() {}

    public static int score(String name, String q) {
        if (q == null || q.isEmpty()) return 0;
        String hay = name == null ? "" : name.toLowerCase();
        String needle = q.toLowerCase();
        int at = hay.indexOf(needle);
        if (at == 0) return HEAD;
        if (at > 0) return INSIDE;
        int i = 0;
        for (int k = 0; k < hay.length() && i < needle.length(); k++) {
            if (hay.charAt(k) == needle.charAt(i)) i++;
        }
        return i == needle.length() ? SCATTERED : NONE;
    }
}
