package dev.sayaya.magi.component;

import dev.sayaya.magi.bridge.Labels;
import elemental2.core.JsDate;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.NodeList;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 나이는 <b>이 창의 시계로</b> 센다 — 그 프레임은 다시 오지 않는다.
 *
 * <p>명단의 모든 행은 그 컴패니언이 쉰 시간을 초로 싣는다. 그래서 그 바이트는 아무 일이
 * 없어도 초마다 달라지고, 서버는 그 프레임을 <b>일부러 보내지 않는다</b>: 프레임을 가르는
 * 열쇠에서 쉰 시간만 빠져 있고(magi-web {@code fleetKey}), 거기 적힌 계약이 바로
 * "세는 것은 화면의 몫"이다 — <i>the counter is drawn from the row when it lands and ticks on
 * the page's own clock.</i></p>
 *
 * <p>그 절반을 아무도 지지 않고 있었다. 화면들은 프레임이 실어 온 초를 낱말로 바꿔 적기만
 * 했고, 그 낱말을 다시 쓰는 문은 <b>다음 프레임</b> 하나뿐이었다. 그런데 다음 프레임은
 * 무언가가 <i>일어나야</i> 오는 것이고, 나이를 보는 사람이 알고 싶은 것은 정확히
 * <b>아무 일도 일어나지 않은 시간</b>이다. 그래서 값이 가장 필요한 자리에서 값이 얼어붙었다:
 * 일을 마치고 쉬러 들어간 행은 그 순간 상태가 바뀌어 프레임을 한 번 받고, 그 프레임의
 * 쉰 시간은 0에 가깝다 — 세 시간을 놀아도 화면은 "방금"이라고 적는다.</p>
 *
 * <p>고치는 자리는 <b>프레임이 아니라 슬롯</b>이다. 낱말을 이고 있는 요소가 마지막 소식의
 * <b>순간</b>을 지니고(창의 시계로 환산해서), 창에 하나뿐인 시계가 그 순간부터 지금까지를
 * 매 초 다시 적는다. 프레임이 실어 오는 것이 타임스탬프가 아니라 <b>초</b>라는 것이 이
 * 환산을 가능하게 한다 — 데몬의 시계와 브라우저의 시계가 합의할 필요가 없다(턴 바가 이미
 * 같은 이유로 같은 일을 한다, shell-ui {@code TurnbarElement}).</p>
 *
 * <p>그리고 이 모양이라야 <b>기억해 둔 노드</b>도 늙는다. 카드 목록은 같은 서명이면 서 있던
 * 노드를 그대로 두고(그 서명에서도 쉰 시간은 빠져 있다 — 넣으면 아무 일 없는 행이 초당 한
 * 번씩 새 노드가 된다), 그래서 낱말을 프레임에 매달아 두면 그 노드는 영영 옛말을 인다.
 * 시계가 노드를 직접 고쳐 쓰면 서명은 뺀 채로 <b>옳다</b>.</p>
 */
public final class Ages {
    /** 마지막 소식의 순간 — 이 창의 시계로 잰 밀리초. */
    public static final String AT = "data-since";
    /** 그 낱말을 감싸는 문장의 키. 없으면 낱말만 선다. */
    public static final String IN = "data-since-in";
    /** 그 문장 안에서 낱말이 들어갈 자리의 이름. */
    public static final String SLOT = "data-since-slot";

    /** 창에 시계는 하나다 — 모듈마다 static이 따로라, 세는 자리는 창에 둔다. */
    private static final String CLOCK = "__magi_ages_clock";

    private Ages() {}

    /** 이 자리는 나이를 인다 — 낱말만. 모르는 나이(음수)면 손대지 않는다(그 말은 부르는 쪽의 것). */
    public static void on(Element el, int sec) { anchor(el, sec, null, null); }

    /** 나이가 문장 <b>안에</b> 있는 자리 — 예: "이 상자는 {ago}부터 조용하다". */
    public static void in(Element el, int sec, String key, String slot) { anchor(el, sec, key, slot); }

    /** 이 자리는 더는 늙지 않는다(그릴 나이가 없어졌을 때). */
    public static void off(Element el) {
        if (el == null) return;
        el.removeAttribute(AT);
        el.removeAttribute(IN);
        el.removeAttribute(SLOT);
    }

    private static void anchor(Element el, int sec, String key, String slot) {
        if (el == null) return;
        if (sec < 0) { off(el); return; }
        el.setAttribute(AT, String.valueOf(JsDate.now() - sec * 1000.0));
        if (key == null || key.isEmpty()) { el.removeAttribute(IN); el.removeAttribute(SLOT); }
        else { el.setAttribute(IN, key); el.setAttribute(SLOT, slot == null ? "" : slot); }
        write(el, JsDate.now());
        start();
    }

    private static void start() {
        JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
        if (win.has(CLOCK)) return;
        // 1초. 낱말은 첫 1분만 초마다 달라지고 그 뒤로는 분·시간이지만, 세는 쪽이 아니라
        // <b>쓰는 쪽</b>에서 거른다(같은 낱말이면 안 쓴다) — 언제 굵어지는지를 여기서 다시
        // 계산하면 Spans.dur와 두 곳이 같은 규칙을 알게 된다.
        win.set(CLOCK, (double) DomGlobal.setInterval(a -> tick(), 1000));
    }

    private static void tick() {
        NodeList<Element> all = DomGlobal.document.querySelectorAll("[" + AT + "]");
        if (all.getLength() == 0) {
            // 아무 자리도 나이를 이고 있지 않으면 시계는 선다 — 탭 수명만큼 도는 타이머는
            // 아무도 보지 않는 일을 위한 웨이크업이다(턴 바가 켜져 있을 때만 도는 그 이유).
            JsPropertyMap<Object> win = Js.asPropertyMap(DomGlobal.window);
            if (win.has(CLOCK)) {
                DomGlobal.clearInterval(Js.asDouble(win.get(CLOCK)));
                Js.asPropertyMap(DomGlobal.window).delete(CLOCK);
            }
            return;
        }
        double now = JsDate.now();
        for (int i = 0; i < all.getLength(); i++) write(all.getAt(i), now);
    }

    private static void write(Element el, double now) {
        String at = el.getAttribute(AT);
        if (at == null || at.isEmpty()) return;
        double from;
        try { from = Double.parseDouble(at); } catch (NumberFormatException e) { return; }
        int sec = (int) Math.max(0, Math.round((now - from) / 1000.0));
        String word = Labels.tr("time.ago", "d", Spans.dur(sec));
        String key = el.getAttribute(IN);
        String said = key == null || key.isEmpty() ? word
                : Labels.tr(key, el.getAttribute(SLOT) == null ? "" : el.getAttribute(SLOT), word);
        // 같은 낱말을 다시 쓰는 것은 무동작이 아니다 — textContent는 글자 노드를 갈아치우므로
        // 판이 바뀌었다고 보는 눈(관찰자·스크린리더)에는 매번 바뀐 것으로 보인다(지도가 밟은 자리).
        if (!said.equals(el.textContent)) el.textContent = said;
    }
}
