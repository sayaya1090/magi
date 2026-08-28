package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.May;

import dev.sayaya.magi.client.usecase.AccessStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import elemental2.dom.KeyboardEvent;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 접근 — 운영 loadAccess의 이식: 누구의 콘솔인지(instance), 그룹(명부는 디렉토리의 것 —
 * 읽기 전용) 먼저, 사람(예외)은 역할 메뉴·범위 칩(빼기는 두 번 확인)·이름 추가 필드,
 * 발치엔 능력 범례 — 칩을 누르면 명부가 그 능력으로 좁혀진다(한 화면에 한 선택).
 * 능력 낱말은 번역하지 않는다: auth.toml에 적히는 그 낱말이다(운영 규칙).
 *
 * 잔여: 사람 추가 다이얼로그(askLine)·삭제 확인 다이얼로그(여기선 두 번 확인으로),
 * 첫 사람=admin 강제 안내문.
 */
@Singleton
public class AccessElement {
    private static final String[][] CAP_SAY = {
            {"read", "cap.read"}, {"answer", "cap.answer"}, {"prompt", "cap.prompt"},
            {"curate", "cap.curate"}, {"configure", "cap.configure"}, {"admin", "cap.admin"},
            {"shell", "cap.shell"}};

    private final AccessStore store;
    private final HTMLElement root = el("div");
    private boolean wired = false;
    private final dev.sayaya.magi.component.Dialogs dialogs;

    @Inject
    public AccessElement(AccessStore store, dev.sayaya.magi.component.Dialogs dialogs) {
        this.store = store;
        this.dialogs = dialogs;
        root.id = "access";
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        if (wired) return;
        wired = true;
        // 말이 바뀌면 이 판도 다시 칠한다 — 언어를 간 사람이 화면을 옮겨 다니며 옛말을
        // 만나지 않게(운영 labels$의 그 구독).
        dev.sayaya.magi.bridge.Labels.onPack(this::render);
        store.subscribe(this::render);
        store.start();
    }

    private void render() {
        if (!store.answered()) return;
        JsPropertyMap<Object> got = Js.uncheckedCast(store.got());
        HTMLElement head = el("h2");
        head.className = "sectionhead";
        HTMLElement word = el("span");
        word.textContent = tr("nav.access");
        head.append(word);
        head.setAttribute("aria-label", tr("nav.access"));
        if (got == null) { root.replaceChildren(head, empty("error.pane", "error.pane_how")); return; }
        List<HTMLElement> kids = new ArrayList<>();
        kids.add(head);
        kids.addAll(instanceLine(Js.uncheckedCast(got.get("instance"))));
        boolean configured = Js.isTruthy(got.get("configured"));
        boolean named = Js.isTruthy(got.get("named"));
        if (!configured) {
            // 빈 표가 아니라 1인 콘솔의 문장 — "내 파일이 읽혔나"의 답(운영 규칙).
            kids.add(empty("access.nobody", named ? "access.nobody_how" : "access.nobody_unnamed"));
            root.replaceChildren(kids.toArray(new HTMLElement[0]));
            return;
        }
        kids.add(cell("accsay", tr("access.lead")));
        String filter = store.capFilter();
        if (filter != null) kids.add(capNote(filter));
        List<String> roles = new ArrayList<>();
        JsArrayLike<Object> roleRows = Js.uncheckedCast(got.get("roles"));
        if (roleRows != null) {
            for (int i = 0; i < roleRows.getLength(); i++) {
                roles.add(str(Js.uncheckedCast(roleRows.getAt(i)), "name"));
            }
        }
        List<HTMLElement> groups = rows(got.get("groups"), filter, r -> groupRow(r));
        List<HTMLElement> people = rows(got.get("people"), filter, r -> personRow(r, roles));
        if (!groups.isEmpty()) {
            kids.add(rosterHead("access.groups", "access.groups_why"));
            kids.add(list(groups));
        }
        if (!people.isEmpty() || filter == null) {
            kids.add(rosterHead("access.exceptions", "access.exceptions_why"));
            kids.add(list(people));
            // 사람을 들이는 문. 아무도 없는 콘솔에서는 <b>첫 사람이 admin일 수 있어야</b> 한다:
            // 사람은 있는데 admin이 없는 콘솔은 아예 뜨지 않아 서버가 그런 첫 줄을 거절한다.
            // 그래서 물을 때 기본값을 거절당하지 않을 쪽으로 놓는다(운영 addPersonButton).
            if (May.can("admin")) kids.add(addPerson(Js.uncheckedCast(got.get("roles")), people.isEmpty()));
        }
        kids.add(rosterHead("access.legend", null));
        kids.add(legend(got));
        root.replaceChildren(kids.toArray(new HTMLElement[0]));
    }

