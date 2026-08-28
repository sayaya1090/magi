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
        if (unreachable()) { cb.accept(null); return; }
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
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_press_refuses");
        return v == null ? "" : String.valueOf(v);
    }

    /**
     * 회선이 끊긴 판 — 명부 읽기가 <b>null</b>로 온다({@code Console.fetchList}가 거부·불통·
     * 깨진 본문을 전부 null로 접으므로).
     *
     * <p>이것이 거절과 <b>겹치는</b> 판을 만들 수 있어야 한다: 쓰기가 못 닿아 우리가 지어낸
     * 말이 설 때는 뒤따르는 읽기도 못 닿는다. 그 겹침이 페이크에 없어서 — 여기 명부 읽기는
     * 언제나 성공했다 — 스펙이 「사유가 명부 실패 갈래에서 버려진다」를 볼 길이 없었다.</p>
     */
    private static boolean unreachable() {
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_unreachable");
        return v != null && !"false".equals(String.valueOf(v));
    }
}
