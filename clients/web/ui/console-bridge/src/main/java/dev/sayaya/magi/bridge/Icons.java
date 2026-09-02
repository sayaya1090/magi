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


    /**
     * 그림이 없을 때 <b>글자 대신 도형</b> — 스프라이트가 있으면 그것으로 갈아입는다.
     *
     * 라이선스가 없는 빌드(기여자·정적 데모)에서는 스프라이트가 없다. 그때 낱자로 떨어지면
     * 새로고침이 "↻", 더보기가 "⋯"으로 읽히는데, 그 자리들은 <b>그림만 있는</b> 컨트롤이라
     * 낱자가 곧 그 버튼의 얼굴이 된다(실측: 데모에서 아홉 자리). 여기 적힌 획은 그 자리에
     * 서는 진짜 도형이고, 스프라이트가 있으면 data-i를 보고 dress가 갈아입힌다 — 운영이
     * 보드의 화살에 쓰던 그 방법이다.
     */
    public static Element shape(String ref, String cls) {
        String d = path(ref);
        if (d == null) return of(ref, cls);
        Element svg = DomGlobal.document.createElementNS(NS, "svg");
        svg.setAttribute("class", "sic " + (cls == null ? "" : cls));
        svg.setAttribute("data-i", ref);
        svg.setAttribute("viewBox", "0 0 24 24");
        // 크기는 <b>적지 않는다</b> — 스프라이트 그림(of)도 적지 않고, 자리마다 CSS가 정한다
        // (트리의 행 메뉴는 16px, 카드의 것은 20px). 여기에 20을 박아 두었더니 그 규칙을 이겨
        // 트리 행이 한 픽셀 자랐다(실측: 28 대 29).
        svg.setAttribute("aria-hidden", "true");
        Element p = DomGlobal.document.createElementNS(NS, "path");
        p.setAttribute("d", d);
        p.setAttribute("fill", "none");
        p.setAttribute("stroke", "currentColor");
        p.setAttribute("stroke-width", "1.7");
        p.setAttribute("stroke-linecap", "round");
        p.setAttribute("stroke-linejoin", "round");
        svg.append(p);
        dress(svg);   // 그림판이 이미 와 있으면 지금 갈아입고, 아니면 이 획으로 산다
        return svg;
    }

    /** 이 콘솔이 그림 없이도 그려야 하는 것들 — 그 밖의 이름은 여기 없다(부르는 쪽이 제 낱자를 쓴다). */
    private static String path(String ref) {
        switch (ref == null ? "" : ref) {
            case "#i-sl-magnifying-glass": return "M10.5 4a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13M20 20l-4.6-4.6";
            case "#i-sl-arrows-rotate": return "M20 12a8 8 0 1 1-2.4-5.7M20 4.5V9h-4.5";
            // 도는 표. 이 표에 없으면 그림은 <b>스프라이트에서만</b> 오는데, 스프라이트는
            // 라이선스 아트가 빌드 때 있을 때만 구워진다(clients/web/server/gen_icons.go) —
            // 없으면 shape()가 null을 돌려주고 자리에 아무것도 안 선다. 그것을 실측으로 잡은
            // 자리가 회의의 「지금 하는 것」 판이다: 판이 있는 이유가 "느린 모델과 죽은 화면을
            // 구분한다"인데, 아트 없는 빌드에서는 그 구분이 통째로 사라지고 있었다.
            case "#i-sl-spinner-third": return "M20 12A8 8 0 1 1 12 4";
            case "#i-sl-sliders": return "M4 7h9M17 7h3M4 12h3M11 12h9M4 17h9M17 17h3M13 4.5v5M7 9.5v5M13 14.5v5";
            case "#i-sl-xmark": return "M6.5 6.5l11 11M17.5 6.5l-11 11";
            case "#i-sl-chevron-left": return "M14.5 5.5L8 12l6.5 6.5";
            case "#i-sl-chevron-right": return "M9.5 5.5L16 12l-6.5 6.5";
            case "#i-sl-chevron-down": return "M5.5 9.5L12 16l6.5-6.5";
            case "#i-sl-check": return "M5 12.5l4.5 4.5L19 7.5";
            case "#i-sl-pen-to-square": return "M4 20h4L18.5 9.5l-4-4L4 16zM14 6l4 4";
            case "#i-sl-floppy-disk": return "M5 5h11l3 3v11H5zM8.5 5v5h7V5M8.5 19v-5h7v5";
            case "#i-sl-share-from-square": return "M14 4h6v6M20 4l-8.5 8.5M18 14v5H5V6h5";
            case "#i-sl-trash-can": return "M5 7h14M10 7V4.5h4V7M7 7l1 13h8l1-13M11 10.5v6M13 10.5v6";
            case "#i-sl-copy": return "M9 4h8a2 2 0 0 1 2 2v8M6 8h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2z";
            case "#i-sl-paper-plane-top": return "M20 4L4 10.5l6.5 2.5L13 20z";
            case "#i-sl-play": return "M7 5l12 7-12 7z";
            case "#i-sl-clock-rotate-left": return "M12 7.5V12l3 2M20 12a8 8 0 1 1-3-6.2M20 4.5V9h-4.5";
            case "#i-sl-layer-group": return "M12 3.5l8 4-8 4-8-4zM4 12l8 4 8-4M4 16.5l8 4 8-4";
            case "#i-sl-flag-checkered": return "M5 3.5v17M5 5h13l-2.5 4L18 13H5";
            case "#i-sl-lightbulb": return "M9.5 17h5M10 20h4M12 3.5a5.5 5.5 0 0 0-3 10.1V17h6v-3.4A5.5 5.5 0 0 0 12 3.5z";
            case "#i-sl-reply": return "M9 6L4 11l5 5M4 11h8a7 7 0 0 1 7 7v1";
            case "#i-sl-plus": return "M12 5v14M5 12h14";
            case "#i-sl-file-lines": return "M6 3.5h8l4 4v13H6zM14 3.5v4h4M9 12.5h6M9 16h6";
            case "#i-sl-wand-magic-sparkles": return "M4 20L15 9M13.5 3.5l.9 1.9 1.9.9-1.9.9-.9 1.9-.9-1.9-1.9-.9 1.9-.9zM19 12l.7 1.5 1.5.7-1.5.7-.7 1.5-.7-1.5-1.5-.7 1.5-.7z";
            default: return null;
        }
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
