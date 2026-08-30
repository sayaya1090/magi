package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;
import jsinterop.annotations.JsFunction;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 지금은 데모인가, 그리고 <b>누가 대신 답하는가</b>.
 *
 * 데모는 정적 파일이라 뒤에 데몬이 없다. 그 답을 화면마다 싣고 있었다(모듈마다 Demo*Source
 * 하나씩, 1193줄) — 운영 번들에 데모가 함께 실리고, 다거는 화면마다 "데모냐 아니냐"를 한 번씩
 * 물었다. 답은 화면의 성질이 아니라 <b>회선의 성질</b>이라, 그 자리는 회선의 이음매다.
 *
 * 그래서 목은 모듈 하나(demo-ui)에 모이고, 이 문에 제 답을 걸어 둔다. 콘솔은 늘 하던 대로
 * Console.raw/stream을 부르고, 그 안에서 걸린 답이 있으면 그것을 받는다 — 프록시다.
 *
 * <b>window.fetch를 갈아끼우지 않는 이유</b>: GWT 모듈은 저마다 제 프레임에서 돌고, 그 안의
 * 맨 fetch는 그 프레임의 것이다. 페이지가 제 창의 fetch를 갈아도 모듈에는 닿지 않는다(실측).
 * 창을 건너 공유되는 것은 <b>속성</b>이라(DomGlobal.window는 호스트 창이다), 답을 그 속성에
 * 걸어 두고 모듈이 그것을 든다.
 */
public final class Demo {
    private static final String FLAG = "MAGI_DEMO";
    private static final String MOCK = "__magi_demo_mock";

    private Demo() {}

    public static boolean on() {
        return Js.isTruthy(Js.asPropertyMap(DomGlobal.window).get(FLAG));
    }

    /** 데모 모듈: 이 창의 답을 건다. 한 번이다. */
    public static void answers(Answerer a) {
        Js.asPropertyMap(DomGlobal.window).set(MOCK, a);
    }

    /** 답이 걸려 있는가 — 데모인데 아직 없으면 <b>기다린다</b>(아래 raw 참조). */
    public static boolean ready() {
        return Js.asPropertyMap(DomGlobal.window).get(MOCK) != null;
    }

    private static Answerer mock() {
        return Js.uncheckedCast(Js.asPropertyMap(DomGlobal.window).get(MOCK));
    }

    /**
     * 이 부름에 목이 답하는가 — 답하면 그 답, 아니면 null(콘솔이 회선을 탄다).
     *
     * 데모인데 목이 아직 안 실렸으면 회선으로 내보내지 않고 기다린다: 정적 사이트에서 그
     * 요청은 404가 되고, 그 404를 받은 화면은 "못 읽었다"를 그린 채 다음 폴까지 서 있는다.
     */
    public static Promise<Response> answer(String path, RequestInit init) {
        if (!on()) return null;
        if (ready()) return mock().call(path, init);
        return new Promise<>((resolve, reject) ->
                DomGlobal.setTimeout(a -> answer(path, init).then(r -> { resolve.onInvoke(r); return null; }), 30));
    }

    /** 이 스트림에 목이 답하는가 — 답하면 EventSource처럼 구는 것, 아니면 null. */
    public static Object stream(String path) {
        if (!on() || !ready()) return null;
        return mock().stream(path);
    }

    /** 목의 계약 — 순수 JS 객체다(모듈을 건너므로 자바 타입일 수 없다). */
    @JsFunction
    public interface Answerer {
        Promise<Response> call(String path, RequestInit init);

        @jsinterop.annotations.JsOverlay
        default Object stream(String path) {
            JsPropertyMap<Object> me = Js.asPropertyMap(this);
            Object s = me.get("stream");
            return s == null ? null : Js.<StreamFn>cast(s).call(path);
        }
    }

    @JsFunction
    public interface StreamFn { Object call(String path); }
}
