package dev.sayaya.magi.demo;

import elemental2.core.JsDate;
import elemental2.dom.Response;
import elemental2.promise.Promise;

/**
 * 지난 일들 — 보드가 날짜로 읽고, 컴패니언의 세션 고르개가 같은 답을 다르게 쓴다.
 *
 * 경로가 하나(/history?d=…)이므로 답도 하나다: 데모가 화면마다 제 목을 들고 있던 시절에는
 * 같은 물음에 화면마다 다른 답이 나왔고(보드의 세션과 사실판의 세션이 서로 달랐다), 그것이
 * 목을 회선이 아니라 화면에 두었을 때 생기는 어긋남이다.
 *
 * 오늘과 어제가 섞여 있어야 날짜 고르개가 무엇을 위한 것인지 보인다. 라벨도 함께 —
 * 그 칩들이 보드에서 좁히는 손잡이다.
 */
final class Sessions {
    private static final String TODAY;
    private static final String YESTERDAY;

    static {
        JsDate now = new JsDate();
        double local = JsDate.now() - now.getTimezoneOffset() * 60000d;
        TODAY = new JsDate(local).toISOString().substring(0, 10);
        YESTERDAY = new JsDate(local - 86400000d).toISOString().substring(0, 10);
    }

    private Sessions() {}

    static Promise<Response> answer(String path, String url) {
        if (!"/history".equals(path)) return null;
        String socket = Mock.param(url, "d");
        String today = TODAY, yesterday = YESTERDAY;
        if (socket != null && socket.contains("design.sock2")) {
            return Mock.json(
                    "[{\"id\":\"p1\",\"title\":\"which surface should the empty state sit on\","
                            + "\"started\":\"" + today + "T11:00:00\",\"current\":true,"
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"empty-state\"]}]");
        }
        if (socket != null && socket.contains("design.sock")) {
            return Mock.json(
                    "[{\"id\":\"d1\",\"title\":\"spec the empty state for the fleet table, and name the exact tokens\","
                            + "\"started\":\"" + today + "T09:00:00\",\"current\":true,"
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"empty-state\",\"tokens\"]},"
                            + "{\"id\":\"d0\",\"title\":\"audit the button emphasis against the M3 scale and fix the inversions\","
                            + "\"started\":\"" + today + "T06:00:00\",\"ended\":\"" + today + "T08:00:00\","
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"tokens\"]},"
                            + "{\"id\":\"c9\",\"title\":\"the filter chips are not reachable with a keyboard on the corrections page\","
                            + "\"started\":\"" + yesterday + "T14:00:00\",\"ended\":\"" + yesterday + "T17:00:00\","
                            + "\"model\":\"qwen3-coder:30b\"}]");
        }
        if (socket != null && socket.contains("api.sock")) {
            return Mock.json(
                    "[{\"id\":\"a1\",\"title\":\"add the idempotency key to the billing endpoint\","
                            + "\"started\":\"" + today + "T08:00:00\",\"current\":true,"
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"billing\"]},"
                            + "{\"id\":\"a0\",\"title\":\"why does the invoice job double-charge on retry\","
                            + "\"started\":\"" + yesterday + "T09:00:00\",\"ended\":\"" + yesterday + "T15:00:00\","
                            + "\"model\":\"qwen3-coder:30b\",\"labels\":[\"billing\"]}]");
        }
        if (socket != null && socket.contains("buttons.sock")) {
            return Mock.json(
                    "[{\"id\":\"b1\",\"title\":\"the toggle should read its state from the store rather than a prop\","
                            + "\"started\":\"" + today + "T07:00:00\",\"ended\":\"" + today + "T09:00:00\","
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"components\"]}]");
        }
        if (socket != null && socket.contains("ops.sock")) {
            return Mock.json(
                    "[{\"id\":\"o1\",\"title\":\"rotate the staging certificates before they expire\","
                            + "\"started\":\"" + yesterday + "T22:00:00\",\"ended\":\"" + today + "T00:00:00\","
                            // 어제 것은 지금 도는 모델이다 — 두 모델이 픽스처에 함께 있는 이유는
                            // 그 라벨이 컴패니언이 아니라 <b>세션</b>의 것임을 보이기 위해서고,
                            // 오래된 것에만 옛 모델을 적는다(운영의 그 규칙: 하루 넘은 것).
                            + "\"model\":\"qwen3-coder-next\"}]");
        }
        return Mock.json("[]");
    }
}
