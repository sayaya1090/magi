package dev.sayaya.magi.client.domain;

/**
 * 여기서 보내면 컴패니언을 <b>옮겨야 하는가</b> — 그리고 지금 옮길 수 있는가. DOM을 모른다.
 *
 * 지난 한 세션을 열어 두고 컴포저에 무언가를 쓰는 일은 흔하다. 그런데 컴패니언은 그 대화에
 * 있지 않다: 지금 있는 대화는 화면 밖의 다른 것이고, 그대로 보내면 <b>아무도 보고 있지 않은</b>
 * 대화에 말이 들어간다. 그래서 보내기 전에 옮기고, 옮기기 전에 묻는다(운영 movingTo의 그 규칙).
 *
 * 판단은 한 술어다: 화면의 세션이 기록이 말하는 그 세션인가. 이 하나가 세 경우를 다 덮는다 —
 * 지금 대화를 보는 보통 화면(옮길 것 없음), 보드나 고르개에서 연 옛 세션(옮겨야 함), 그리고
 * 읽는 동안 컴패니언이 다른 대화로 떠나 버린 화면(그것도 옮겨야 함).
 */
public final class Moves {
    private Moves() {}

    /**
     * 옮겨 갈 세션 — 옮길 일이 없으면 빈 문자열.
     *
     * @param past    지금 보는 층위(null=지난 일 층위가 아님, ""=목록, 값=그 세션)
     * @param session 명단이 말하는 이 컴패니언의 지금 세션(모르면 null)
     */
    public static String to(String past, String session) {
        if (past == null || past.isEmpty()) return "";
        // 명단을 아직 못 읽었으면 "지금 세션"을 모른다 — 모르는 것을 같다고 치지 않는다(운영은
        // 행이 없을 때 그대로 옮길 곳을 답한다). 옮기기 전에 한 번 더 묻기 때문에 안전하다.
        if (session != null && session.equals(past)) return "";
        return past;
    }

    /**
     * 옮길 곳이 있는데 컴패니언이 아직 말하고 있으면 보낼 수 없다.
     *
     * 도는 중인지는 <b>데몬</b>의 상태지 세션의 상태가 아니다: 지금 세션이 아닌 대화는 결코
     * 돌고 있지 않고, 가만히 있어야 하는 쪽은 컴패니언이다(운영 고르개가 스스로를 흐리게 하는
     * 그 술어와 같은 것).
     */
    public static boolean blocked(String to, String state) {
        if (to == null || to.isEmpty()) return false;
        return !(state == null || "idle".equals(state) || "stopped".equals(state));
    }
}
