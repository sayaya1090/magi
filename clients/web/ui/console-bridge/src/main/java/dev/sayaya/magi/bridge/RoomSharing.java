package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 회의 참가자가 <b>지금</b> 하고 있는 것의 창 브리지 — 명단과 같은 회선에 실려 온다.
 *
 * 서버는 이 프레임을 이미 낸다(`event: room`, `{who, rows}`, 변한 것만): 회의의 방 하나하나가
 * 그 컴패니언의 제 세션이고, 콘솔이 그것을 읽어 <b>회의 스트림 하나에</b> 얹는다(MANUAL §12.5).
 * 빠져 있던 것은 듣는 쪽이다 — 셸이 /events에 `?m=`을 안 실었고, 그래서 이 프레임은 한 번도
 * 나간 적이 없었다.
 *
 * 문이 둘인 이유는 조준이 화면의 몫이기 때문이다. 셸은 주소의 조각(`?m=`)을 싣고 되읽지만
 * <b>그 값이 무엇을 뜻하는지 모른다</b>(Place.piece의 계약). 셸이 "m이면 회의"라고 알기
 * 시작하면 그 화면 하나가 셸의 지식이 되므로, 뜻을 아는 회의 화면이 aim()으로 걸어 준다.
 *
 * 구독자에게 가는 값은 파싱된 프레임(`{who, rows}`)이다. 명단과 달리 null을 흘리지 않는다:
 * 이 판의 침묵은 "못 읽었다"가 아니라 "아직 아무것도 안 했다"이고, 그 둘은 화면에서 다른 말이다.
 */
public final class RoomSharing {
    private static final String SUB = "__magi_room_subscribe";
    private static final String AIM = "__magi_room_aim";

    private RoomSharing() {}

    @JsFunction
    public interface FrameFn { void call(Object frame); }

    @JsFunction
    public interface SubscribeFn { void call(FrameFn cb); }

    @JsFunction
    public interface AimFn { void call(String meetingIdOrNull); }

    /** 셸 측: 듣는 문과 조준하는 문을 건다. */
    public static void host(SubscribeFn sub, AimFn aim) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set(SUB, sub);
        win.set(AIM, aim);
    }

    public static boolean hosted() { return Js.asPropertyMap(DomGlobal.window).has(SUB); }

    /** 화면 측: 프레임을 듣는다. 호스트가 없으면 조용히 무시 — 폴백은 호출자의 몫. */
    public static void subscribe(FrameFn cb) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(SUB)) return;
        Js.<SubscribeFn>cast(win.get(SUB)).call(cb);
    }

    /** 화면 측: 이 회의를 봐 달라. null이면 조준을 푼다(회의를 떠났다). */
    public static void aim(String meetingIdOrNull) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (!win.has(AIM)) return;
        Js.<AimFn>cast(win.get(AIM)).call(meetingIdOrNull);
    }
}
