package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.bridge.GoSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.May;
import dev.sayaya.magi.bridge.FleetAgent;
import dev.sayaya.magi.bridge.Tips;
import dev.sayaya.magi.client.domain.Roster;
import dev.sayaya.magi.client.domain.Updates;
import dev.sayaya.magi.client.domain.Versions;
import dev.sayaya.magi.client.usecase.CompanionStore;
import dev.sayaya.magi.component.Dialogs;
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
 * 도구·루프·보고 서식 셋은 문이다(actionsRow) — 카드로 서고 줄에 제 탭이 생긴다. 셋 다
 * 재어 두었다(CompanionPanelTest): 문 셋이 한 줄에 머무는 폭, 카드가 서면 사실판이 물러나는
 * 것, 낡은 데몬의 빈 답에 목록 대신 사정을 적는 것, 갈라져 나온 세션에서만 원본·차이가
 * 서는 것.
 *
 * 잔여(대조표): 결재/모델/세션은 읽기 — 운영의 메뉴 컨트롤은 데몬 질의와 함께 온다.
 * 그리고 <b>주소</b>: 운영은 ?insp=tools로 문을 열어 두고 뒤로가기로 닫는데, 여기서는
 * 문을 누른 사람만 그 카드를 볼 수 있다(?ask=와 같은 모양의 구멍이다).
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
        elemental2.dom.Element caret = dev.sayaya.magi.bridge.Icons.shape("#i-sl-chevron-down", "caret");
        caret.setAttribute("aria-hidden", "true");
        sum.className = "sum";
        bar.append(caret, cell("k", tr("field.facts")), sum);
        bar.addEventListener("click", evt -> fold(!card.hasAttribute("folded"), true));
        wrap.className = "foldwrap";
        grid.className = "grid";
        wrap.append(grid);
        card.append(bar, wrap);
        store.onContext(c -> { ctx = c; render(); });
        // 명단 전체가 아니라 <b>내 행</b>을 듣는다 — 그 행이 같은 말을 다시 하면 스토어가
        // 흘리지 않으므로, 이 판은 "바뀌었나"를 스스로 따질 필요가 없다.
        store.aimed().subscribe(row -> { a = row; render(); });
        store.onContextInfo(i -> { info = i; render(); });
        // 뒤처졌는지는 <b>명단 전체</b>가 아는 사실이라 여기서만 명단을 통으로 읽는다 — 그리고
        // 그 답이 달라졌을 때만 다시 그린다(명단은 몇 초마다 흐르고, 그 답은 거의 늘 같다).
        store.onRoster(list -> {
            String top = list == null ? "" : Roster.newest(Js.uncheckedCast(list));
            if (top.equals(newestVer)) return;
            newestVer = top;
            render();
        });
        // 접힘의 기본은 창이 정한다 — 누른 적 있는 독자만 기억된다(운영 규칙).
        String said = stored("facts");
        fold(said == null ? DomGlobal.window.innerWidth < 1200 : "folded".equals(said), false);
    }

    public HTMLElement element() { return card; }

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

    /**
     * 어느 컴패니언의 판인가 — 사람이 다른 컴패니언으로 옮기면 그때는 통째로 다시 세운다
     * (같은 키의 줄이 남의 값을 이어받지 않게).
     */
    private String shownFor = "";

    /** 이 판에 할 말이 있는가 — 보일지는 부모가 정한다. */
    public boolean hasFacts() { return full; }

    /** 그 사실이 바뀌면 부모가 다시 배치한다. */
    public interface Changed { void call(); }

    private Changed changed = () -> { };
    private boolean full = false;

    public void onChanged(Changed c) { this.changed = c; }

    // ── 격자를 제자리에서 고치는 일 ─────────────────────────────────────────
    //
    // 운영 drawDetail의 그 화해(put): 줄을 새로 짓되, <b>말이 그대로면 서 있던 것을 둔다</b>.
    // 통째로 갈아엎으면 그 안의 md-select가 문서를 떠났다 돌아오고, 떠난 고르개는 열려 있던
    // 메뉴를 닫는다 — 명단이 몇 초마다 흐르니 사람은 고르는 족족 손 밑에서 닫히는 것을 본다.
    private final java.util.Set<String> seen = new java.util.HashSet<>();

    private void put(HTMLElement row) {
        if (row == null) return;
        String k = row.getAttribute("data-k");
        if (k == null || k.isEmpty()) { grid.append(row); return; }
        seen.add(k);
        HTMLElement had = null;
        for (int i = 0; i < grid.childNodes.getLength(); i++) {
            elemental2.dom.Element c = Js.uncheckedCast(grid.childNodes.getAt(i));
            if (k.equals(c.getAttribute("data-k"))) { had = Js.uncheckedCast(c); break; }
        }
        if (had == null) { grid.append(row); return; }
        if (had == row) return;   // 계속 쓰는 줄(컨트롤을 인 줄) — 이미 제자리다
        // 말이 같으면 서 있던 줄이 남는다. 컨트롤을 이고 있는 줄은 방금 그 컨트롤을 새 줄로
        // 데려갔으므로 말이 달라지고, 그때는 새 줄이 들어선다 — 어느 쪽이든 고르개는 문서 안이다.
        if (words(had).equals(words(row))) return;
        had.replaceWith(row);
    }

    /** 이번에 아무도 대지 않은 줄은 이제 없는 사실이다. */
    private void sweep() {
        for (int i = grid.childNodes.getLength() - 1; i >= 0; i--) {
            elemental2.dom.Element c = Js.uncheckedCast(grid.childNodes.getAt(i));
            String k = c.getAttribute("data-k");
            if (k != null && !k.isEmpty() && !seen.contains(k)) c.remove();
        }
    }

    private static String words(elemental2.dom.Element n) {
        return n == null || n.textContent == null ? "" : n.textContent;
    }

    private void render() {
        // 이 판이 <b>보일지</b>는 여기서 정하지 않는다: 폰에서는 제 탭에서만 서고, 그 사실은
        // 배치를 아는 부모의 것이다. 여기서 hidden을 손대면 명단이 흐를 때마다 그 규칙이
        // 뒤집힌다(실측: 폰의 대화 탭 위에 사실판이 다시 섰다). 여기서는 "속이 있는가"만 말한다.
        boolean has = ctx != null && a != null;
        if (has != full) { full = has; changed.call(); }
        if (!has) { grid.replaceChildren(); kept.clear(); return; }
        say(sum, stateWord(a.state) + " · " + (a.workdir == null ? "" : a.workdir));
        Tips.on(sum, sum.textContent);
        // 명단은 몇 초마다 흐른다. 그때마다 이 격자를 다시 지으면 그 안의 고르개가 <b>다시
        // 부모를 얻고</b>, 부모가 바뀐 md-select는 열려 있던 메뉴를 닫는다 — 사람이 고르는 중에
        // 손 밑에서 닫히니 편집이 아예 되지 않는다(실측: 깜빡이며 못 고름). 말이 그대로면 그대로 둔다.
        String who = a.socket == null ? "" : a.socket;
        if (!who.equals(shownFor)) {
            shownFor = who;
            grid.replaceChildren();
            kept.clear();
            // 떠나며 끝난 말을 잊는다 — 받는 중인 것은 남긴다(Updates가 그 둘을 가른다).
            updates.forgetFinished();
        }
        seen.clear();
        // 질문이 오는 순서 — 이 목록이 곧 배치다(그리드는 DOM 순서로 짠다, 운영 규칙).
        String load = carrying(a);
        if (!load.isEmpty()) {
            put(field("field.status", stateWord(a.state) + " · " + load, "state " + a.state));
        }
        put(field("field.steps", a.steps > 0 ? String.valueOf(a.steps) : "—", null));
        put(field("field.last_activity", a.idle >= 0 ? tr("time.ago", "d", dur(a.idle)) : "—", null));
        if (a.role != null && !a.role.isEmpty()) put(wide(field("field.role", a.role, null)));
        if (a.team != null && !a.team.isEmpty()) {
            put(field("field.team", a.team + (a.hub ? " · " + tr("team.speaks") : ""), null));
        }
        String host = (a.instance != null && !a.instance.isEmpty() ? a.instance : a.host)
                + (a.addr != null && !a.addr.isEmpty() ? " · " + a.addr : "")
                + (a.pid > 0 ? " · pid " + a.pid : "");
        put(field("field.host", host, null));
        if (a.version != null && !a.version.isEmpty()) put(versionField());
        put(wide(field("field.workspace", a.workdir, null)));
        put(wide(sessionField()));
        put(permField());
        // 이 줄이 컨텍스트 줄들보다 <b>앞</b>이다. 운영에서는 순서가 저절로 그렇게 났다: 모델·캐시·
        // 창은 /context가 답한 뒤에야 놓이는 줄이라(`dataset.late='1'`) 뒤로 밀렸고, 이 줄은
        // drawDetail 안에서 그 자리에 놓였다. 여기서는 모델을 명단이 이미 실어 와서 순서가 코드가
        // 정하는 것이 되었으니, 운영이 그리는 순서를 그대로 적는다(실측으로 갈렸던 한 칸).
        put(actionsRow());
        contextRows();
        sweep();
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
    private String modelWas = "";
    private String sessFor = null;
    private Object sessList = null;
    private static final String[][] PERM_MODES = {
            {"ask", "perm.ask"}, {"auto", "perm.auto"}, {"allow", "perm.allow"}, {"deny", "perm.deny"}};

    /**
     * 컨트롤을 이고 있는 줄 — <b>한 번만 짓고 계속 그것을 쓴다</b>.
     *
     * 값만 바뀐 줄은 새로 지어 갈아 끼워도 그만이지만(위 put), 고르개가 든 줄은 다르다:
     * 새 껍데기를 지으면 그 고르개를 새 껍데기로 데려가야 하고, 옮겨진 md-select는 열려 있던
     * 메뉴를 닫는다. 그래서 껍데기를 키로 기억해 두고 속만 고친다 — 고르개는 문서에서 한 번도
     * 떠나지 않는다.
     */
    private final java.util.Map<String, HTMLElement> kept = new java.util.LinkedHashMap<>();

    private HTMLElement rowFor(String key, String cls) {
        HTMLElement f = kept.get(key);
        if (f == null) {
            f = cell(cls, null);
            f.setAttribute("data-k", key);
            f.append(cell("k", null), cell("v", null));
            kept.put(key, f);
        } else f.className = cls;
        say(Js.uncheckedCast(f.childNodes.getAt(0)), tr(key));
        return f;
    }

    /** 같은 말이면 다시 쓰지 않는다 — 대입은 그때마다 새 글자 노드를 만든다. */
    private static void say(HTMLElement e, String word) {
        if (word.equals(e.textContent)) return;
        e.textContent = word;
    }

    /** 그 줄의 값 칸. */
    private static HTMLElement vOf(HTMLElement row) { return Js.uncheckedCast(row.childNodes.getAt(1)); }

    /** 이 칸에 저것 하나만 — 이미 그렇다면 손대지 않는다. */
    private static void hold(HTMLElement v, HTMLElement one) {
        if (v.childNodes.getLength() == 1 && v.childNodes.getAt(0) == one) return;
        v.replaceChildren(one);
    }

    private HTMLElement permField() {
        HTMLElement f = rowFor("field.permission", "f");
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
        hold(vOf(f), permSel);
        return f;
    }

    // ── 빌드 칸, 그리고 그 안의 갱신 ────────────────────────────────────────

    private final Updates updates = new Updates();
    private final HTMLElement vnum = cell("vnum", null);
    private final HTMLElement updBtn = el("md-text-button");
    private final HTMLElement updSay = cell("updsay", null);
    private boolean updWired = false;
    private String newestVer = "";

    /**
     * 이 데몬이 도는 빌드 — 그리고 <b>이 기계 것</b>이면 그것을 갱신하는 버튼.
     *
     * 셋 다 조건이다. own이 아니면 이 콘솔이 시킬 일이 아니고, elsewhere면 잰 사람이 따로 있고,
     * peer가 있으면 남의 기계 것이다(페더레이션된 콘솔의 명단은 <b>그쪽</b> 신뢰를 싣고 온다 —
     * trust만 보면 남의 기계 데몬에 버튼이 선다). BFF도 같은 자리를 막지만, 여기서 먼저 막는
     * 이유는 눌러도 403이 오는 버튼을 세우지 않기 위해서다.
     *
     * 뒤처진 빌드에만 세우는 것도 같은 규칙이다: 최신인 것을 최신으로 만드는 버튼은 누른 사람에게
     * 아무 일도 일어나지 않는 버튼이다. 볼 수만 있는 사람에게는 <b>잠가서</b> 보인다 — 걷어 내면
     * 이 컴패니언이 뒤처졌다는 사실까지 함께 걷힌다.
     */
    private HTMLElement versionField() {
        HTMLElement f = rowFor("field.version", "f");
        HTMLElement v = vOf(f);
        if (!own(a)) {
            // 컨트롤을 이고 있던 칸이 그냥 글자로 돌아가는 자리다(신뢰가 바뀌면 그렇게 된다) —
            // say는 글자만 견주므로 숨은 버튼이 남아 있어도 같은 말로 읽힌다. 먼저 비운다.
            if (v.childElementCount > 0) v.replaceChildren();
            say(v, a.version);
            return f;
        }
        if (!updWired) {
            updWired = true;
            updBtn.addEventListener("click", evt -> ask());
        }
        // 말은 매번 확인한다(언어가 바뀌면 따라간다) — 다만 같은 말이면 손대지 않는다:
        // textContent 대입은 표까지 지우고 다시 만든다. 표는 운영이 청하는 그것을 청한다
        // (withMark). 스프라이트 없는 빌드에는 아무것도 달리지 않고, 그래서 이 빌드의 데모
        // 실측에서 두 콘솔 다 말만 있다(valueHTML에 slot=icon 없음).
        String word = tr("action.update");
        if (!word.equals(updBtn.textContent)) Icons.say(updBtn, word, "#i-sl-cloud-arrow-down");
        if (v.childNodes.getLength() != 3 || v.childNodes.getAt(0) != vnum) {
            v.replaceChildren(vnum, updBtn, updSay);
        }
        say(vnum, a.version);
        String who = a.socket == null ? "" : a.socket;
        String line = updates.line(who);
        say(updSay, line);
        show(updSay, !line.isEmpty());
        boolean behind = !newestVer.isEmpty() && Versions.compare(a.version, newestVer) < 0;
        show(updBtn, updates.button(who, behind));
        gate(updBtn, May.can("configure") && !updates.busy(who));
        return f;
    }

    /** 이 콘솔이 갱신을 시킬 만한 데몬인가 — 이 기계 것이고, 이 콘솔이 직접 재고 있는 것. */
    private static boolean own(FleetAgent a) {
        return "own".equals(a.trust) && !a.elsewhere && (a.peer == null || a.peer.isEmpty());
    }

    /**
     * 눌렀다. 답이 올 때까지 버튼은 사라지고 그 자리에 "확인 중"이 선다 — 두 번 보내지 않게.
     * 답이 오면 그 말을 그대로 세운다: 거부도 답이고, 거부는 대개 다음에 무엇을 하라는 말이다.
     */
    private void ask() {
        final String who = a == null || a.socket == null ? "" : a.socket;
        if (updates.busy(who)) return;
        updates.began(who, tr("update.working"));
        render();
        store.update(said -> {
            updates.ended(who, said, tr("update.failed"));
            render();
            // 갱신된 데몬은 다시 서는 데 시간이 걸린다. 이 콘솔이 이고 있는 빌드 사실은 그
            // 사이에 낡았으므로 두 번 다시 읽는다 — 한 번은 빠른 경우, 한 번은 그렇지 않은 경우.
            DomGlobal.setTimeout(p -> dev.sayaya.magi.bridge.Facts.reread(), 4000);
            DomGlobal.setTimeout(p -> dev.sayaya.magi.bridge.Facts.reread(), 12000);
        });
    }

    private static void show(HTMLElement e, boolean want) {
        if (want) e.removeAttribute("hidden"); else e.setAttribute("hidden", "");
    }

    private final HTMLElement provSel = el("md-outlined-select");
    private boolean provWired = false;
    private Object provList = null;
    private boolean modelBlank = false;   // 백엔드를 갈아탄 뒤: 이 칸은 비어 있어야 한다

    private HTMLElement modelField(String showing) {
        HTMLElement f = rowFor("field.provider_model", "f wide");
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
        // 갈아탄 뒤에는 비워 둔다 — 그 이름은 이전 백엔드의 것이다(데몬이 새 이름을 말하기 전까지).
        // 갈아탄 뒤 데몬이 <b>새</b> 이름을 말하면 그때 다시 채운다 — 그 전까지는 비어 있다.
        if (modelBlank && !showing.isEmpty() && !showing.equals(modelWas)) modelBlank = false;
        modelWas = showing;
        final String now = !modelWant.isEmpty() ? modelWant : modelBlank ? "" : showing;
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
        // 백엔드가 앞, 모델이 뒤 — 물음이 그 순서다("어느 백엔드, 그 다음 그 백엔드의 어느 모델").
        // 한 줄인 이유는 앞의 것을 바꾸면 뒤의 것이 내놓을 수 있는 것이 바뀌기 때문이다.
        // 이 짝도 한 번만 짓는다: 다시 지으면 두 고르개가 함께 옮겨진다.
        if (modelPair.childNodes.getLength() == 0) {
            modelPair.className = "modelpair";
            modelPair.append(providerPick(), modelSel);
        } else providerPick();
        hold(vOf(f), modelPair);
        return f;
    }

    private final HTMLElement modelPair = el("div");

    /**
     * 어느 백엔드에 물려 있나 — 그리고 갈아타는 문.
     *
     * 서빙하는 것이 하나도 없으면 서지 않는다: 뒤에 아무것도 없는 컨트롤이 된다. 갈아타면 모델
     * 이름은 <b>비운다</b>: 백엔드끼리 어휘를 나눠 쓰지 않아서 이전 이름은 새 백엔드가 모르는
     * 이름이고, 남겨 두면 다음 요청에서 거절당할 값이 아무 말 없이 서 있게 된다. 대신 고르지도
     * 않는다 — 남의 카탈로그에서 무엇을 뜻했는지 아는 규칙이 없다. 그래서 비우고 캐럿을 준다.
     */
    private HTMLElement providerPick() {
        if (!provWired) {
            provWired = true;
            provSel.className = "permsel";
            provSel.setAttribute("aria-label", tr("field.provider"));
            provSel.addEventListener("change", evt -> {
                JsPropertyMap<Object> chosen = providerNamed(value(provSel));
                if (chosen == null) return;
                store.useProvider(str(chosen, "base"), why -> {
                    if (why != null && !why.isEmpty()) return;
                    modelWant = "";
                    modelBlank = true;
                    // 다음 폴은 3초 뒤다 — 그때까지 이전 백엔드의 모델을 이 백엔드의 것인 양
                    // 세워 두지 않는다.
                    Js.asPropertyMap(modelSel).set("value", "");
                    Js.<HTMLElement>uncheckedCast(modelSel).focus();
                    render();
                });
            });
        }
        provSel.setAttribute("aria-label", tr("field.provider"));
        store.providers(got -> {
            provList = got;
            JsArrayLike<Object> all = Js.uncheckedCast(got);
            int n = all == null ? 0 : all.getLength();
            java.util.List<String[]> opts = new java.util.ArrayList<>();
            for (int i = 0; i < n; i++) {
                opts.add(new String[]{str(Js.<JsPropertyMap<Object>>uncheckedCast(all.getAt(i)), "name"), null});
            }
            options(provSel, opts.toArray(new String[0][]));
            pick(provSel, nameOfBackend());
            if (n < 1) provSel.setAttribute("hidden", ""); else provSel.removeAttribute("hidden");
            // 하나뿐이면 고를 것이 없다 — 읽을 수는 있게 두고 누름만 죽인다.
            gate(provSel, May.can("configure") && n > 1);
        });
        return provSel;
    }

    /** 지금 물려 있는 백엔드의 이름 — 명단은 주소(backend)로 말한다. */
    private String nameOfBackend() {
        String base = a == null || a.backend == null ? "" : a.backend;
        if (base.isEmpty()) return "";
        JsArrayLike<Object> all = Js.uncheckedCast(provList);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            if (base.equals(str(one, "base"))) return str(one, "name");
        }
        return "";
    }

    private JsPropertyMap<Object> providerNamed(String name) {
        JsArrayLike<Object> all = Js.uncheckedCast(provList);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            if (str(one, "name").equals(name)) return one;
        }
        return null;
    }

    private HTMLElement sessionField() {
        HTMLElement f = rowFor("field.session", "f wide");
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
        Tips.on(sessSel, tr(idle ? "hint.session_pick" : "hint.session_busy"));
        hold(vOf(f), sessSel);
        return f;
    }

    /**
     * 고르개 한 줄에 적을 말 — <b>id와 제목 둘 다</b>다(운영 규칙 그대로). 아이디만 적으면 메뉴가
     * 해시 목록이 되고, 제목만 적으면 비슷한 두 줄 사이에서 무엇을 고르는지 알 수 없다.
     *
     * 제목은 첫 프롬프트에서 나므로 <b>방금 연 세션에는 없다</b> — 그리고 화면에 가장 자주 서
     * 있는 줄이 바로 그것이다. 그래서 없음을 두 가지로 나눠 적는다: 지금 이 세션이면 "아직 제목이
     * 없다"고, 지난 세션이면 "말이 오간 적이 없다"고.
     */
    private static String what(String title, boolean current) {
        if (title != null && !title.isEmpty()) return oneLine(title, 48);
        return tr(current ? "session.thisone" : "session.untitled");
    }

    /** 한 줄로 접어 자른다 — 메뉴 한 줄이 문단이 되지 않게(운영 oneLine). */
    private static String oneLine(String s, int n) {
        if (s == null) return "";
        String one = s.replaceAll("\\s+", " ").trim();
        return one.length() <= n ? one : one.substring(0, n - 1) + "\u2026";
    }

    private void paintSessions() {
        java.util.List<String[]> opts = new java.util.ArrayList<>();
        JsArrayLike<Object> all = Js.uncheckedCast(sessList);
        for (int i = 0; all != null && i < all.getLength(); i++) {
            JsPropertyMap<Object> one = Js.uncheckedCast(all.getAt(i));
            String id = str(one, "id");
            if (id.isEmpty()) continue;
            String title = str(one, "title");
            opts.add(new String[]{id, null, id + " \u00B7 " + what(title, bool(one, "current"))});
        }
        String now = a == null || a.session == null ? "" : a.session;
        if (!now.isEmpty()) {
            boolean known = false;
            for (String[] o : opts) if (now.equals(o[0])) known = true;
            // 명단이 아직 못 따라잡은 세션도 화면에 서 있는 세션이다(운영 규칙).
            if (!known) opts.add(0, new String[]{now, null, now + " \u00B7 " + what("", true)});
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
    /**
     * 결재를 청할 때 실을 보고서의 뼈대 — <b>다이얼로그</b>다(운영도 그렇다).
     *
     * 카드가 아닌 이유: 이것은 보는 것이 아니라 고쳐서 저장하는 짧은 일이고, 카드로 열면 그 자리에
     * 있던 파일이나 전사를 밀어낸 채 사람이 저장을 누를 때까지 남는다. 그리고 이것은 취향이 아니라
     * <b>빠뜨리면 거절당하는</b> 목록이라, 어디서 온 뼈대인지도 함께 적는다.
     */
    private void showFormat() {
        HTMLElement dialog = el("md-dialog");
        dialog.id = "fmtDialog";
        HTMLElement head = el("div");
        head.setAttribute("slot", "headline");
        head.id = "fmtK";
        head.textContent = tr("fmt.headline");
        HTMLElement content = el("div");
        content.setAttribute("slot", "content");
        content.append(cell("dnote", tr("detail.loading")));
        HTMLElement actions = el("div");
        actions.setAttribute("slot", "actions");
        HTMLElement cancel = el("md-text-button");
        cancel.textContent = tr("action.cancel");
        cancel.addEventListener("click", evt -> close(dialog));
        HTMLElement save = el("md-filled-button");
        save.textContent = tr("action.save");
        actions.append(cancel, save);
        dialog.append(head, content, actions);
        Dialogs.closeX(dialog, () -> close(dialog));
        DomGlobal.document.body.append(dialog);
        open(dialog);
        store.reportFormat(got -> {
            content.replaceChildren();
            JsPropertyMap<Object> f = got == null ? null : Js.uncheckedCast(got);
            content.append(cell("dlgsup", tr("fmt.about")));
            String from = f == null ? "" : str(f, "from");
            content.append(cell("dlgsup from", tr("workspace".equals(from) ? "fmt.from_workspace"
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
            content.append(form);
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
                store.reportFormat(keys, prompts, why -> close(dialog));
            });
        });
    }

    private static native void open(HTMLElement dialog) /*-{
        if (dialog.show) dialog.show(); else dialog.setAttribute('open', '');
    }-*/;

    private static void close(HTMLElement dialog) {
        Js.asPropertyMap(dialog).set("open", false);
        DomGlobal.setTimeout(a -> dialog.remove(), 300);
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
        drop.append(Icons.shape("#i-sl-trash-can", "mk"));
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
    /**
     * 고르개의 항목들 — <b>같으면 손대지 않는다</b>.
     *
     * 명단은 몇 초마다 흐르고 그때마다 이 목록을 다시 지으면, 열어 둔 메뉴의 항목이 사람 손
     * 밑에서 통째로 갈린다(실측: 12초에 600번). 그러면 고를 수가 없다 — 누르려던 줄이 그
     * 순간 다른 노드다. 그래서 지금 서 있는 것과 대 보고, 다른 때만 다시 짓는다.
     */
    private static void options(HTMLElement sel, String[][] all) {
        if (sameOptions(sel, all)) return;
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

    private static boolean sameOptions(HTMLElement sel, String[][] all) {
        if (sel.childNodes.getLength() != all.length) return false;
        for (int i = 0; i < all.length; i++) {
            elemental2.dom.Element opt = Js.uncheckedCast(sel.childNodes.getAt(i));
            String[] o = all[i];
            String want = o.length > 2 && o[2] != null ? o[2] : (o[1] == null ? o[0] : tr(o[1]));
            String had = opt.getAttribute("value");
            if (had == null || !had.equals(o[0])) return false;
            if (!want.equals(opt.textContent == null ? "" : opt.textContent.trim())) return false;
        }
        return true;
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
        put(modelField(model));
        if (c == null) return;
        boolean estimated = Js.isTruthy(c.get("estimated"));
        boolean cacheReported = Js.isTruthy(c.get("cacheReported"));
        if (!cacheReported && !estimated) {
            put(field("field.cache", tr("context.no_cache_report"), null));
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
        Icons.say(fold, tr("action.compact_now"), "#i-sl-compress");
        fold.addEventListener("click", evt -> {
            fold.setAttribute("disabled", "");
            store.compact(() -> fold.removeAttribute("disabled"));
        });
        f.append(fold);
        put(f);
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
            put(cf);
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

    private static boolean bool(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v != null && Js.isTruthy(v);
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
