package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.client.usecase.RosterStore;
import elemental2.core.JsDate;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 턴바 — 지금 보는 컴패니언의 턴 하나가 열려 있다는 말, 창 맨 위를 가로지르는 한 줄.
 *
 * 왜 셸의 것인가: 그 줄은 창의 가로 전체를 재는 것이라(운영 css: position:fixed; left:0;
 * right:0) 어떤 기둥 안에 서면 그 기둥이 기준 상자가 된다 — 실측으로 신판의 기준 상자는
 * 창이 아니라 #stream이었고, 그래서 바가 가운데 기둥 폭에만 걸쳐 있었다. 게다가 이 바를
 * 켜는 사실(turn 프레임)은 스트림을 가진 셸에게 이미 와 있다. 자식이 제자리에 그리려면
 * "창 폭짜리 요소를 body에 붙여라"라는 계약을 화면마다 지켜야 하는데, 그건 부모가 한 번
 * 하면 되는 일이다.
 *
 * 마크업·표시 규칙은 운영 콘솔의 것 그대로다(#turnwrap[hidden] > md-linear-progress#turnbar
 * + #turnfor). 바가 조용한 이유(aria-hidden)도 운영과 같다: 행이 이미 말로 말했다.
 */
@Singleton
public class TurnbarElement {
    private final HTMLElement box = el("div");
    private final HTMLElement forSpan = el("span");
    private boolean open = false;
    private double from = 0;
    private double tick = -1;

    @Inject
    public TurnbarElement(RosterStore roster) {
        box.id = "turnwrap";
        box.setAttribute("hidden", "");
        HTMLElement bar = el("md-linear-progress");
        bar.id = "turnbar";
        Js.asPropertyMap(bar).set("indeterminate", true);
        bar.setAttribute("aria-hidden", "true");
        forSpan.id = "turnfor";
        forSpan.setAttribute("aria-hidden", "true");
        box.append(bar, forSpan);
        roster.onTurn(this::paint);
    }

    public HTMLElement element() { return box; }

    /**
     * 프레임은 나이를 초로 싣는다 — 타임스탬프가 아니다. 그래서 읽는 값이 브라우저 시계와
     * 데몬 시계의 합의에 걸리지 않는다(운영 주석의 그 이유). 여기서부터는 이 창의 시계로 센다.
     */
    private void paint(boolean nowOpen, double forSec) {
        open = nowOpen;
        from = JsDate.now() - forSec * 1000;
        if (open) box.removeAttribute("hidden");
        else box.setAttribute("hidden", "");
        if (tick >= 0) { DomGlobal.clearInterval(tick); tick = -1; }
        paintFor();
        // 1초, 켜져 있는 동안만 — 숨은 요소를 상대로 도는 타이머는 탭 수명만큼의 웨이크업이다.
        if (open) tick = DomGlobal.setInterval(a -> paintFor(), 1000);
    }

    private void paintFor() {
        forSpan.textContent = open
                ? dur((int) Math.max(0, Math.round((JsDate.now() - from) / 1000))) : "";
    }

    /** s/m/h/d — 운영 dur()와 같은 축약: 단위는 언어를 타지 않는다. */
    private static String dur(int s) {
        if (s < 60) return s + "s";
        if (s < 3600) return Math.round(s / 60f) + "m";
        if (s < 86400) return Math.round(s / 3600f) + "h";
        return Math.round(s / 86400f) + "d";
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
