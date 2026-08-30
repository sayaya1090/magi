package dev.sayaya.magi.client.domain;

/**
 * "vA.B.C[-extra]"를 숫자 코어로 비교한다 — page.js verCmp의 이식.
 * git-describe 접미(-14-gabc)는 그 태그를 지난 빌드라는 뜻이라 동률만 깬다.
 */
public final class Versions {
    private Versions() {}

    public static int compare(String a, String b) {
        String[] pa = parse(a), pb = parse(b);
        String[] na = pa[0].split("\\."), nb = pb[0].split("\\.");
        for (int i = 0; i < Math.max(na.length, nb.length); i++) {
            int d = num(na, i) - num(nb, i);
            if (d != 0) return d;
        }
        return (pa[1].isEmpty() ? 0 : 1) - (pb[1].isEmpty() ? 0 : 1);
    }

    private static String[] parse(String v) {
        String s = v == null ? "" : v;
        if (s.startsWith("v")) s = s.substring(1);
        int dash = s.indexOf('-');
        return dash < 0 ? new String[]{s, ""} : new String[]{s.substring(0, dash), s.substring(dash + 1)};
    }

    private static int num(String[] parts, int i) {
        if (i >= parts.length) return 0;
        try { return Integer.parseInt(parts[i]); } catch (NumberFormatException e) { return 0; }
    }
}
