package dev.sayaya.magi.demo;

import elemental2.dom.Response;
import elemental2.promise.Promise;

/**
 * 목이 답하는 법 — 콘솔이 회선에서 받는 것과 <b>같은 모양</b>으로 답한다(Response).
 *
 * 데모가 화면 안에 살던 시절에는 포트마다 자바 값을 바로 돌려줬다. 그때 목은 화면의 진짜
 * 회선 경로(무엇을 어떤 이름으로 묻는가)를 한 번도 지나지 않았고, 그래서 데모가 도는 것이
 * 그 화면이 도는 증거가 되지 못했다. 여기서는 같은 이음매를 지난다.
 */
final class Mock {
    private Mock() {}

    /** 이 본문으로 답한다(200). */
    static Promise<Response> json(String body) {
        return Promise.resolve(new Response(body));
    }

    /** 답할 것이 없다는 뜻 — 콘솔이 제 회선으로 나간다(자산·말 팩이 그 길로 간다). */
    static Promise<Response> none() { return null; }

    /** 물음표 앞의 길만 — 목은 경로로 답하고, 딸린 것들은 각자가 읽는다. */
    static String pathOf(String url) {
        String u = url == null ? "" : url;
        int q = u.indexOf('?');
        return q < 0 ? u : u.substring(0, q);
    }

    /** 쓰는 부름인가 — 몸이 있으면 쓴 것이다(운영의 그 계약: 같은 길이 읽기이자 쓰기다). */
    static boolean wrote(elemental2.dom.RequestInit init) {
        return init != null && jsinterop.base.Js.asPropertyMap(init).get("body") != null;
    }

    /** 보낸 몸에서 값 하나 — 콘솔은 URLSearchParams로 보낸다(Console.post). */
    static String field(elemental2.dom.RequestInit init, String key) {
        if (init == null) return "";
        Object body = jsinterop.base.Js.asPropertyMap(init).get("body");
        if (body == null) return "";
        Object got = jsinterop.base.Js.<elemental2.dom.URLSearchParams>uncheckedCast(body).get(key);
        return got == null ? "" : String.valueOf(got);
    }

    /** 같은 이름으로 여럿 딸려 오는 것(트리의 &path=… 여러 개). */
    static java.util.List<String> params(String url, String key) {
        java.util.List<String> out = new java.util.ArrayList<>();
        String u = url == null ? "" : url;
        int q = u.indexOf('?');
        if (q < 0) return out;
        for (String part : u.substring(q + 1).split("&")) {
            int eq = part.indexOf('=');
            if (eq > 0 && part.substring(0, eq).equals(key)) {
                out.add(elemental2.core.Global.decodeURIComponent(part.substring(eq + 1)));
            }
        }
        return out;
    }

    /** 딸린 값 하나 — 없으면 빈 문자열. */
    static String param(String url, String key) {
        String u = url == null ? "" : url;
        int q = u.indexOf('?');
        if (q < 0) return "";
        for (String part : u.substring(q + 1).split("&")) {
            int eq = part.indexOf('=');
            if (eq > 0 && part.substring(0, eq).equals(key)) {
                return elemental2.core.Global.decodeURIComponent(part.substring(eq + 1));
            }
        }
        return "";
    }
}
