package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Confirm;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.RosterSharing;
import dev.sayaya.magi.bridge.Says;
import dev.sayaya.magi.component.Dialogs;
import dev.sayaya.magi.component.Rank;
import dev.sayaya.magi.client.usecase.KnowledgeStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
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
 * 지식 화면 — 운영 콘솔 loadSkills/loadWiki/loadMCP의 이식: 경험(스킬·기억, 검색·읽기·
 * 잊기·적기), 위키(정본 페이지 — 읽기 전용, 낡은 묘비 포함), 서버(MCP — 목록·제거).
 * 마크업 id(#skills/#wiki/#mcp)와 클래스(.sk/.srv/.top/.tier/.what/.meta/.body/.skfind/
 * .skwrite/.sectionhead/.empty/.filesnote)는 운영 그대로 — console.css가 입힌다.
 *
 * 운영과 같은 직계 구조로 프레임에 앉는다 — 판(#skills/#wiki/#mcp)을 새 래퍼로 감싸면
 * 운영 CSS의 자식 결합자 여백이 죽는다(rect 대조로 실측). 잔여(대조표): 없음 — 폰의
 * 반쪽 스위처·낭독 요약·다이얼로그 닫기 X·필드별 에러 매핑까지 모두 이 판의 것이다.
 */
@Singleton
public class KnowledgeElement {
    private final KnowledgeStore store;
    private final Pane skills;
    private final Pane wiki;
    private final Pane mcp;
    private final HTMLElement write = el("div");
    private final HTMLElement tabs = el("div");   // 좁은 창의 판 고르개(#sharedTabs)
    private String shows = "skills";              // 좁을 때 보이는 판 하나
    private final McpDialog dialog;
    private Object roster = null;   // 서버 다이얼로그의 "누구에게" 옵션 — 셸의 스트림에서
    private boolean wired = false;

    @Inject
    public KnowledgeElement(KnowledgeStore store) {
        this.store = store;
        // 사유는 <b>그 판의 것</b>이다 — 하나로 모으면 서버를 지우려다 들은 말이 규칙 목록
        // 위에 선다. 위키는 이 화면에서 쓰지 않으므로 세울 사유가 없다.
        skills = new Pane("skills", "nav.lessons", true, store::skillQuery, store::skillsRefusal);
        // 위키는 아직 이 판에서 쓰지 않는다 — 거절이 올 press가 없다(있게 되면 여기가 바뀐다).
        wiki = new Pane("wiki", "nav.wiki", false, store::wikiQuery, () -> "");
        mcp = new Pane("mcp", "nav.mcp", false, store::mcpQuery, store::mcpRefusal);
        dialog = new McpDialog();
        // 다이얼로그는 <b>창의 것</b>이라 창(body)에 붙인다 — 판 안에 두면 그 판의 등장
        // 애니메이션이 남긴 transform이 고정 위치의 기준을 그 판으로 바꿔, 화면을 덮어야 할
        // 스크림이 판 크기로 잘린다(실측: 1400×950이어야 할 것이 120,509에서 1256×409).
        DomGlobal.document.body.append(dialog.element);
        tabs.id = "sharedTabs";
        tabs.className = "onetabs";
        tabs.setAttribute("hidden", "");
        // 탭 하나가 아니라 탭 한 벌이다: 역할을 말했으면 화살표에도 답해야 한다(md-tabs가 아니라
        // 평범한 div라 이 손잡이는 여기서 단다 — 운영의 그 규칙과 같은 이유).
        tabs.setAttribute("role", "tablist");
        tabs.addEventListener("keydown", evt -> {
            elemental2.dom.KeyboardEvent k = Js.uncheckedCast(evt);
            boolean right = "ArrowRight".equals(k.key), left = "ArrowLeft".equals(k.key);
            if (!right && !left) return;
            elemental2.dom.NodeList<elemental2.dom.Element> all = tabList();
            elemental2.dom.Element now = DomGlobal.document.activeElement;
            int at = -1;
            for (int i = 0; i < all.getLength(); i++) if (all.getAt(i) == now) at = i;
            if (at < 0) return;
            k.preventDefault();
            int to = right ? Math.min(at + 1, all.getLength() - 1) : Math.max(at - 1, 0);
            if (to != at) Js.<HTMLElement>uncheckedCast(all.getAt(to)).focus();
        });
    }

    public void mount(HTMLElement frame) {
        // 판들은 운영처럼 프레임의 직계다 — 래퍼가 끼면 운영 CSS의 안쪽 여백이 죽는다.
        frame.replaceChildren(tabs, skills.box, wiki.box, mcp.box);
        layout();
        if (wired) return;
        wired = true;
        // 말이 바뀌면 이 판도 다시 칠한다 — 언어를 간 사람이 화면을 옮겨 다니며 옛말을
        // 만나지 않게(운영 labels$의 그 구독).
        dev.sayaya.magi.bridge.Labels.onPack(this::render);
        // 폭이 바뀌면 다시 정한다 — 넓어진 창은 셋을 한 열에 세우고 고르개를 걷는다.
        DomGlobal.window.addEventListener("resize", evt -> layout());
        RosterSharing.subscribe(list -> roster = list);
        store.subscribe(this::render);
        store.askConsole();
        store.start();
        // 그림판은 셸이 가져온다 — 이 판이 그보다 먼저 그려지면 낱말만 남고 다시 그릴 일이 없다.
        Icons.onReady(this::render);
    }

    /**
     * 좁은 창에서는 셋 중 하나만 — 이 화면에는 세 가지가 산다(배운 것, 위키, 서버). 폰에서
     * 그것은 한 화면에 세 가지 목적이라, 운영은 고르개를 세우고 하나씩 보인다. 기준은 운영의
     * 그 폭(52.5em)이고, 그 위에서는 셋이 한 열에 서므로 고르개를 그리지 않는다.
     */
    private void layout() {
        boolean narrow = DomGlobal.window.matchMedia("(max-width:52.4375em)").matches;
        if (narrow) tabs.removeAttribute("hidden"); else tabs.setAttribute("hidden", "");
        show(skills.box, !narrow || "skills".equals(shows));
        show(wiki.box, !narrow || "wiki".equals(shows));
        show(mcp.box, !narrow || "mcp".equals(shows));
        drawTabs();
    }

    /** 고르개의 말과 상태 — 팩이 바뀌면 다시 부른다(이름이 같으면 다시 짓지 않는다). */
    private void drawTabs() {
        String[][] want = {{"skills", "nav.experience"}, {"wiki", "nav.wiki"}, {"mcp", "nav.mcp"}};
        tabs.setAttribute("aria-label", tr("nav.shared"));
        elemental2.dom.NodeList<elemental2.dom.Element> had = tabList();
        StringBuilder now = new StringBuilder(), next = new StringBuilder();
        for (int i = 0; i < had.getLength(); i++) now.append(had.getAt(i).textContent).append('|');
        for (String[] t : want) next.append(tr(t[1])).append('|');
        if (!now.toString().equals(next.toString())) {
            tabs.replaceChildren();
            for (String[] t : want) {
                HTMLElement tab = el("md-secondary-tab");
                tab.textContent = tr(t[1]);
                final String key = t[0];
                tab.addEventListener("click", evt -> {
                    if (key.equals(shows)) return;
                    shows = key;
                    layout();
                });
                tabs.append(tab);
            }
        }
        elemental2.dom.NodeList<elemental2.dom.Element> all = tabList();
        for (int i = 0; i < all.getLength() && i < want.length; i++)
            Js.asPropertyMap(all.getAt(i)).set("active", want[i][0].equals(shows));
    }

    private elemental2.dom.NodeList<elemental2.dom.Element> tabList() {
        return tabs.querySelectorAll("md-secondary-tab");
    }

    private static void show(HTMLElement e, boolean on) {
        if (on) e.removeAttribute("hidden"); else e.setAttribute("hidden", "");
    }

    // ── 한 판: 머리(+액션) + 찾기(고정 — 다시 그려도 포커스를 잃지 않는다) + 동적 행들 ──
    // 행은 운영처럼 판의 직계 자식이다 — 목록 래퍼가 끼면 운영 CSS와 구조가 어긋난다.
    private final class Pane {
        final HTMLElement box = el("div");
        final HTMLElement head;
        final HTMLElement findBox = el("div");
        HTMLElement say = null;      // 판을 소개하는 한 줄 — 쉬고 있을 때만 선다
        final HTMLElement find = el("md-outlined-text-field");
        final List<HTMLElement> dyn = new ArrayList<>();
        /**
         * 이 판의 거절 사유를 어디서 읽는가 — <b>판이 스스로</b> 안다.
         *
         * <p>처음엔 그리는 갈래마다 손으로 한 줄씩 끼워 넣었다. 갈래가 여섯이었고, 그 중
         * <b>넷을 빠뜨렸다</b> — 하필 목록을 못 읽은 갈래들(`list == null`)을. 그런데 우리가
         * 지어낸 말(`error.unreachable`)이 서는 경우가 바로 그 경우다: 쓰기가 못 닿았으면
         * 뒤따르는 읽기도 못 닿아 목록이 null로 온다. <b>말할 것이 우리 것뿐인 때에만 말하지
         * 않는</b> 화면이었던 셈이다. 그래서 자리를 갈래에서 걷어 판으로 옮겼다.</p>
         */
        private final java.util.function.Supplier<String> refusal;
        private HTMLElement refused = null;
        private String saidWhy = null;   // 마지막으로 <b>세운</b> 말 — 같은 말이면 다시 세우지 않는다

        /**
         * 사유를 어디서 읽는지는 <b>부르는 쪽이 반드시 적는다</b> — 「없음」도 적어서 고른다
         * ({@code () -> ""}). 편의 생성자를 하나 두었더니 그것이 곧 「사유 없음」의 기본값이
         * 되어, 다음에 판이 하나 더 늘 때 <b>아무 말도 안 하는 판</b>이 조용히 생길 자리였다.
         */
        Pane(String id, String headKey, boolean lead, java.util.function.Consumer<String> onQuery,
             java.util.function.Supplier<String> refusal) {
            this.refusal = refusal;
            box.id = id;
            head = el("h2");
            head.className = "sectionhead";
            HTMLElement word = el("span");
            word.textContent = tr(headKey);
            head.append(word);
            head.setAttribute("aria-label", tr(headKey));
            box.append(head);
            if (lead) {
                say = el("div");
                say.className = "accsay";
                say.textContent = tr("shared.lead");
                box.append(say);
            }
            findBox.className = "skfind";
            find.setAttribute("label", tr("label.find"));
            Icons.glass(find);
            find.addEventListener("input", evt -> onQuery.accept(value(find)));
            findBox.append(find);
            box.append(findBox);
        }

        /**
         * 거절당한 press의 사유 — 목록 <b>위</b>, 머리 바로 아래에 선다. 거절되면 눌린 줄은
         * 눌리기 전 그대로라 곁에 세워 봐야 가리킬 것이 없고, 목록을 아예 못 읽었을 수도 있다
         * (그때가 이 줄이 가장 필요한 때다). 서버의 문장을 그대로 옮긴다.
         */
        private void sayRefusal() {
            String why = refusal.get();
            if (why == null || why.isEmpty()) {
                if (refused != null) { refused.remove(); refused = null; saidWhy = null; }
                return;
            }
            // 이미 서 있는 <b>그 말</b>이면 손대지 않는다. role=alert는 이 줄이 바뀔 때마다
            // 읽는 기계가 다시 읽어 주는 자리인데, 이 판은 찾기 칸에 글자 하나가 들어올 때마다
            // 다시 그려진다(skillQuery → told → render → fill) — 그대로 두면 한 번 거절당한
            // 사람은 타이핑하는 내내 같은 문장을 듣는다. 이웃한 안내 줄이 saidQuery/saidCount로
            // 이미 이렇게 굴고 있었다.
            if (refused != null && why.equals(saidWhy) && refused.parentNode == box) return;
            if (refused == null) {
                refused = cell("refused", null);
                refused.setAttribute("role", "alert");
            }
            refused.textContent = why;
            saidWhy = why;
            // 머리(그리고 있으면 소개 줄) 바로 다음 — 찾기 상자보다 위다.
            box.insertBefore(refused, say != null ? say.nextSibling : head.nextSibling);
        }

        private String saidQuery = null;
        private int saidCount = -1;

        void action(HTMLElement control) {
            // 판의 액션은 머리에 산다 — 목록을 지나 스크롤하게 하지 않는다(운영 sectionHead).
            head.append(control);
        }

        void fill(String query, int shownOfQuery, boolean findShown, HTMLElement... kids) {
            for (HTMLElement d : dyn) d.remove();
            dyn.clear();
            sayRefusal();
            findBox.setAttribute("hidden", "");
            if (findShown) findBox.removeAttribute("hidden");
            // 소개 한 줄은 <b>쉴 때만</b>: 아직 아무것도 없는 판은 빈 상태가 그 말을 대신하고,
            // 좁혀 읽는 중에는 몇 건인지가 그 자리의 말이다(운영 loadSkills의 그 두 분기).
            if (say != null) {
                boolean atRest = findShown && query.trim().isEmpty();
                if (atRest) say.removeAttribute("hidden"); else say.setAttribute("hidden", "");
            }
            if (!query.trim().isEmpty()) {
                String much = tr(shownOfQuery == 1 ? "find.result" : "find.results", "n", String.valueOf(shownOfQuery));
                HTMLElement note = cell("filesnote", much);
                dyn.add(note);
                box.append(note);
                // 눈으로 보는 사람은 목록이 줄어드는 것을 본다. 안 보는 사람에게는 그 줄어듦이
                // 아무 소리도 아니어서 셸에 소리내어 청한다 — <b>0건일 때가 가장 중요하다</b>
                // (운영이 배운 자리: 그 announce가 early return 뒤에 있어 0건만 조용했다).
                // 같은 물음에 같은 수면 다시 말하지 않는다: 명단이 흘러 다시 그려질 뿐인데
                // 세 걸음마다 같은 말을 반복하면 그 줄은 못 쓰게 된다.
                if (!query.equals(saidQuery) || shownOfQuery != saidCount) Says.say(much);
            }
            saidQuery = query;
            saidCount = shownOfQuery;
            for (HTMLElement k : kids) {
                dyn.add(k);
                box.append(k);
            }
        }
    }

    private void render() {
        // 요약이 먼저다: 같은 순간에 찾기의 "몇 건"도 나갈 수 있고, 그때 그 자리의 말은
        // 방금 친 글자에 대한 답이어야 한다(운영도 loadSkills 머리에서 이것을 먼저 말한다).
        sayShared();
        renderSkills();
        renderWiki();
        renderMcp();
    }

    private String saidShared = null;
    private boolean wasFinding = false;

    /**
     * 이 화면의 요약을 소리로 — 운영 sayShared의 이식.
     *
     * 목록이 줄고 느는 것은 눈으로 보는 사람에게만 정보다. 보지 않는 사람에게 이 화면은
     * 세 판이 아니라 한 문장이어야 하고, 그 문장이 여기서 나간다.
     *
     * 대답한 것만 센다. 초기값은 0이 아니라 "아직 모름"이라, /skills가 거절당했는데 요약이
     * 나가면 그것은 실패를 덮는 <b>거짓 주장</b>이 된다(운영이 겪은 자리: 거절 문장 위로
     * "0 rules · 0 remembered"가 덧씌워졌다). 그래서 스킬이 답하기 전에는 아무 말도 하지
     * 않고, 위키·서버 수는 그 둘이 답한 뒤에만 붙는다.
     *
     * 상태줄이 아니라 낭독이다 — 상태줄에 적으면 지워지지 않아 폰의 좁은 바를 영구히
     * 차지하고, 방금 일어난 일(거절·실패)이 그 자리를 두고 다퉈야 한다.
     */
    private void sayShared() {
        if (!store.skillsAnswered()) return;
        JsArrayLike<Object> sk = Js.uncheckedCast(store.skills());
        if (sk == null) return;   // 거절은 판이 말한다 — 요약이 그 위를 덮지 않는다
        int rules = 0, crossing = 0;
        for (int i = 0; i < sk.getLength(); i++) {
            JsPropertyMap<Object> r = Js.uncheckedCast(sk.getAt(i));
            if (!"memory".equals(str(r, "kind"))) rules++;
            if ("global".equals(str(r, "tier"))) crossing++;
        }
        int facts = sk.getLength() - rules;
        List<String> bits = new ArrayList<>();
        bits.add(tr(rules == 1 ? "count.rule" : "count.rules", "n", String.valueOf(rules)));
        bits.add(tr(facts == 1 ? "count.remembered_one" : "count.remembered", "n", String.valueOf(facts)));
        bits.add(tr("count.crossing", "n", String.valueOf(crossing)));
        JsArrayLike<Object> wk = Js.uncheckedCast(store.wiki());
        if (store.wikiAnswered() && wk != null)
            bits.add(tr(wk.getLength() == 1 ? "count.page" : "count.pages", "n", String.valueOf(wk.getLength())));
        JsArrayLike<Object> sv = Js.uncheckedCast(store.mcp());
        if (store.mcpAnswered() && sv != null)
            bits.add(tr(sv.getLength() == 1 ? "count.server" : "count.servers", "n", String.valueOf(sv.getLength())));
        String much = String.join(" \u00b7 ", bits);
        // 바뀐 것만 말한다. 이 화면은 글자 하나에도 다시 그려지는데(찾기는 여기서 거르므로
        // 목록을 다시 읽지 않는다), 같은 문장을 걸음마다 되풀이하면 그 줄은 못 쓰게 된다.
        // 예외 하나: 찾기 상자를 비운 순간. 그때 화면은 좁혀 본 것에서 다시 전체가 되므로
        // 같은 문장이어도 다시 말한다(운영도 그 자리에서 다시 말한다 — 실측: 정적 데모 둘).
        boolean finding = !store.skillQuery().trim().isEmpty();
        boolean back = wasFinding && !finding;
        wasFinding = finding;
        if (much.equals(saidShared) && !back) return;
        saidShared = much;
        Says.say(much);
    }

    // ── 경험: 규칙과 기억 ────────────────────────────────────────────────────
    private void renderSkills() {
        if (!store.skillsAnswered()) return;
        JsArrayLike<Object> list = Js.uncheckedCast(store.skills());
        if (list == null) { skills.fill("", 0, false, failed()); return; }
        if (list.getLength() == 0) {
            skills.fill("", 0, false, empty("empty.nothing_learned", "empty.nothing_learned_how"),
                    writeBox(list));
            return;
        }
        List<JsPropertyMap<Object>> shown = ranked(list, store.skillQuery(), r ->
                join(str(r, "description"), str(r, "name"), str(r, "body"), str(r, "source")));
        List<HTMLElement> kids = new ArrayList<>();
        String q = store.skillQuery();
        if (!q.trim().isEmpty() || !hasBothKinds(shown)) {
            for (JsPropertyMap<Object> sk : shown) kids.add(skillRow(sk));
        } else {
            kids.add(subHead("nav.rules"));
            for (JsPropertyMap<Object> sk : shown) if (!"memory".equals(str(sk, "kind"))) kids.add(skillRow(sk));
            kids.add(subHead("nav.memories"));
            for (JsPropertyMap<Object> sk : shown) if ("memory".equals(str(sk, "kind"))) kids.add(skillRow(sk));
        }
        if (!q.trim().isEmpty() && shown.isEmpty()) kids.add(empty("empty.no_match", "empty.no_match_how"));
        kids.add(writeBox(list));
        skills.fill(q, shown.size(), true, kids.toArray(new HTMLElement[0]));
    }

    private HTMLElement skillRow(JsPropertyMap<Object> sk) {
        boolean memory = "memory".equals(str(sk, "kind"));
        HTMLElement row = cell("sk " + str(sk, "tier") + (memory ? " fact" : ""));
        HTMLElement top = cell("top");
        top.append(cell("tier", tierWords(sk)));
        String name = str(sk, "name");
        String desc = str(sk, "description");
        top.append(cell("what", desc.isEmpty() ? name : desc));
        String body = stripSource(str(sk, "body"));
        HTMLElement folded = null;
        if (!body.isEmpty()) {
            folded = cell("body");
            folded.textContent = body;
            folded.setAttribute("hidden", "");
            top.append(readFold(folded, name));
        }
        HTMLElement drop = button("drop", tr("action.forget_named", "name", name), "#i-sl-eraser");
        Confirm.arm(drop, tr("action.forget"), () -> store.forget(name, str(sk, "tier"), str(sk, "team"),
                str(sk, "socket"), nul(str(sk, "peer"))));
        top.append(drop);
        row.append(top);
        List<String> bits = new ArrayList<>();
        bits.add(memory ? "memory" : "skill");
        bits.add(name);
        int seen = num(sk, "observed");
        if (!memory && seen > 1) bits.add("seen " + seen + "×");
        String first = str(sk, "firstSeen"), last = str(sk, "lastSeen");
        if (!last.isEmpty() && !first.isEmpty() && !first.equals(last)) bits.add(first + " → " + last);
        else if (!last.isEmpty()) bits.add("last " + last);
        // 누구의 것인지와 무엇으로 묶였는지 — 이 둘이 빠져 있었다(실측: 운영의 메타는 두 줄,
        // 우리 것은 한 줄이었고 카드가 18px 낮았다).
        String groups = joinList(sk, "groups");
        if (!groups.isEmpty()) bits.add("only agents in " + groups);
        String tags = joinList(sk, "tags");
        if (!tags.isEmpty()) bits.add("tagged " + tags);
        String src = sourceOf(str(sk, "body"));
        if (!src.isEmpty()) bits.add(tr("skill.learned_from", "src", src));
        row.append(cell("meta", String.join(" · ", bits)));
        // 규칙 그 자체는 메타 뒤에 온다(운영의 순서) — 읽는 차례가 곧 문서의 차례다.
        if (folded != null) row.append(folded);
        return row;
    }

    // ── 위키: 정본 페이지, 읽기 전용 ─────────────────────────────────────────
    private void renderWiki() {
        if (!store.wikiAnswered()) return;
        JsArrayLike<Object> list = Js.uncheckedCast(store.wiki());
        if (list == null) { wiki.fill("", 0, false, failed()); return; }
        if (list.getLength() == 0) { wiki.fill("", 0, false, empty("empty.no_pages", "empty.no_pages_how")); return; }
        List<JsPropertyMap<Object>> shown = ranked(list, store.wikiQuery(), r ->
                join(str(r, "title"), str(r, "summary"), str(r, "body"), str(r, "editor")));
        List<HTMLElement> kids = new ArrayList<>();
        for (JsPropertyMap<Object> p : shown) kids.add(wikiRow(p));
        wiki.fill(store.wikiQuery(), shown.size(), true, kids.toArray(new HTMLElement[0]));
    }

    private HTMLElement wikiRow(JsPropertyMap<Object> p) {
        boolean stale = Js.isTruthy(p.get("stale"));
        HTMLElement row = cell("sk " + str(p, "tier") + (stale ? " fact" : ""));
        HTMLElement top = cell("top");
        top.append(cell("tier", tierWords(p)));
        String title = str(p, "title");
        top.append(cell("what", (stale ? "⚠ " : "") + title));
        String body = str(p, "body").trim();
        HTMLElement folded = null;
        if (!body.isEmpty()) {
            folded = cell("body");
            folded.textContent = body;
            folded.setAttribute("hidden", "");
            top.append(readFold(folded, title));
        }
        row.append(top);
        List<String> bits = new ArrayList<>();
        if (stale) bits.add(tr("wiki.stale"));
        String editor = str(p, "editor");
        if (!editor.isEmpty()) bits.add(tr("wiki.edited_by", "name", editor));
        String updated = str(p, "updated");
        if (!updated.isEmpty()) bits.add(updated.length() > 10 ? updated.substring(0, 10) : updated);
        String summary = str(p, "summary");
        if (!summary.isEmpty()) bits.add(summary);
        // 이 쪽이 가리키는 다른 쪽들 — 위키가 낱장이 아니라 얽힌 것이라는 사실이 이 한 줄이다
        // (운영과 같은 자리·같은 표시: "→ 이름, 이름").
        String links = joinList(p, "links");
        if (!links.isEmpty()) bits.add("\u2192 " + links);
        if (!bits.isEmpty()) row.append(cell("meta", String.join(" · ", bits)));
        if (folded != null) row.append(folded);   // 본문은 메타 다음 — 읽는 차례가 문서의 차례다
        return row;
    }

    // ── 서버: 닿을 수 있는 것 ────────────────────────────────────────────────
    private boolean mcpActionOn = false;

    private void renderMcp() {
        if (!store.mcpAnswered()) return;
        if (!mcpActionOn) {
            mcpActionOn = true;
            HTMLElement open = el("md-filled-tonal-button");
            open.className = "mcpopen";
            Icons.say(open, tr("action.add_server"), "#i-sl-plus");
            open.addEventListener("click", evt -> dialog.open(null));
            mcp.action(open);
        }
        JsArrayLike<Object> list = Js.uncheckedCast(store.mcp());
        if (list == null) { mcp.fill("", 0, false, failed()); return; }
        if (list.getLength() == 0) {
            mcp.fill("", 0, false, empty("empty.no_servers", "empty.no_servers_how"));
            return;
        }
        List<JsPropertyMap<Object>> shown = ranked(list, store.mcpQuery(), r ->
                join(str(r, "name"), str(r, "command"), str(r, "url"), str(r, "companion")));
        List<HTMLElement> kids = new ArrayList<>();
        for (JsPropertyMap<Object> sv : shown) kids.add(serverRow(sv));
        if (shown.isEmpty()) kids.add(empty("empty.no_match", "empty.no_match_how"));
        mcp.fill(store.mcpQuery(), shown.size(), true, kids.toArray(new HTMLElement[0]));
    }

    private HTMLElement serverRow(JsPropertyMap<Object> sv) {
        HTMLElement row = cell("srv " + str(sv, "tier"));
        HTMLElement top = cell("top");
        String companion = str(sv, "companion");
        top.append(cell("tier", companion.isEmpty() ? tr("access.everywhere")
                : tr("reach.only", "name", companion)));
        String name = str(sv, "name");
        top.append(cell("what", name));
        HTMLElement edit = button("srvedit", tr("action.edit_named", "name", name), "#i-sl-pen-to-square");
        Icons.reword(edit, tr("action.edit"));
        edit.addEventListener("click", evt -> dialog.open(sv));
        top.append(edit);
        HTMLElement drop = button("drop", tr("action.remove_named", "name", name), "#i-ss-trash-can");
        Confirm.arm(drop, tr("action.remove"), () -> store.removeServer(name, nul(str(sv, "socket"))));
        top.append(drop);
        row.append(top);
        String url = str(sv, "url");
        StringBuilder how = new StringBuilder(url.isEmpty() ? str(sv, "command") : url);
        JsArrayLike<Object> args = Js.uncheckedCast(sv.get("args"));
        if (url.isEmpty() && args != null) {
            for (int i = 0; i < args.getLength(); i++) how.append(' ').append(args.getAt(i));
        }
        row.append(cell("how", how.toString()));
        List<String> bits = new ArrayList<>();
        JsArrayLike<Object> env = Js.uncheckedCast(sv.get("envNames"));
        if (env != null && env.getLength() > 0) {
            StringBuilder need = new StringBuilder("needs ");
            for (int i = 0; i < env.getLength(); i++) {
                if (i > 0) need.append(", ");
                need.append(env.getAt(i));
            }
            bits.add(need.toString());
        }
        bits.add(str(sv, "file"));
        row.append(cell("where", String.join(" · ", bits)));
        return row;
    }

    // ── 적어 두기 — 읽은 것 아래에, 운영의 그 순서 ───────────────────────────
    private HTMLElement writeBox(JsArrayLike<Object> all) {
        write.replaceChildren();
        write.className = "skwrite";
        HTMLElement where = el("md-outlined-select");
        where.setAttribute("label", tr("label.reaches"));
        Set<String> teams = new LinkedHashSet<>();
        for (int i = 0; i < all.getLength(); i++) {
            JsPropertyMap<Object> sk = Js.uncheckedCast(all.getAt(i));
            if ("team".equals(str(sk, "team")) || !str(sk, "team").isEmpty()) teams.add(str(sk, "team"));
        }
        where.append(option("global", tr("reach.every_companion")));
        for (String t : teams) where.append(option("team:" + t, tr("reach.team", "team", t)));
        HTMLElement note = el("md-outlined-text-field");
        note.setAttribute("label", tr("label.write_down"));
        note.setAttribute("type", "textarea");
        note.setAttribute("rows", "1");
        HTMLElement save = el("md-filled-button");
        save.id = "skSave";
        Icons.say(save, tr("action.write_down"), "#i-sl-plus");
        save.setAttribute("disabled", "");
        note.addEventListener("input", evt -> {
            if (value(note).trim().isEmpty()) save.setAttribute("disabled", "");
            else save.removeAttribute("disabled");
        });
        save.addEventListener("click", evt -> {
            String v = value(note).trim();
            if (v.isEmpty()) return;
            Object pickRaw = Js.asPropertyMap(where).get("value");
            String pick = pickRaw == null || String.valueOf(pickRaw).isEmpty() ? "global" : String.valueOf(pickRaw);
            String tier = pick.startsWith("team:") ? "team" : "global";
            store.remember(v, tier, pick.startsWith("team:") ? pick.substring(5) : null);
            set(note, "");
        });
        write.append(where, note, save);
        // 이 머신의 임베딩 모델 — 팀이 갈리면 검색이 조용히 어긋나는 그 설정(운영 규칙).
        String embed = store.embedModel();
        if (embed != null) {
            HTMLElement model = cell("skmodel",
                    embed.isEmpty() ? tr("embed.none") : tr("embed.model", "model", embed));
            write.append(model);
        }
        return write;
    }

    // ── 운영 헬퍼들의 이식 ───────────────────────────────────────────────────

    /** 읽기 토글 — 본문은 접혀 도착하고, 열림·닫힘이 이름에 실린다(운영 aria 계약). */
    /** 접기 버튼 — 본문 상자는 부르는 쪽이 만들어 제자리에 붙인다(운영의 순서: 메타 다음). */
    private HTMLElement readFold(HTMLElement text, String name) {
        HTMLElement more = button("fold", tr("action.read_named", "name", name));
        Icons.say(more, tr("action.read"), "#i-sl-file-lines");
        more.setAttribute("aria-expanded", "false");
        more.addEventListener("click", evt -> {
            boolean open = text.hasAttribute("hidden");
            if (open) text.removeAttribute("hidden"); else text.setAttribute("hidden", "");
            more.setAttribute("aria-expanded", String.valueOf(open));
            more.textContent = tr(open ? "action.collapse" : "action.read");
            more.setAttribute("aria-label", open ? tr("action.collapse") + " — " + name
                    : tr("action.read_named", "name", name));
        });
        return more;
    }

    /** 배열 필드를 쉼표로 — 없으면 빈 문자열. */
    private static String joinList(JsPropertyMap<Object> m, String key) {
        Object v = m.get(key);
        if (v == null) return "";
        JsArrayLike<Object> arr = Js.uncheckedCast(v);
        List<String> out = new ArrayList<>();
        for (int i = 0; i < arr.getLength(); i++) {
            String one = String.valueOf(arr.getAt(i)).trim();
            if (!one.isEmpty()) out.add(one);
        }
        return String.join(", ", out);
    }

    private String tierWords(JsPropertyMap<Object> r) {
        String tier = str(r, "tier");
        String words = "global".equals(tier) ? tr("reach.every_companion")
                : "team".equals(tier) ? tr("reach.team", "team", str(r, "team"))
                : tr("reach.only", "name", str(r, "companion"));
        String peer = str(r, "peer");
        return words + (peer.isEmpty() ? "" : tr("reach.on_peer", "peer", peer));
    }

    private List<JsPropertyMap<Object>> ranked(JsArrayLike<Object> list, String query,
                                               java.util.function.Function<JsPropertyMap<Object>, String> doc) {
        List<JsPropertyMap<Object>> rows = new ArrayList<>();
        for (int i = 0; i < list.getLength(); i++) rows.add(Js.uncheckedCast(list.getAt(i)));
        if (query == null || query.trim().isEmpty()) return rows;
        List<String> docs = new ArrayList<>();
        for (JsPropertyMap<Object> r : rows) docs.add(doc.apply(r));
        int[] order = Rank.order(query, docs);
        List<JsPropertyMap<Object>> out = new ArrayList<>();
        for (int i : order) out.add(rows.get(i));
        return out;
    }

    private static boolean hasBothKinds(List<JsPropertyMap<Object>> rows) {
        boolean rule = false, fact = false;
        for (JsPropertyMap<Object> r : rows) {
            if ("memory".equals(str(r, "kind"))) fact = true; else rule = true;
        }
        return rule && fact;
    }

    private static HTMLElement subHead(String key) {
        HTMLElement h = el("h3");
        h.className = "sectionhead";
        HTMLElement word = el("span");
        word.textContent = tr(key);
        h.append(word);
        h.setAttribute("aria-label", tr(key));
        return h;
    }

    private static HTMLElement empty(String whatKey, String howKey) {
        HTMLElement e = el("div");
        e.className = "empty";
        // 두 문장 다 이 바이너리가 싣는 팩에서 온다 — 네트워크의 말은 여기 닿지 않는다(운영 규칙).
        e.innerHTML = tr(whatKey) + "<br>" + tr(howKey);
        return e;
    }

    // ── 서버 추가/편집 다이얼로그 — 운영 loadMCP의 그 한 벌(추가=편집) ────────────
    @jsinterop.annotations.JsType(isNative = true)
    interface Dialog {
        void show();
        void close(String value);
    }

    private final class McpDialog {
        final HTMLElement element = el("md-dialog");
        final HTMLElement headline = el("div");
        final HTMLElement form = el("form");
        final HTMLElement kind = el("md-outlined-select");
        final HTMLElement who = el("md-outlined-select");
        final HTMLElement go = el("md-text-button");
        final List<HTMLElement> fields = new ArrayList<>();
        boolean editing = false;

        McpDialog() {
            element.id = "mcpDialog";
            headline.setAttribute("slot", "headline");
            headline.id = "mcpDialogK";
            form.setAttribute("slot", "content");
            form.id = "mcpForm";
            form.setAttribute("method", "dialog");
            kind.setAttribute("label", tr("label.mcp_kind"));
            kind.append(option("http", tr("mcp.kind_http")), option("stdio", tr("mcp.kind_stdio")));
            who.setAttribute("label", tr("label.reach"));
            // [필드, 라벨키, 힌트키, 필수, 어느 쪽] — 운영 MCP_FIELDS 그대로.
            String[][] specs = {
                    {"url", "label.mcp_url", "hint.mcp_url", "", "http"},
                    {"command", "label.mcp_command", "hint.mcp_command", "y", "stdio"},
                    {"args", "label.mcp_args", "hint.mcp_args", "", "stdio"},
                    {"env", "label.mcp_env", "hint.mcp_env", "", "stdio"},
                    {"name", "label.mcp_name", "hint.mcp_name", "y", "both"},
            };
            for (String[] f : specs) {
                HTMLElement i = el("md-outlined-text-field");
                i.setAttribute("name", f[0]);
                i.setAttribute("data-kind", f[4]);
                i.setAttribute("label", tr(f[1]));
                i.setAttribute("supporting-text", tr(f[2]));
                if (!f[3].isEmpty()) i.setAttribute("required", "");
                fields.add(i);
            }
            form.append(kind, who);
            for (HTMLElement f : fields) form.append(f);
            kind.addEventListener("change", evt -> showKind());
            HTMLElement actions = el("div");
            actions.setAttribute("slot", "actions");
            HTMLElement cancel = el("md-text-button");
            cancel.setAttribute("form", "mcpForm");
            cancel.setAttribute("value", "cancel");
            cancel.textContent = tr("action.cancel");
            go.setAttribute("form", "mcpForm");
            go.setAttribute("value", "add");
            actions.append(cancel, go);
            element.append(headline, form, actions);
            Dialogs.closeX(element, () -> Js.<Dialog>uncheckedCast(element).close("cancel"));
            // 버튼의 폼 연계(form=)는 컴포넌트 업그레이드 타이밍에 기대는 길이라(실측:
            // submit 미발화) 명시 클릭으로 결정적으로 간다. Enter 제출은 폼 리스너가 받는다.
            cancel.addEventListener("click", evt -> Js.<Dialog>uncheckedCast(element).close("cancel"));
            go.addEventListener("click", evt -> { evt.preventDefault(); submit(); });
            form.addEventListener("submit", evt -> {
                evt.preventDefault();
                submit();
            });
        }

        void showKind() {
            String k = value(kind);
            for (HTMLElement f : fields) {
                String forKind = f.getAttribute("data-kind");
                boolean on = "both".equals(forKind) || forKind.equals(k.isEmpty() ? "http" : k);
                if (on) f.removeAttribute("hidden"); else f.setAttribute("hidden", "");
            }
        }

        void open(JsPropertyMap<Object> sv) {
            editing = sv != null;
            set(kind, sv != null && !str(sv, "command").isEmpty() ? "stdio" : "http");
            // 누구에게 — 셸이 나른 명단에서. 없으면(단독) "모든 컴패니언" 하나다.
            who.replaceChildren();
            who.append(option("", tr("reach.every_companion")));
            JsArrayLike<Object> list = Js.uncheckedCast(roster);
            if (list != null) {
                for (int i = 0; i < list.getLength(); i++) {
                    JsPropertyMap<Object> a = Js.uncheckedCast(list.getAt(i));
                    if (str(a, "peer").isEmpty()) {
                        who.append(option(str(a, "socket"), tr("reach.only", "name", str(a, "name"))));
                    }
                }
            }
            put("url", sv == null ? "" : str(sv, "url"));
            put("command", sv == null ? "" : str(sv, "command"));
            put("args", sv == null ? "" : joined(sv, "args"));
            put("env", sv == null ? "" : joined(sv, "envNames"));
            put("name", sv == null ? "" : str(sv, "name"));
            // 이름은 이 서버가 적히는 키다 — 편집 중에 바꾸면 둘이 된다(운영 규칙: readonly).
            HTMLElement name = fields.get(fields.size() - 1);
            if (editing) name.setAttribute("readonly", ""); else name.removeAttribute("readonly");
            set(who, sv == null ? "" : str(sv, "socket"));
            headline.textContent = tr(editing ? "label.edit_server" : "label.add_server");
            go.textContent = tr(editing ? "action.save" : "action.add_or_replace");
            showKind();
            // 지난번의 빨간 줄은 지난번의 것이다 — 다시 열린 상자가 아직 아무것도 안 보낸 채로
            // 거절을 이고 서 있으면, 사람은 방금 제가 한 일 때문이라고 읽는다(운영도 열 때 지운다).
            for (HTMLElement f : fields) { f.removeAttribute("error"); f.removeAttribute("error-text"); }
            Js.<Dialog>uncheckedCast(element).show();
        }

        void submit() {
            JsPropertyMap<String> body = Js.uncheckedCast(JsPropertyMap.of());
            for (HTMLElement f : fields) {
                String v = value(f).trim();
                if (!v.isEmpty()) body.set(f.getAttribute("name"), v);
            }
            String socket = value(who);
            store.saveServer(socket.isEmpty() ? null : socket, body, why -> {
                if (why == null || why.isEmpty()) {
                    Js.<Dialog>uncheckedCast(element).close("add");
                    return;
                }
                // 거부는 그 필드의 라벨에 — 어느 필드의 일인지 사유가 이름을 댄다(운영 규칙).
                for (HTMLElement f : fields) { f.removeAttribute("error"); f.removeAttribute("error-text"); }
                // 지금 <b>서 있는</b> 칸 중에서 고른다. 종류가 stdio면 주소 칸은 접혀 있고(실측
                // 0×0 display:none), 서버의 그 거절은 "either a url (HTTP) or a command (stdio)"라
                // 두 낱말을 다 부른다 — 이름 순서로만 고르면 접힌 칸에 빨간 줄을 긋고 보이지 않는
                // 필드에 focus()를 부른다(운영 page.js:5571의 두 갈래를 그 페이지에서 그대로
                // 돌려 재 봤다: picked=url, 0×0). 사람이 고칠 수 있는 칸은 서 있는 칸이다.
                HTMLElement at = null;
                for (HTMLElement f : fields) {
                    if (f.hasAttribute("hidden")) continue;
                    if (why.contains(f.getAttribute("name"))) { at = f; break; }
                }
                if (at != null) {
                    at.setAttribute("error", "");
                    at.setAttribute("error-text", why.length() > 120 ? why.substring(0, 120) : why);
                    at.focus();
                    return;
                }
                // 서 있는 어느 칸의 것도 아닌 거절은 <b>어딘가에</b> 적혀야 한다. 여기 없으면
                // 사람이 누른 버튼은 아무 일도 안 한 것이 된다. 실제로 오는 말이다: 여럿이 닿는
                // 콘솔은 이 쓰기를 통째로 거절하고(403 refuseWhenShared) 그 문장은 어느 칸의
                // 이름도 부르지 않으며, 파일 쓰기 실패(500)도 그렇다. 상자는 열린 채 둔다 —
                // 사유를 읽고 고칠 자리가 이 상자다.
                Says.note(why.length() > 80 ? why.substring(0, 80) : why);
            });
        }

        void put(String name, String v) {
            for (HTMLElement f : fields) if (name.equals(f.getAttribute("name"))) set(f, v);
        }

        String joined(JsPropertyMap<Object> sv, String key) {
            JsArrayLike<Object> arr = Js.uncheckedCast(sv.get(key));
            if (arr == null) return "";
            StringBuilder b = new StringBuilder();
            for (int i = 0; i < arr.getLength(); i++) {
                if (i > 0) b.append(' ');
                b.append(arr.getAt(i));
            }
            return b.toString();
        }
    }

    private static HTMLElement failed() {
        return emptyPair();
    }

    private static HTMLElement emptyPair() {
        HTMLElement e = el("div");
        e.className = "empty";
        e.innerHTML = tr("error.pane") + "<br>" + tr("error.pane_how");
        return e;
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────
    private static final String SOURCE_HEAD = "(source: ";

    private static String stripSource(String body) {
        String b = body == null ? "" : body.trim();
        int at = b.lastIndexOf(SOURCE_HEAD);
        if (at >= 0 && b.endsWith(")")) b = b.substring(0, at).trim();
        return b;
    }

    private static String sourceOf(String body) {
        String b = body == null ? "" : body.trim();
        int at = b.lastIndexOf(SOURCE_HEAD);
        if (at < 0 || !b.endsWith(")")) return "";
        return b.substring(at + SOURCE_HEAD.length(), b.length() - 1);
    }

    private static HTMLElement option(String value, String label) {
        HTMLElement o = el("md-select-option");
        o.setAttribute("value", value);
        HTMLElement h = el("div");
        h.setAttribute("slot", "headline");
        h.textContent = label;
        o.append(h);
        return o;
    }

    private static HTMLElement button(String cls, String ariaLabel) {
        return button(cls, ariaLabel, null);
    }

    /**
     * 이 화면의 낱말 버튼 — 운영은 <b>슬롯에</b> 그림을 함께 단다(.sic slot="icon").
     *
     * 그림 없이 낱말만 두면 이 판만 다른 데서 온 화면처럼 읽힌다: 옆 화면들의 같은 무게 버튼은
     * 전부 그림을 이고 있다(실측: 읽기·잊기·적어 두기·서버 더하기 넷 다 빠져 있었다).
     * 스프라이트가 없는 빌드에서는 아무것도 넣지 않는다 — 낱말 옆의 글자 도형은 낱말이 아니다.
     */
    private static HTMLElement button(String cls, String ariaLabel, String mark) {
        HTMLElement b = el("md-text-button");
        b.className = cls;
        if (ariaLabel != null) b.setAttribute("aria-label", ariaLabel);
        // 표는 적어만 둔다 — 이 버튼들은 말을 나중에 받고("정말?"로 갈아입기도 한다), 그때
        // Icons.reword가 표를 다시 세운다.
        if (mark != null) b.setAttribute("data-mark", mark);
        return b;
    }

    private static String value(HTMLElement field) {
        Object v = Js.asPropertyMap(field).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    private static void set(HTMLElement field, String v) { Js.asPropertyMap(field).set("value", v); }

    private static String join(String... parts) {
        StringBuilder b = new StringBuilder();
        for (String p : parts) if (p != null && !p.isEmpty()) b.append(p).append(' ');
        return b.toString();
    }

    private static String str(JsPropertyMap<Object> r, String key) {
        Object v = r.get(key);
        return v == null ? "" : String.valueOf(v);
    }

    private static int num(JsPropertyMap<Object> r, String key) {
        Object v = r.get(key);
        return v == null ? 0 : (int) Js.coerceToDouble(v);
    }

    private static String nul(String s) { return s == null || s.isEmpty() ? null : s; }

    private static HTMLElement cell(String cls) { return cell(cls, null); }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
