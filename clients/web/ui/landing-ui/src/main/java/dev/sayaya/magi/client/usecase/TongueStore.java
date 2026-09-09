package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.Told;
import dev.sayaya.magi.client.domain.Tongue;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 지금 무슨 말로 그리는가. 페이지의 유일한 상태다.
 *
 * 첫 값은 브라우저에게 물어 규칙이 정하고, 그 뒤로는 사람이 정한다. 바뀌면 소식만 흐르고
 * (Told), 다시 그리는 것은 판의 몫이다 — 판이 하나뿐이라 조각을 내려보낼 것이 없다.
 */
@Singleton
public class TongueStore extends Told {
    private final TongueSource source;
    private Tongue now;

    @Inject
    public TongueStore(TongueSource source) {
        this.source = source;
        this.now = Tongue.pick(source.stored(), source.preferred());
    }

    public Tongue now() { return now; }

    public void choose(Tongue tongue) {
        if (tongue == null || tongue == now) return;
        now = tongue;
        source.keep(tongue.code());
        told();
    }

    public void toggle() { choose(now.other()); }
}
