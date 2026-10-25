package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.PaletteSharing;
import dev.sayaya.magi.client.domain.Destination;
import dev.sayaya.magi.client.domain.Match;
import dev.sayaya.magi.client.usecase.Navigation;
import dev.sayaya.magi.client.usecase.RosterStore;
import dev.sayaya.magi.component.Dialogs;
import dev.sayaya.ui.elements.SearchElementBuilder;
import dev.sayaya.ui.elements.SearchElementBuilder.Suggestion;
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
 * 스스로 더한 것들</b>(PaletteSharing — 셸이 자식의 기능을 알 필요가 없게). 그 마지막은 둘로
 * 온다: 미리 등록해 둔 것(current)과 지금 친 글자로 물어야 아는 것(ask — 워크스페이스의 파일
 * 이름 같은, 왕복이 드는 것).
 *
 * 상자는 md-dialog(#palDialog)이고, 그 안의 찾기 칸과 목록은 sayaya-ui의 M3 search(#palField)다:
 * 입력은 combobox, 목록은 listbox/option, ↑/↓·Enter·IME 조합 처리와 늦은 답 버리기는 컴포넌트가
 * 한다. 셸이 쥔 것은 무엇을 어떤 순서로 보이느냐(점수·스무 줄)와 고르면 무엇이 도느냐뿐이다.
 */
@Singleton
public class PaletteElement {
    private final Navigation nav;
    private final RosterStore roster;
    private final HTMLElement dialog = el("md-dialog");
    // 앱이 목록을 정한다(filter=false) — 점수로 줄 세우고 스물로 자르는 것은 이 판의 규칙이다.
    // inline: 다이얼로그 안이라 목록이 겹쳐 뜨거나 스크림이 한 겹 더 덮지 않는다.
    private final SearchElementBuilder search = SearchElementBuilder.search().docked().inline(true).filter(false);
    private final HTMLElement field = Js.uncheckedCast(search.element());
    private final HTMLElement none = el("div");
    private final HTMLElement headline = el("div");
    private final HTMLElement cancel = el("md-text-button");
    private final List<Row> rows = new ArrayList<>();
    // 몇 번째 물음인가 — 늦게 오는 답이 방금 친 글자를 덮지 않게(운영 palAsked). 컴포넌트도
    // 입력이 바뀐 답을 버리지만(setSuggestions(forQuery,…)), 같은 글자를 두 번 물은 경우는 이 수가 가른다.
    private int asked = 0;
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
        search.placeholder(tr("pal.label")).ariaLabel(tr("pal.label"));
        elemental2.dom.Element listbox = field.querySelector("[role=listbox]");
        if (listbox != null) listbox.setAttribute("aria-label", tr("pal.results"));
        cancel.textContent = tr("action.cancel");
        search.reset();
        gather();
        // md-dialog는 속성이 아니라 제 메서드로 연다 — open을 적어 두면 컴포넌트가 그것을
        // 제 상태로 삼지 않아 아무 일도 일어나지 않는다(실측: 상자가 서지 않음).
        openIt(dialog);
        DomGlobal.setTimeout(args -> { search.focus(); search.open(true); }, 30);
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
        none.id = "palNone";
        none.setAttribute("hidden", "");
        body.append(field, none);
        HTMLElement actions = el("div");
        actions.setAttribute("slot", "actions");
        cancel.id = "palCancel";
        cancel.addEventListener("click", evt -> close());
        actions.append(cancel);
        dialog.append(headline, body, actions);
        Dialogs.closeX(dialog, this::close);
        search.onInput(q -> gather());
        // 아무것도 고르지 않고 Enter면 맨 위의 것 — 적어서 좁힌 사람은 대개 맨 위를 원한다(옛 판의
        // 첫 줄 기본 선택과 같은 약속). 컴포넌트는 처음엔 아무것도 하이라이트하지 않는다.
        search.onSearch(q -> run(0));
        // Esc는 상자를 닫는다. 컴포넌트는 열린 목록에서 Esc를 소비해 다이얼로그까지 가지 않게
        // 하는데, 이 판에서 목록과 상자는 하나다. 같은 입력의 리스너라 그 소비와 상관없이 불린다.
        search.input().addEventListener("keydown", evt -> {
            JsPropertyMap<Object> k = Js.asPropertyMap(evt);
            if (Js.isTruthy(k.get("isComposing")) || Js.coerceToDouble(k.get("keyCode")) == 229) return;
            if ("Escape".equals(String.valueOf(k.get("key")))) close();
        });
    }

    /** 후보를 모은다 — 화면, 컴패니언, 그리고 지금 화면이 더한 것들. */
    private void gather() {
        final String typed = search.value() == null ? "" : search.value();
        String q = typed.trim();
        rows.clear();
        for (Destination d : Destination.all()) {
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
            rows.add(row(e, score));
        }
        rows.sort((x, y) -> y.score - x.score);
        trim();
        draw(null);
        // 그리고 화면에 <b>묻는다</b> — 지금 친 글자로만 알 수 있는 것들(그 화면의 파일 이름
        // 같은)은 등록된 목록에 없다. 목록은 먼저 서고 답은 뒤에 얹힌다: 왕복이 끝날 때까지
        // 아무것도 안 그리면, 아는 것(화면·컴패니언)까지 남의 디스크를 기다리게 된다.
        final int turn = ++asked;
        PaletteSharing.ask(q, entries -> {
            // 먼저 보낸 물음의 늦은 답은 버린다(운영 palAsked의 그 수) — 그것을 얹으면 사람은
            // 제가 이미 지운 글자의 결과를 고르게 된다. 걷힌 상자에도 얹지 않는다.
            if (turn != asked || !open()) return;
            JsArrayLike<Object> got = Js.uncheckedCast(entries);
            for (int i = 0; got != null && i < got.getLength(); i++) {
                // 점수는 고정이다(운영과 같은 자리): 찾은 쪽이 이미 물음으로 골라 온 것이라
                // 여기서 다시 재면 저쪽의 규칙과 이쪽의 규칙 둘이 서게 된다.
                rows.add(row(Js.uncheckedCast(got.getAt(i)), Match.INSIDE));
            }
            rows.sort((x, y) -> y.score - x.score);
            trim();
            draw(typed);
        });
    }

    /**
     * 스무 줄까지만 남긴다(운영 palGather의 그 수).
     *
     * 파일 이름이 붙기 전에는 닿을 일이 없던 자리다: 갈 곳 여섯에 명단 몇이면 스물이 안 된다.
     * 이제 물어서 온 것이 뒤에 얹히므로, 자르지 않으면 상자보다 긴 목록이 서고 그 아래쪽은
     * 화살표로 내려가야만 보인다 — 고를 수 있는 것이 아니다.
     */
    private void trim() {
        while (rows.size() > 20) rows.remove(rows.size() - 1);
    }

    /** 화면이 건넨 항목 하나를 이 판의 줄로 — 고르면 그 화면의 것이 불린다. */
    private Row row(JsPropertyMap<Object> e, int score) {
        Object run = e.get("run");
        return new Row(str(e, "kind"), str(e, "name"), str(e, "hint"), score,
                () -> { if (run != null) Js.<PaletteSharing.Runner>cast(run).call(); });
    }

    /**
     * 줄들을 컴포넌트의 제안으로 — 종류는 앞 요소(.palkind), 설명은 보조 문구. key가 같으면 목록을
     * 갈아도 하이라이트가 그 줄을 따라간다. forQuery가 있으면 그 글자일 때만 갈아 끼운다(늦은 답).
     */
    private void draw(String forQuery) {
        List<Suggestion> items = new ArrayList<>();
        for (int i = 0; i < rows.size(); i++) {
            final Row r = rows.get(i);
            HTMLElement kind = el("span");
            kind.className = "palkind";
            kind.textContent = r.kind;
            items.add(SearchElementBuilder.suggestion(r.name, r.hint == null || r.hint.isEmpty() ? null : r.hint,
                    kind, () -> go(r)).key(r.kind + "\u0000" + r.name + "\u0000" + r.hint));
        }
        if (forQuery == null) search.setSuggestions(items); else search.setSuggestions(forQuery, items);
        // 아무것도 없으면 그렇게 말한다 — 빈 목록은 고장과 구별되지 않는다.
        if (rows.isEmpty()) {
            none.removeAttribute("hidden");
            none.textContent = tr("pal.nothing");
        } else {
            none.setAttribute("hidden", "");
        }
    }

    private void run(int i) {
        if (i < 0 || i >= rows.size()) return;
        go(rows.get(i));
    }

    private void go(Row r) {
        close();
        r.go.run();
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
