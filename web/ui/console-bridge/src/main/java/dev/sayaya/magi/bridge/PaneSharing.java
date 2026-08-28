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

    // ── 그 자리가 지금 열려 있는가 ────────────────────────────────────────────
    //
    // 아무도 열어 본 적 없는 판은 <b>한 번도 요청을 쓰지 않는다</b>(운영 규칙). 그 판이 열려
    // 있는지는 배치를 아는 부모만 알고, 무엇을 청할지는 자식만 안다 — 그래서 사실 하나가 오간다.

    private static final String OPEN = "__magi_pane_open";
    private static final String OPEN_OBS = "__magi_pane_open_obs";

    @JsFunction
    public interface Opened { void call(String slot, boolean open); }

    /** 부모: 이 자리가 열렸다/닫혔다. */
    public static void opened(String slot, boolean open) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        JsPropertyMap<Object> all = Js.uncheckedCast(win.get(OPEN));
        if (all == null) { all = JsPropertyMap.of(); win.set(OPEN, all); }
        Object had = all.get(slot);
        if (had != null && Js.isTruthy(had) == open) return;   // 같은 말을 다시 하지 않는다
        all.set(slot, open);
        Object l = win.get(OPEN_OBS);
        if (l != null) Js.<Opened>cast(l).call(slot, open);
    }

    /** 자식: 지금 열려 있는가(부모가 말한 적 없으면 열린 것으로 본다 — 부모 없는 페이지). */
    public static boolean isOpen(String slot) {
        JsPropertyMap<Object> all = Js.uncheckedCast(Js.asPropertyMap(DomGlobal.window).get(OPEN));
        Object v = all == null ? null : all.get(slot);
        return v == null || Js.isTruthy(v);
    }

    /** 자식: 그 사실이 바뀌면 알려 달라. */
    public static void onOpened(Opened l) { Js.asPropertyMap(DomGlobal.window).set(OPEN_OBS, l); }
}