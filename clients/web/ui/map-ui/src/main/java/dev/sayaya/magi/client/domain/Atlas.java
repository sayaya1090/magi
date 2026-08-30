package dev.sayaya.magi.client.domain;

/**
 * 맵의 순수 규칙 — 운영 loadMap의 뼈: 두 경계(머신, 그 위의 계정)와 신뢰의 순서,
 * 그리고 와이어의 세 상태. DOM과 좌표는 모른다(기하는 측정의 것 — interfaces).
 */
public final class Atlas {
    private Atlas() {}

    /** 신뢰의 순서 — 내 것, 들인 것, 모르는 것, 말 없는 것. */
    public static int trustRank(String trust) {
        if (trust == null || trust.isEmpty()) return 3;
        switch (trust) {
            case "own": return 0;
            case "admitted": return 1;
            case "unknown": return 2;
            default: return 3;
        }
    }

    /** 계정 반쪽 — 머신은 이미 상자라 "you@buildbox"의 you만(운영 accountOf). */
    public static String accountOf(String instance) {
        if (instance == null) return "";
        int at = instance.indexOf('@');
        return at > 0 ? instance.substring(0, at) : instance;
    }

    /** 와이어의 세 상태: 닿을 수 없음이 먼저다 — 보낼 때 건강해 보였어도(운영 규칙). */
    public static String edgeClass(boolean toLive, String handoffState) {
        if (!toLive) return "down";
        return "working".equals(handoffState) ? "flight" : "ok";
    }
}
