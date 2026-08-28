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

    // ── 물어야 아는 것 ────────────────────────────────────────────────────────
    //
    // 등록(provide)은 <b>미리 아는 것</b>이다: 이 화면이 할 수 있는 일들. 워크스페이스의 파일
    // 이름처럼 물어봐야 아는 것은 미리 실어 둘 수 없다 — 팔레트가 열릴 때마다 저장소를 통째로
    // 걷는 일이 되고, 그것은 키 하나가 남의 디스크를 도는 값이다(운영 palGather의 그 주석).
    // 그래서 문이 하나 더 있다: 셸이 지금 친 글자를 묻고(ask), 화면이 답한다(onAsk). 무엇을
    // 물으면 값이 드는지는 화면만 알기 때문에, <b>답하지 않을 자유</b>도 화면 쪽에 있다.

    private static final String ASK = KEY + "_ask";

    @JsFunction
    public interface Answer { void call(Object entries); }

    @JsFunction
    public interface Asker {
        /** q는 지금 친 글자(다듬은 것), back은 그 물음에 대한 답 하나. */
        void call(String q, Answer back);
    }

    /** 화면 측: 물으면 답하겠다 — 늦게 답해도 된다(그 사이 바뀐 글자는 셸이 가려낸다). */
    public static void onAsk(Asker a) {
        Js.asPropertyMap(DomGlobal.window).set(ASK, a);
    }

    /** 셸 측: 지금 서 있는 화면에 묻는다. 답하는 화면이 없으면 back은 불리지 않는다. */
    public static void ask(String q, Answer back) {
        Object a = Js.asPropertyMap(DomGlobal.window).get(ASK);
        if (a != null) Js.<Asker>cast(a).call(q, back);
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
