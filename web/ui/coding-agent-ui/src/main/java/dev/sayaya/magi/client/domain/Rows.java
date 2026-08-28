package dev.sayaya.magi.client.domain;

/**
 * 전사 행의 순수 규칙 — 행이 입는 클래스와 요약의 길이. DOM을 모른다.
 * 클래스 이름들은 기존 콘솔 page.js rowNode의 그것과 일대일 — console.css가 읽는 계약.
 */
public final class Rows {
    private Rows() {}

    /** 카운슬의 세 자리 — 이름을 그대로 선택자로 쓰지 않는다(로그의 문자열이 클래스가 되는 것). */
    public static String seatClass(String member) {
        if (member == null) return "";
        switch (member.toLowerCase()) {
            case "melchior": return "m-melchior";
            case "balthasar": return "m-balthasar";
            case "casper": return "m-casper";
            default: return "";
        }
    }

    /**
     * 행의 클래스 목록. ok는 셋 중 하나다: 모름(null 취급, hasOk=false)·성공·실패.
     * 실패에 주석(note)이 달리면 toolfail 대신 toolnote — 원본의 그 규칙.
     *
     * <p>카운슬 행은 표(v-*)와 자리(seated m-*)를 함께 입는다. 없이 그리면 아홉 행이 한 색이다 —
     * 실측: 운영은 자리마다 다른 세 색이고 여기는 아홉 줄 전부 같은 갈색이었다. 표의 색은 요약
     * 줄이 갖고, 홈통은 자리가 갖는다(그래서 seated 가 따로 있다).
     */
    public static String rowClass(String who, boolean hasOk, boolean ok, boolean note,
                                  boolean pending, boolean abandoned,
                                  String decision, String member) {
        StringBuilder r = new StringBuilder("row ").append(who == null ? "" : who);
        if (decision != null && !decision.isEmpty()) r.append(" v-").append(decision);
        String seat = seatClass(member);
        if (!seat.isEmpty()) r.append(" seated ").append(seat);
        if (abandoned) r.append(" abandoned");
        if ("tool".equals(who) && hasOk) {
            if (!ok && !note) r.append(" toolfail");
            if (note) r.append(" toolnote");
            if (ok) r.append(" toolok");
        }
        if (pending) r.append(" pending");
        return r.toString();
    }

    /** 접혀 도착하는 목소리들 — 원본 foldedKinds 와 일대일. */
    public static boolean folded(String who) {
        if (who == null) return false;
        switch (who) {
            case "thinking": case "tool": case "result": case "failed": case "council": case "shell":
                return true;
            default:
                return false;
        }
    }

    /** 유니파이드 디프로 보이는가 — GWT 클라이언트라 정규식 없이 줄 머리만 본다(원본과 같은 판정). */
    public static boolean looksLikeDiff(String text) {
        if (text == null) return false;
        for (String line : text.split("\n")) {
            if (line.startsWith("@@") || line.startsWith("diff --git ")) return true;
        }
        return false;
    }

    /** 디프 한 줄이 입는 클래스 — 원본 diffInto 의 그 다섯. */
    public static String diffLineClass(String line) {
        if (line == null) return "dctx";
        if (line.startsWith("+++") || line.startsWith("---") || line.startsWith("diff ")) return "dfile";
        if (line.startsWith("@@")) return "dhunk";
        if (line.startsWith("+")) return "dadd";
        if (line.startsWith("-")) return "ddel";
        return "dctx";
    }

    /** 결과의 첫 의미 있는 줄 — 호출 옆에 놓을 답의 헤드라인(원본 answerLine의 순수 반). */
    public static String firstLine(String decoded, int n) {
        if (decoded == null) return "";
        for (String line : decoded.split("\n")) {
            if (!line.trim().isEmpty()) return oneLine(line, n);
        }
        return "";
    }

    /** 요약 한 줄 — 개행은 공백으로, 길면 말줄임. n은 남기는 길이다. */
    public static String oneLine(String s, int n) {
        if (s == null) return "";
        String flat = s.replace('\n', ' ').replace('\r', ' ').trim();
        if (flat.length() <= n) return flat;
        return flat.substring(0, Math.max(0, n - 1)) + "…";
    }

    /**
     * 인자를 <b>있는 그대로</b> 읽는다 — 아는 키를 가진 납작한 객체. 없으면 null.
     *
     * 문자열 값은 제 줄바꿈과 따옴표를 지킨다(그게 이 표의 전부다). 그 밖의 것은 JSON으로 다시
     * 적는다 — "[object Object]"는 원래의 JSON보다 나쁘다. 운영 jsonPairs의 순수 반.
     */
    public static java.util.List<String[]> jsonPairs(String text) {
        String t = text == null ? "" : text.trim();
        if (!t.startsWith("{")) return null; // 전사를 파서에 넘기기 전의 값싼 거절
        elemental2.core.JsObject parsed;
        try {
            Object v = elemental2.core.Global.JSON.parse(t);
            if (v == null || !(v instanceof Object) || jsinterop.base.Js.isTruthy(
                    elemental2.core.JsArray.isArray(v))) return null;
            parsed = jsinterop.base.Js.uncheckedCast(v);
        } catch (Throwable e) {
            return null;
        }
        jsinterop.base.JsPropertyMap<Object> m = jsinterop.base.Js.uncheckedCast(parsed);
        java.util.List<String[]> out = new java.util.ArrayList<>();
        elemental2.core.JsArray<String> keys = elemental2.core.JsObject.keys(parsed);
        for (int i = 0; i < keys.length; i++) {
            String k = keys.getAt(i);
            Object raw = m.get(k);
            String val;
            if (raw instanceof String) val = (String) raw;
            else val = elemental2.core.Global.JSON.stringify(raw);
            out.add(new String[]{k, val == null ? "" : val});
        }
        return out.isEmpty() ? null : out;
    }
}
