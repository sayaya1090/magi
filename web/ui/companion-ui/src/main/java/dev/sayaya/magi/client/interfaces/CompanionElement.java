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
    private final HTMLElement turnbar = el("div");
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
        turnbar.id = "turnbar";
        turnbar.setAttribute("aria-hidden", "true");
        facts.className = "cfacts";
        log.id = "log";
        root.append(turnbar, facts, log, composer());
    }

    public void mount(HTMLElement frame) {
        css();
        frame.replaceChildren(root);
        if (wired) return;   // 재방문: 캐시된 렌더가 다시 앉는 것 — 구독은 이미 흐른다
        wired = true;
        store.start();
        store.onContext(ctx -> { lastSig = null; paintFacts(ctx); });
        store.onRows(this::paintRows);
        store.onTurn(on -> turnbar.className = on ? "on" : "");
        // 사실 줄은 명단에서 읽는다 — 셸이 호스팅하는 그 스트림, 요청 0 추가.
        RosterSharing.subscribe(list -> {
            if (list != null) roster = Js.uncheckedCast(list);
            paintFacts(store.context());
        });
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
        field.id = "say";
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
        HTMLElement d = el("div");
        d.className = Rows.rowClass(who, hasOk, hasOk && Js.isTruthy(r.get("ok")),
                Js.isTruthy(r.get("note")), Js.isTruthy(r.get("pending")), Js.isTruthy(r.get("abandoned")));
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
        HTMLElement t = el("div");
        t.className = "txt";
        if ("tool".equals(who)) {
            // 접힌 요약 한 줄 — 인자·출력 펼침은 잔여. 무엇이 얼마나 잘렸는지는 클래스가 말한다.
            t.textContent = str(r, "tool") + "  " + Rows.oneLine(str(r, "args"), 120);
        } else {
            t.textContent = str(r, "text");
        }
        d.append(w, t);
        return d;
    }

    private static String rowSig(Object row) {
        if (row == null) return "";
        JsPropertyMap<Object> r = Js.uncheckedCast(row);
        return str(r, "who") + str(r, "text") + str(r, "tool") + r.get("ok") + r.get("pending");
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
