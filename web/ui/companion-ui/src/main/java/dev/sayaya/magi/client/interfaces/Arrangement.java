package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.ChromeSharing;
import dev.sayaya.magi.bridge.Render;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 창을 어떻게 배치할 것인가 — 컴패니언 화면의 기둥 여닫이와 도크. 부모가 진다.
 *
 * 이것이 부모에게 있는 이유: 자식(타입 UI)이 제 자리를 채우려고 알아야 할 것을 최대한
 * 줄이는 것이 이 층의 일이기 때문이다. 자식은 body 속성도, --dock이라는 변수도, 도크가
 * 창 바닥에 고정된 상자라는 사실도 모른다 — 부모가 옷을 입혀 건넨 자리에 그리기만 한다.
 *
 * 규칙은 전부 운영 콘솔의 것이다:
 * <ul>
 *   <li>기둥 둘은 <b>기본이 닫힘</b>이고 기억된다(localStorage) — "the conversation is what
 *       the page is for, and a reader who wants the tree asks for it once". 닫히면 폭이
 *       0이라 대화가 창을 다 갖는다.</li>
 *   <li>손잡이는 열리는 기둥이 아니라 <b>마스트헤드</b>에 선다 — 그것이 여는 것은 기둥이지만
 *       손잡이 자체는 이 창의 배치에 대한 것이라서. 셸이 내준 자리(ChromeSharing)에 민다.</li>
 *   <li>도크는 창 바닥에 고정된 상자이고, 그 높이는 실측해 --dock에 넣는다 — main의 바닥
 *       여백이 그 변수로 잡혀 있어(page.css), 재지 않으면 기본값이 남는다.</li>
 * </ul>
 */
@Singleton
public class Arrangement {
    private final HTMLElement dock = el("footer");
    private final HTMLElement bay = el("div");
    private final HTMLElement dockFill = el("div");
    private final HTMLElement handles = el("span");
    private final HTMLElement filesToggle = el("md-icon-button");
    private final HTMLElement sideToggle = el("md-icon-button");
    private boolean built = false;
    private boolean engaged = false;

    @Inject
    public Arrangement() {}

    /** 자식이 컴포저를 놓을 자리 — 도크 안쪽이다. 자식은 이것이 도크인 줄 모른다. */
    public HTMLElement dockSlot() { return dockFill; }

    /** 컴패니언 화면이 섰다: 도크를 세우고, 손잡이를 마스트헤드에 놓고, 기둥 상태를 되살린다. */
    public void engage() {
        build();
        engaged = true;
        DomGlobal.document.body.appendChild(dock);
        ChromeSharing.next((Render) box -> { box.replaceChildren(handles); return true; });
        say("files", remembered("files"));
        say("side", remembered("side"));
        measure();
    }

    /** 화면을 떠났다: 도크와 손잡이를 걷고, 배치 속성도 지운다 — 목록에는 기둥이 없다. */
    public void dismiss() {
        if (!engaged) return;
        engaged = false;
        dock.remove();
        dockWas = -1;
        ChromeSharing.clear();
        DomGlobal.document.body.removeAttribute("files");
        DomGlobal.document.body.removeAttribute("side");
        DomGlobal.document.documentElement.style.setProperty("--dock", "0px");
    }

    private int dockWas = -1;

    /**
     * 도크의 높이를 재어 --dock에 넣는다 — 컴포저가 자라면(여러 줄) 본문 바닥도 함께 물러난다.
     *
     * ⚠ 바뀐 값만 쓴다. 쓰는 순간 본문 여백이 달라지고, 그 배치 변화가 도크를 관찰하던
     * ResizeObserver를 다시 깨운다 — 같은 값을 계속 쓰면 그 왕복이 멈추지 않는다(실측:
     * 브라우저가 응답을 멈춰 스펙의 evaluate까지 타임아웃).
     */
    public void measure() {
        if (!engaged) return;
        int h = (int) Math.round(dock.getBoundingClientRect().height);
        if (h == dockWas) return;
        dockWas = h;
        DomGlobal.document.documentElement.style.setProperty("--dock", h + "px");
    }

