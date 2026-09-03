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

    /**
     * 턴이 열려 있는가 — <b>스텝 0의 뜻을 가르는 술어</b>.
     *
     * <p>명단은 열린 턴이 있을 때만 스텝을 채운다. 그래서 쉬는 컴패니언의 0 은 "셀 것이 없다"
     * 이고 대시가 맞지만, 도는 턴의 0 은 "아직 아무 도구도 안 불렀다"이고 그건 사람이 바로
     * 알고 싶은 소식이다 — 실측: 43초째 도는 컴패니언이 툴 0회였고, 두 화면 모두 그것을
     * 「모른다」와 같은 글자로 그리고 있었다.
     *
     * <p>여기 사는 이유는 groupOf·answering과 같다: 이 판단을 하는 화면이 둘이다(사실판과
     * 명단 표). 화면마다 제 판을 두면 한쪽만 고쳐지고 — 실제로 그렇게 됐다. 사실판을 고친
     * 커밋이 표를 놓쳤고, 같은 0 이 한 화면에서는 0 으로 다른 화면에서는 대시로 그려졌다.
     */
    public static boolean turning(FleetAgent a) {
        if (a == null) return false;
        return "working".equals(a.state) || "waiting".equals(a.state) || "abandoned".equals(a.state);
    }

    /** 스텝 칸에 적을 말. 도는 턴이면 0 도 숫자로, 아니면 대시. */
    public static String stepsText(FleetAgent a) {
        if (a == null) return "—";
        return turning(a) || a.steps > 0 ? String.valueOf(a.steps) : "—";
    }
}
