package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

/**
 * 이 브라우저의 취향 — 켜고 끄는 것들.
 *
 * 여기(console-bridge)에 사는 이유는 <b>쓰는 쪽과 읽는 쪽이 다른 모듈</b>이기 때문이다:
 * 스위치는 설정 화면(settings-ui)에 서 있고, 그 답에 따라 달라지는 것은 편집기와 컴포저
 * (coding-agent-ui)다. 두 곳이 각자 저장소를 만지면 낱말이 갈린다 — 없는 값을 무엇으로
 * 읽을지가 특히 그렇다.
 *
 * 규칙 하나: <b>없음은 기본값</b>이다. 기본이 켬인 설정은 `off`만 적힐 값이 있고, 저장소를
 * 만질 수 없는 창(사적 창은 접근 자체가 던진다)에서도 기본으로 산다. 운영이 세 스위치를
 * `localStorage.getItem('autocomplete') !== 'off'`로 읽는 그 규칙 그대로다.
 */
public final class Prefs {
    private Prefs() {}

    /** 켜져 있는가 — 적힌 적 없으면 그 설정의 기본이 답한다. */
    public static boolean on(String key, boolean byDefault) { return means(stored(key), byDefault); }

    /** 적힌 낱말이 뜻하는 것. 저장소를 모르는 규칙이라 브라우저 없이도 잴 수 있다. */
    public static boolean means(String stored, boolean byDefault) {
        if ("on".equals(stored)) return true;
        if ("off".equals(stored)) return false;
        return byDefault;
    }

    /** 저장소에 적을 낱말. 읽는 쪽과 같은 자리에 둔다 — 갈리면 조용히 어긋난다. */
    public static String word(boolean on) { return on ? "on" : "off"; }

    public static void keep(String key, boolean on) { write(key, word(on)); }

    public static String text(String key, String byDefault) {
        String v = stored(key);
        return v == null || v.isEmpty() ? byDefault : v;
    }

    public static void keepText(String key, String value) { write(key, value); }

    /** 사적 창에서는 접근 자체가 던진다 — 기억이 없으면 기본값으로 산다. */
    private static String stored(String key) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls == null) return null;
            Object v = Js.asPropertyMap(ls).get(key);
            return v == null ? null : String.valueOf(v);
        } catch (Exception e) { return null; }
    }

    private static void write(String key, String value) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls != null) Js.asPropertyMap(ls).set(key, value);
        } catch (Exception ignore) { /* 저장이 거부될 수 있다 */ }
    }
}
