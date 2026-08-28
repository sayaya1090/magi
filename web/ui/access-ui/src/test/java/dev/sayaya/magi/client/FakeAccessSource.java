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
    public void setPerson(String who, String role, String companions, Consumer<String> why) {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        win.set("__magi_test_set", who + "|" + role + "|" + companions);
        String no = refuses();
        if (!no.isEmpty()) { why.accept(no); return; }
        if (who.startsWith("sam")) { samRole = role; samScope = companions == null ? "" : companions; }
        why.accept("");
    }

    @Override
    public void removePerson(String who, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_removed_person", who);
        String no = refuses();
        if (!no.isEmpty()) { why.accept(no); return; }
        if (who.startsWith("sam")) samGone = true;
        why.accept("");
    }

    /** 스펙이 창에 적어 두면 그 다음 쓰기가 거절당한다 — 서버가 사유를 실어 돌려보내듯. */
    private static String refuses() {
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_access_refuses");
        return v == null ? "" : String.valueOf(v);
    }
}
