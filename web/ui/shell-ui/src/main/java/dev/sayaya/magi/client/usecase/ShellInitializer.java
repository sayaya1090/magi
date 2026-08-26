package dev.sayaya.magi.client.usecase;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 셸의 흐름: 명단 스트림을 소유하고(브리지 호스팅 포함), 목적지가 정해지면 문을 칠하고
 * 캐시된 렌더를 앉히거나 모듈을 들인다. 도착한 렌더는 프레임으로.
 */
@Singleton
public class ShellInitializer {
    private final Navigation nav;
    private final RenderStore renders;
    private final ModuleLoader loader;
    private final RailView rail;
    private final FrameView frame;
    private final RosterStore roster;

    @Inject
    public ShellInitializer(Navigation nav, RenderStore renders, ModuleLoader loader,
                            RailView rail, FrameView frame, RosterStore roster) {
        this.nav = nav;
        this.renders = renders;
        this.loader = loader;
        this.rail = rail;
        this.frame = frame;
        this.roster = roster;
    }

    public void initialize() {
        // 화면 모듈보다 먼저 — 브리지의 문이 걸린 뒤에 모듈이 들어와야 제 회선을 안 연다.
        roster.start();
        renders.onRender(frame::mount);
        nav.subscribe(dest -> {
            rail.select(dest);
            Object cached = renders.renderOf(dest.id);
            if (cached != null) { frame.mount(cached); return; }
            renders.expect(dest.id);
            loader.ensure(dest);
        });
        nav.start();
    }
}
