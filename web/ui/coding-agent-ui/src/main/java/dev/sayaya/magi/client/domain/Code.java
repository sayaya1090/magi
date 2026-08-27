package dev.sayaya.magi.client.domain;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/**
 * 파일 본문의 순수 규칙 — 운영 codeBlocks/codeParts/plainText/unifiedDiff의 뼈.
 *
 * 에이전트의 read 툴이 내는 본문은 `번호⇥내용`이다. 그 번호는 사람과 컴패니언이 같은 40행을
 * 가리키기 위한 것이라 다시 매기지도 지우지도 않는다 — 대신 화면에서 <b>기둥으로 갈라</b>
 * 옆에 세운다(그래야 코드를 끌어 복사할 때 번호가 딸려오지 않는다).
 */
public final class Code {
    private Code() {}

    /** 한 줄 안에서 표시할 조각 — cls가 비면 그냥 글자다. */
    public static final class Part {
        public final String text;
        public final String cls;
        Part(String text, String cls) { this.text = text; this.cls = cls; }
    }

    private static final List<String> SLASH = Arrays.asList(
            "go", "js", "mjs", "ts", "tsx", "jsx", "css", "java", "c", "h", "cc", "cpp", "rs",
            "swift", "kt", "scala", "php", "sql");
    private static final List<String> HASH = Arrays.asList(
            "py", "sh", "bash", "zsh", "rb", "yml", "yaml", "toml", "conf", "ini", "mk");

    /** 이 파일에서 주석은 무엇으로 시작하는가 — 모르면 빈 문자열(표시하지 않는다). */
    public static String commentMark(String path) {
        String p = path == null ? "" : path;
        String[] dots = p.split("\\.");
        String ext = dots.length > 1 ? dots[dots.length - 1].toLowerCase() : "";
        String[] parts = p.split("/");
        String name = parts.length == 0 ? "" : parts[parts.length - 1].toLowerCase();
        if (SLASH.contains(ext)) return "//";
        if (HASH.contains(ext) || "makefile".equals(name) || "dockerfile".equals(name)) return "#";
        if ("lua".equals(ext)) return "--";
        return "";
    }

    /**
     * 한 줄을 표시할 조각들로 — <b>훑기이지 파싱이 아니다</b>: 따옴표가 열면 같은 따옴표가 닫고,
     * 문자열 밖의 주석 표시는 줄 끝까지다. 자리를 못 정한 것은 그냥 글자로 남는다(운영과 같다).
     */
    public static List<Part> parts(String code, String comment) {
        List<Part> out = new ArrayList<>();
        String src = code == null ? "" : code;
        StringBuilder plain = new StringBuilder();
        for (int i = 0; i < src.length(); i++) {
            char c = src.charAt(i);
            if (comment != null && !comment.isEmpty() && src.startsWith(comment, i)) {
                flush(out, plain);
                out.add(new Part(src.substring(i), "tok-note"));
                return out;
            }
            if (c == '"' || c == '\'' || c == '`') {
                int j = i + 1;
                while (j < src.length() && src.charAt(j) != c) j += src.charAt(j) == '\\' ? 2 : 1;
                flush(out, plain);
                out.add(new Part(src.substring(i, Math.min(j + 1, src.length())), "tok-text"));
                i = j;
                continue;
            }
            char prev = i == 0 ? ' ' : src.charAt(i - 1);
            if (c >= '0' && c <= '9' && !word(prev)) {
                int j = i;
                while (j < src.length() && word(src.charAt(j))) j++;
                flush(out, plain);
                out.add(new Part(src.substring(i, j), "tok-num"));
                i = j - 1;
                continue;
            }
            plain.append(c);
        }
        flush(out, plain);
        return out;
    }

    private static boolean word(char c) {
        return Character.isLetterOrDigit(c) || c == '_' || c == '.';
    }

    private static void flush(List<Part> out, StringBuilder plain) {
        if (plain.length() == 0) return;
        out.add(new Part(plain.toString(), null));
        plain.setLength(0);
    }

    /** 번호 기둥 — 툴이 번호를 안 붙인 줄은 빈 줄로 자리를 지킨다(아래 번호가 어긋나지 않게). */
    public static String gutter(String text) {
        StringBuilder g = new StringBuilder();
        for (String line : String.valueOf(text).split("\n", -1)) {
            String n = numberOf(line);
            g.append(n == null ? "" : n).append('\n');
        }
        return g.toString();
    }

