package dev.sayaya.magi.client.domain;

/**
 * 보드의 순수 규칙 — 운영 loadBoard의 뼈: 레인은 팀(무팀은 제 이름), 순서는 플릿과 같은
 * "골칫거리 먼저", 하루에 속함은 시작·끝의 날짜가 그 날을 걸치는가다(밤을 새운 일은
 * 양쪽 날 모두의 것 — 어느 쪽에도 없는 것이 최악이라서). DOM과 시계는 모른다: 날짜는
 * 이미 지역화된 YYYY-MM-DD 문자열로 받는다.
 */
public final class Lanes {
    private Lanes() {}

    /** 플릿과 같은 상태 순서 — 운영 ORDER의 이식. */
    public static int rank(String state) {
        if (state == null) return 6;
        switch (state) {
            case "waiting": return 0;
            case "working": return 1;
            case "idle": return 2;
            case "abandoned": return 3;
            case "stopped": return 4;
            case "remote": return 5;
            default: return 6;
        }
    }

    /** 레인의 이름 — 팀, 없으면 컴패니언 제 이름(팀을 지어내지 않는다). */
    public static String laneOf(String team, String name) {
        return team == null || team.isEmpty() ? (name == null ? "" : name) : team;
    }

    /** 그 날 돌고 있었는가 — 시작한 날만이 아니라 걸친 날 전부. 빈 날짜는 열린 끝으로 본다. */
    public static boolean onDay(String startedDay, String endedDay, String day) {
        if (day == null || day.isEmpty()) return false;
        boolean startedOk = startedDay == null || startedDay.isEmpty() || startedDay.compareTo(day) <= 0;
        boolean endedOk = endedDay == null || endedDay.isEmpty() || endedDay.compareTo(day) >= 0;
        return startedOk && endedOk;
    }
}
