package dev.sayaya.magi.demo;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Demo;
import elemental2.dom.Response;
import elemental2.promise.Promise;
import jsinterop.base.Js;

/**
 * 데모의 목 — <b>화면이 아니라 회선</b>에 걸린다.
 *
 * 지금까지 목은 화면마다 하나씩 실려 있었다(Demo*Source 11개, 1193줄): 운영 번들이 데모를
 * 함께 나르고, 다거는 화면마다 "데모냐"를 물었으며, 목이 답하는 자리는 화면의 포트라서
 * 그 화면이 실제로 쓰는 회선 경로는 데모에서 한 번도 지나지 않았다.
 *
 * 여기서는 하나다: 경로를 보고 답한다. 화면은 데모를 모른 채 늘 하던 대로 묻고, 콘솔의
 * 이음매(Console.raw/stream)가 이 답을 건네준다 — 프록시. 그래서 운영 번들에는 이 모듈이
 * 없고, 데모에서만 페이지가 이것을 싣는다.
 */
public class DemoApplication implements EntryPoint {
    @Override
    public void onModuleLoad() {
        Demo.Answerer a = (url, init) -> answer(url, init);
        Js.asPropertyMap(a).set("stream", (Demo.StreamFn) DemoApplication::stream);
        Demo.answers(a);
    }

    private static Promise<Response> answer(String url, elemental2.dom.RequestInit init) {
        String path = Mock.pathOf(url);
        Promise<Response> got = Fleet.answer(path);
        return got;
    }

    private static Object stream(String url) {
        return Mock.pathOf(url).equals("/events") ? Fleet.stream(url) : null;
    }
}