    /** 사람을 들이는 문 — 누구를, 어떤 역할로. */
    private HTMLElement addPerson(JsArrayLike<Object> roles, boolean first) {
        List<String> names = new ArrayList<>();
        String adminRole = "", plain = "";
        for (int i = 0; roles != null && i < roles.getLength(); i++) {
            JsPropertyMap<Object> r = Js.uncheckedCast(roles.getAt(i));
            String name = str(r, "name");
            names.add(name);
            JsArrayLike<Object> can = Js.uncheckedCast(r.get("can"));
            for (int k = 0; adminRole.isEmpty() && can != null && k < can.getLength(); k++) {
                if ("admin".equals(String.valueOf(can.getAt(k)))) adminRole = name;
            }
            if ("viewer".equals(name)) plain = name;
        }
        if (plain.isEmpty() && !names.isEmpty()) plain = names.get(0);
        String[][] choices = new String[names.size()][];
        for (int i = 0; i < names.size(); i++) choices[i] = new String[]{names.get(i), names.get(i)};
        final String start = first && !adminRole.isEmpty() ? adminRole : plain;
        final boolean firstOne = first && !adminRole.isEmpty();
        HTMLElement b = el("md-text-button");
        b.textContent = tr("access.add");
        b.addEventListener("click", evt -> dialogs.line(tr("access.add"),
                tr(firstOne ? "access.add_first" : "access.add_who"),
                tr("access.who"), "", choices, start,
                (who, role) -> {
                    if (who.trim().isEmpty()) return;
                    store.setPerson(who.trim(), role == null || role.isEmpty() ? start : role, "");
                }));
        return b;
    }

    private List<HTMLElement> rows(Object arr, String filter,
                                   java.util.function.Function<JsPropertyMap<Object>, HTMLElement> draw) {
        List<HTMLElement> out = new ArrayList<>();
        JsArrayLike<Object> list = Js.uncheckedCast(arr);
        if (list == null) return out;
        for (int i = 0; i < list.getLength(); i++) {
            JsPropertyMap<Object> r = Js.uncheckedCast(list.getAt(i));
            if (filter != null && !caps(r).contains(filter)) continue;
            out.add(draw.apply(r));
        }
        return out;
    }

    private HTMLElement groupRow(JsPropertyMap<Object> g) {
        HTMLElement row = cell("acc", null);
        row.append(whoLine("@" + str(g, "who"), cell("role", str(g, "role"))),
                capsLine(g, true));
        return row;
    }

