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
 */
public final class Console {
    private Console() {}

    public interface ListHandler { void take(Object parsedOrNull); }

    public static void fetchList(String path, ListHandler h) {
        DomGlobal.fetch(path)
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
        return DomGlobal.fetch(path + (q.length() > 0 ? "?" + q : ""), init)
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