    /** 번호를 뗀 본문 — 화면에도, 편집기에도 이것이 들어간다(운영 plainText). */
    public static String plainText(String text) {
        StringBuilder out = new StringBuilder();
        String[] lines = String.valueOf(text).split("\n", -1);
        for (int i = 0; i < lines.length; i++) {
            if (i > 0) out.append('\n');
            out.append(bodyOf(lines[i]));
        }
        return out.toString();
    }

    /** 이 줄의 본문 — `번호⇥`가 앞에 있으면 뗀다. */
    public static String bodyOf(String line) {
        return numberOf(line) == null ? line : line.substring(line.indexOf('\t') + 1);
    }

    private static String numberOf(String line) {
        int tab = line.indexOf('\t');
        if (tab <= 0) return null;
        String head = line.substring(0, tab);
        String trimmed = head.trim();
        if (trimmed.isEmpty()) return null;
        for (int i = 0; i < head.length(); i++) {
            char c = head.charAt(i);
            if (!Character.isWhitespace(c) && (c < '0' || c > '9')) return null;
        }
        return trimmed;
    }

    /**
     * 저장은 <b>고친 자리만</b> 보낸다 — 패치는 작고, 무엇보다 <b>거부할 수 있다</b>: 열어 둔
     * 사이 컴패니언이 그 파일을 고쳤으면 git apply가 안 맞는다고 답한다(파일 통째로는 그냥
     * 덮어쓴다). 만들 수 없으면 빈 문자열이고, 그때는 본문을 보낸다.
     */
    public static String unifiedDiff(String before, String after, String path) {
        if (before.equals(after)) return "";
        List<String> a = rows(before), b = rows(after);
        int head = 0;
        while (head < a.size() && head < b.size() && a.get(head).equals(b.get(head))) head++;
        int tail = 0;
        while (tail < a.size() - head && tail < b.size() - head
                && a.get(a.size() - 1 - tail).equals(b.get(b.size() - 1 - tail))) tail++;
        List<String> midA = a.subList(head, a.size() - tail);
        List<String> midB = b.subList(head, b.size() - tail);
        // 한쪽 4000줄이면 수백 줄짜리 손편집이고, 그 표는 천육백만 칸이다 — 그 너머는 파일이 싸다.
        if (midA.size() > 4000 || midB.size() > 4000) return "";
        int ctx = 3;
        int from = Math.max(0, head - ctx);
        int toEnd = Math.min(a.size(), a.size() - tail + ctx);
        StringBuilder lines = new StringBuilder();
        for (int i = from; i < head; i++) lines.append(' ').append(a.get(i)).append('\n');
        for (String op : lcsOps(midA, midB)) lines.append(op).append('\n');
        for (int i = a.size() - tail; i < toEnd; i++) lines.append(' ').append(a.get(i)).append('\n');
        int oldCount = toEnd - from;
        int newCount = oldCount - midA.size() + midB.size();
        return "diff --git a/" + path + " b/" + path + "\n"
                + "--- a/" + path + "\n+++ b/" + path + "\n"
                + "@@ -" + (from + 1) + "," + oldCount + " +" + (from + 1) + "," + newCount + " @@\n"
                + lines;
    }

    /**
     * 줄로 가른다 — 끝의 개행 뒤 빈 문자열은 줄이 아니다. 그것을 줄로 세면 패치 끝에 디스크의
     * 어느 줄과도 안 맞는 문맥 한 줄이 붙어, 평범한 파일의 모든 저장이 거부된다(운영에서 겪은 것).
     */
    private static List<String> rows(String text) {
        List<String> l = new ArrayList<>(Arrays.asList(String.valueOf(text).split("\n", -1)));
        if (!l.isEmpty() && l.get(l.size() - 1).isEmpty()) l.remove(l.size() - 1);
        return l;
    }

    private static List<String> lcsOps(List<String> a, List<String> b) {
        int n = a.size(), m = b.size();
        int[][] table = new int[n + 1][m + 1];
        for (int i = n - 1; i >= 0; i--) {
            for (int j = m - 1; j >= 0; j--) {
                table[i][j] = a.get(i).equals(b.get(j)) ? table[i + 1][j + 1] + 1
                        : Math.max(table[i + 1][j], table[i][j + 1]);
            }
        }
        List<String> out = new ArrayList<>();
        int i = 0, j = 0;
        while (i < n && j < m) {
            if (a.get(i).equals(b.get(j))) { out.add(" " + a.get(i)); i++; j++; }
            else if (table[i + 1][j] >= table[i][j + 1]) { out.add("-" + a.get(i)); i++; }
            else { out.add("+" + b.get(j)); j++; }
        }
        while (i < n) { out.add("-" + a.get(i)); i++; }
        while (j < m) { out.add("+" + b.get(j)); j++; }
        return out;
    }
}
