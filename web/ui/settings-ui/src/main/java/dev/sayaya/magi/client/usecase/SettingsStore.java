package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.Prefs;
import dev.sayaya.magi.bridge.Windows;

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
public class SettingsStore extends dev.sayaya.magi.bridge.Told {
    private final SettingsSource source;
    private Object complete = null;   // 데몬 쪽 설정(못 읽었으면 null)

    @Inject
    public SettingsStore(SettingsSource source) { this.source = source; }



    public Object complete() { return complete; }

    /** 이 설정이 어느 것의 것인가 — 주소가 컴패니언을 대고 있으면 그 컴패니언의 것. */
    public String socket() { return Windows.query("d"); }

    public String peer() { return Windows.query("p"); }

    public void read() {
        readProfilesOnce();
        String socket = socket();
        source.read(socket.isEmpty() ? null : socket, peer().isEmpty() ? null : peer(), got -> {
            complete = got;
            told();
        });
    }

    private boolean askedProfiles = false;

    private void readProfilesOnce() {
        if (askedProfiles) return;
        askedProfiles = true;
        readProfiles();
    }

    /**
     * 사유를 <b>먼저</b> 쥐고 나서 다시 읽는다 — 다시 읽기가 이 판을 칠하므로 순서가 곧 그림이다.
     *
     * <p>이 화면의 사유는 <b>어느 칸</b>의 것인지가 함께 있어야 한다: 설정 줄이 스무 개인데
     * 사유만 하나 세우면, 사람은 방금 만진 칸이 그것인지 알 수 없다. 그래서 칸 이름과 문장을
     * 같이 쥔다 — 다음 쓰기가 답하면 어느 쪽이든 여기를 지나므로, 됐을 때 오는 빈 문자열이
     * 앞의 사유를 덮는다(따로 비우는 줄을 두지 않는다).</p>
     */
    public void save(String field, String value) {
        String socket = socket();
        source.save(socket.isEmpty() ? null : socket, peer().isEmpty() ? null : peer(),
                field, value, why -> {
                    refusal = why == null ? "" : why;
                    refusedField = refusal.isEmpty() ? "" : field;
                    read();
                });
    }

    private String refusal = "";
    private String refusedField = "";

    /** 거절당한 그 칸의 이름 — 빈 것이면 세울 사유가 없다. */
    public String refusedField() { return refusedField; }

    /** 서버가 한 말 그대로 — 우리가 지어낼 수 있는 말이 아니다. */
    public String refusal() { return refusal; }

    private Object profiles = null;

    public Object profiles() { return profiles; }

    /** 제공자 목록 — 화면이 물을 때 한 번(없으면 그 줄은 서지 않는다). */
    public void providers(java.util.function.Consumer<Object> cb) { source.providers(cb); }

    public void readProfiles() {
        String socket = socket();
        source.profiles(socket.isEmpty() ? null : socket, got -> { profiles = got; told(); });
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

    public void push(String endpoint, String p256dh, String auth, boolean delete,
                     java.util.function.Consumer<String> why) {
        source.push(endpoint, p256dh, auth, delete, why);
    }

    /** 데모면 키가 없는 이유가 다르다 — "이 콘솔엔 키가 없다"가 아니라 "여긴 사본이다". */
    public boolean demo() { return dev.sayaya.magi.bridge.Demo.on(); }

    // ── 이 브라우저의 것 ───────────────────────────────────────────────────────

    // 저장소를 직접 만지지 않는다: 이 화면이 적는 값을 읽는 것은 다른 모듈이라
    // 읽는 규칙과 적는 낱말이 한 자리(bridge/Prefs)에 있어야 한다.

    public String pref(String key, String byDefault) {
        return Prefs.text(key, byDefault);
    }

    public boolean switchOn(String key, boolean byDefault) {
        return Prefs.on(key, byDefault);
    }

    public void keep(String key, String value) { Prefs.keepText(key, value); }

    /** 스위치가 적는 값 — 낱말을 화면이 고르지 않게 한다(읽는 쪽이 다른 모듈이다). */
    public void keep(String key, boolean on) { Prefs.keep(key, on); }

    /** 데몬 쪽 스위치도 같은 낱말을 쓴다 — config가 읽는 것은 "on"/"off"다. */
    public void save(String field, boolean on) { save(field, Prefs.word(on)); }
}
