package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.client.domain.Place;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 셸의 흐름: 스트림을 소유하고(브리지 호스팅 포함), 서는 곳이 정해지면 문을 칠하고
 * 캐시된 렌더를 앉히거나 모듈을 들인다. 컴패니언이면 모듈은 타입 카탈로그가 정한다 —
 * 화면(플릿 행·레일 2단)은 GoSharing으로 이동을 청할 뿐, 주소는 셸의 것이다.
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
        GoSharing.host(nav::goCompanion);
        GoSharing.hostView(v -> nav.go(dev.sayaya.magi.client.domain.Destination.byId(v)));
        GoSharing.hostPast(nav::goPast);
        renders.onRender(frame::mount);
        nav.subscribe(place -> {
            // 운영 showDestination의 그 속성: 폰의 하단 바가 컴패니언 화면에서 물러나는
            // 규칙(body[at=agent] #rail 숨김)이 console.css에 있고, 속성은 셸의 몫이다.
            elemental2.dom.DomGlobal.document.body.setAttribute("at", place.isCompanion() ? "agent" : "list");
            // ⚠ body[view=…]는 적지 않는다: 운영의 화면 전환 CSS 기계(판 숨김·오프스크린)가
            // 그 속성에 걸려 있어, 마운트로 가시성을 관리하는 이 셸에서 켜면 판이 사라진다
            // (실측: #skills w=0, left=-1000). at(폰에서 레일이 물러나는 규칙)만 운영 계약이다.
            rail.select(place.section);
            // 스트림 조준과 컨텍스트가 모듈보다 먼저다 — 모듈의 첫 구독이 현재값을 재생받는다.
            roster.aim(place.socket, place.peer);
            roster.past(place.past);
            String module = place.isCompanion()
                    ? roster.typeOf(place.socket).module
                    : place.screen.id;
            Object cached = renders.renderOf(module);
            if (cached != null) { frame.mount(cached); return; }
            renders.expect(module);
            loader.ensure(module);
        });
        nav.start();
    }
}
