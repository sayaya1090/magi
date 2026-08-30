package dev.sayaya.magi.bridge;

import elemental2.core.JsArray;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.Node;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

/**
 * 마크다운 — <b>토큰이 들어오고 노드가 나간다</b>.
 *
 * 터미널은 처음부터 마크다운을 그렸고 이 페이지는 원문을 보였다. 표는 파이프의 벽으로,
 * 펜스 블록은 백틱 셋과 붙어 버린 본문으로 도착했다. 읽으라고 쓴 것이 유일하게 못 읽는
 * 것이었다.
 *
 * 렉서는 옛 콘솔이 쓰는 그 번들이다(`/vendor/marked.js`, console.html이 전역 `mdlex`로
 * 올린다) — 두 콘솔이 같은 바이트를 읽어야 같은 것을 그린다. <b>렉서만</b>이고 렌더러는
 * 안 쓴다: 여기서 만드는 노드는 전부 createElement + textContent라 HTML 문자열이 한 번도
 * 생기지 않으므로, 새니타이저가 옳거나 그를 자리 자체가 없다. 위험이 닿는 유일한 곳은
 * 마크다운의 raw-HTML 토큰인데, 그건 <b>글자로</b> 그린다(운영과 같은 판단, 테스트 있음).
 */
public final class Markdown {
    private Markdown() {}

    /** 렉서가 실려 있나 — 없으면 원문 그대로 그린다(옛 콘솔의 폴백과 같은 자리). */
    public static boolean ready() {
        return Js.isTruthy(Js.asPropertyMap(DomGlobal.window).get("mdlex"));
    }

    /**
     * node를 마크다운으로 채운다. 렉서가 던지면 <b>원문</b>을 글자로 넣는다 — 이해 못 하는
     * 토큰 흐름이 형식을 잃게 하되 내용을 잃게 하지는 않는다(운영 md()의 그 폴백).
     */
    public static HTMLElement into(HTMLElement node, String text) {
        String src = text == null ? "" : text;
        JsArray<Object> toks = lex(src);
        if (toks == null) {
            node.textContent = src;
            return node;
        }
        blocks(node, toks);
        return node;
    }

    private static JsArray<Object> lex(String text) {
        if (!ready()) return null;
        try {
            return Js.uncheckedCast(callLexer(text));
        } catch (Throwable t) {
            return null;
        }
    }

    private static native Object callLexer(String text) /*-{
        return $wnd.mdlex(text || '');
    }-*/;

