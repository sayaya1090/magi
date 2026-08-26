package dev.sayaya.magi.client.domain;

/**
 * 전사 행의 순수 규칙 — 행이 입는 클래스와 요약의 길이. DOM을 모른다.
 * 클래스 이름들은 기존 콘솔 page.js rowNode의 그것과 일대일 — console.css가 읽는 계약.
 */
public final class Rows {
    private Rows() {}

    /**
     * 행의 클래스 목록. ok는 셋 중 하나다: 모름(null 취급, hasOk=false)·성공·실패.
     * 실패에 주석(note)이 달리면 toolfail 대신 toolnote — 원본의 그 규칙.
     */
    public static String rowClass(String who, boolean hasOk, boolean ok, boolean note,
                                  boolean pending, boolean abandoned) {
        StringBuilder r = new StringBuilder("row ").append(who == null ? "" : who);
        if (abandoned) r.append(" abandoned");
        if ("tool".equals(who) && hasOk) {
            if (!ok && !note) r.append(" toolfail");
            if (note) r.append(" toolnote");
            if (ok) r.append(" toolok");
        }
        if (pending) r.append(" pending");
        return r.toString();
    }

    /** 요약 한 줄 — 개행은 공백으로, 길면 말줄임. n은 남기는 길이다. */
    public static String oneLine(String s, int n) {
        if (s == null) return "";
        String flat = s.replace('\n', ' ').replace('\r', ' ').trim();
        if (flat.length() <= n) return flat;
        return flat.substring(0, Math.max(0, n - 1)) + "…";
    }
}
