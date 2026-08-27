package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.client.usecase.CompanionStore;
import elemental2.core.JsDate;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

import static dev.sayaya.magi.bridge.Labels.stateWord;
import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 사실판 — 운영 drawDetail의 읽기 반쪽 이식: md-outlined-card#detail, 접는 바(상태·워크스페이스
 * 요약, 접힘 기억은 localStorage 'facts', 기본은 창이 1200 미만이면 접힘 — 누른 것만 선호다),
 * 질문이 오는 순서의 필드 그리드(상태·짐, 스텝, 마지막 활동, 역할, 팀, 호스트, 빌드,
 * 워크스페이스, 세션, 결재, 모델, 캐시, 컨텍스트 창+지금 접기, 접혀 나간 것).
 *
 * 잔여(대조표): 결재/모델/세션은 읽기 — 운영의 메뉴 컨트롤은 데몬 질의와 함께 온다.
 * 도구/루프/보고 서식 문, 자체 빌드 업데이트 컨트롤도 그 편이다.
 */
@Singleton
public class DetailElement {
    private final CompanionStore store;
    private final HTMLElement card = el("md-outlined-card");
    private final HTMLElement bar = el("button");
    private final HTMLElement sum = el("div");
    private final HTMLElement wrap = el("div");
    private final HTMLElement grid = el("div");
    private FleetAgent a = null;
    private CompanionContext ctx = null;
    private Object info = null;

    @Inject
    public DetailElement(CompanionStore store) {
        this.store = store;
        card.id = "detail";
        card.setAttribute("hidden", "");
        bar.setAttribute("type", "button");
        bar.className = "foldbar hit48";
        // 스프라이트의 셰브런을 쓴다 — 없는 빌드에서만 늘 그리던 글자다(운영 iconOr). 여기서
        // 글자를 박아 두면, 그림이 있는 빌드에서도 이 판의 머리만 글자를 이고 선다(실측: 이
        // 카드의 캐럿만 span, 나머지 110개는 svg). 시트가 90° 돌리는 것은 .caret 쪽이라 어느
        // 쪽이든 한 요소이면 된다.
        elemental2.dom.Element caret = dev.sayaya.magi.bridge.Icons.orGlyph("#i-sl-chevron-down", "▾", "caret");
        caret.setAttribute("aria-hidden", "true");
        sum.className = "sum";
        bar.append(caret, cell("k", tr("field.facts")), sum);
        bar.addEventListener("click", evt -> fold(!card.hasAttribute("folded"), true));
        wrap.className = "foldwrap";
        grid.className = "grid";
        wrap.append(grid);
        card.append(bar, wrap);
        store.onContext(c -> { ctx = c; render(); });
        store.onRoster(list -> { a = rowOf(list); render(); });
        store.onContextInfo(i -> { info = i; render(); });
        // 접힘의 기본은 창이 정한다 — 누른 적 있는 독자만 기억된다(운영 규칙).
        String said = stored("facts");
        fold(said == null ? DomGlobal.window.innerWidth < 1200 : "folded".equals(said), false);
    }

    public HTMLElement element() { return card; }

    private FleetAgent rowOf(Object list) {
        if (list == null || ctx == null) return a;
        JsArrayLike<Object> rows = Js.uncheckedCast(list);
        for (int i = 0; i < rows.getLength(); i++) {
            FleetAgent r = Js.uncheckedCast(rows.getAt(i));
            if (ctx.socket.equals(r.socket)) return r;
        }
        return null;
    }

    private void fold(boolean want, boolean chosen) {
        if (want) card.setAttribute("folded", ""); else card.removeAttribute("folded");
        bar.setAttribute("aria-expanded", want ? "false" : "true");
        if (chosen) store(want ? "folded" : "open");
    }

    private void render() {
        if (ctx == null || a == null) { card.setAttribute("hidden", ""); return; }
        card.removeAttribute("hidden");
        sum.textContent = stateWord(a.state) + " · " + (a.workdir == null ? "" : a.workdir);
        sum.setAttribute("title", sum.textContent);
        grid.replaceChildren();
        // 질문이 오는 순서 — 이 목록이 곧 배치다(그리드는 DOM 순서로 짠다, 운영 규칙).
        String load = carrying(a);
        if (!load.isEmpty()) {
            grid.append(field("field.status", stateWord(a.state) + " · " + load, "state " + a.state));
        }
        grid.append(field("field.steps", a.steps > 0 ? String.valueOf(a.steps) : "—", null));
        grid.append(field("field.last_activity", a.idle >= 0 ? tr("time.ago", "d", dur(a.idle)) : "—", null));
        if (a.role != null && !a.role.isEmpty()) grid.append(wide(field("field.role", a.role, null)));
        if (a.team != null && !a.team.isEmpty()) {
            grid.append(field("field.team", a.team + (a.hub ? " · " + tr("team.speaks") : ""), null));
        }
        String host = (a.instance != null && !a.instance.isEmpty() ? a.instance : a.host)
                + (a.addr != null && !a.addr.isEmpty() ? " · " + a.addr : "")
                + (a.pid > 0 ? " · pid " + a.pid : "");
        grid.append(field("field.host", host, null));
        if (a.version != null && !a.version.isEmpty()) grid.append(field("field.version", a.version, null));
        grid.append(wide(field("field.workspace", a.workdir, null)));
        if (a.session != null && !a.session.isEmpty()) grid.append(wide(field("field.session", a.session, null)));
        if (a.permission != null && !a.permission.isEmpty()) {
            grid.append(field("field.permission", tr("perm." + a.permission), null));
        }
        contextRows();
    }