    private HTMLElement personRow(JsPropertyMap<Object> p, List<String> roles) {
        boolean me = Js.isTruthy(p.get("me"));
        HTMLElement row = cell("acc person" + (me ? " now" : ""), null);
        row.append(whoLine(str(p, "who"), me ? cell("you", tr("access.you")) : null), capsLine(p, false));
        HTMLElement controls = cell("acccontrols", null);
        HTMLElement pick = el("md-outlined-select");
        pick.setAttribute("label", tr("access.role"));
        for (String r : roles) {
            HTMLElement o = el("md-select-option");
            o.setAttribute("value", r);
            if (r.equals(str(p, "role"))) o.setAttribute("selected", "");
            HTMLElement t = el("div");
            t.setAttribute("slot", "headline");
            t.textContent = r;
            o.append(t);
            pick.append(o);
        }
        final String who = str(p, "who");
        final String scope = joined(p, "companions");
        pick.addEventListener("change", evt -> store.setPerson(who, value(pick), scope));
        HTMLElement drop = el("md-text-button");
        drop.className = "drop";
        drop.setAttribute("aria-label", tr("action.remove_named", "name", who));
        arm(drop, tr("action.remove"), () -> store.removePerson(who));
        controls.append(pick, drop);
        row.append(controls, scopeSection(p));
        return row;
    }

    /** 범위 — 사람의 하위 절: 같은 역할을 이름 댄 컴패니언으로 좁힌 것(빈 것=모든 컴패니언). */
    private HTMLElement scopeSection(JsPropertyMap<Object> p) {
        HTMLElement box = cell("scopes", null);
        List<String> on = names(p, "companions");
        box.append(cell("scopek", tr(on.isEmpty() ? "access.everywhere" : "access.only_on")));
        HTMLElement chips = el("md-chip-set");
        final String who = str(p, "who");
        final String role = str(p, "role");
        for (String name : on) {
            HTMLElement c = el("md-input-chip");
            c.setAttribute("label", name);
            c.className = "scopechip";
            // 컴포넌트의 후행 ×가 remove 이벤트를 낸다 — 화면에서만 지우고 서버에 말 안 하는
            // 절반짜리가 되지 않게, 이 이벤트가 유일한 삭제 경로다(운영 규칙).
            List<String> rest = new ArrayList<>(on);
            rest.remove(name);
            c.addEventListener("remove", evt -> store.setPerson(who, role, String.join(",", rest)));
            chips.append(c);
        }
        HTMLElement add = el("md-outlined-text-field");
        add.setAttribute("label", tr("access.add_companion"));
        add.addEventListener("keydown", evt -> {
            KeyboardEvent ke = Js.uncheckedCast(evt);
            if (!"Enter".equals(ke.key)) return;
            String name = value(add).trim();
            if (name.isEmpty() || on.contains(name)) return;
            List<String> next = new ArrayList<>(on);
            next.add(name);
            store.setPerson(who, role, String.join(",", next));
        });
        HTMLElement one = cell("scopebox", null);
        one.append(chips, add);
        box.append(one);
        return box;
    }

    private HTMLElement whoLine(String who, HTMLElement trailing) {
        HTMLElement line = cell("accwho", null);
        line.append(cell("who", who));
        if (trailing != null) line.append(trailing);
        return line;
    }

    private HTMLElement capsLine(JsPropertyMap<Object> r, boolean withScope) {
        HTMLElement box = cell("acccaps", null);
        HTMLElement caps = cell("caps", null);
        for (String c : caps(r)) {
            HTMLElement t = cell("captag", c);
            t.setAttribute("data-cap", c);
            caps.append(t);
        }
        box.append(caps);
        if (withScope) {
            List<String> on = names(r, "companions");
            if (!on.isEmpty()) box.append(cell("scope", tr("access.scoped", "list", String.join(", ", on))));
        }
        return box;
    }

    private HTMLElement rosterHead(String key, String whyKey) {
        HTMLElement h = el("h3");
        h.className = "rosterhead";
        h.textContent = tr(key);
        if (whyKey != null) {
            HTMLElement say = el("span");
            say.className = "why";
            say.textContent = tr(whyKey);
            h.append(say);
        }
        return h;
    }

    private HTMLElement capNote(String filter) {
        HTMLElement box = cell("capnote", null);
        box.append(cell("", tr("access.only", "cap", filter)));
        HTMLElement all = el("md-text-button");
        all.textContent = tr("access.show_all");
        all.addEventListener("click", evt -> store.filter(null));
        box.append(all);
        return box;
    }