    // ── 블록 ────────────────────────────────────────────────────────────────
    private static void blocks(Element parent, JsArray<Object> toks) {
        for (int i = 0; toks != null && i < toks.length; i++) {
            JsPropertyMap<Object> t = Js.uncheckedCast(toks.getAt(i));
            String type = str(t, "type");
            switch (type) {
                case "heading": {
                    // h3..h6으로 눌러 둔다: 페이지에 제 제목 순서가 있고, 전사가 제가 앉은
                    // 절보다 높은 단계를 열어서는 안 된다.
                    int depth = (int) num(t, "depth", 1) + 2;
                    Element n = el("h" + Math.min(6, Math.max(3, depth)));
                    inline(n, kids(t));
                    parent.appendChild(n);
                    break;
                }
                case "paragraph": {
                    Element n = el("p");
                    inline(n, kids(t));
                    parent.appendChild(n);
                    break;
                }
                case "text": {
                    Element n = el("p");
                    if (kids(t) != null) inline(n, kids(t));
                    else n.textContent = str(t, "text");
                    parent.appendChild(n);
                    break;
                }
                case "code": {
                    String lang = str(t, "lang");
                    if (!lang.isEmpty()) lang = lang.split("\\s+")[0];
                    String body = str(t, "text");
                    Element pre = el("pre");
                    if ("diff".equals(lang) || "patch".equals(lang) || looksLikeDiff(body)) {
                        pre.setAttribute("class", "diff");
                        diffInto(pre, body);
                    } else {
                        Element code = el("code");
                        code.textContent = body;
                        if (!lang.isEmpty()) code.setAttribute("data-lang", lang);
                        pre.appendChild(code);
                    }
                    parent.appendChild(pre);
                    break;
                }
                case "blockquote": {
                    Element n = el("blockquote");
                    blocks(n, kids(t));
                    parent.appendChild(n);
                    break;
                }
                case "hr":
                    parent.appendChild(el("hr"));
                    break;
                case "list": {
                    boolean ordered = Js.isTruthy(t.get("ordered"));
                    Element list = el(ordered ? "ol" : "ul");
                    double start = num(t, "start", 1);
                    if (ordered && start != 1) list.setAttribute("start", String.valueOf((int) start));
                    JsArray<Object> items = Js.uncheckedCast(t.get("items"));
                    for (int k = 0; items != null && k < items.length; k++) {
                        JsPropertyMap<Object> item = Js.uncheckedCast(items.getAt(k));
                        Element li = el("li");
                        if (Js.isTruthy(item.get("task"))) {
                            // 그려질 뿐 누를 수는 없다 — 전사가 기록한 것을 여기서 바꿀 수 있는
                            // 것은 없다.
                            Element box = el("input");
                            box.setAttribute("type", "checkbox");
                            if (Js.isTruthy(item.get("checked"))) box.setAttribute("checked", "");
                            box.setAttribute("disabled", "");
                            li.appendChild(box);
                            li.appendChild(DomGlobal.document.createTextNode(" "));
                        }
                        blocks(li, kids(item));
                        list.appendChild(li);
                    }
                    parent.appendChild(list);
                    break;
                }
                case "table": {
                    Element wrap = el("div");
                    wrap.setAttribute("class", "tablewrap");
                    Element table = el("table"), thead = el("thead"), head = el("tr");
                    JsArray<Object> align = Js.uncheckedCast(t.get("align"));
                    JsArray<Object> header = Js.uncheckedCast(t.get("header"));
                    for (int c = 0; header != null && c < header.length; c++) {
                        JsPropertyMap<Object> cell = Js.uncheckedCast(header.getAt(c));
                        Element th = el("th");
                        inline(th, kids(cell));
                        alignTo(th, align, c);
                        head.appendChild(th);
                    }
                    thead.appendChild(head);
                    table.appendChild(thead);
                    Element body = el("tbody");
                    JsArray<Object> rows = Js.uncheckedCast(t.get("rows"));
                    for (int r = 0; rows != null && r < rows.length; r++) {
                        JsArray<Object> cells = Js.uncheckedCast(rows.getAt(r));
                        Element tr = el("tr");
                        for (int c = 0; cells != null && c < cells.length; c++) {
                            JsPropertyMap<Object> cell = Js.uncheckedCast(cells.getAt(c));
                            Element td = el("td");
                            inline(td, kids(cell));
                            alignTo(td, align, c);
                            tr.appendChild(td);
                        }
                        body.appendChild(tr);
                    }
                    table.appendChild(body);
                    wrap.appendChild(table);
                    parent.appendChild(wrap);
                    break;
                }
                case "space":
                    break;
                case "html":
                    // 원문의 raw HTML은 <b>그것이 무엇인지</b>로 보인다. 마크업으로 가득한 도구
                    // 결과가 마크업이 되지 않게 하는 줄이 이것이다.
                    parent.appendChild(text("p", raw(t)));
                    break;
                default:
                    parent.appendChild(text("p", raw(t)));
            }
        }
    }