    private void build() {
        if (built) return;
        built = true;
        dock.id = "dock";
        bay.className = "bay";
        // display:contents — 자식이 넣은 것이 곧 bay의 자식으로 배치된다. 자식은 제 마크업을
        // 그대로 쓰고(운영의 form#f/.composer), 그 사이에 낀 상자 때문에 규칙이 어긋나지 않는다.
        dockFill.className = "cfill";
        bay.append(dockFill);
        dock.append(bay);
        handles.className = "panehandles";
        handle(filesToggle, "files", "M4 5h16v14H4z", "M9 9.5v9.5", "lead");
        handle(sideToggle, "side", "M4 5h16v14H4z", "M15 9.5v9.5", "");
        handles.append(filesToggle, sideToggle);
        // 도크가 자라고 줄 때마다 다시 잰다(여러 줄 질문·답 상자). 실측 없이는 마지막 줄이 가린다.
        observe(dock, this::measure);
    }

    /** 손잡이 하나 — 운영의 그 그림(화면과 칸막이, 칸막이는 제 쪽에서 들어온다)과 그 말. */
    private void handle(HTMLElement btn, String key, String frame, String split, String lead) {
        btn.id = key + "Toggle";
        btn.setAttribute("toggle", "");
        btn.setAttribute("aria-expanded", "false");
        btn.innerHTML = "<svg class=\"panelic " + lead + "\" viewBox=\"0 0 24 24\" width=\"24\" height=\"24\""
                + " aria-hidden=\"true\"><path d=\"" + frame + "\" stroke=\"currentColor\" stroke-width=\"1.6\""
                + " fill=\"none\" stroke-linejoin=\"round\"/><path d=\"M4 9.5h16\" stroke=\"currentColor\""
                + " stroke-width=\"1.6\" stroke-linecap=\"round\"/><path class=\"split\" d=\"" + split + "\""
                + " stroke=\"currentColor\" stroke-width=\"1.6\" stroke-linecap=\"round\"/></svg>";
        btn.addEventListener("change", evt -> say(key, Js.isTruthy(Js.asPropertyMap(btn).get("selected"))));
    }

    /** 열림/닫힘을 한 곳에서 말한다: 속성·기억·손잡이의 상태와 이름이 같은 사실에서 나온다. */
    private void say(String key, boolean open) {
        DomGlobal.document.body.setAttribute(key, open ? "open" : "shut");
        store("magi." + key, open ? "open" : "shut");
        HTMLElement btn = "files".equals(key) ? filesToggle : sideToggle;
        Js.asPropertyMap(btn).set("selected", open);
        btn.setAttribute("aria-expanded", String.valueOf(open));
        // 이름 없는 아이콘 버튼은 스크린리더에 "버튼"이다 — 무엇을 주는지 말한다.
        String word = tr(key + (open ? ".hide" : ".show"));
        btn.setAttribute("aria-label", word);
        btn.setAttribute("title", word);
        measure();
    }

    /** 기본은 닫힘 — 처음 온 사람에게 이 페이지는 대화다. */
    private boolean remembered(String key) {
        return "open".equals(stored("magi." + key));
    }

    // 사적 창에서는 localStorage 접근 자체가 던진다 — 기억이 없으면 기본값으로 산다.
    private static String stored(String key) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls == null) return null;
            Object v = Js.asPropertyMap(ls).get(key);
            return v == null ? null : String.valueOf(v);
        } catch (Exception e) { return null; }
    }

    private static void store(String key, String val) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls != null) Js.asPropertyMap(ls).set(key, val);
        } catch (Exception ignored) { }
    }

    private static native void observe(HTMLElement el, Runnable then) /*-{
        if (typeof $wnd.ResizeObserver !== 'function') return;
        new $wnd.ResizeObserver(function () { then.@java.lang.Runnable::run()(); }).observe(el);
    }-*/;

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