    private HTMLElement legend(JsPropertyMap<Object> got) {
        // 역할이 적힌 순서대로 — 알파벳이 못 하는 말을 그 순서가 한다(운영 everyCap).
        Set<String> seen = new LinkedHashSet<>();
        JsArrayLike<Object> roles = Js.uncheckedCast(got.get("roles"));
        if (roles != null) {
            for (int i = 0; i < roles.getLength(); i++) {
                for (String c : caps(Js.uncheckedCast(roles.getAt(i)))) seen.add(c);
            }
        }
        HTMLElement box = cell("caplegend", null);
        for (String c : seen) {
            String sayKey = null;
            for (String[] k : CAP_SAY) if (k[0].equals(c)) sayKey = k[1];
            if (sayKey == null) continue;
            HTMLElement row = cell("capdef", null);
            HTMLElement chip = el("md-filter-chip");
            chip.className = "capchip";
            chip.setAttribute("label", c);
            chip.setAttribute("data-cap", c);
            if (c.equals(store.capFilter())) Js.asPropertyMap(chip).set("selected", true);
            chip.addEventListener("click", evt -> store.filter(c));
            row.append(chip, cell("capsay", tr(sayKey)));
            box.append(row);
        }
        return box;
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    private List<HTMLElement> instanceLine(JsPropertyMap<Object> inst) {
        List<HTMLElement> out = new ArrayList<>();
        if (inst == null) return out;
        String who = str(inst, "who"), dir = str(inst, "configDir");
        if (who.isEmpty() && dir.isEmpty()) return out;
        HTMLElement line = cell("instance", null);
        if (!who.isEmpty()) {
            HTMLElement b = el("b");
            b.textContent = who;
            line.append(b);
        }
        if (!dir.isEmpty()) line.append(DomGlobal.document.createTextNode((who.isEmpty() ? "" : "  ·  ") + dir));
        HTMLElement why = el("span");
        why.className = "why";
        why.textContent = tr("access.instance_why");
        line.append(why);
        out.add(line);
        return out;
    }

    private static void arm(HTMLElement btn, String word, Runnable act) {
        btn.textContent = word;
        final boolean[] armed = {false};
        final double[] timer = {-1};
        btn.addEventListener("click", evt -> {
            if (armed[0]) {
                DomGlobal.clearTimeout(timer[0]);
                armed[0] = false;
                btn.className = btn.className.replace(" armed", "");
                btn.textContent = word;
                act.run();
                return;
            }
            armed[0] = true;
            btn.className += " armed";
            btn.textContent = tr("action.confirm");
            timer[0] = DomGlobal.setTimeout(a -> {
                armed[0] = false;
                btn.className = btn.className.replace(" armed", "");
                btn.textContent = word;
            }, 5000);
        });
    }

    private static HTMLElement list(List<HTMLElement> rows) {
        HTMLElement box = cell("acclist", null);
        for (HTMLElement r : rows) box.append(r);
        return box;
    }

    private static List<String> caps(JsPropertyMap<Object> r) { return names(r, "can"); }

    private static List<String> names(JsPropertyMap<Object> r, String key) {
        List<String> out = new ArrayList<>();
        JsArrayLike<Object> arr = Js.uncheckedCast(r.get(key));
        if (arr != null) for (int i = 0; i < arr.getLength(); i++) out.add(String.valueOf(arr.getAt(i)));
        return out;
    }

    private static String joined(JsPropertyMap<Object> r, String key) {
        return String.join(",", names(r, key));
    }

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement empty(String whatKey, String howKey) {
        HTMLElement e = el("div");
        e.className = "empty";
        e.innerHTML = tr(whatKey) + "<br>" + tr(howKey);
        return e;
    }

    private static String str(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
