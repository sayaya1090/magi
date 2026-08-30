package dev.sayaya.magi.component;

import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;

/**
 * 검색의 순위(공용 — 지식·보드가 같은 식을 쓴다) — 운영 page.js rankByIDF의 이식: 부분 문자열 필터가 아니라 드문 공유 단어
 * 우선의 랭킹이다("cache"는 캐시를 지나가며 언급한 항목보다 캐싱 규칙을 먼저 찾아야 한다).
 * ⚠한 공식의 두 구현은 이 트리가 결함을 찾아온 모양이라, 운영과 같은 식(BM25풍 IDF 합)을
 * 자바로 그대로 옮기고 JVM 테스트가 같은 코퍼스로 순서를 못박는다.
 */
public final class Rank {
    private Rank() {}

    /** 질의가 비거나 3자 미만 토큰뿐이면 원래 순서(전부). 아니면 매치만, 점수 내림차순. */
    public static int[] order(String query, List<String> docs) {
        Set<String> toks = new LinkedHashSet<>();
        for (String w : String.valueOf(query == null ? "" : query).toLowerCase()
                .split("[^a-z0-9]+")) {
            if (w.length() >= 3) toks.add(w);
        }
        int n = docs.size();
        if (toks.isEmpty() || n == 0) {
            int[] all = new int[n];
            for (int i = 0; i < n; i++) all[i] = i;
            return all;
        }
        List<String> lower = new ArrayList<>(n);
        for (String d : docs) lower.add(String.valueOf(d).toLowerCase());
        List<double[]> hits = new ArrayList<>(); // {i, score, matched}
        for (int i = 0; i < n; i++) {
            double score = 0;
            int matched = 0;
            for (String t : toks) {
                if (lower.get(i).contains(t)) {
                    int df = 0;
                    for (String d : lower) if (d.contains(t)) df++;
                    score += Math.log(1 + (n - df + 0.5) / (df + 0.5));
                    matched++;
                }
            }
            if (matched > 0) hits.add(new double[]{i, score, matched});
        }
        hits.sort((a, b) -> {
            if (a[1] != b[1]) return a[1] > b[1] ? -1 : 1;
            if (a[2] != b[2]) return a[2] > b[2] ? -1 : 1;
            return a[0] < b[0] ? -1 : 1;
        });
        int[] out = new int[hits.size()];
        for (int i = 0; i < hits.size(); i++) out[i] = (int) hits.get(i)[0];
        return out;
    }
}
