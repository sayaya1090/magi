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
        // 데모면 목이 먼저 — 회선의 이음매가 여기라서, 프록시도 여기 선다(Demo.answer).
        Promise<Response> mocked = Demo.answer(path, init);
        if (mocked != null) return mocked;
        return init == null ? DomGlobal.fetch(path) : DomGlobal.fetch(path, init);
    }

    /** 이 창의 EventSource — fetch와 같은 이유로 한 자리에 모아 둔다. */
    public static elemental2.dom.EventSource stream(String path) {
        Object mocked = Demo.stream(path);
        if (mocked != null) return jsinterop.base.Js.uncheckedCast(mocked);
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

    /**
     * POST하고 <b>돌아온 말을 그대로</b> — 상태 코드와 <b>무관하게</b> 본문을 읽는다.
     *
     * 위의 둘로는 안 되는 자리가 있다. 갱신(/update)의 거부는 "실패"가 아니라 <b>지시</b>다:
     * 남의 기계 것을 여기서 갱신하려 하면 403과 함께 "그 기계에서 하라"가 오고, 데몬이 답을
     * 못 내면 502와 함께 그 데몬이 말한 사유가 온다. postText는 그 몸을 버리고(성공만 읽는다),
     * post는 성공한 몸을 버린다(사유만 읽는다) — 이 문은 어느 쪽이든 사람이 읽을 한 줄이다.
     *
     * 빈 문자열은 <b>회선이 끊긴 것</b>만 뜻한다(fetch 자체가 거절됨): 그때는 아무도 아무 말도
     * 하지 않았으므로 부르는 쪽이 제 말을 대신 세우고 다시 눌러 볼 수 있게 둔다.
     *
     * 시한을 다는 이유: 답이 영영 오지 않으면 promise가 영영 안 풀리고, 그동안 "하는 중"으로
     * 잠가 둔 컨트롤은 영영 잠긴 채다. 릴리스 하나를 받아 세우는 시간이라 넉넉해야 한다.
     */
    public static Promise<String> postSaid(String path, String socket, String peer, int timeoutMs) {
        RequestInit init = RequestInit.create();
        init.setMethod("POST");
        // 없는 브라우저에서는 시한 없이 간다 — 시한이 이 부름의 값어치를 정하지는 않는다.
        try { init.setSignal(elemental2.dom.AbortSignal.timeout(timeoutMs)); } catch (Exception ignore) { }
        return raw(path + query(socket, peer), init)
                .then(r -> r.text().then(said -> Promise.resolve(said.trim())))
                .catch_(err -> Promise.resolve(""));
    }

    /** ?d=<socket>[&p=<peer>] — 어느 컴패니언인가(운영 qFor). */
    private static String query(String socket, String peer) {
        StringBuilder q = new StringBuilder();
        if (socket != null && !socket.isEmpty()) q.append("d=").append(elemental2.core.Global.encodeURIComponent(socket));
        if (peer != null && !peer.isEmpty()) {
            if (q.length() > 0) q.append('&');
            q.append("p=").append(elemental2.core.Global.encodeURIComponent(peer));
        }
        return q.length() > 0 ? "?" + q : "";
    }
}
