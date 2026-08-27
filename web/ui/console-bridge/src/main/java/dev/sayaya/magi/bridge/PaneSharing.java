package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 컴패니언 패널이 자식(타입 UI)에게 내주는 자리들 — 셸↔화면 관계를 한 겹 더 반복한다.
 *
 * 범용 컴패니언 패널은 레이아웃을 소유한다: 위의 사실판과 오른쪽 판은 어떤 타입이든 같은
 * 것을 답하기 때문이다(무엇이고, 무엇을 하는 중이고, 무엇을 계획했나). 가운데와 왼쪽은
 * 타입의 몫이다 — 코딩 에이전트에게 가운데는 대화이고 왼쪽은 워크스페이스지만, 디자인
 * 에이전트에게는 다른 것일 수 있다. 그래서 부모는 자리(슬롯)만 내주고 무엇이 오는지 모른다.
 *
 * 슬롯 이름은 "centre"와 "left"다. 왼쪽은 여럿일 수 있어(left를 여러 번 밀면 쌓인다)
 * 이름이 아니라 순서가 자리를 정한다 — 부모가 판을 세는 쪽이지 이름을 아는 쪽이 아니다.
 */
public final class PaneSharing {
    private static final String KEY = "__magi_pane";

    private PaneSharing() {}

    @JsFunction
    public interface SlotFn {
        /** slot은 "centre" 또는 "left"; render는 Render(프레임을 받아 그리는 함수)다. */
        void call(String slot, Object render);
    }

    /** 부모 측: 자식이 미는 렌더를 받을 자리를 건다. */
    public static void host(SlotFn slots) {
        Js.asPropertyMap(DomGlobal.window).set(KEY, slots);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(KEY); }

    /** 자식 측: 제 렌더를 슬롯에 민다. 부모가 없으면 조용히 무시 — 단독으로 열어본 경우다. */
    public static void next(String slot, Object render) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(KEY)) return;
        Js.<SlotFn>cast(win.get(KEY)).call(slot, render);
    }
}
