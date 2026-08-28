package dev.sayaya.magi.client.domain;

/**
 * 이 콘솔의 취향 — 순수 규칙만. 어디에 저장되는지도, 어떻게 그려지는지도 모른다.
 *
 * 값이 셋인 것이 하나 있다(테마): 라이트·다크에 더해 <b>기계를 따름</b>이 있고, 그것이
 * 처음 값이다. 두 상태 토글로는 그 자리로 돌아올 수 없어(한 번 고르면 저장소를 비우기
 * 전까지 영영), 운영도 셋을 돌린다.
 */
public final class Prefs {
    public static final String[] THEMES = {"system", "light", "dark"};

    private Prefs() {}

    /** 다음 테마 — 셋을 돌린다. 모르는 값은 처음(기계를 따름)으로 친다. */
    public static String nextTheme(String now) {
        for (int i = 0; i < THEMES.length; i++) {
            if (THEMES[i].equals(now)) return THEMES[(i + 1) % THEMES.length];
        }
        return THEMES[1];
    }

    /** 문서에 적을 값 — "기계를 따름"은 <b>적지 않는 것</b>이다(속성이 없어야 매체 질의가 답한다). */
    public static String themeAttribute(String pref) {
        return "light".equals(pref) || "dark".equals(pref) ? pref : null;
    }

    // 켬/끔의 낱말("on"/"off")과 없는 값을 무엇으로 읽을지는 여기 없다: 그 규칙은 이 화면이
    // 적고 <b>다른 모듈</b>(편집기·컴포저)이 읽으므로 둘 다 보는 자리인 bridge/Prefs에 산다.

    /**
     * 이 설정이 어느 파일의 것인가 — 컴패니언을 보고 있으면 그 컴패니언의 것, 아니면
     * 이 기계 전체의 것. 같은 컨트롤이 어디에 서 있느냐로 다른 파일을 고치는 것이 운영이
     * 이 화면을 다이얼로그에서 화면으로 옮긴 이유이고, 그래서 그 사실을 소리 내어 적는다.
     */
    public static String scopeKey(String socket) {
        return socket == null || socket.isEmpty() ? "settings.scope_global" : "settings.scope_project";
    }
}
