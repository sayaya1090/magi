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
        tryEach(new String[]{"/", "../"}, 0, done);
    }

    private static void tryEach(String[] paths, int at, Runnable done) {
        if (DomGlobal.document.getElementById(SPRITE) != null || at >= paths.length) { done.run(); return; }
        borrowFrom(paths[at], () -> tryEach(paths, at + 1, done));
    }

    private static void borrowFrom(String pagePath, Runnable done) {
        DomGlobal.fetch(pagePath)
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
