package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.Console;
import dev.sayaya.magi.client.usecase.AccessSource;
import elemental2.dom.URLSearchParams;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.function.Consumer;

@Singleton
public class FetchAccessSource implements AccessSource {
    @Inject
    public FetchAccessSource() {}

    @Override
    public void roster(Consumer<Object> cb) { Console.fetchList("/access", cb::accept); }

    @Override
    public void setPerson(String who, String role, String companions, Runnable done) {
        URLSearchParams body = new URLSearchParams();
        body.set("who", who);
        body.set("role", role);
        body.set("companions", companions == null ? "" : companions);
        Console.post("/access", body, null, null).then(w -> { done.run(); return null; });
    }

    @Override
    public void removePerson(String who, Runnable done) {
        URLSearchParams body = new URLSearchParams();
        body.set("who", who);
        body.set("remove", "1");
        Console.post("/access", body, null, null).then(w -> { done.run(); return null; });
    }
}
