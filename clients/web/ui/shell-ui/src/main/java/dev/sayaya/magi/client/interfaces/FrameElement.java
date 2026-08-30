package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Labels;
import dev.sayaya.magi.bridge.Motion;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.client.usecase.FrameView;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 화면이 앉는 자리 — <main>. page.css의 main 규칙(가로 중앙·페이지 캡·레일 거터)이
 * 그대로 이 요소를 입는다.
 */
@Singleton
public class FrameElement implements FrameView {
    private final HTMLElement element;

    @Inject
    public FrameElement() {
        element = Js.uncheckedCast(DomGlobal.document.createElement("main"));
        element.id = "frame";
        // page.css의 main은 바닥 여백을 calc(var(--dock, 매우 큰 기본값) + …)로 잡는다:
        // 운영은 컴포저 도크를 실측해 --dock에 넣고(measureDock), 이 셸엔 그 도크가 없어
        // 기본값이 남아 160px이 깔렸다(실측: 운영 32px 대 여기 160px). 도크가 없다는 것을
        // 말해 둔다 — 컴패니언 화면이 제 도크를 갖게 되면 그때 그 화면이 실측해 덮는다.
        DomGlobal.document.documentElement.style.setProperty("--dock", "0px");
    }

    public HTMLElement element() { return element; }

    /**
     * 화면을 이 자리에 앉힌다 — <b>말이 도착한 뒤에</b>.
     *
     * 언어 팩 대기가 여기 있는 이유: 화면마다 제 마운트를 Labels.load로 감싸게 하면, 그것은
     * 화면마다 지켜야 할 계약이 하나 느는 일이고 잊으면 키 문자열이 그대로 그려진다(실측:
     * 화면이 팩보다 빨랐던 첫 그리기). 부를 수 있는 시점을 아는 쪽은 부르는 쪽이다.
     */
    @Override
    public void mount(Object render) {
        Render r = Js.cast(render);
        Labels.load(() -> {
            r.onInvoke(element);
            reveal();
        });
    }

    /**
     * 새 화면은 들어온다 — 운영은 목적지가 그려질 때마다 그 판에 .enter를 붙인다(fadeThrough).
     * 부르는 쪽이 하는 이유는 늘 같다: 화면마다 시키면 잊은 화면만 뚝 나타난다.
     *
     * 판을 하나하나 알지 못하므로 프레임의 직계들을 들인다 — 그것이 곧 그 화면의 몸이다.
     * 컴패니언은 예외다: 거기서 움직이는 것은 무대 전체가 아니라 대화 기둥 하나라, 그 화면이
     * 제 것을 스스로 들인다(운영도 streamEl 하나만 들인다).
     */
    private void reveal() {
        if (Windows.companionAimed()) return;
        elemental2.dom.NodeList<elemental2.dom.Element> kids = element.querySelectorAll(":scope > *");
        for (int i = 0; i < kids.getLength(); i++) Motion.enter(kids.getAt(i));
    }
}
