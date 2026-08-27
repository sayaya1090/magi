package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.client.domain.Prefs;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/**
 * 취향이 어디 사는가 — 두 곳이고, 그 둘은 다른 것이다.
 *
 * <b>이 브라우저의 것</b>(테마·언어·모델 보조 스위치)은 localStorage에 산다: 다른 기계에서
 * 이 콘솔을 열면 그 기계의 취향이 있어야 한다. <b>데몬이 읽는 것</b>(완성 프로파일·주변
 * 파일·과거 프롬프트)은 config 파일에 산다 — 그것은 에이전트가 어떻게 도는지에 대한 것이라
 * 브라우저의 사정이 아니다.
 */
@Singleton
public class SettingsStore {
    private final SettingsSource source;
    private final List<Runnable> obs = new ArrayList<>();
    private Object complete = null;   // 데몬 쪽 설정(못 읽었으면 null)

    @Inject
    public SettingsStore(SettingsSource source) { this.source = source; }

    public void subscribe(Runnable o) { obs.add(o); }

    private void tell() { for (Runnable o : new ArrayList<>(obs)) o.run(); }

    public Object complete() { return complete; }

    /** 이 설정이 어느 것의 것인가 — 주소가 컴패니언을 대고 있으면 그 컴패니언의 것. */
    public String socket() { return Windows.query("d"); }

    public String peer() { return Windows.query("p"); }

    public void read() {
        readProfilesOnce();
        String socket = socket();
        source.read(socket.isEmpty() ? null : socket, peer().isEmpty() ? null : peer(), got -> {
            complete = got;
            tell();
        });
    }

    private boolean askedProfiles = false;

    private void readProfilesOnce() {
        if (askedProfiles) return;
        askedProfiles = true;
        readProfiles();
    }

    public void save(String field, String value) {
        String socket = socket();
        source.save(socket.isEmpty() ? null : socket, peer().isEmpty() ? null : peer(),
                field, value, this::read);
    }

    private Object profiles = null;

    public Object profiles() { return profiles; }

    public void readProfiles() {
        String socket = socket();
        source.profiles(socket.isEmpty() ? null : socket, got -> { profiles = got; tell(); });
    }

    public void saveProfile(String name, String baseUrl, String model, String key, boolean delete,
                            java.util.function.Consumer<String> why) {
        String socket = socket();
        source.saveProfile(socket.isEmpty() ? null : socket, name, baseUrl, model, key, delete, w -> {
            why.accept(w);
            if (w == null || w.isEmpty()) { readProfiles(); read(); }
        });
    }

    public void pushKey(java.util.function.Consumer<String> key) { source.pushKey(key::accept); }

    public void push(String endpoint, String p256dh, String auth, boolean delete, Runnable then) {
        source.push(endpoint, p256dh, auth, delete, then);
    }

    /** 데모면 키가 없는 이유가 다르다 — "이 콘솔엔 키가 없다"가 아니라 "여긴 사본이다". */
    public boolean demo() { return dev.sayaya.magi.bridge.Demo.on(); }

    // ── 이 브라우저의 것 ───────────────────────────────────────────────────────

    public String pref(String key, String byDefault) {
        String v = stored(key);
        return v == null || v.isEmpty() ? byDefault : v;
    }

    public boolean switchOn(String key, boolean byDefault) {
        return Prefs.on(stored(key), byDefault);
    }

    public void keep(String key, String value) { store(key, value); }

    /** 사적 창에서는 접근 자체가 던진다 — 기억이 없으면 기본값으로 산다. */
    private static String stored(String key) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls == null) return null;
            Object v = Js.asPropertyMap(ls).get(key);
            return v == null ? null : String.valueOf(v);
        } catch (Exception e) { return null; }
    }

    private static void store(String key, String value) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls != null) Js.asPropertyMap(ls).set(key, value);
        } catch (Exception ignored) { }
    }
}
