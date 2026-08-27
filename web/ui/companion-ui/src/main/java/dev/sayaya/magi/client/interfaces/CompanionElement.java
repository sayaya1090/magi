package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.client.domain.Rows;
import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.core.JsDate;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import elemental2.dom.HTMLFormElement;
import elemental2.dom.HTMLLinkElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.stateWord;
import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 컴패니언 화면(타입 1 = 코딩 에이전트)의 첫 조각: 사실 줄 · 전사 · 컴포저.
 *
 * 전사 행의 클래스(.row/.who/.txt, toolok…)는 기존 콘솔 page.js rowNode와 같은 계약 —
 * console.css 가 그대로 입힌다. 이 화면 자신의 것(사실 줄·컴포저 배치·턴 바)만
 * companion.css 가 말하고, 모듈이 로드될 때 스스로 <link>를 단다.
 *
 * 아직 아닌 것(대조표의 잔여): 접히는 사실판 전체·워크스페이스 판·툴 행 펼침(인자·출력)·
 * 마크다운·행 재사용 윈도우잉. 여기 없는 것은 없다고 그린다 — 반쯤 그리지 않는다.
 */
@Singleton
public class CompanionElement {
    private final CompanionStore store;
    private final HTMLElement root = el("section");
    private final HTMLElement turnwrap = el("div");
    private final HTMLElement turnfor = el("span");
    private final HTMLElement facts = el("div");
    private final HTMLElement log = el("div");
    private final HTMLFormElement form = Js.uncheckedCast(DomGlobal.document.createElement("form"));
    private final HTMLElement field = el("md-outlined-text-field");
    private FleetAgent[] roster = null;
    private String lastSig = null;
    private boolean wired = false;     // 재방문 마운트가 구독을 겹으로 쌓지 않게

    @Inject
    public CompanionElement(CompanionStore store) {
        this.store = store;
        root.id = "companion";
        // 턴바는 운영 콘솔의 그 마크업 그대로다 — #turnwrap[hidden] 속의 md-linear-progress
        // #turnbar(aria-hidden: 행이 이미 말로 말한다)와 경과 숫자 #turnfor. 스타일도 표시
        // 규칙도 console.css의 것이라, 여기는 hidden 토글과 1초 틱만 진다.
        turnwrap.id = "turnwrap";
        turnwrap.setAttribute("hidden", "");
        HTMLElement bar = el("md-linear-progress");
        bar.id = "turnbar";
        Js.asPropertyMap(bar).set("indeterminate", true);
        bar.setAttribute("aria-hidden", "true");
        turnfor.id = "turnfor";
        turnwrap.append(bar, turnfor);
        facts.className = "cfacts";
        log.id = "log";
        root.append(turnwrap, facts, log, composer());
    }

    public void mount(HTMLElement frame) {
        css();
        frame.replaceChildren(root);
        if (wired) return;   // 재방문: 캐시된 렌더가 다시 앉는 것 — 구독은 이미 흐른다
        wired = true;
        store.start();
        store.onContext(ctx -> { lastSig = null; paintFacts(ctx); });
        store.onRows(this::paintRows);
        store.onTurn(this::paintTurn);
        // 사실 줄은 명단에서 읽는다 — 셸이 호스팅하는 그 스트림, 요청 0 추가.
        RosterSharing.subscribe(list -> {
            if (list != null) roster = Js.uncheckedCast(list);
            paintFacts(store.context());
        });
    }

    // ── 턴바: 운영 page.js showTurnbar/paintTurnFor의 이식 ───────────────────
    private double turnFrom = 0;
    private double turnTick = -1;
    private boolean turnOpen = false;