    /** 이 화면의 요점 — 어느 모델의 창이 얼마나 찼고, 무엇이 접혀 나갔나(운영 drawContext의 읽기). */
    private void contextRows() {
        JsPropertyMap<Object> c = info == null ? null : Js.uncheckedCast(info);
        String model = c != null && !str(c, "model").isEmpty() ? str(c, "model")
                : a.model == null ? "" : a.model;
        if (!model.isEmpty()) grid.append(field("field.model", model, null));
        if (c == null) return;
        boolean estimated = Js.isTruthy(c.get("estimated"));
        boolean cacheReported = Js.isTruthy(c.get("cacheReported"));
        if (!cacheReported && !estimated) {
            grid.append(field("field.cache", tr("context.no_cache_report"), null));
        }
        double used = num(c, "used"), window = num(c, "window");
        HTMLElement size = cell("v", (estimated ? "~" : "") + fmt(used)
                + (window > 0 ? " / " + fmt(window) : "") + " tokens");
        HTMLElement note = el("small");
        String words = " " + tr(estimated ? "context.estimated" : "context.measured");
        if (num(c, "messages") > 0) words += " · " + tr("context.messages", "n", fmt(num(c, "messages")));
        if (cacheReported && used > 0) {
            words += " · " + tr("context.cached_share", "pct",
                    String.valueOf(Math.round(num(c, "cached") * 100 / used)));
        }
        note.textContent = words;
        size.append(note);
        HTMLElement f = cell("f", null);
        f.setAttribute("data-k", "field.context");
        f.append(cell("k", tr("field.context")), size);
        // 창을 아는 때만 바를 그린다 — 빈 트랙은 "거의 비었다"로 읽힌다(운영 규칙).
        if (window > 0) {
            int pct = (int) Math.min(100, Math.round(used * 100 / window));
            HTMLElement bar2 = cell("bar" + (pct >= 80 ? " tight" : ""), null);
            HTMLElement fill = el("i");
            fill.style.width = elemental2.dom.CSSProperties.WidthUnionType.of(pct + "%");
            bar2.append(fill);
            f.append(bar2);
        }
        // 레버는 읽기 곁에 — 지금, 턴 사이에 접고 싶은 사람의 것(운영 규칙).
        HTMLElement fold = el("md-text-button");
        fold.className = "fold";
        fold.textContent = tr("action.compact_now");
        fold.addEventListener("click", evt -> {
            fold.setAttribute("disabled", "");
            store.compact(() -> fold.removeAttribute("disabled"));
        });
        f.append(fold);
        grid.append(f);
        double folds = num(c, "compactions");
        if (folds > 0) {
            HTMLElement v = cell("v", folds == 1 ? tr("context.fold")
                    : tr("context.folds", "n", String.valueOf((int) folds)));
            HTMLElement s2 = el("small");
            String tail = " · " + tr("context.shed", "n", fmt(num(c, "shed")));
            if (num(c, "lastBefore") > 0) {
                tail += " · " + tr("context.last_run", "before", fmt(num(c, "lastBefore")),
                        "after", fmt(num(c, "lastAfter")));
            }
            String at = hhmm(str(c, "lastAt"));
            if (!at.isEmpty()) tail += " · " + tr("context.at", "time", at);
            s2.textContent = tail;
            v.append(s2);
            HTMLElement cf = cell("f", null);
            cf.setAttribute("data-k", "field.summarised_away");
            cf.append(cell("k", tr("field.summarised_away")), v);
            grid.append(cf);
        }
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    private static String carrying(FleetAgent a) {
        List<String> parts = new ArrayList<>();
        if (a.handling) parts.add(tr("load.in_hand"));
        if (a.waiting > 0) parts.add(tr("load.waiting", "n", String.valueOf(a.waiting)));
        return String.join(", ", parts);
    }

    private static HTMLElement field(String key, String v, String cls) {
        HTMLElement f = cell("f", null);
        f.setAttribute("data-k", key);
        f.append(cell("k", tr(key)), cell("v" + (cls == null ? "" : " " + cls), v));
        return f;
    }

    private static HTMLElement wide(HTMLElement f) {
        f.className = "f wide";
        return f;
    }

    private static String fmt(double n) {
        // toLocaleString의 자리 — GWT엔 없어 손으로 3자리 콤마.
        String s = String.valueOf((long) n);
        StringBuilder b = new StringBuilder();
        int c = 0;
        for (int i = s.length() - 1; i >= 0; i--) {
            b.append(s.charAt(i));
            if (++c % 3 == 0 && i > 0) b.append(',');
        }
        return b.reverse().toString();
    }

    private static String dur(int s) {
        if (s < 60) return s + "s";
        if (s < 3600) return Math.round(s / 60f) + "m";
        if (s < 86400) return Math.round(s / 3600f) + "h";
        return Math.round(s / 86400f) + "d";
    }

    private static String hhmm(String ts) {
        double t = JsDate.parse(ts == null ? "" : ts);
        if (Double.isNaN(t)) return "";
        return new JsDate(t - new JsDate(t).getTimezoneOffset() * 60000d).toISOString().substring(11, 16);
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static double num(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? 0 : Js.coerceToDouble(v);
    }

    private static String stored(String key) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls == null) return null;
            Object v = Js.asPropertyMap(ls).get(key);
            return v == null ? null : String.valueOf(v);
        } catch (Exception e) { return null; }
    }

    private static void store(String val) {
        try {
            Object ls = Js.asPropertyMap(DomGlobal.window).get("localStorage");
            if (ls != null) Js.asPropertyMap(ls).set("facts", val);
        } catch (Exception ignore) { }
    }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
