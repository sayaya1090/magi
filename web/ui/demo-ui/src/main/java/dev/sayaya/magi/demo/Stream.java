package dev.sayaya.magi.demo;

import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * EventSource처럼 구는 것 — 콘솔은 진짜 회선에 하는 그대로 이것에 귀를 붙인다
 * (addEventListener("fleet"|"message"|"turn"|"open"|"error") · close).
 *
 * 데모가 화면 안에 살던 시절에는 전사를 리스너에 바로 밀어 넣었다. 그러면 화면이 스트림을
 * 여는 코드는 데모에서 한 번도 돌지 않고, 데모가 도는 것이 그 코드가 도는 증거가 되지 못한다.
 */
final class Stream {
    private final JsPropertyMap<Object> me = Js.uncheckedCast(JsPropertyMap.of());
    private final JsPropertyMap<Object> ears = Js.uncheckedCast(JsPropertyMap.of());
    private final String aimed;
    private int shown = 0;
    private double timer = -1;
    private boolean closed = false;

    Stream(String socket) {
        this.aimed = socket == null ? "" : socket;
        me.set("addEventListener", (Listen) (type, fn) -> ears.set(type, fn));
        me.set("removeEventListener", (Listen) (type, fn) -> ears.delete(type));
        me.set("close", (Runner) () -> { closed = true; if (timer >= 0) DomGlobal.clearTimeout(timer); });
        // 회선이 서는 것도 소식이다 — 콘솔은 그 소식으로 "이어졌다"를 그린다.
        //
        // 곧바로 알리지 않는다: 진짜 회선은 아무리 빨라도 한 번의 왕복이고, 화면은 그 사이에
        // 제 자리를 잡는다. 목이 그보다 빠르면 아직 서지 않은 것들에게 말을 걸게 된다
        // (실측: 첫 소식이 마운트보다 빨라 셸의 리스너가 아직 null이었다). 50ms면 사람에게는
        // 즉시이고, 화면에게는 늦다.
        DomGlobal.setTimeout(a -> { fire("open", ""); roster(); begin(); }, 50);
    }

    Object js() { return me; }

    /** 지금의 명단 — 박자마다. */
    void roster() {
        if (closed) return;
        fire("fleet", Global.JSON.stringify(Fleet.fleet()));
    }

    /**
     * 조준된 컴패니언의 전사를 한 턴씩 — 진짜 스트림이 그렇고, 완성된 대화를 통째로 건네는
     * 데모는 그 사실을 한 번도 보여 주지 않는다(구 콘솔 데모의 그 박자: 200ms 뒤 첫 턴,
     * 이후 1.4초마다).
     */
    private void begin() {
        if (aimed.isEmpty()) return;
        jsinterop.base.JsArrayLike<Object> all = Js.uncheckedCast(Global.JSON.parse(Fleet.transcript()));
        fire("turn", aimed.contains("docs") ? "{\"open\":false,\"forSec\":0}" : "{\"open\":true,\"forSec\":42}");
        fire("message", "[]");
        timer = DomGlobal.setTimeout(a -> step(all), 200);
    }

    private void step(jsinterop.base.JsArrayLike<Object> all) {
        if (closed) return;
        shown++;
        Object[] out = new Object[Math.min(shown, all.getLength())];
        for (int i = 0; i < out.length; i++) out[i] = all.getAt(i);
        fire("message", Global.JSON.stringify(out));
        if (shown < all.getLength()) timer = DomGlobal.setTimeout(a -> step(all), 1400);
    }

    private void fire(String type, String data) {
        Object ear = ears.get(type);
        if (ear == null) return;
        // 진짜 이벤트를 만든다 — 받는 쪽은 이것을 MessageEvent로 읽고, 그 안의 것들(target,
        // type…)을 짚는다. 흉내 낸 평범한 객체를 건네면 그 짚기가 undefined에서 넘어진다
        // (실측: "Cannot read properties of undefined").
        elemental2.dom.MessageEventInit<String> init = elemental2.dom.MessageEventInit.create();
        init.setData(data);
        elemental2.dom.MessageEvent<String> evt = new elemental2.dom.MessageEvent<>(type, init);
        // 브라우저가 하는 그대로: 함수면 부르고, 아니면 그 안의 handleEvent를 부른다.
        // DOM의 addEventListener가 둘 다 받고(EventListener 인터페이스), elemental2의
        // EventListener는 @JsFunction이 아니라 native @JsType이라 자바 람다가 <b>객체로</b>
        // 건너온다 — 함수인 줄 알고 부르면 "d is not a function"이다(실측).
        try {
            if ("function".equals(Js.typeof(ear))) { Js.<Fn>uncheckedCast(ear).call(evt); return; }
            // 객체 귀는 <b>그 객체에게</b> 물어야 한다: handleEvent만 떼어내 부르면 그 안의
            // this가 사라져, 리스너가 제 필드를 읽는 순간 undefined에서 넘어진다(실측:
            // "Cannot read properties of undefined" — 셸의 리스너가 제 소스를 못 찾았다).
            Js.<elemental2.dom.EventListener>uncheckedCast(ear).handleEvent(Js.uncheckedCast(evt));
        } catch (Throwable t) {
            Object raw = Js.asPropertyMap(t).get("backingJsObject");
            DomGlobal.console.warn("demo mock: " + type + " listener threw",
                    raw == null ? t.getMessage() : Js.asPropertyMap(raw).get("stack"));
        }
    }

    @jsinterop.annotations.JsFunction
    interface Listen { void call(String type, Object fn); }

    @jsinterop.annotations.JsFunction
    interface Runner { void call(); }

    @jsinterop.annotations.JsFunction
    interface Fn { void call(Object evt); }
}
