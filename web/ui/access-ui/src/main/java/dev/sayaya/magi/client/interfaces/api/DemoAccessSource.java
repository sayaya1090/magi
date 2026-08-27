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
    private String samRole = "viewer";
    private String samScope = "docs";
    private boolean samGone = false;

    @Inject
    public DemoAccessSource() {}

    @Override
    public void roster(Consumer<Object> cb) {
        String sam = samGone ? "" :
                ",{\"who\":\"sam@laptop\",\"role\":\"" + samRole + "\",\"can\":[\"read\"]" +
                (samScope.isEmpty() ? "" : ",\"companions\":[\"" + samScope.replace(",", "\",\"") + "\"]") + "}";
        cb.accept(Global.JSON.parse(
                "{\"configured\":true,\"named\":true," +
                "\"instance\":{\"who\":\"you@devbox\",\"configDir\":\"~/.config/magi\"}," +
                "\"roles\":[{\"name\":\"viewer\",\"can\":[\"read\"]}," +
                          "{\"name\":\"operator\",\"can\":[\"read\",\"answer\",\"prompt\"]}," +
                          "{\"name\":\"admin\",\"can\":[\"read\",\"answer\",\"prompt\",\"admin\"]}]," +
                "\"groups\":[{\"who\":\"platform\",\"role\":\"operator\",\"can\":[\"read\",\"answer\",\"prompt\"],\"companions\":[\"build\"]}]," +
                "\"people\":[{\"who\":\"you@devbox\",\"role\":\"admin\",\"can\":[\"read\",\"answer\",\"prompt\",\"admin\"],\"me\":true}" +
                sam + "]}"));
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
