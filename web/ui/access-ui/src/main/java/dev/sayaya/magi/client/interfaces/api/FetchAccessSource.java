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
        // ⚠ 사유가 설 자리가 이 포트에 없다(done은 명단을 다시 읽는 일이다) — 거절당하면
        // 명단이 그대로 다시 서고, 사람은 제가 누른 것이 왜 안 됐는지 듣지 못한다.
        Console.post("/access", body, null, null, (ok, w) -> done.run());
    }

    @Override
    public void removePerson(String who, Runnable done) {
        URLSearchParams body = new URLSearchParams();
        body.set("who", who);
        body.set("remove", "1");
        Console.post("/access", body, null, null, (ok, w) -> done.run());   // ⚠ 위와 같다
    }
}
