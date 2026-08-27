package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.client.usecase.BoardSource;
import dev.sayaya.magi.bridge.RosterSharing;
import elemental2.core.Global;
import elemental2.core.JsDate;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/**
 * 두 팀(core=둘·docs=하나)과 오늘의 일들 — 지금 도는 것 하나, 라벨 단 것 하나,
 * 어제로 걸친 밤일 하나(오늘과 어제 양쪽에 보여야 한다). 타임스탬프는 Z 없이 —
 * 보드의 하루는 읽는 사람의 시계다(dayOf가 로컬로 접는다).
 */
/**
 * 데몬 없이 이 화면이 답하는 것 — 이 모듈이 <b>제 목을 싣는다</b>.
 *
 * 목이 모듈 안에 있는 이유는 배포가 모듈 단위이기 때문이다: 화면은 저마다 컴파일돼 저마다의
 * 주기로 나가고 제 창에서 제 회선으로 말한다. 페이지가 남의 창에 목을 밀어 넣는 방식은 그
 * 구조를 거스르고, 창 하나만 갈아끼우면 iframe 안의 모듈에는 닿지도 않는다(실측).
 */
@Singleton
public class DemoBoardSource implements BoardSource {
    private final String today;
    private final String yesterday;

    @Inject
    public DemoBoardSource() {
        JsDate now = new JsDate();
        double local = JsDate.now() - now.getTimezoneOffset() * 60000d;
        today = new JsDate(local).toISOString().substring(0, 10);
        yesterday = new JsDate(local - 86400000d).toISOString().substring(0, 10);
    }

    @Override
    public void fleet(Consumer<Object> cb) {
        // 이 화면의 명단도 <b>셸의 명단</b>이다 — 데모가 제 함대를 따로 지으면 같은 창의 두
        // 화면이 서로 다른 이름을 이야기한다(진짜 콘솔에서도 명단의 주인은 셸 하나다).
        RosterSharing.subscribe(cb::accept);
    }

    @Override
    public void history(String socket, String peer, Consumer<Object> cb) {
        // 구 콘솔의 데모와 같은 지난 일들 — 오늘 것과 어제 것이 섞여 있어야 날짜 고르개가
        // 무엇을 위한 것인지 보인다. 라벨도 함께: 그 칩들이 이 화면의 좁히는 손잡이다.
        if (socket != null && socket.contains("design.sock2")) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"p1\",\"title\":\"which surface should the empty state sit on\","
                            + "\"started\":\"" + today + "T11:00:00\",\"current\":true,"
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"empty-state\"]}]"));
            return;
        }
        if (socket != null && socket.contains("design.sock")) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"d1\",\"title\":\"spec the empty state for the fleet table, and name the exact tokens\","
                            + "\"started\":\"" + today + "T09:00:00\",\"current\":true,"
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"empty-state\",\"tokens\"]},"
                            + "{\"id\":\"d0\",\"title\":\"audit the button emphasis against the M3 scale and fix the inversions\","
                            + "\"started\":\"" + today + "T06:00:00\",\"ended\":\"" + today + "T08:00:00\","
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"tokens\"]},"
                            + "{\"id\":\"c9\",\"title\":\"the filter chips are not reachable with a keyboard on the corrections page\","
                            + "\"started\":\"" + yesterday + "T14:00:00\",\"ended\":\"" + yesterday + "T17:00:00\","
                            + "\"model\":\"qwen3-coder:30b\"}]"));
            return;
        }
        if (socket != null && socket.contains("api.sock")) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"a1\",\"title\":\"add the idempotency key to the billing endpoint\","
                            + "\"started\":\"" + today + "T08:00:00\",\"current\":true,"
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"billing\"]},"
                            + "{\"id\":\"a0\",\"title\":\"why does the invoice job double-charge on retry\","
                            + "\"started\":\"" + yesterday + "T09:00:00\",\"ended\":\"" + yesterday + "T15:00:00\","
                            + "\"model\":\"qwen3-coder:30b\",\"labels\":[\"billing\"]}]"));
            return;
        }
        if (socket != null && socket.contains("buttons.sock")) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"b1\",\"title\":\"the toggle should read its state from the store rather than a prop\","
                            + "\"started\":\"" + today + "T07:00:00\",\"ended\":\"" + today + "T09:00:00\","
                            + "\"model\":\"qwen3-coder-next\",\"labels\":[\"components\"]}]"));
            return;
        }
        if (socket != null && socket.contains("ops.sock")) {
            cb.accept(Global.JSON.parse(
                    "[{\"id\":\"o1\",\"title\":\"rotate the staging certificates before they expire\","
                            + "\"started\":\"" + yesterday + "T22:00:00\",\"ended\":\"" + yesterday + "T23:59:00\","
                            + "\"model\":\"qwen3-coder:30b\"}]"));
            return;
        }
        cb.accept(Global.JSON.parse("[]"));
    }
}
