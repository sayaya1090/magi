package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.May;
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

    /**
     * 가서 보는 것 하나를 세우는 문 — 이 판은 그것을 <b>어디에</b> 세울지 알지 못한다.
     * 자리를 아는 쪽(가운데 기둥의 탭 줄을 그리는 쪽)이 이 문을 걸어 준다.
     */
    public interface Cards { void show(String key, String title, HTMLElement body); }

    private Cards cards = (k, t, b) -> { };

    public void cardsGo(Cards go) { this.cards = go; }

    /** 이 판에 할 말이 있는가 — 보일지는 부모가 정한다. */
    public boolean hasFacts() { return full; }

    /** 그 사실이 바뀌면 부모가 다시 배치한다. */
    public interface Changed { void call(); }

    private Changed changed = () -> { };
    private boolean full = false;

    public void onChanged(Changed c) { this.changed = c; }

    private void render() {
        // 이 판이 <b>보일지</b>는 여기서 정하지 않는다: 폰에서는 제 탭에서만 서고, 그 사실은
        // 배치를 아는 부모의 것이다. 여기서 hidden을 손대면 명단이 흐를 때마다 그 규칙이
        // 뒤집힌다(실측: 폰의 대화 탭 위에 사실판이 다시 섰다). 여기서는 "속이 있는가"만 말한다.
        boolean has = ctx != null && a != null;
        if (has != full) { full = has; changed.call(); }
        if (!has) { grid.replaceChildren(); return; }
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
        grid.append(wide(sessionField()));
        grid.append(permField());
        contextRows();
        grid.append(actionsRow());
    }

    // ── 바꿀 수 있는 것들 ────────────────────────────────────────────────────
    //
    // 셋 다 같은 규칙으로 산다(운영 permField/modelField의 그것):
    //   · 컨트롤은 다시 그리기 사이에 <b>살려 둔다</b> — 명단은 몇 초마다 흐르고, 그때 갈아치우면
    //     열어 둔 메뉴가 사람 손 밑에서 닫힌다.
    //   · 청한 값은 데몬이 답할 때까지 들고 있는다 — 사이에 낀 폴이 방금 누른 것을 되돌리지 않게.
    //   · 그리는 것은 데몬이 말한 것이다 — 거부된 바꿈은 눈에 띄게 되돌아온다.
    //   · 볼 수만 있는 사람에게도 그리되 잠근다: 어떤 결재 방식인지 못 보면, 무엇이든 멈추는
    //     컴패니언과 아무것도 안 멈추는 컴패니언을 구별할 수 없다.

    private final HTMLElement permSel = el("md-outlined-select");
    private final HTMLElement modelSel = el("md-outlined-select");
    private final HTMLElement sessSel = el("md-outlined-select");
    private boolean permWired = false, modelWired = false, sessWired = false;
    private String permWant = "", modelWant = "";
    private String sessFor = null;
    private Object sessList = null;
    private static final String[][] PERM_MODES = {
            {"ask", "perm.ask"}, {"auto", "perm.auto"}, {"allow", "perm.allow"}, {"deny", "perm.deny"}};

    private HTMLElement permField() {
        HTMLElement f = cell("f", null);
        f.setAttribute("data-k", "field.permission");
        f.append(cell("k", tr("field.permission")));
        if (!permWired) {
            permWired = true;
            permSel.className = "permsel";
            permSel.addEventListener("change", evt -> {
                String want = value(permSel);
                permWant = want;
                store.permission(want, why -> { permWant = ""; });
            });
        }
        // 이름은 <b>매번</b> 다시 적는다: 한 번만 적으면 그때 실려 있던 언어로 굳는다(운영 실측).
        permSel.setAttribute("aria-label", tr("field.permission"));
        options(permSel, PERM_MODES);
        String now = !permWant.isEmpty() ? permWant : (a.permission == null ? "" : a.permission);
        pick(permSel, now);
        gate(permSel, May.can("configure"));
        HTMLElement v = cell("v", null);
        v.append(permSel);
        f.append(v);
        return f;
    }

    private HTMLElement modelField(String showing) {
        HTMLElement f = cell("f wide", null);
        f.setAttribute("data-k", "field.model");
        f.append(cell("k", tr("field.model")));
        if (!modelWired) {
            modelWired = true;
            modelSel.className = "permsel";
            modelSel.addEventListener("change", evt -> {
                String want = value(modelSel);
                if (want.isEmpty()) return;
                modelWant = want;
                store.model(want, why -> { modelWant = ""; });
            });
        }
        modelSel.setAttribute("aria-label", tr("field.model"));
        String now = !modelWant.isEmpty() ? modelWant : showing;
        // 목록은 그 <b>데몬</b>이 답한 것이다 — 콘솔의 설정에서 뽑으면 그 컴패니언이 닿지도 못하는
        // 모델을 내놓는다. 답이 비면(너무 낡은 데몬, 죽은 백엔드) 지금 것 하나만 세운다.
        store.models(names -> {
            java.util.List<String[]> opts = new java.util.ArrayList<>();
            JsArrayLike<Object> all = Js.uncheckedCast(names);
            for (int i = 0; all != null && i < all.getLength(); i++) {
                String n = String.valueOf(all.getAt(i));
                opts.add(new String[]{n, null});
            }
            if (opts.isEmpty() && !now.isEmpty()) opts.add(new String[]{now, null});
            options(modelSel, opts.toArray(new String[0][]));
            pick(modelSel, now);
        });
        gate(modelSel, May.can("configure"));
        HTMLElement v = cell("v", null);
        v.append(modelSel);
        f.append(v);
        return f;
    }

    private HTMLElement sessionField() {
        HTMLElement f = cell("f wide", null);
        f.setAttribute("data-k", "field.session");
        f.append(cell("k", tr("field.session")));
        if (!sessWired) {
            sessWired = true;
            sessSel.className = "permsel";
            sessSel.addEventListener("change", evt -> {
                String want = value(sessSel);
                if (!want.isEmpty() && !want.equals(a == null ? "" : a.session)) GoSharing.past(want);
            });
        }
        sessSel.setAttribute("aria-label", tr("field.session"));
        if (sessFor == null || !sessFor.equals(a.socket)) {
            sessFor = a.socket;
            sessList = null;
            store.history(got -> { sessList = got; paintSessions(); });
        }
        paintSessions();
        // 쉬는 동안에만: 도는 턴은 <b>이</b> 세션의 것이라, 그것을 두고 떠나자는 제안은 지킬 수
        // 없는 제안이다.
        boolean idle = "idle".equals(a.state) || "stopped".equals(a.state);
        gate(sessSel, idle);
        sessSel.setAttribute("title", tr(idle ? "hint.session_pick" : "hint.session_busy"));
        HTMLElement v = cell("v", null);
        v.append(sessSel);
        f.append(v);
        return f;
    }

    private void paintSessions() {
        java.util.List<String[]> opts = new java.util.ArrayList<>();
        JsArrayLike<Object> all = Js.uncheckedCast(sessList);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            String id = str(one, "id");
            if (id.isEmpty()) continue;
            String title = str(one, "title");
            opts.add(new String[]{id, null, title.isEmpty() ? id : title});
        }
        String now = a == null || a.session == null ? "" : a.session;
        if (!now.isEmpty()) {
            boolean known = false;
            for (String[] o : opts) if (now.equals(o[0])) known = true;
            if (!known) opts.add(0, new String[]{now, null, tr("session.this_one")});
        }
        options(sessSel, opts.toArray(new String[0][]));
        pick(sessSel, now);
    }

    /**
     * 이 컴패니언에 대해 <b>가서 보는</b> 것들 — 도구·루프·보고서 양식. 전사의 행이 아니라 여기
     * 있는 이유: 이것들은 누가 물어서 나온 답이지 일어난 일의 기록이 아니고, 전사는 이미 그 둘이
     * 섞이는 유일한 자리다(운영의 그 판단).
     */
    private HTMLElement actionsRow() {
        HTMLElement row = cell("f wide", null);
        row.setAttribute("data-k", "field.what_it_has");
        row.append(cell("k", tr("field.what_it_has")));
        HTMLElement v = cell("v", null);
        HTMLElement group = cell("bgroup", null);
        group.append(deeper(tr("insp.tools"), "#i-sl-screwdriver-wrench", this::showTools),
                deeper(tr("insp.loop"), "#i-sl-arrows-rotate", this::showLoop),
                deeper(tr("insp.format"), "#i-sl-file-lines", this::showFormat));
        v.append(group);
        row.append(v);
        return row;
    }

    private HTMLElement deeper(String word, String mark, Runnable go) {
        HTMLElement b = el("button");
        b.setAttribute("type", "button");
        b.className = "deeper hit48";
        elemental2.dom.Element m = Icons.of(mark, null);
        if (m != null) b.append(m);
        b.append(DomGlobal.document.createTextNode(word));
        b.addEventListener("click", evt -> go.run());
        return b;
    }

    // ── 가서 보는 것들 ───────────────────────────────────────────────────────

    /**
     * 무엇을 할 수 있는가(/tools) — 그 데몬에게 묻는다. 콘솔이 제 목록을 적으면, 있지도 않은
     * 컴패니언을 설명하는 셈이고 하필 플러그인이 실패한 그 하나에서 가장 자신 있게 틀린다.
     */
    private void showTools() {
        HTMLElement box = deepBox();
        box.append(cell("dnote", tr("detail.loading")));
        store.tools(names -> {
            box.replaceChildren();
            JsArrayLike<Object> all = Js.uncheckedCast(names);
            if (all == null || all.getLength() == 0) {
                // "도구가 없다"가 아니다 — 컴패니언은 늘 무언가를 갖고 있다. 빈 답이 뜻하는 것은
                // 이 데몬이 물어볼 수 없을 만큼 낡았다는 것이고, 다른 말을 적으면 화면이 사실을
                // 지어내는 것이 된다.
                box.append(cell("dnote", tr("insp.tools_unknown")));
                return;
            }
            box.append(cell("dk dhero", tr("insp.tools_have")));
            HTMLElement list = cell("dlog", null);
            for (int i = 0; i < all.getLength(); i++) {
                HTMLElement row = cell("f", null);
                row.append(cell("k", String.valueOf(all.getAt(i))));
                list.append(row);
            }
            box.append(list);
        });
        cards.show("insp.tools", tr("insp.tools"), box);
    }

    /** 턴의 지도(/loop)와, 갈라져 나온 세션이면 그 원본과 그 뒤의 차이. */
    private void showLoop() {
        HTMLElement box = deepBox();
        box.append(cell("dnote", tr("detail.loading")));
        store.loop(shape -> {
            box.replaceChildren();
            if (shape == null) { box.append(cell("dnote", tr("error.unreachable"))); return; }
            JsPropertyMap<Object> m = Js.uncheckedCast(shape);
            String map = str(m, "map");
            // 미리 짜인 글로 둔다 — 이 지도는 <b>정렬이 곧 내용</b>이라, 공백을 접으면 걸음 번호가
            // 줄줄이 붙은 문단이 된다.
            if (map.trim().isEmpty()) box.append(cell("dnote", tr("detail.nothing_yet")));
            else box.append(cell("dk", tr("insp.loop_map")), pre(map));
            String origin = str(m, "origin");
            if (!origin.isEmpty()) {
                box.append(cell("dk", tr("insp.forked_from")), cell("dv", origin));
                box.append(cell("dk", tr("insp.since_fork")), pre(str(m, "diff")));
            }
        });
        cards.show("insp.loop", tr("insp.loop"), box);
    }

    /**
     * 결재를 청할 때 실을 보고서의 뼈대 — 이것은 취향이 아니라 <b>빠뜨리면 거절당하는</b> 목록이다.
     * 그래서 어디서 온 뼈대인지도 함께 적는다(이 워크스페이스·이 콘솔·아직 아무것도).
     */
    private void showFormat() {
        HTMLElement box = deepBox();
        box.append(cell("dnote", tr("detail.loading")));
        store.reportFormat(got -> {
            box.replaceChildren();
            JsPropertyMap<Object> f = got == null ? null : Js.uncheckedCast(got);
            box.append(cell("dlgsup", tr("fmt.about")));
            String from = f == null ? "" : str(f, "from");
            box.append(cell("dlgsup from", tr("workspace".equals(from) ? "fmt.from_workspace"
                    : "console".equals(from) ? "fmt.from_console" : "fmt.from_default")));
            HTMLElement form = cell("fmtform", null);
            HTMLElement more = el("md-text-button");
            more.setAttribute("type", "button");
            more.textContent = "+ " + tr("fmt.add_section");
            JsArrayLike<Object> secs = f == null ? null : Js.uncheckedCast(f.get("sections"));
            for (int i = 0; secs != null && i < secs.getLength(); i++) {
                JsPropertyMap<Object> sec = Js.uncheckedCast(secs.getAt(i));
                form.insertBefore(fmtRow(str(sec, "key"), str(sec, "prompt")), more);
            }
            more.addEventListener("click", evt -> form.insertBefore(fmtRow("", ""), more));
            form.append(more);
            HTMLElement save = el("md-filled-button");
            save.textContent = tr("action.save");
            Icons.mark(save, "#i-sl-floppy-disk");
            HTMLElement said = cell("dnote", "");
            said.setAttribute("hidden", "");
            save.addEventListener("click", evt -> {
                java.util.List<String> keys = new ArrayList<>(), prompts = new ArrayList<>();
                elemental2.dom.NodeList<elemental2.dom.Element> rows = form.querySelectorAll(".fmtrow");
                for (int i = 0; i < rows.getLength(); i++) {
                    elemental2.dom.Element row = rows.getAt(i);
                    String k = fieldValue(row, "key"), pmt = fieldValue(row, "prompt");
                    if (k.trim().isEmpty()) continue;   // 이름 없는 절은 절이 아니다
                    keys.add(k);
                    prompts.add(pmt);
                }
                store.reportFormat(keys, prompts, why -> {
                    said.textContent = why == null || why.isEmpty() ? tr("fmt.saved") : why;
                    said.removeAttribute("hidden");
                });
            });
            box.append(form, save, said);
        });
        cards.show("insp.format", tr("insp.format"), box);
    }

    private HTMLElement fmtRow(String key, String prompt) {
        HTMLElement row = cell("fmtrow", null);
        HTMLElement k = el("md-outlined-text-field");
        k.setAttribute("label", tr("fmt.key"));
        k.setAttribute("data-name", "key");
        Js.asPropertyMap(k).set("value", key);
        HTMLElement p = el("md-outlined-text-field");
        p.setAttribute("label", tr("fmt.prompt"));
        p.setAttribute("data-name", "prompt");
        // 문장이니 문장의 모양을 준다 — 한 줄짜리 칸에서는 편집하는 동안 그 글을 읽을 수 없다.
        p.setAttribute("type", "textarea");
        p.setAttribute("rows", String.valueOf(Math.min(4, Math.max(2, (prompt.length() + 25) / 26))));
        Js.asPropertyMap(p).set("value", prompt);
        HTMLElement drop = el("md-icon-button");
        drop.setAttribute("type", "button");
        drop.className = "fmtdrop";
        drop.setAttribute("aria-label", tr("action.remove"));
        drop.append(Icons.orGlyph("#i-sl-trash-can", "✕", "mk"));
        drop.addEventListener("click", evt -> row.remove());
        row.append(k, p, drop);
        return row;
    }

    private static String fieldValue(elemental2.dom.Element row, String name) {
        elemental2.dom.Element f = row.querySelector("[data-name=" + name + "]");
        Object v = f == null ? null : Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static HTMLElement pre(String text) {
        HTMLElement p = el("pre");
        p.className = "dpre";
        p.textContent = text;
        return p;
    }

    /** 가서 보는 것들이 서는 상자 — 카드의 속이다(배치에서는 없는 셈 친다). */
    private static HTMLElement deepBox() {
        HTMLElement box = el("div");
        box.className = "dinsp";
        return box;
    }

    /** 고를 것들을 다시 적는다 — 말은 지금 실려 있는 언어의 것이다. */
    private static void options(HTMLElement sel, String[][] all) {
        sel.replaceChildren();
        for (String[] o : all) {
            HTMLElement opt = el("md-select-option");
            opt.setAttribute("value", o[0]);
            HTMLElement head = el("div");
            head.setAttribute("slot", "headline");
            head.textContent = o.length > 2 && o[2] != null ? o[2] : (o[1] == null ? o[0] : tr(o[1]));
            opt.append(head);
            sel.append(opt);
        }
    }

    /** 고른 것 — 사람이 그 안에 서 있으면(포커스) 손대지 않는다. */
    private static void pick(HTMLElement sel, String now) {
        if (now == null || now.isEmpty()) return;
        if (DomGlobal.document.activeElement == sel) return;
        Js.asPropertyMap(sel).set("value", now);
    }

    private static void gate(HTMLElement sel, boolean may) {
        if (may) sel.removeAttribute("disabled"); else sel.setAttribute("disabled", "");
    }

    private static String value(HTMLElement sel) {
        Object v = Js.asPropertyMap(sel).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    /** 이 화면의 요점 — 어느 모델의 창이 얼마나 찼고, 무엇이 접혀 나갔나(운영 drawContext의 읽기). */
    private void contextRows() {
        JsPropertyMap<Object> c = info == null ? null : Js.uncheckedCast(info);
        String model = c != null && !str(c, "model").isEmpty() ? str(c, "model")
                : a.model == null ? "" : a.model;
        grid.append(modelField(model));
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
