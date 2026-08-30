package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

/**
 * "이 프로그램"을 뜻하는 키의 이름 — 컨트롤이 제 단축키를 말할 수 있게.
 *
 * 여기(console-bridge)에 사는 이유: 이 낱말을 적는 자리가 한 모듈이 아니다(셸의 팔레트 버튼,
 * 편집기의 두 청하기 버튼). 두 곳이 각자 코를 킁킁대면 한쪽만 고쳐지고, 그때 어긋난 것은
 * <b>같은 기계에서 다른 키를 광고하는 화면 둘</b>이 된다.
 *
 * 킁킁대는 것은 웹의 모든 편집기가 그러듯 어쩔 수 없다. 다만 키를 듣는 쪽은 두 수정자를
 * <b>둘 다</b> 받으므로(metaKey 또는 ctrlKey), 잘못 짚어도 대가는 툴팁의 낱말 하나이지
 * 동작하지 않는 단축키가 아니다.
 */
public final class Keys {
    private Keys() {}

    /** ⌘ 아니면 Ctrl. */
    public static String mod() { return mac() ? "⌘" : "Ctrl"; }

    public static boolean mac() {
        try {
            // userAgentData.platform은 브라우저가 <b>대답하려고</b> 두는 값이고, userAgent는
            // 호환을 위해 얼려 둔 문자열이다. 앞의 것을 먼저 묻고, 없으면 뒤의 것.
            Object nav = Js.asPropertyMap(DomGlobal.window).get("navigator");
            if (nav == null) return false;
            Object data = Js.asPropertyMap(nav).get("userAgentData");
            String plat = data == null ? null : str(Js.asPropertyMap(data).get("platform"));
            if (plat == null || plat.isEmpty()) plat = str(Js.asPropertyMap(nav).get("userAgent"));
            String p = plat == null ? "" : plat.toLowerCase();
            return p.contains("mac") || p.contains("iphone") || p.contains("ipad");
        } catch (Exception e) { return false; }
    }

    private static String str(Object v) { return v == null ? null : String.valueOf(v); }
}
