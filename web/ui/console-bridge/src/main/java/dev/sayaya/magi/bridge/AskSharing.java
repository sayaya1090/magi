package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.annotations.JsPackage;
import jsinterop.annotations.JsType;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 지금 이 컴패니언이 <b>맨 질문</b>에 걸려 있는가 — 그리고 걸려 있다면 어느 부름에 답해야
 * 하는가. 컴패니언 패널(부모)이 알리고, 답할 입력을 가진 자식이 듣는다.
 *
 * 왜 이것만 알리나: 도크의 질문 상자는 부모의 것이지만, 목록 없는 질문의 답은 <b>컴포저</b>다
 * (글 상자 둘을 위아래로 세우지 않는다는 운영의 규칙). 컴포저는 자식의 것이라, 부모가 할 수
 * 있는 말은 "지금 그 입력은 이 부름에 답하는 자리다"뿐이다.
 *
 * 듣지 않아도 깨지지 않는다: 답할 입력이 없는 타입은 이 알림을 무시하면 되고, 상자는 제
 * 몫(퍼미션·보기 목록)을 그대로 한다. 구독은 <b>현재값을 재생</b>하므로 자식이 늦게 와도
 * 지금 걸린 질문을 놓치지 않는다.
 */
public final class AskSharing {
    private static final String KEY = "__magi_ask";
    private static final String OBS = "__magi_ask_obs";
    private static final String SEND = "__magi_ask_send";

    private AskSharing() {}

    @JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Object")
    public static class Ask {
        public String call;    // 어느 부름에 답하는가
        public String kind;    // 무엇을 물었나 — 여기서는 늘 "question"
        public String socket;  // 어느 컴패니언에게
        public String peer;    // 어느 콘솔을 거쳐서
    }

    /** 사실 하나를 짓는다 — 네이티브 Object라 창을 건너도 남의 클래스가 되지 않는다. */
    public static Ask ask(String call, String kind, String socket, String peer) {
        JsPropertyMap<Object> o = JsPropertyMap.of();
        o.set("call", call);
        o.set("kind", kind);
        o.set("socket", socket);
        o.set("peer", peer);
        return Js.uncheckedCast(o);
    }

    @JsFunction
    public interface NextFn { void call(Object ask); }

    /** 답을 보내는 문 — 부모가 걸고, 답할 입력을 가진 자식이 쓴다. */
    @JsFunction
    public interface SendFn { void call(String text, Landed landed); }

    @JsFunction
    public interface Landed { void call(String whyOrEmpty); }

    /** 부모: 지금의 사실을 알린다(없으면 null). 창에 남겨 뒤에 오는 자식도 든다. */
    public static void publish(Object ask) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(KEY, ask);
        Object obs = win.get(OBS);
        if (obs == null) return;
        JsArray list = Js.uncheckedCast(obs);
        for (int i = 0; i < list.length; i++) Js.<NextFn>cast(at(list, i)).call(ask);
    }

    /**
     * 부모: 답을 보내는 문을 건다. /answer의 주인이 하나여야 하기 때문이다 — 기다리는 질문을
     * 아는 쪽도, 그 답이 어느 부름으로 가는지 아는 쪽도 부모다. 자식은 사람이 쓴 글만 넘긴다.
     */
    public static void hostSend(SendFn send) {
        Js.asPropertyMap(DomGlobal.window).set(SEND, send);
    }

    /** 자식: 사람이 쓴 답을 넘긴다. 문이 없으면(부모 없는 페이지) 사유를 남긴다. */
    public static void answer(String text, Landed landed) {
        Object send = Js.asPropertyMap(DomGlobal.window).get(SEND);
        if (send == null) { landed.call("no companion panel"); return; }
        Js.<SendFn>cast(send).call(text, landed);
    }

    /** 자식: 지금 값을 받고, 바뀔 때마다 다시 받는다. */
    public static void subscribe(NextFn fn) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        Object obs = win.get(OBS);
        JsArray list = obs == null ? new JsArray() : Js.uncheckedCast(obs);
        list.push(fn);
        win.set(OBS, list);
        fn.call(win.get(KEY));
    }

    @JsType(isNative = true, namespace = JsPackage.GLOBAL, name = "Array")
    private static class JsArray {
        public int length;
        public native void push(Object v);
    }

    private static Object at(JsArray list, int i) {
        return Js.asPropertyMap(list).get(String.valueOf(i));
    }
}
