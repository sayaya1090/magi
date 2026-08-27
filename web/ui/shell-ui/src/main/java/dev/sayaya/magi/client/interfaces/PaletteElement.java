package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.PaletteSharing;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Match;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.RosterStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import elemental2.dom.KeyboardEvent;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.List;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * ⌘K — 이름을 아는 것으로 가는 길.
 *
 * 콘솔이 커지면 "그게 어디 있더라"가 사람이 가장 자주 하는 일이 된다. 팔레트는 그 질문에
 * 한 상자로 답한다: 갈 수 있는 화면들, 지금 있는 컴패니언들, 그리고 <b>지금 서 있는 화면이
 * 스스로 더한 것들</b>(PaletteSharing — 셸이 자식의 기능을 알 필요가 없게).
 *
 * 마크업은 운영의 것이다(#palDialog/#palField/#palList/.palrow): console.css가 그 이름으로
 * 입히고, 목록은 role=listbox와 option으로 스크린리더에게도 목록이다.
 */
@Singleton
public class PaletteElement {
    private final Navigation nav;
    private final RosterStore roster;
    private final HTMLElement dialog = el("md-dialog");
    private final HTMLElement field = el("md-outlined-text-field");
    private final HTMLElement list = el("div");
    private final HTMLElement none = el("div");
    private final HTMLElement headline = el("div");
    private final HTMLElement cancel = el("md-text-button");
    private final List<Row> rows = new ArrayList<>();
    private int at = 0;
    private FleetAgent[] fleet = null;

    private static final class Row {
        final String kind, name, hint;
        final Runnable go;
        final int score;
        Row(String kind, String name, String hint, int score, Runnable go) {
            this.kind = kind; this.name = name; this.hint = hint; this.score = score; this.go = go;
        }
    }

    @Inject
    public PaletteElement(Navigation nav, RosterStore roster) {
        this.nav = nav;
        this.roster = roster;
        build();
        roster.subscribe(list -> fleet = list);
        // 화면이 제 것을 더하거나 걷으면 지금 열려 있는 목록도 따라간다.
        PaletteSharing.onChange(entries -> { if (open()) gather(); });
        DomGlobal.window.addEventListener("keydown", evt -> {
            KeyboardEvent k = Js.uncheckedCast(evt);
            // ⌘K / Ctrl+K — 두 기계의 같은 손가락. 입력 중이어도 연다: 그것이 이 키의 약속이다.
            if ((k.metaKey || k.ctrlKey) && ("k".equals(k.key) || "K".equals(k.key))) {
                evt.preventDefault();
                show();
            }
        });
    }

    public HTMLElement element() { return dialog; }

    /** 마스트헤드의 버튼도 같은 문이다 — 수식키가 없는 손(폰)에게는 그것이 유일한 길이다. */
    public void show() {
        if (open()) return;
        headline.textContent = tr("pal.head");
        field.setAttribute("label", tr("pal.label"));
        list.setAttribute("aria-label", tr("pal.results"));
        cancel.textContent = tr("action.cancel");
        Js.asPropertyMap(field).set("value", "");
        at = 0;
        gather();
        // md-dialog는 속성이 아니라 제 메서드로 연다 — open을 적어 두면 컴포넌트가 그것을
        // 제 상태로 삼지 않아 아무 일도 일어나지 않는다(실측: 상자가 서지 않음).
        openIt(dialog);
        DomGlobal.setTimeout(args -> Js.<HTMLElement>uncheckedCast(field).focus(), 30);
    }

    private boolean open() { return Js.isTruthy(Js.asPropertyMap(dialog).get("open")); }

    private void close() { closeIt(dialog); }

    private static native void openIt(HTMLElement d) /*-{
        if (d.show) d.show(); else d.open = true;
    }-*/;

    private static native void closeIt(HTMLElement d) /*-{
        if (d.close) d.close(); else d.open = false;
    }-*/;

    private void build() {
        dialog.id = "palDialog";
        headline.id = "palK";
        headline.setAttribute("slot", "headline");
        HTMLElement body = el("div");
        body.setAttribute("slot", "content");
        field.id = "palField";
        field.setAttribute("autofocus", "");
        list.id = "palList";
        list.setAttribute("role", "listbox");
        list.setAttribute("tabindex", "-1");
        none.id = "palNone";
        none.setAttribute("hidden", "");
        body.append(field, list, none);
        HTMLElement actions = el("div");
        actions.setAttribute("slot", "actions");
        cancel.id = "palCancel";
        cancel.addEventListener("click", evt -> close());
        actions.append(cancel);
        dialog.append(headline, body, actions);
        field.addEventListener("input", evt -> gather());
        field.addEventListener("keydown", evt -> {
            KeyboardEvent k = Js.uncheckedCast(evt);
            if ("ArrowDown".equals(k.key) || "ArrowUp".equals(k.key)) {
                evt.preventDefault();
                if (rows.isEmpty()) return;
                at = (at + ("ArrowDown".equals(k.key) ? 1 : rows.size() - 1)) % rows.size();
                draw();
                return;
            }
            // 조합 중의 Enter는 글자를 확정하는 것이지 고르는 것이 아니다(한국어·일본어 입력).
            if ("Enter".equals(k.key) && !Js.isTruthy(Js.asPropertyMap(k).get("isComposing"))) {
                evt.preventDefault();
                run(at);
            }
            if ("Escape".equals(k.key)) close();
        });
    }

    /** 후보를 모은다 — 화면, 컴패니언, 그리고 지금 화면이 더한 것들. */
    private void gather() {
        String q = value(field).trim();
        rows.clear();
        for (Destination d : Destination.all()) {
            if (d == Destination.FLEET && !q.isEmpty()) { /* 이름으로도 찾히게 아래에서 함께 */ }
            String name = tr(d.labelKey);
            int score = q.isEmpty() ? Match.HEAD : Match.score(name, q);
            if (score > 0) rows.add(new Row(tr("pal.kind_verb"), name, tr(d.subKey), score,
                    () -> nav.go(d)));
        }
        if (fleet != null) {
            for (FleetAgent a : fleet) {
                if (a.elsewhere) continue;
                String hay = a.name + " " + (a.role == null ? "" : a.role);
                int score = q.isEmpty() ? Match.INSIDE : Match.score(hay, q);
                if (score <= 0) continue;
                final String socket = a.socket, peer = a.peer;
                rows.add(new Row(tr("pal.kind_companion"), a.name,
                        a.role != null && !a.role.isEmpty() ? a.role : a.workdir, score,
                        () -> nav.goCompanion(socket, peer)));
            }
        }
        JsArrayLike<Object> mine = Js.uncheckedCast(PaletteSharing.current());
        for (int i = 0; mine != null && i < mine.getLength(); i++) {
            JsPropertyMap<Object> e = Js.uncheckedCast(mine.getAt(i));
            String name = str(e, "name");
            int score = q.isEmpty() ? Match.INSIDE : Match.score(name + " " + str(e, "hint"), q);
            if (score <= 0) continue;
            Object run = e.get("run");
            rows.add(new Row(str(e, "kind"), name, str(e, "hint"), score,
                    () -> { if (run != null) Js.<PaletteSharing.Runner>cast(run).call(); }));
        }
        rows.sort((x, y) -> y.score - x.score);
        at = 0;
        draw();
    }

    private void draw() {
        list.replaceChildren();
        for (int i = 0; i < rows.size(); i++) {
            Row r = rows.get(i);
            HTMLElement row = el("button");
            row.setAttribute("type", "button");
            row.className = "palrow" + (i == at ? " at" : "");
            row.id = "palrow-" + i;
            row.setAttribute("role", "option");
            row.setAttribute("aria-selected", String.valueOf(i == at));
            HTMLElement kind = el("span");
            kind.className = "palkind";
            kind.textContent = r.kind;
            HTMLElement name = el("span");
            name.className = "palname";
            name.textContent = r.name;
            row.append(kind, name);
            if (r.hint != null && !r.hint.isEmpty()) {
                HTMLElement hint = el("span");
                hint.className = "palhint";
                hint.textContent = r.hint;
                row.append(hint);
            }
            final int idx = i;
            row.addEventListener("click", evt -> run(idx));
            list.append(row);
        }
        // 아무것도 없으면 그렇게 말한다 — 빈 목록은 고장과 구별되지 않는다.
        if (rows.isEmpty()) {
            none.removeAttribute("hidden");
            none.textContent = tr("pal.nothing");
        } else {
            none.setAttribute("hidden", "");
        }
        list.setAttribute("aria-activedescendant", rows.isEmpty() ? "" : "palrow-" + at);
    }

    private void run(int i) {
        if (i < 0 || i >= rows.size()) return;
        Runnable go = rows.get(i).go;
        close();
        go.run();
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
