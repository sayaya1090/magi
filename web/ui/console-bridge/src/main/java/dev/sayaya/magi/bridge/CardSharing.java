package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 가운데 기둥의 <b>카드 자리</b> — 사실판 옆에 나란히 서는 것들.
 *
 * 왜 자식이 제자리에 그리지 않고 등록하나: 그 자리에 무엇이 서 있는지 고르는 것은 탭 줄이고,
 * 탭 줄에는 부모의 사실판도 함께 선다. 한 자리에 둘이 그리면 누가 지금 보이는지 아무도 모른다.
 * 그래서 자식은 "이런 카드가 있다"고 말하고, 부모가 그 줄을 그리고 하나를 고른다.
 *
 * 열린 파일이 하나도 없으면 줄은 서지 않는다 — 고를 것이 하나뿐인 탭 줄은 furniture다.
 */
public final class CardSharing {
    private static final String KEY = "__magi_cards";
    private static final String OBS = "__magi_cards_obs";

    private CardSharing() {}

    @JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
    public static class Card {
        public String key;     // 무엇을 여는가(경로 같은 것) — 같은 것을 두 번 열지 않게
        public String title;   // 탭에 적히는 짧은 이름
        public Object render;  // Render — 이 카드의 속을 그린다
        public Runner close;   // 닫으면 자식이 제 기록에서 지운다
    }

    @JsFunction
    public interface Runner { void call(); }

    @JsFunction
    public interface Listener { void call(Object cards); }

    /** 자식: 지금 열려 있는 카드 전부(배열). 없으면 빈 배열 — 그때 줄이 걷힌다. */
    public static void provide(Object cards) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(KEY, cards);
        Object l = win.get(OBS);
        if (l != null) Js.<Listener>cast(l).call(cards);
    }

    public static Object current() { return Js.asPropertyMap(DomGlobal.window).get(KEY); }

    /** 부모: 바뀌면 다시 그린다. */
    public static void onChange(Listener l) { Js.asPropertyMap(DomGlobal.window).set(OBS, l); }

    public static Card card(String key, String title, Object render, Runner close) {
        JsPropertyMap<Object> o = JsPropertyMap.of();
        o.set("key", key);
        o.set("title", title);
        o.set("render", render);
        o.set("close", close);
        return Js.uncheckedCast(o);
    }
}
