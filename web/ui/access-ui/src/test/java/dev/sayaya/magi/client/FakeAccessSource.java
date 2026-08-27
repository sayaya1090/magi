package dev.sayaya.magi.client;

import dev.sayaya.magi.client.usecase.AccessSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

/** 그룹 하나(범위 딸림)와 사람 둘(나=admin, viewer 하나 — docs로 좁힘). 쓰기는 창에 적는다. */
@Singleton
public class FakeAccessSource implements AccessSource {
    private String samRole = "viewer";
    private String samScope = "docs";
    private boolean samGone = false;

    @Inject
    public FakeAccessSource() {}

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
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_set", who + "|" + role + "|" + companions);
        if (who.startsWith("sam")) { samRole = role; samScope = companions == null ? "" : companions; }
        done.run();
    }

    @Override
    public void removePerson(String who, Runnable done) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_removed_person", who);
        if (who.startsWith("sam")) samGone = true;
        done.run();
    }
}
