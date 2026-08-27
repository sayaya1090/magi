package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.Console;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;
import java.util.function.Consumer;

/**
 * 내가 무엇을 해도 되는가 — /me를 한 번 물어 문을 거른다(운영 loadMe/applyMay의 이식).
 * 아무도 설정 안 된 콘솔은 "전부"라 답하고, 그때 이 저장소는 아무것도 바꾸지 않는다:
 * 1인 콘솔이 청하지 않은 권한 모델을 얻으면 안 된다(운영 규칙). 게이트는 늘 서버가 진다 —
 * 여기는 눌러서 거절에 닿는 문을 접는 것뿐이다.
 */
@Singleton
public class MayStore {
    private final List<Consumer<MayStore>> observers = new ArrayList<>();
    private List<String> can = null;   // null = 전부 허용

    @Inject
    public MayStore() {}

    public void start() {
        Console.fetchList("/me", parsed -> {
            if (parsed == null) return;   // 못 물었으면 그려진 대로 — 서버가 여전히 거부한다
            Object caps = Js.asPropertyMap(parsed).get("can");
            if (caps == null) return;
            JsArrayLike<Object> arr = Js.uncheckedCast(caps);
            List<String> got = new ArrayList<>();
            for (int i = 0; i < arr.getLength(); i++) got.add(String.valueOf(arr.getAt(i)));
            can = got;
            for (Consumer<MayStore> o : observers) o.accept(this);
        });
    }

    public boolean may(String cap) {
        return cap == null || cap.isEmpty() || can == null || can.contains(cap);
    }

    public void subscribe(Consumer<MayStore> o) {
        observers.add(o);
        o.accept(this);
    }
}
