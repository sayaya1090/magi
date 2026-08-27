package dev.sayaya.magi.bridge;

/**
 * 상태가 입는 마크 — 한 곳에 적는다(운영 STATE_MARK): 칩이 세는 그룹 기준이라 stopped와
 * abandoned는 한 타일 "gone"이다. 세는 칩과 그 상태를 이고 있는 행이 서로 다른 말을 하지
 * 않게 하려는 것이고, 도는 그림은 쓰지 않는다 — 세는 자리의 유일한 움직임이 되니까.
 */
public final class StateMark {
    private StateMark() {}

    public static String of(String group) {
        if (group == null) return "";
        switch (group) {
            case "waiting": return "#i-ss-circle-pause";
            case "working": return "#i-ss-play";
            case "idle": return "#i-ss-moon";
            case "gone": return "#i-ss-circle-stop";
            case "remote": return "#i-sl-satellite-dish";
            default: return "";
        }
    }
}
