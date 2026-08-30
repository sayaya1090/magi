package dev.sayaya.magi.bridge;

/**
 * 상태 어휘 — 눈이 다녀야 할 순서와 요약 그룹. 순수 함수라 JVM 테스트가 바로 문다.
 *
 * 브리지에 사는 이유: 이 어휘를 쓰는 화면이 둘 이상이다(명단·지도). 화면마다 제 판을 두면
 * 같은 상태가 화면마다 다른 무리에 들어가고, 그때 세는 칩과 그것을 이고 있는 행이 서로 다른
 * 말을 한다 — 마크를 한 곳에 적어 둔 것과 같은 이유다([[StateMark]]).
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

    /**
     * 그 행이 아직 답하는가 — <b>명단에 있다는 것과 답한다는 것은 다른 사실이다</b>(운영
     * `live !== false`). 소켓은 남아 있는데 데몬이 죽은 경우가 있어서, 명단은 그 행을 계속
     * 실어 나르며 답하지 않는다고만 적는다.
     *
     * 말하지 않은 것은 산 것으로 읽는다. 이 값을 안 싣는 자리가 있고(오래된 데몬, 테스트 목),
     * 없음을 죽음으로 읽으면 멀쩡한 컴패니언의 판이 통째로 사라진다.
     *
     * 여기 사는 이유는 groupOf와 같다: 이 한 줄을 읽는 화면이 셋이다(마스트헤드의 점, 사실판,
     * 전사의 빈 자리). 화면마다 제 판을 두면 같은 컴패니언이 한 화면에서는 죽고 다른 화면에서는
     * 살아 있다.
     */
    public static boolean answering(FleetAgent a) {
        return a != null && (!jsinterop.base.Js.asPropertyMap(a).has("live") || a.live);
    }
}
