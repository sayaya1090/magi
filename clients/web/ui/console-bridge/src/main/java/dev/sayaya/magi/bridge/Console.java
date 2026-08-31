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

    // 이 콘솔이 볼 수 있는 백엔드들. 컴패니언과 무관한 <b>콘솔 하나의 사실</b>이라 한 번만 묻고,
    // 묻는 중이면 같이 기다린다 — 두 모듈(컴패니언 상세와 설정)이 각자 물었고, 상세는 명단이
    // 흐를 때마다 다시 그려지므로 같은 목록을 한 화면에서 여러 번 물어 오던 자리다.
    //
    // 잊지 않는다: 이 목록은 사람이 설정을 고쳐야 바뀌고, 그때는 화면을 다시 여는 것이 그
    // 사람의 다음 동작이다. 살면서 바뀌는 값을 이 자리에 두면 안 된다.
    private static Object providersMemo = null;
    private static boolean providersAsking = false;
    private static final java.util.List<ListHandler> providersWaiting = new java.util.ArrayList<>();

    public static void providers(ListHandler h) {
        if (providersMemo != null) { h.take(providersMemo); return; }
        providersWaiting.add(h);
        if (providersAsking) return;
        providersAsking = true;
        fetchList("/providers", got -> {
            providersAsking = false;
            if (got != null) providersMemo = got;
            java.util.List<ListHandler> waiting = new java.util.ArrayList<>(providersWaiting);
            providersWaiting.clear();
            for (ListHandler w : waiting) w.take(got);
        });
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

    /** 답이 왔는가와 그 본문 — 둘을 함께 넘기지 않으면 부르는 쪽이 셋을 가릴 수 없다. */
    public interface Said { void call(boolean ok, String text); }

    /**
     * POST하고 <b>답했는가와 그 글</b>을 받는다 — 이 콘솔이 무언가를 시키는 유일한 문.
     * <p>
     * 셋을 갈라 넘긴다: 답했고 그 글이 이것 · <b>거절했고 그 사유가 이것</b> · 닿지 못했다.
     * 앞서 여기에는 문이 <b>둘</b>이었고, 둘 다 셋을 빈 문자열 하나로 접었다 — 서로 반대쪽
     * 절반을 버리면서. `postText`는 성공 본문만 읽고 거절 본문을 버려, 사람이 누른 단추가
     * <b>조용히</b> 아무것도 안 하거나(초안 둘) 서버가 사유까지 적어 보낸 거절을 "닿지
     * 못했다"로 적게 했다(/git-pr). `post`는 거절 본문만 읽고, 몸 없는 4xx를 <b>성공</b>으로
     * 읽었으며(빈 문자열이 이 문의 말로 성공이다), 닿지 못한 것에는 `"unreachable"`이라는
     * 번역되지 않은 영어 낱말을 지어내 "서버가 이렇게 말했다" 자리에 앉혔다.
     * <p>
     * 셋을 가르고 나니 두 문이 같은 함수가 됐다 — 애초에 버리는 절반이 다를 뿐이었다.
     * 사유를 버리는 것이 옳은 자리도 있지만(사람이 누르지 않은 도움), 그 버림은 부르는
     * 쪽이 <b>골라서</b> 할 일이지 이 문이 대신 정할 일이 아니다.
     */
    public static void post(String path, URLSearchParams body, String socket, String peer, Said then) {
        RequestInit init = RequestInit.create();
        init.setMethod("POST");
        if (body != null) init.setBody(body);
        raw(path + query(socket, peer), init)
                .then(r -> r.text().then(said -> {
                    String text = said == null ? "" : said.trim();
                    if (!r.ok) DomGlobal.console.warn("magi-console", r.status, path, text);
                    then.call(r.ok, text);
                    return null;
                }))
                // 닿지 못한 것은 거절이 아니다 — 아무도 아무 말도 하지 않았으므로 사유 자리는 빈다.
                .catch_(err -> { then.call(false, ""); return null; });
    }

    /**
     * 셋을 <b>사유 한 줄</b>로 옮긴다 — 포트의 말이 "빈 문자열이면 됐다"인 자리들을 위해.
     * <p>
     * 셋 중 둘은 그 말로 적을 수 있다: 됐으면 빈 문자열, 거절이면 서버가 적어 보낸 사유.
     * 셋째는 적을 수 없어서 <b>우리 말을 대신 세운다</b> — 아무도 아무 말도 하지 않았으니
     * 서버의 말을 옮기는 것이 아니라 우리가 아는 것(닿지 못했다)을 이 사람의 말로 적는 것이다.
     * 앞서 그 자리에는 `"unreachable"`이 있었다: 번역되지 않은 영어 낱말이, 하필 "서버가
     * 이렇게 말했다"고 그려지는 자리에.
     * <p>
     * 이 옮김은 <b>ok를 잃는다</b>(거절과 불통이 한 줄이 된다). 그래도 되는 자리에서만 쓴다 —
     * 둘을 갈라 달리 굴어야 하는 자리는 `Said`를 그대로 받아 제가 조합한다.
     */
    public static String why(boolean ok, String text) {
        if (ok) return "";
        return text == null || text.isEmpty() ? Labels.tr("error.unreachable") : text;
    }

    /**
     * POST하고 <b>돌아온 말을 그대로</b> — 상태 코드와 <b>무관하게</b> 본문을 읽는다.
     *
     * 위의 둘로는 안 되는 자리가 있다. 갱신(/update)의 거부는 "실패"가 아니라 <b>지시</b>다:
     * 남의 기계 것을 여기서 갱신하려 하면 403과 함께 "그 기계에서 하라"가 오고, 데몬이 답을
     * 못 내면 502와 함께 그 데몬이 말한 사유가 온다. post는 답했는가와 글을 <b>갈라</b> 넘겨
     * 부르는 쪽이 조합하게 하지만, 이 문은 가를 것도 없이 어느 쪽이든 사람이 읽을 한 줄이다.
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
