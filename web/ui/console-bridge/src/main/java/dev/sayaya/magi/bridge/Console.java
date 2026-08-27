package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.dom.URLSearchParams;
import elemental2.promise.Promise;

/**
 * BFF와 말하는 두 가지 방법 — 기존 콘솔 page.js의 fetchList/post 이식.
 *
 * fetchList: 거부(HTTP 에러)와 불통(fetch reject)과 깨진 본문을 전부 null로 접되, 콘솔 로그에
 * 원문을 남긴다("stale and confident"가 셋 중 최악이라는 규칙). post: 대상 컴패니언을
 * ?d=<socket>&p=<peer>로 지목하고, 성공은 빈 문자열·거부는 사유 문자열을 되돌린다.
 *
 * 회선은 <b>이 모듈 제 창의 fetch</b>로 나간다 — GWT가 모듈을 숨은 iframe에서 돌리므로 그것은
 * 그 iframe의 fetch다. 배포판이 실제로 그렇게 도는 것이고, 부모 창의 fetch로 몰아 두면 모듈이
 * 페이지의 전역에 매이는 숨은 계약이 하나 는다. 정적 데모의 목은 그 사실에 맞춰 <b>창마다</b>
 * 들어간다(internal/webdemo — 페이지와 모듈 iframe 모두).
 */
public final class Console {
    private Console() {}

    public interface ListHandler { void take(Object parsedOrNull); }

    /** 이 창의 fetch — 회선이 한 자리에서 나가도록 모아 둔 문(데모의 목도 이 창에 들어온다). */
    public static Promise<Response> raw(String path, RequestInit init) {
        return init == null ? DomGlobal.fetch(path) : DomGlobal.fetch(path, init);
    }

    /** 이 창의 EventSource — fetch와 같은 이유로 한 자리에 모아 둔다. */
    public static elemental2.dom.EventSource stream(String path) {
        return new elemental2.dom.EventSource(path);
    }

    public static void fetchList(String path, ListHandler h) {
        raw(path, null)
                .then(r -> {
                    if (!r.ok) {
                        r.text().then(body -> { DomGlobal.console.warn("magi-console", r.status, path, body); return null; });
                        h.take(null);
                        return null;
                    }
                    r.text().then(body -> {
                        try { h.take(elemental2.core.Global.JSON.parse(body)); }
                        catch (Exception e) { DomGlobal.console.warn("magi-console garbled", path); h.take(null); }
                        return null;
                    });
                    return null;
                })
                .catch_(err -> { h.take(null); return null; });
    }

    /**
     * POST하고 <b>본문을 그대로</b> 받는다 — 답이 곧 글인 것들(완성·제안·초안)의 문.
     * 닿지 못하면 빈 문자열이다: 이런 자리에서 실패는 "아무 말도 없음"이지 오류 화면이 아니다.
     */
    public static Promise<String> postText(String path, URLSearchParams body, String socket, String peer) {
        StringBuilder q = new StringBuilder();
        if (socket != null && !socket.isEmpty()) q.append("d=").append(elemental2.core.Global.encodeURIComponent(socket));
        if (peer != null && !peer.isEmpty()) {
            if (q.length() > 0) q.append('&');
            q.append("p=").append(elemental2.core.Global.encodeURIComponent(peer));
        }
        RequestInit init = RequestInit.create();
        init.setMethod("POST");
        if (body != null) init.setBody(body);
        return raw(path + (q.length() > 0 ? "?" + q : ""), init)
                .then(r -> r.text().then(said -> Promise.resolve(r.ok ? said.trim() : "")))
                .catch_(err -> Promise.resolve(""));
    }

    /** POST path?d=socket&p=peer. resolve 값: 성공 ""; 거부면 사유 본문. */
    public static Promise<String> post(String path, URLSearchParams body, String socket, String peer) {
        StringBuilder q = new StringBuilder();
        if (socket != null && !socket.isEmpty()) q.append("d=").append(elemental2.core.Global.encodeURIComponent(socket));
        if (peer != null && !peer.isEmpty()) {
            if (q.length() > 0) q.append('&');
            q.append("p=").append(elemental2.core.Global.encodeURIComponent(peer));
        }
        RequestInit init = RequestInit.create();
        init.setMethod("POST");
        if (body != null) init.setBody(body);
        return raw(path + (q.length() > 0 ? "?" + q : ""), init)
                .then(r -> {
                    if (r.ok) return Promise.resolve("");
                    return r.text().then(why -> {
                        DomGlobal.console.warn("magi-console", r.status, path, why);
                        return Promise.resolve(why);
                    });
                })
                .catch_(err -> Promise.resolve("unreachable"));
    }
}
