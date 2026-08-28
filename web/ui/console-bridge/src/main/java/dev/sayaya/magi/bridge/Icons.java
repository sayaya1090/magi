package dev.sayaya.magi.bridge;

import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.Response;
import jsinterop.base.Js;

/**
 * 아이콘 — 그림은 기존 콘솔이 굽고 이 콘솔은 빌려 쓴다.
 *
 * 스프라이트(#isprite: <symbol id="i-…"> 뭉치)는 빌드 타임에 구 콘솔 페이지에 박힌다
 * (icons.go: Font Awesome Pro라 파일로 재배포하지 않는다는 라이선스 조건). 그래서 새
 * 콘솔은 제 사본을 만들지 않고 그 페이지에서 한 번 가져와 문서에 심는다 — 단일 원천
 * 복사와 같은 규칙이고, 컷오버 때 이 대여가 자체 생성으로 바뀐다.
 *
 * 스프라이트가 없는 빌드(라이선스 없는 CI, 기여자의 빌드)도 정상이다: has()가 거짓이면
 * 화면은 늘 그리던 제 도형을 그린다(운영 icon()/iconOr의 그 계약).
 */
public final class Icons {
    private static final String SPRITE = "isprite";
    private static final String NS = "http://www.w3.org/2000/svg";
    private static final String XLINK = "http://www.w3.org/1999/xlink";

    private Icons() {}

    /**
     * 구 콘솔 페이지에서 스프라이트를 한 번 빌려 문서 맨 앞에 심는다. 실패는 조용하다.
     *
     * 후보를 순서대로 시도한다: 개발 서버에선 "/"가 프록시 너머의 구 콘솔이고, 정적
     * 데모에선 이 셸이 하위 경로(next/)에 살아 "../"가 그 자리다. 둘 다 아니면 그리던
     * 도형으로 산다.
     */
    public static void borrow(Runnable done) {
        // 다 끝나면(있든 없든) 알린다: 기다리는 화면은 "그림판이 왔다"가 아니라 "이제 물어봐도
        // 답이 확정이다"를 기다린다.
        Runnable andSay = () -> { ready(); done.run(); };
        borrowInner(andSay);
    }

    private static void borrowInner(Runnable done) {
        tryEach(new String[]{"/", "../"}, 0, done);
    }

    private static void tryEach(String[] paths, int at, Runnable done) {
        if (DomGlobal.document.getElementById(SPRITE) != null || at >= paths.length) { done.run(); return; }
        borrowFrom(paths[at], () -> tryEach(paths, at + 1, done));
    }

    private static void borrowFrom(String pagePath, Runnable done) {
        Console.raw(pagePath, null)
                .then(Response::text)
                .then(html -> {
                    int at = html.indexOf("<svg id=\"" + SPRITE + "\"");
                    int end = at < 0 ? -1 : html.indexOf("</svg>", at);
                    if (at >= 0 && end > at) {
                        HTMLElement holder = Js.uncheckedCast(DomGlobal.document.createElement("div"));
                        holder.innerHTML = html.substring(at, end + 6);
                        Element sprite = holder.firstElementChild;
                        if (sprite != null) DomGlobal.document.body.insertBefore(sprite, DomGlobal.document.body.firstChild);
                    }
                    done.run();
                    return null;
                })
                .catch_(err -> { done.run(); return null; });
    }

    /** 이 빌드에 그 그림이 있는가 — 없으면 부르는 쪽이 제 도형을 그린다. */
    public static boolean has(String ref) {
        return DomGlobal.document.getElementById(name(ref)) != null;
    }

    /**
     * 스프라이트의 그림, 없으면 null — 운영 icon()과 같은 계약이라 부르는 쪽이 분기한다.
     * 클래스는 'sic': 마크업이 오래 써 온 'ic'와 다른 이름이다(운영에서 실측된 그 충돌).
     */
    /**
     * 그림판이 도착하면 알려 준다 — 이미 와 있으면 지금.
     *
     * 그림판은 셸이 <b>가져와서</b> 심는다(fetch). 한 번만 그리는 화면은 그보다 먼저 그려질 수
     * 있고, 그때 물어보면 "없다"는 답을 받아 낱말만 남는다 — 그러고는 다시 그릴 일이 없다
     * (실측: 지식 판의 네 버튼만 그림이 없었다). 그래서 도착을 알리고, 화면은 그때 다시 그린다.
     */
    public static void onReady(Runnable then) {
        if (Js.asPropertyMap(DomGlobal.window).has(READY)) { then.run(); return; }
        waiters().push(then::run);
    }

    /**
     * 창에 두는 목록은 자바가 아니라 자바스크립트 배열이다 — 모듈마다 따로 컴파일되므로
     * 한 모듈의 java.util.List를 다른 모듈이 제 타입으로 쓰면 없는 메서드를 부른다.
     */
    private static elemental2.core.JsArray<Runner> waiters() {
        Object had = Js.asPropertyMap(DomGlobal.window).get(WAITERS);
        if (had != null) return Js.uncheckedCast(had);
        elemental2.core.JsArray<Runner> made = new elemental2.core.JsArray<>();
        Js.asPropertyMap(DomGlobal.window).set(WAITERS, made);
        return made;
    }