    // ── 인라인 ──────────────────────────────────────────────────────────────
    private static void inline(Element parent, JsArray<Object> toks) {
        for (int i = 0; toks != null && i < toks.length; i++) {
            JsPropertyMap<Object> t = Js.uncheckedCast(toks.getAt(i));
            switch (str(t, "type")) {
                case "strong": {
                    Element n = el("strong");
                    inline(n, kids(t));
                    parent.appendChild(n);
                    break;
                }
                case "em": {
                    Element n = el("em");
                    inline(n, kids(t));
                    parent.appendChild(n);
                    break;
                }
                case "del": {
                    Element n = el("del");
                    inline(n, kids(t));
                    parent.appendChild(n);
                    break;
                }
                case "codespan":
                    parent.appendChild(text("code", str(t, "text")));
                    break;
                case "br":
                    parent.appendChild(el("br"));
                    break;
                case "link": {
                    // href는 믿는 게 아니라 검사한다. 전사는 javascript:·data: 주소를 실어 나를
                    // 수 있고, 그것을 실행할 수 있는 노드는 앵커 하나뿐이다.
                    Element a = el("a");
                    inline(a, kids(t));
                    String href = str(t, "href");
                    String low = href.toLowerCase();
                    if (low.startsWith("http://") || low.startsWith("https://") || low.startsWith("mailto:")) {
                        a.setAttribute("href", href);
                        a.setAttribute("target", "_blank");
                        a.setAttribute("rel", "noopener noreferrer");
                    }
                    parent.appendChild(a);
                    break;
                }
                case "html":
                default:
                    parent.appendChild(DomGlobal.document.createTextNode(raw(t)));
            }
        }
    }

    // ── 디프 ────────────────────────────────────────────────────────────────
    /** 유니파이드 디프인가 — 훵크 머리가 있어야 한다(운영 hunkHeader의 그 판정). */
    public static boolean looksLikeDiff(String text) {
        if (text == null) return false;
        for (String line : text.split("\n")) {
            if (line.startsWith("@@ -") && line.contains(" @@")) return true;
        }
        return false;
    }

    /** 디프 한 줄이 입는 클래스 — 운영 diffInto의 그 다섯. */
    public static String diffLineClass(String line) {
        if (line == null) return "dctx";
        if (line.startsWith("+++") || line.startsWith("---") || line.startsWith("diff ")) return "dfile";
        if (line.startsWith("@@")) return "dhunk";
        if (line.startsWith("+")) return "dadd";
        if (line.startsWith("-")) return "ddel";
        return "dctx";
    }

    private static void diffInto(Element pre, String body) {
        for (String line : (body == null ? "" : body).split("\n", -1)) {
            Element span = el("span");
            span.setAttribute("class", diffLineClass(line));
            span.textContent = line + "\n";
            pre.appendChild(span);
        }
    }

    // ── 잔손 ────────────────────────────────────────────────────────────────
    private static Element el(String tag) { return DomGlobal.document.createElement(tag); }

    private static Node text(String tag, String s) {
        Element e = el(tag);
        e.textContent = s;
        return e;
    }

    private static void alignTo(Element cell, JsArray<Object> align, int at) {
        if (align == null || at >= align.length) return;
        Object a = align.getAt(at);
        if (a == null) return;
        String word = String.valueOf(a);
        if (!word.isEmpty() && !"null".equals(word)) cell.setAttribute("style", "text-align:" + word);
    }

    private static JsArray<Object> kids(JsPropertyMap<Object> t) {
        Object v = t.get("tokens");
        return v == null ? null : Js.uncheckedCast(v);
    }

    private static String str(JsPropertyMap<Object> t, String k) {
        Object v = t.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static double num(JsPropertyMap<Object> t, String k, double dflt) {
        Object v = t.get(k);
        return v == null ? dflt : Js.coerceToDouble(v);
    }

    /** raw가 있으면 raw, 없으면 text — 모르는 토큰도 <b>내용은</b> 잃지 않는다. */
    private static String raw(JsPropertyMap<Object> t) {
        Object v = t.get("raw");
        return v != null ? String.valueOf(v) : str(t, "text");
    }
}
