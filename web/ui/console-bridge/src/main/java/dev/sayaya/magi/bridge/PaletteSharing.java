package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 화면이 팔레트에 제 것을 더하는 문.
 *
 * 셸의 팔레트는 셸이 아는 것만 안다 — 갈 수 있는 화면들과 명단의 컴패니언들. 그 화면만
 * 할 수 있는 일(코딩 에이전트의 파일 찾기 같은)은 그 화면이 스스로 등록한다. 셸이 자식의
 * 기능을 알아야 하는 구조를 만들지 않으려는 것이고, 이는 툴 레일(ToolList)과 같은 규칙이다.
 *
 * 등록은 <b>지금 서 있는 화면의 것</b>이다: 화면을 떠나면 그 항목도 함께 걷힌다(빈 배열).
 */
public final class PaletteSharing {
    private static final String KEY = "__magi_palette";

    private PaletteSharing() {}

    @JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
    public static class Entry {
        public String kind;    // 사람이 읽는 갈래말(팩에서 온 것)
        public String name;    // 무엇인가
        public String hint;    // 곁의 한 줄(없으면 빈 문자열)
        public Runner run;     // 고르면 하는 일
    }

    @JsFunction
    public interface Runner { void call(); }

    @JsFunction
    public interface Listener { void call(Object entries); }

    /** 화면 측: 지금 이 화면이 더하는 것들(떠날 때는 빈 배열). */
    public static void provide(Object entries) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(KEY, entries);
        Object l = win.get(KEY + "_obs");
        if (l != null) Js.<Listener>cast(l).call(entries);
    }

    /** 셸 측: 지금 등록된 것들(없으면 null). */
    public static Object current() {
        return Js.asPropertyMap(DomGlobal.window).get(KEY);
    }

    /** 셸 측: 바뀌면 알려 달라 — 팔레트가 열려 있는 동안에도 화면은 바뀐다. */
    public static void onChange(Listener l) {
        Js.asPropertyMap(DomGlobal.window).set(KEY + "_obs", l);
    }

    public static Entry entry(String kind, String name, String hint, Runner run) {
        JsPropertyMap<Object> o = JsPropertyMap.of();
        o.set("kind", kind);
        o.set("name", name);
        o.set("hint", hint == null ? "" : hint);
        o.set("run", run);
        return Js.uncheckedCast(o);
    }
}