    private void paintTurn(boolean open, double forSec) {
        turnOpen = open;
        turnFrom = JsDate.now() - forSec * 1000;
        if (open) turnwrap.removeAttribute("hidden");
        else turnwrap.setAttribute("hidden", "");
        if (turnTick >= 0) { DomGlobal.clearInterval(turnTick); turnTick = -1; }
        paintTurnFor();
        // 1초, 그리고 켜져 있는 동안만 — 숨은 요소를 상대로 도는 타이머는 탭의 수명만큼의
        // 웨이크업이다(운영의 그 규칙).
        if (open) turnTick = DomGlobal.setInterval(a -> paintTurnFor(), 1000);
    }

    private void paintTurnFor() {
        turnfor.textContent = turnOpen
                ? dur((int) Math.max(0, Math.round((JsDate.now() - turnFrom) / 1000))) : "";
    }

    /** s/m/h/d — 운영 dur()와 같은 축약: 단위는 언어를 타지 않는다. */
    private static String dur(int s) {
        if (s < 60) return s + "s";
        if (s < 3600) return Math.round(s / 60f) + "m";
        if (s < 86400) return Math.round(s / 3600f) + "h";
        return Math.round(s / 86400f) + "d";
    }

    /** 이 화면 자신의 스타일시트 — 셸이 아는 파일이 아니라 이 모듈의 것이라 스스로 단다. */
    private void css() {
        if (DomGlobal.document.getElementById("companionCss") != null) return;
        HTMLLinkElement link = Js.uncheckedCast(DomGlobal.document.createElement("link"));
        link.id = "companionCss";
        link.rel = "stylesheet";
        link.href = "/ui/companion.css";
        DomGlobal.document.head.append(link);
    }

    private HTMLElement composer() {
        form.className = "composer";
        // 운영 컴포저의 그 계약: .composer 안의 md-outlined-text-field#t — 구분선·간격·
        // flex 전부 console.css의 것이다.
        field.id = "t";
        field.setAttribute("type", "textarea");
        field.setAttribute("rows", "1");
        HTMLElement send = el("md-filled-button");
        send.setAttribute("type", "submit");
        send.id = "send";
        send.textContent = tr("action.send");
        form.append(field, send);
        form.addEventListener("submit", evt -> {
            evt.preventDefault();
            String v = value().trim();
            if (v.isEmpty()) return;
            // 비우고, 거부되면 되돌린다 — 타이핑을 잃는 쪽이 늘 더 나쁘다(기존 콘솔 규칙).
            value("");
            store.submit(v, why -> {
                if (why != null && !why.isEmpty() && value().trim().isEmpty()) value(v);
            });
        });
        return form;
    }

