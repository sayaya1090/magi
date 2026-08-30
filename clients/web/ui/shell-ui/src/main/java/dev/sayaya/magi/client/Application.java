package dev.sayaya.magi.client;

import com.google.gwt.core.client.EntryPoint;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.Labels;
import elemental2.dom.DomGlobal;

/**
 * 셸: 마스트헤드·드로어(레일)·프레임을 세우고, 명단 스트림을 소유하며, 주소가 대는
 * 목적지의 화면 모듈을 들인다. 언어 팩이 먼저다 — 문과 바의 말이 팩에서 온다.
 */
public class Application implements EntryPoint {
    @Override
    public void onModuleLoad() {
        ShellComponent component = DaggerShellComponent.create();
        // 그림은 구 콘솔이 굽는다 — 한 번 빌려 심고 나서 그린다(Icons.borrow). 없어도 정상:
        // 그때는 각 화면이 늘 그리던 제 도형을 그린다.
        Icons.borrow(() -> Labels.load(() -> {
            DomGlobal.document.body.append(
                    // 창을 가로지르는 것이 먼저다 — 운영 page.html도 마스트헤드 위에 둔다.
                    component.turnbar().element(),
                    component.masthead().element(),
                    component.rail().scrim(),
                    component.rail().element(),
                    component.frame().element(),
                    // 팔레트는 창의 것이라 화면 밖에 선다 — 어느 화면에서 눌러도 같은 상자다.
                    component.palette().element(),
                    // 툴팁도 창의 것이다: 판 하나를 세워 두면 어느 화면의 컨트롤이든 제
                    // data-tip만 적으면 된다(셸이 그 화면들을 알 필요가 없다).
                    component.tip().element());
            component.masthead().paint();
            component.rail().paint();
            component.initializer().initialize();
            Icons.dress(DomGlobal.document.body);
        }));
    }
}
