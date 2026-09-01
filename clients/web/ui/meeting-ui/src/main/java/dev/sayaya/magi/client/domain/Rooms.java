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

    /**
     * 지금 누가 일하고 있는가 — 「작업 중」 판이 누구 것을 그릴지.
     *
     * 방이 열리기 전과 후가 다르다. 열리기 전(준비)에는 <b>전원이 병렬로</b> 자기 워크스페이스를
     * 읽는다(MANUAL §12.5: "준비는 병렬이라 대기는 합이 아니라 가장 느린 하나") — 그러니 아직
     * 준비가 안 된 사람 전부가 일하는 중이다. 열린 뒤에는 바닥을 쥔 하나뿐이다: 그것이 회의를
     * 회의로 만드는 규칙이고, 그때 여럿을 그리면 아무도 안 쥔 것처럼 읽힌다.
     *
     * 끝난 방은 아무도 아니다. 사람은 세지 않는다 — 사람에게는 읽을 방이 없다.
     *
     * @param names   참가자 이름(로스터 순서)
     * @param person  각자가 사람인가
     * @param ready   각자가 준비를 마쳤는가(사람은 늘 참)
     * @param holder  바닥을 쥔 이름, 아무도 아니면 빈 문자열
     */
    public static String[] workingNow(String[] names, boolean[] person, boolean[] ready,
                                      String holder, boolean opened, boolean closed) {
        if (names == null || closed) return new String[0];
        if (opened) {
            if (holder == null || holder.isEmpty()) return new String[0];
            for (int i = 0; i < names.length; i++) {
                // 바닥을 쥔 것이 사람이면 그릴 방이 없다 — 사람이 타이핑하는 동안 방은 조용하다.
                if (holder.equals(names[i])) return person[i] ? new String[0] : new String[]{names[i]};
            }
            return new String[0];
        }
        int n = 0;
        for (int i = 0; i < names.length; i++) if (!person[i] && !ready[i]) n++;
        String[] out = new String[n];
        int k = 0;
        for (int i = 0; i < names.length; i++) if (!person[i] && !ready[i]) out[k++] = names[i];
        return out;
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
