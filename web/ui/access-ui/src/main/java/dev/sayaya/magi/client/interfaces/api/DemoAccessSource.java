package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.client.usecase.AccessSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** 그룹 하나(범위 딸림)와 사람 둘(나=admin, viewer 하나 — docs로 좁힘). 쓰기는 창에 적는다. */
/**
 * 데몬 없이 이 화면이 답하는 것 — 이 모듈이 <b>제 목을 싣는다</b>.
 *
 * 목이 모듈 안에 있는 이유는 배포가 모듈 단위이기 때문이다: 화면은 저마다 컴파일돼 저마다의
 * 주기로 나가고 제 창에서 제 회선으로 말한다. 페이지가 남의 창에 목을 밀어 넣는 방식은 그
 * 구조를 거스르고, 창 하나만 갈아끼우면 iframe 안의 모듈에는 닿지도 않는다(실측).
 */
@Singleton
public class DemoAccessSource implements AccessSource {
    private String samRole = "responder";
    private String samScope = "api";
    private boolean samGone = false;

    @Inject
    public DemoAccessSource() {}

    @Override
    public void roster(Consumer<Object> cb) {
        // 구 콘솔의 데모와 같은 명부다. 무리가 위, 사람이 아래 — 디렉터리에 물린 콘솔에서는
        // <b>무리가 곧 명부</b>이고(사람은 고용되고 떠나는 자리에서 관리된다) 개인은 그 예외다.
        // 그래서 예외 둘을 담는다: 한 컴패니언으로 좁혀진 사람, 그리고 일을 시작하지는 못해도
        // 물음에는 답할 수 있는 사람.
        String sam = samGone ? "" :
                ",{\"who\":\"sam@example.com\",\"role\":\"" + samRole + "\",\"can\":[\"read\",\"answer\"]" +
                (samScope.isEmpty() ? "" : ",\"companions\":[\"" + samScope.replace(",", "\",\"") + "\"]") + "}";
        cb.accept(Global.JSON.parse(
                "{\"configured\":true,\"named\":true," +
                "\"instance\":{\"who\":\"you@studio\",\"configDir\":\"/Users/you/.config/magi\"}," +
                "\"roles\":[{\"name\":\"operator\",\"can\":[\"read\",\"answer\",\"prompt\",\"curate\"," +
                          "\"configure\",\"admin\",\"shell\"]}," +
                          "{\"name\":\"responder\",\"can\":[\"read\",\"answer\"]}," +
                          "{\"name\":\"viewer\",\"can\":[\"read\"]}]," +
                "\"groups\":[{\"who\":\"platform-team\",\"role\":\"operator\",\"can\":[\"read\",\"answer\"," +
                          "\"prompt\",\"curate\",\"configure\",\"admin\",\"shell\"]}," +
                          "{\"who\":\"design-guild\",\"role\":\"responder\",\"can\":[\"read\",\"answer\"]," +
                          "\"companions\":[\"design\",\"palette\"]}," +
                          "{\"who\":\"everyone\",\"role\":\"viewer\",\"can\":[\"read\"]}]," +
                "\"people\":[{\"who\":\"you@studio\",\"role\":\"operator\",\"can\":[\"read\",\"answer\"," +
                          "\"prompt\",\"curate\",\"configure\",\"admin\",\"shell\"],\"me\":true}" +
                sam +
                ",{\"who\":\"contractor@example.com\",\"role\":\"viewer\",\"can\":[\"read\"]}]}"));
    }

    @Override
    public void setPerson(String who, String role, String companions, Runnable done) {
        if (who.startsWith("sam")) { samRole = role; samScope = companions == null ? "" : companions; }
        done.run();
    }

    @Override
    public void removePerson(String who, Runnable done) {
        if (who.startsWith("sam")) samGone = true;
        done.run();
    }
}