    private String value() {
        Object v = Js.asPropertyMap(field).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private void value(String v) { Js.asPropertyMap(field).set("value", v); }

    private void paintFacts(CompanionContext ctx) {
        facts.replaceChildren();
        if (ctx == null) return;
        FleetAgent a = rowOf(ctx.socket);
        HTMLElement name = el("span");
        name.className = "cname";
        name.textContent = a != null ? a.name : ctx.socket;
        // 이 모듈이 곧 타입의 화면이다 — 코딩 에이전트라는 이름은 여기 것.
        HTMLElement type = el("span");
        type.className = "ctype";
        type.textContent = tr("type.coding");
        facts.append(name, type);
        if (a != null) {
            HTMLElement word = el("span");
            word.className = "cword " + (a.state == null ? "" : a.state);
            word.textContent = stateWord(a.state);
            facts.append(word);
            if (a.role != null && !a.role.isEmpty()) facts.append(chip("crole", a.role));
            if (a.model != null && !a.model.isEmpty()) facts.append(chip("cmodel", a.model));
            if (a.session != null && !a.session.isEmpty()) facts.append(chip("csession", a.session));
        }
    }

    private static HTMLElement chip(String cls, String text) {
        HTMLElement c = el("span");
        c.className = cls;
        c.textContent = text;
        return c;
    }

    private FleetAgent rowOf(String socket) {
        if (roster == null || socket == null) return null;
        for (FleetAgent a : roster) if (socket.equals(a.socket)) return a;
        return null;
    }

    private void paintRows(Object rowsOrNull) {
        if (rowsOrNull == null) {
            // 아직 모른다 — 이전 컴패니언의 대화가 새 화면에 비치면 안 된다.
            lastSig = null;
            log.replaceChildren();
            return;
        }
        JsArrayLike<Object> rows = Js.uncheckedCast(rowsOrNull);
        String sig = rows.getLength() + "|" + rowSig(rows.getLength() == 0 ? null : rows.getAt(rows.getLength() - 1));
        if (sig.equals(lastSig)) return;
        lastSig = sig;
        boolean stick = atBottom();
        // 첫 조각은 통째 다시 그린다 — 재사용 윈도우잉은 잔여(원본 draw()의 몫).
        log.replaceChildren();
        for (int i = 0; i < rows.getLength(); i++) log.append(rowNode(Js.uncheckedCast(rows.getAt(i))));
        if (stick) toBottom();
    }

    private HTMLElement rowNode(JsPropertyMap<Object> r) {
        String who = str(r, "who");
        boolean hasOk = r.has("ok") && r.get("ok") != null;
        boolean ok = hasOk && Js.isTruthy(r.get("ok"));
        boolean pending = Js.isTruthy(r.get("pending"));
        HTMLElement d = el("div");
        d.className = Rows.rowClass(who, hasOk, ok,
                Js.isTruthy(r.get("note")), pending, Js.isTruthy(r.get("abandoned")));
        HTMLElement w = el("div");
        w.className = "who";
        w.textContent = who;
        String at = str(r, "at");
        if (!at.isEmpty()) {
            HTMLElement when = el("div");
            when.className = "when";
            when.textContent = hhmm(at);
            w.append(when);
        }
        if (Rows.folded(who)) {
            d.append(w, foldNode(r, who, hasOk, ok, pending));
            return d;
        }
        if (pending) w.append(tag("row.working"));
        if (Js.isTruthy(r.get("abandoned"))) w.append(tag("row.abandoned"));
        HTMLElement t = el("div");
        t.className = "txt";
        t.textContent = str(r, "text");
        d.append(w, t);
        return d;
    }

    /**
     * 접힌 행 — 원본 rowNode 의 details.txt.fold 계약: 요약(마크+한 줄)이 닫혀 있어도 결말을
     * 말하고, 속은 fold.asked/fold.answered(디프면 fold.changed) 블록이다. 실패·주석은 열려서
     * 도착한다 — 읽으라고 온 행이라서. kind별 열림 선호는 localStorage, 프로그램 토글의
     * 메아리는 쓰지 않는다(원본에서 실측된 그 결함).
     */
    private HTMLElement foldNode(JsPropertyMap<Object> r, String who, boolean hasOk, boolean ok, boolean pending) {
        HTMLElement det = el("details");
        det.className = "txt fold";
        det.setAttribute("data-kind", who);
        boolean openNow = "failed".equals(who) || (hasOk && !ok) || "open".equals(stored("fold." + who));
        if (openNow) det.setAttribute("open", "");
        final boolean[] userToggle = {false};
        det.addEventListener("toggle", evt -> {
            if (!userToggle[0]) return;
            store("fold." + who, det.hasAttribute("open") ? "open" : "shut");
        });
        DomGlobal.setTimeout(a -> userToggle[0] = true, 0);

        HTMLElement head = el("summary");
        HTMLElement mk = mark(who, hasOk, ok, Js.isTruthy(r.get("note")));
        if (mk != null) head.append(mk, DomGlobal.document.createTextNode(" "));
        head.append(DomGlobal.document.createTextNode(summaryLine(r, who, hasOk)));
        det.append(head);

        HTMLElement body = el("div");
        body.className = "foldbody";
        String args = str(r, "args");
        String out = str(r, "out");
        String diff = str(r, "diff");
        if ("tool".equals(who) || "result".equals(who) || "failed".equals(who) || "shell".equals(who)) {
            int blocks = 0;
            if (!diff.isEmpty()) blocks = (pathOf(args).isEmpty() ? 0 : 1) + 1;
            else blocks = (args.isEmpty() ? 0 : 1) + (out.isEmpty() ? 0 : 1);
            if (!diff.isEmpty()) {
                String path = pathOf(args);
                if (!path.isEmpty()) { if (blocks > 1) body.append(foldKey("fold.asked")); body.append(pre(path, false)); }
                if (blocks > 1) body.append(foldKey("fold.changed"));
                body.append(diffPre(diff));
            } else if (!args.isEmpty() || !out.isEmpty()) {
                if (!args.isEmpty()) {
                    if (blocks > 1) body.append(foldKey("fold.asked"));
                    body.append(pre(args, Rows.looksLikeDiff(args)));
                }
                if (!out.isEmpty()) {
                    if (blocks > 1) body.append(foldKey("fold.answered"));
                    body.append(pre(out, Rows.looksLikeDiff(out)));
                }
            } else {
                body.append(pre(str(r, "text"), Rows.looksLikeDiff(str(r, "text"))));
            }
        } else {
            // thinking·council: 요약이 첫 줄을 이미 말했으니 속은 전체 본문이다.
            HTMLElement t = el("div");
            t.textContent = str(r, "text");
            body.append(t);
        }
        det.append(body);
        if (pending) {
            HTMLElement bar = el("md-linear-progress");
            Js.asPropertyMap(bar).set("indeterminate", true);
            bar.className = "runbar";
            bar.setAttribute("aria-label", tr("row.working"));
            det.append(bar);
        }
        return det;
    }

    /** 요약 마크 — 어떻게 끝났나. 스프라이트가 없는 페이지라 원본의 폴백 글리프를 그대로 쓴다. */
    private static HTMLElement mark(String who, boolean hasOk, boolean ok, boolean note) {
        String glyph = null, cls = null;
        if ("tool".equals(who)) {
            if (!hasOk) { glyph = "\u2699"; cls = "spin"; }
            else if (ok) { glyph = "\u2713"; cls = "ok"; }
            else if (note) { glyph = "\u26A0"; cls = "note"; }
            else { glyph = "\u2717"; cls = "bad"; }
        } else if ("result".equals(who)) { glyph = "\u2713"; cls = "ok"; }
        else if ("failed".equals(who)) { glyph = "\u2717"; cls = "bad"; }
        if (glyph == null) return null;
        HTMLElement m = el("span");
        m.className = "mk " + cls;
        m.setAttribute("aria-hidden", "true");
        m.textContent = glyph;
        return m;
    }

    /** 접힌 행의 한 줄 — 열지 않고도 판단할 수 있어야 한다(원본 summaryFor의 이식). */
    private String summaryLine(JsPropertyMap<Object> r, String who, boolean hasOk) {
        String text = str(r, "text");
        if ("tool".equals(who)) {
            String args = str(r, "args");
            String asked = !str(r, "diff").isEmpty() ? pathOf(args) : Rows.oneLine(args, 60);
            String said = hasOk ? Rows.firstLine(decodeToolText(str(r, "out")), 44) : "";
            return str(r, "tool") + (asked.isEmpty() ? "" : " " + asked)
                    + (said.isEmpty() ? "" : "  \u27F6 " + said);
        }
        if ("council".equals(who)) return text.split("\n")[0];
        if ("shell".equals(who)) return "! " + text;
        if ("thinking".equals(who)) return tr("row.reasoning") + " \u00B7 " + Rows.oneLine(text, 80);
        return Rows.oneLine(text, 88);
    }

    private static HTMLElement foldKey(String key) {
        HTMLElement k = el("div");
        k.className = "foldk";
        k.textContent = tr(key);
        return k;
    }

    /** 본문 블록 — 디프면 줄마다 클래스, 아니면 디코드된 텍스트 한 덩어리(pre). */
    private static HTMLElement pre(String raw, boolean asDiff) {
        if (asDiff) return diffPre(raw);
        HTMLElement pre = el("pre");
        pre.textContent = decodeToolText(raw);
        return pre;
    }

    private static HTMLElement diffPre(String text) {
        HTMLElement pre = el("pre");
        pre.className = "diff";
        String body = text == null ? "" : text;
        if (body.endsWith("\n")) body = body.substring(0, body.length() - 1);
        for (String line : body.split("\n", -1)) {
            HTMLElement row = el("span");
            row.className = Rows.diffLineClass(line);
            row.textContent = line + "\n";
            pre.append(row);
        }
        return pre;
    }

    /** 결과의 JSON 인코딩을 그것이 뜻하는 텍스트로 — 원본 decodeToolText의 이식. */
    private static String decodeToolText(String text) {
        if (text == null) return "";
        String trimmed = text.trim();
        if (trimmed.isEmpty() || (trimmed.charAt(0) != '"' && trimmed.charAt(0) != '[')) return text;
        try {
            Object v = elemental2.core.Global.JSON.parse(trimmed);
            if (v instanceof String) return (String) v;
            if (elemental2.core.JsArray.isArray(v)) {
                elemental2.core.JsArray<Object> arr = Js.uncheckedCast(v);
                StringBuilder b = new StringBuilder();
                for (int i = 0; i < arr.length; i++) {
                    if (i > 0) b.append('\n');
                    Object x = arr.getAt(i);
                    b.append(x == null || !"object".equals(Js.typeof(x))
                            ? String.valueOf(x) : elemental2.core.Global.JSON.stringify(x));
                }
                return b.toString();
            }
        } catch (Exception ignore) { /* JSON이 아니면 온 그대로 */ }
        return text;
    }

    /** 호출이 대는 파일 — 디프 위에 놓을 그 경로(원본 pathOf). */
    private static String pathOf(String args) {
        try {
            Object v = elemental2.core.Global.JSON.parse(args == null || args.isEmpty() ? "{}" : args);
            Object path = Js.asPropertyMap(v).get("path");
            return path instanceof String ? (String) path : "";
        } catch (Exception e) { return ""; }
    }

    private static HTMLElement tag(String key) {
        HTMLElement t = el("span");
        t.className = "pendtag";
        t.textContent = " \u00B7 " + tr(key);
        return t;
    }

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
        } catch (Exception ignore) { /* storage can be denied */ }
    }

    private static String rowSig(Object row) {
        if (row == null) return "";
        JsPropertyMap<Object> r = Js.uncheckedCast(row);
        return str(r, "who") + str(r, "text") + str(r, "tool") + r.get("ok") + r.get("pending")
                + str(r, "out").length() + str(r, "args").length();
    }

    private static String str(JsPropertyMap<Object> r, String key) {
        Object v = r.get(key);
        return v == null ? "" : String.valueOf(v);
    }

    private static String hhmm(String rfc3339) {
        JsDate d = new JsDate(rfc3339);
        double h = d.getHours(), m = d.getMinutes();
        if (Double.isNaN(h)) return "";
        return (h < 10 ? "0" : "") + (int) h + ":" + (m < 10 ? "0" : "") + (int) m;
    }

    private static boolean atBottom() {
        Element s = DomGlobal.document.scrollingElement;
        return s == null || s.scrollHeight - s.scrollTop - clientHeight(s) < 80;
    }

    private static void toBottom() {
        Element s = DomGlobal.document.scrollingElement;
        if (s != null) s.scrollTop = s.scrollHeight;
    }

    private static double clientHeight(Element e) { return Js.coerceToDouble(Js.asPropertyMap(e).get("clientHeight")); }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
