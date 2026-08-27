package dev.sayaya.magi.client.domain;

/**
 * 회의실의 순수 규칙 — DOM도 회선도 모른다.
 *
 * 한 회의가 어느 단계인지(한 줄로 말하는 것), 말한 이를 색으로 가르는 자리, 그리고 한
 * 사람의 n번째 차례가 전사의 어디부터 어디까지인지. 셋 다 화면 여러 곳이 같은 답을 내야
 * 하는 것이라 여기 한 벌만 둔다.
 */
public final class Rooms {
    private Rooms() {}

    /** 어느 단계인가 — 팩 키를 돌려준다(문구는 팩의 몫). 값이 없으면 빈 문자열. */
    public static String stageKey(boolean closed, boolean held, boolean collecting,
                                  boolean spent, int tasks, int round, int max) {
        // 무엇이 막고 있는지가 먼저다: 바닥을 쥔 사람이 있으면 그것이 아무 일도 없는 이유다.
        // 그 말이 없으면 "5라운드 중 2"만 남아 멈춘 것처럼 읽힌다 — 멈춘 게 아닌데도.
        if (!closed && held) return "meet.yours";
        if (collecting) return "meet.collecting";
        if (closed) {
            if (tasks == 0) return "meet.closing";
            // 끝나는 방식 둘은 읽는 사람에게 다른 뜻이다: 할 말이 떨어진 방은 답을 냈고,
            // 천장이 멈춘 방은 논쟁 중이었을 수 있다.
            return spent ? "meet.done_spent" : "meet.done";
        }
        // 숫자가 있을 때만. 낡은 데몬은 0을 보내지 키를 빼지 않는다 — "0라운드 중 0"이 그렇게 나왔다.
        if (round <= 0 || max <= 0) return "";
        return "meet.round";
    }

    /** 말한 이의 색자리 — 사람은 세지 않는다(사람의 말은 색이 아니라 자리로 갈린다). */
    public static String tint(int index) { return "sp" + (index % 6); }

    /**
     * 한 사람의 n번째 차례가 그 방 전사의 어디부터 어디까지인가.
     *
     * 차례는 사용자 행이 연다: 회의가 그 컴패니언에게 던진 질문이 'user'로 들어가고, 그
     * 뒤부터 다음 질문 전까지가 그 차례에 한 일이다. n번째 질문이 없으면 아직 그 차례가
     * 오지 않은 것이라 빈 구간이다.
     */
    public static int[] turnSpan(boolean[] isUser, int nth) {
        int seen = 0, from = -1;
        for (int i = 0; i < isUser.length; i++) {
            if (!isUser[i]) continue;
            if (seen == nth) { from = i + 1; break; }
            seen++;
        }
        if (from < 0) return new int[]{0, 0};
        int to = isUser.length;
        for (int i = from; i < isUser.length; i++) {
            if (isUser[i]) { to = i; break; }
        }
        return new int[]{from, Math.max(from, to)};
    }

    /** 회의를 열 수 있는가 — 주제가 있고, 둘 이상을 골랐을 때만(혼자 하는 회의는 회의가 아니다). */
    public static boolean canConvene(String topic, int picked) {
        return topic != null && !topic.trim().isEmpty() && picked >= 2;
    }

    /** 아직 안 되는 이유 — 버튼이 흐린 채 말이 없으면 사람은 무엇이 빠졌는지 모른다. */
    public static String blockedKey(String topic, int picked, int here) {
        if (here < 2) return "meet.need_two";
        if (picked < 2) return "meet.need_two";
        if (topic == null || topic.trim().isEmpty()) return "meet.need_topic";
        return "";
    }
}
