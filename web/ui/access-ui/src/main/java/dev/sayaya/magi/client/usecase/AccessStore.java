package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

/** 접근 화면의 저장소 — 명부와 능력 필터(화면의 보는 방식이지 명부의 사실이 아니다). */
@Singleton
public class AccessStore {
    private final AccessSource source;
    private final List<Runnable> observers = new ArrayList<>();
    private Object got = null;
    private boolean answered = false;
    private String capFilter = null;
    private boolean started = false;

    @Inject
    public AccessStore(AccessSource source) { this.source = source; }

    public void start() {
        if (started) return;
        started = true;
        reload();
    }

    public void reload() { source.roster(g -> { got = g; answered = true; emit(); }); }

    public Object got() { return got; }
    public boolean answered() { return answered; }
    public String capFilter() { return capFilter; }

    public void filter(String cap) {
        capFilter = cap != null && cap.equals(capFilter) ? null : cap;
        emit();
    }

    public void setPerson(String who, String role, String companions) {
        source.setPerson(who, role, companions, this::reload);
    }

    public void removePerson(String who) { source.removePerson(who, this::reload); }

    public void subscribe(Runnable o) { observers.add(o); o.run(); }

    private void emit() { for (Runnable o : observers) o.run(); }
}
