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
    public FakeAccessSource() {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        // 붙잡아 둔 대답을 스펙이 <b>고른 순서로</b> 놓아준다. 순서를 바꿔 볼 길이 없으면
        // "먼저 누른 것이 나중에 답한다"는 판을 스펙이 만들 수 없고, 그 판에서만 드러나는
        // 결함은 영영 안 보인다.
        win.set("__magi_test_release", (Release) i -> {
            if (i < 0 || i >= held.size()) return false;
            Runnable one = held.get(i);
            held.set(i, null);
            if (one == null) return false;
            one.run();
            return true;
        });
        win.set("__magi_test_held", (Held) () -> held.size());
    }

    /** 놓아준 것이 <b>실제로 있었는지</b>를 돌려준다 — 아무 일도 안 일어난 초록과 갈라야 한다. */
    @jsinterop.annotations.JsFunction
    public interface Release { boolean call(int i); }

    @jsinterop.annotations.JsFunction
    public interface Held { int call(); }

    private final java.util.List<Runnable> held = new java.util.ArrayList<>();

    /**
     * 어느 갈래를 손에 쥘 것인가 — `'write'`, `'read'`, 또는 둘 다(`'write read'`).
     *
     * <p>갈래를 갈라 쥘 수 있어야 한다: 쓰기의 순서를 재는 스펙은 그 사이의 명부 읽기까지
     * 붙잡히면 판이 안 그려져서, 무엇을 재는지가 흐려진다.</p>
     */
    private static boolean holding(String kind) {
        Object v = Js.asPropertyMap(DomGlobal.window).get("__magi_test_hold");
        return v != null && String.valueOf(v).contains(kind);
    }

    private void answer(Consumer<String> why, String said) {
        if (holding("write")) held.add(() -> why.accept(said));
        else why.accept(said);
    }

    private void answer(Consumer<Object> cb, Object got) {
        if (holding("read")) held.add(() -> cb.accept(got));
        else cb.accept(got);
    }

    @Override
    public void roster(Consumer<Object> cb) {
        if (unreachable()) { answer(cb, null); return; }
        String sam = samGone ? "" :
                ",{\"who\":\"sam@laptop\",\"role\":\"" + samRole + "\",\"can\":[\"read\"]" +
                (samScope.isEmpty() ? "" : ",\"companions\":[\"" + samScope.replace(",", "\",\"") + "\"]") + "}";
        answer(cb, Global.JSON.parse(
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
        if (!no.isEmpty()) { answer(why, no); return; }
        if (who.startsWith("sam")) { samRole = role; samScope = companions == null ? "" : companions; }
        answer(why, "");
    }

    @Override
    public void removePerson(String who, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_removed_person", who);
        String no = refuses();
        if (!no.isEmpty()) { answer(why, no); return; }
        if (who.startsWith("sam")) samGone = true;
        answer(why, "");
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
