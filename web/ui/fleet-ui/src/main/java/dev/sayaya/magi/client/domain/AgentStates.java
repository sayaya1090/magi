package dev.sayaya.magi.client.domain;

/**
 * 상태 어휘 — 눈이 다녀야 할 순서와 요약 그룹. 순수 함수라 JVM 테스트가 바로 문다.
 *
 * remote는 한 경우에만 살아남는 상태다: 자기 컴패니언이 뭘 하는지 말하기엔 너무 낡은
 * 데몬의 레코드. "아무 말도 못 들었다"와 "idle이라고 들었다"는 다른 사실이다.
 */
public final class AgentStates {
    private AgentStates() {}

    /** 정렬 무게: 사람을 기다리는 것(0)부터 사라진 것까지. 모르는 상태는 idle 취급. */
    public static int orderOf(String state) {
        if (state == null) return 2;
        switch (state) {
            case "waiting": return 0;
            case "working": return 1;
            case "idle": return 2;
            case "abandoned": return 3;
            case "stopped": return 4;
            case "remote": return 5;
            default: return 2;
        }
    }

    /** 요약 타일이 세는 그룹: stopped와 abandoned는 하나의 "gone"이다. */
    public static String groupOf(String state) {
        if (state == null) return "idle";
        switch (state) {
            case "waiting": case "working": case "idle": return state;
            case "abandoned": case "stopped": return "gone";
            case "remote": return "remote";
            default: return "idle";
        }
    }
}