    @jsinterop.annotations.JsFunction
    public interface Runner { void call(); }

    /** 셸: 그림판이 문서에 들어왔다. */
    public static void ready() {
        Js.asPropertyMap(DomGlobal.window).set(READY, true);
        elemental2.core.JsArray<Runner> waiting = waiters();
        while (waiting.length > 0) waiting.shift().call();
    }

    private static final String READY = "__magi_sprite_ready";
    private static final String WAITERS = "__magi_sprite_waiters";

    public static Element of(String ref, String cls) {
        if (!has(ref)) return null;
        Element svg = DomGlobal.document.createElementNS(NS, "svg");
        svg.setAttribute("class", "sic " + (cls == null ? "" : cls));
        svg.setAttribute("aria-hidden", "true");
        Element use = DomGlobal.document.createElementNS(NS, "use");
        use.setAttribute("href", "#" + name(ref));
        use.setAttributeNS(XLINK, "xlink:href", "#" + name(ref));
        svg.append(use);
        return svg;
    }

    /**
     * 버튼의 표 — <b>슬롯에</b> 넣는다(운영 withMark).
     *
     * 그냥 자식으로 붙이면 라벨과 같은 글자 크기를 물려받아 작아진다(실측: 18px 자리에 14px).
     * md-*-button은 slot="icon"에 온 것을 제 규격으로 그린다.
     */
    /**
     * 낱말과 표를 한 번에 — <b>순서가 계약이다</b>: textContent는 자식을 통째로 갈아 치우므로
     * 표를 먼저 달면 낱말이 그것을 지운다(실측: 지식 판의 네 버튼이 그렇게 낱말만 남았다).
     * 부르는 쪽이 그 순서를 기억하게 두지 않고 여기서 지킨다. 표는 버튼에 적어 두어(data-mark)
     * 나중에 말이 바뀌어도(reword) 되살아난다 — "정말?"로 갈아입는 버튼이 그런 자리다.
     */
    public static void say(HTMLElement button, String word, String ref) {
        if (ref != null) button.setAttribute("data-mark", ref);
        reword(button, word);
    }

    /** 버튼의 말을 갈아 끼운다 — 제 표(data-mark)는 잃지 않는다. */
    public static void reword(HTMLElement button, String word) {
        button.textContent = word;
        String ref = button.getAttribute("data-mark");
        if (ref != null && !ref.isEmpty()) mark(button, ref);
    }

    public static void mark(HTMLElement button, String ref) {
        elemental2.dom.Element m = of(ref, null);
        // 없으면 아무것도 넣지 않는다 — 이 버튼에는 이미 말이 적혀 있다. 대신 글자 도형을 세우면
        // 그 낱자가 말 옆에 붙어 버튼이 "☰Convene"으로 읽힌다(실측). 글자 도형은 그림만 있는
        // 컨트롤의 것이다(orGlyph): 거기서는 그것이 유일하게 남은 표시다.
        if (m == null) return;
        m.setAttribute("slot", "icon");
        button.insertBefore(m, button.firstChild);
    }

    /**
     * 목록을 좁혀 읽는 칸의 돋보기 — 필드는 그림을 <b>슬롯</b>으로 받는다(자식으로 넣으면
     * 글자 옆에 그냥 놓인다). 이 콘솔에서 타이핑으로 목록을 좁히는 칸은 네 곳이고 넷 다 같은
     * 일이라, 그 표시는 한 곳에 적는다(운영 withGlass).
     */
    public static HTMLElement glass(HTMLElement field) {
        Element g = of("#i-sl-magnifying-glass", null);
        if (g != null) {
            g.setAttribute("slot", "leading-icon");
            field.append(g);
        }
        return field;
    }

    /** 그림이 있으면 그림, 없으면 늘 그리던 글자 — 어느 쪽이든 노드를 돌려준다(운영 iconOr). */
    public static Element orGlyph(String ref, String glyph, String cls) {
        Element drawn = of(ref, cls);
        if (drawn != null) return drawn;
        HTMLElement s = Js.uncheckedCast(DomGlobal.document.createElement("span"));
        s.className = "gl " + (cls == null ? "" : cls);
        s.textContent = glyph;
        s.setAttribute("aria-hidden", "true");
        return s;
    }

    /**
     * 마크업이 이미 그린 도형을 스프라이트 그림으로 갈아입힌다(운영 dressIcons):
     * data-i를 단 <svg>의 속을 <use>로 바꾼다 — 스프라이트가 없으면 그대로 둔다.
     */
    public static void dress(Element root) {
        elemental2.dom.NodeList<Element> boxes = (root == null ? DomGlobal.document.body : root)
                .querySelectorAll("[data-i]");
        for (int i = 0; i < boxes.getLength(); i++) {
            Element box = boxes.getAt(i);
            Element drawn = of(box.getAttribute("data-i"), null);
            if (drawn == null) continue;
            box.replaceChildren();
            while (drawn.firstElementChild != null) box.append(drawn.firstElementChild);
        }
    }

    private static String name(String ref) {
        String r = ref == null ? "" : ref;
        return r.startsWith("#") ? r.substring(1) : r;
    }
}
