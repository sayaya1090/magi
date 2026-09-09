package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.client.domain.Copy;
import dev.sayaya.magi.client.domain.Page;
import dev.sayaya.magi.client.domain.Tongue;
import dev.sayaya.magi.client.usecase.TongueStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;

/**
 * 페이지 — 판이 하나뿐이라 말이 바뀌면 통째로 다시 그린다.
 *
 * 콘솔의 화면들과 달리 조각을 내려보내지 않는 이유: 여기서 달라지는 사실은 <b>말 하나</b>이고,
 * 그것이 바뀌면 어차피 페이지의 모든 글자가 바뀐다. 스크롤 자리는 브라우저가 지킨다.
 *
 * <p>글을 {@code innerHTML} 로 넣는 자리가 있다(강조를 위한 &lt;b&gt;). 넣는 것은 전부 이
 * 모듈이 컴파일 시점에 들고 있는 상수이고, 사람이나 회선에서 온 글은 이 페이지에 하나도
 * 없다 — 랜딩은 아무것도 묻지 않는다.
 */
@Singleton
public class LandingElement {
    private static final String REPO = "https://github.com/sayaya1090/magi";
    private static final String DOCS = REPO + "/blob/main/docs/";

    private final TongueStore tongues;
    private final HTMLElement root = el("div");

    @Inject
    public LandingElement(TongueStore tongues) {
        this.tongues = tongues;
        root.id = "page";
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        tongues.subscribe(this::render);
    }

    private void render() {
        Tongue t = tongues.now();
        DomGlobal.document.documentElement.setAttribute("lang", t.code());
        root.replaceChildren(bar(t), main(t), foot(t));
    }

    // ── 위 띠 ────────────────────────────────────────────────────────────────
    private HTMLElement bar(Tongue t) {
        HTMLElement bar = el("header");
        bar.id = "top";
        HTMLElement brand = link("#hero", "magi");
        brand.className = "brand";
        bar.append(brand);
        HTMLElement doors = el("nav");
        doors.className = "doors";
        for (String one : Page.NAV) doors.append(link("#" + one, word(t, "nav." + one)));
        bar.append(doors);
        HTMLElement acts = el("div");
        acts.className = "acts";
        HTMLElement demo = link("demo/", word(t, "cta.demo"));
        demo.className = "ghost";
        acts.append(demo);
        HTMLElement repo = link(REPO, word(t, "cta.repo"));
        repo.className = "ghost";
        acts.append(repo);
        acts.append(switcher(t));
        bar.append(acts);
        return bar;
    }

    /**
     * 말 고르개 — 두 말뿐이라 목록이 아니라 단추 하나다. 적히는 것은 <b>지금 말이 아니라
     * 눌렀을 때 갈 말</b>이고, 그래서 제 말로 적힌다("한국어" / "English").
     */
    private HTMLElement switcher(Tongue t) {
        HTMLElement button = el("button");
        button.id = "tongue";
        button.className = "ghost";
        button.textContent = t.other().tongueName();
        button.setAttribute("aria-label", word(t, "lang.switch"));
        button.setAttribute("lang", t.other().code());
        button.addEventListener("click", evt -> tongues.toggle());
        return button;
    }

    // ── 본문 ─────────────────────────────────────────────────────────────────
    private HTMLElement main(Tongue t) {
        HTMLElement main = el("main");
        main.append(hero(t), problem(t), council(t), record(t), fleet(t), features(t), start(t));
        return main;
    }

    private HTMLElement hero(Tongue t) {
        HTMLElement sec = section("hero");
        sec.append(kicker(word(t, "hero.eyebrow")));
        HTMLElement name = el("h1");
        name.textContent = "magi";
        sec.append(name);
        sec.append(para("tagline", word(t, "hero.tagline")));
        sec.append(para("lede", word(t, "hero.lede")));
        HTMLElement acts = el("div");
        acts.className = "acts";
        HTMLElement start = link("#start", word(t, "cta.start"));
        start.className = "cta primary";
        HTMLElement demo = link("demo/", word(t, "cta.demo"));
        demo.className = "cta";
        acts.append(start, demo);
        sec.append(acts);
        sec.append(snippet(word(t, "hero.install"), Page.INSTALL));
        sec.append(para("fineprint", word(t, "hero.note")));
        return sec;
    }

    private HTMLElement problem(Tongue t) {
        HTMLElement sec = head(t, "what");
        HTMLElement fig = el("figure");
        fig.className = "term";
        HTMLElement cap = el("figcaption");
        cap.className = "termhead";
        cap.textContent = word(t, "figure.title");
        HTMLElement pre = el("pre");
        pre.textContent = String.join("\n", Page.TRANSCRIPT);
        HTMLElement below = el("figcaption");
        below.className = "under";
        below.textContent = word(t, "figure.caption");
        fig.append(cap, pre, below);
        sec.append(fig);
        return sec;
    }

    private HTMLElement council(Tongue t) {
        HTMLElement sec = head(t, "council");
        HTMLElement members = el("div");
        members.className = "members";
        for (String one : Page.MEMBERS) {
            HTMLElement card = el("article");
            card.className = "member m-" + one;
            HTMLElement who = el("h3");
            // 이름은 사람 이름이라 번역하지 않는다 — 첫 글자만 올린다.
            who.textContent = one.substring(0, 1).toUpperCase() + one.substring(1);
            HTMLElement lens = el("p");
            lens.className = "lens";
            lens.textContent = word(t, "council." + one + ".lens");
            card.append(who, lens, para("walk", word(t, "council." + one + ".walk")));
            members.append(card);
        }
        sec.append(members);
        HTMLElement pair = el("div");
        pair.className = "pair";
        pair.append(note(word(t, "council.walk.head"), word(t, "council.walk.body")));
        pair.append(note(word(t, "council.close.head"), word(t, "council.close.body")));
        sec.append(pair);
        sec.append(tally(t));
        sec.append(para("note", word(t, "tally.note")));
        return sec;
    }

    private HTMLElement tally(Tongue t) {
        HTMLElement table = el("table");
        table.className = "tally";
        HTMLElement head = el("tr");
        head.append(cell("th", word(t, "tally.col.rule")), cell("th", word(t, "tally.col.when")));
        HTMLElement thead = el("thead");
        thead.append(head);
        HTMLElement body = el("tbody");
        for (int i = 0; i < Page.TALLY.length; i++) {
            HTMLElement row = el("tr");
            HTMLElement rule = el("td");
            rule.className = "rule";
            HTMLElement code = el("code");
            code.textContent = Page.TALLY_RULE[i];
            rule.append(code);
            row.append(rule, cell("td", word(t, "tally." + Page.TALLY[i] + ".when")));
            body.append(row);
        }
        table.append(thead, body);
        return table;
    }

    private HTMLElement record(Tongue t) {
        HTMLElement sec = head(t, "record");
        HTMLElement list = el("ul");
        list.className = "knows";
        for (String key : Page.RECORD) {
            HTMLElement item = el("li");
            item.innerHTML = word(t, key);
            list.append(item);
        }
        sec.append(list);
        HTMLElement pull = el("p");
        pull.className = "pull";
        pull.textContent = word(t, "record.line");
        sec.append(pull);
        return sec;
    }

    private HTMLElement fleet(Tongue t) {
        HTMLElement sec = head(t, "fleet");
        HTMLElement list = el("ul");
        list.className = "fleetlist";
        for (String key : Page.FLEET) {
            HTMLElement item = el("li");
            HTMLElement what = el("b");
            what.textContent = word(t, key + ".t");
            HTMLElement says = el("span");
            says.innerHTML = word(t, key + ".b");
            item.append(what, says);
            list.append(item);
        }
        sec.append(list);
        HTMLElement shots = el("div");
        shots.className = "shots";
        for (int i = 0; i < Page.SHOTS.length; i++) {
            HTMLElement fig = el("figure");
            fig.className = "shot";
            HTMLElement a = link(Page.SHOT_HREF[i], "");
            HTMLElement img = el("img");
            img.setAttribute("src", Page.SHOT_IMG[i]);
            img.setAttribute("alt", word(t, "shot." + Page.SHOTS[i] + ".cap"));
            img.setAttribute("loading", "lazy");
            a.append(img);
            HTMLElement cap = el("figcaption");
            cap.textContent = word(t, "shot." + Page.SHOTS[i] + ".cap");
            fig.append(a, cap);
            shots.append(fig);
        }
        sec.append(shots);
        return sec;
    }

    private HTMLElement features(Tongue t) {
        HTMLElement sec = section("features");
        HTMLElement title = el("h2");
        title.textContent = word(t, "sec.feature.head");
        sec.append(title);
        HTMLElement cards = el("div");
        cards.className = "cards";
        for (String one : Page.FEATURES) {
            cards.append(note(word(t, "feature." + one + ".t"), word(t, "feature." + one + ".b")));
        }
        sec.append(cards);
        return sec;
    }

    private HTMLElement start(Tongue t) {
        HTMLElement sec = head(t, "start");
        sec.append(snippet(word(t, "start.build"), Page.BUILD));
        sec.append(snippet(word(t, "start.run"), Page.RUN));
        sec.append(para("note", word(t, "start.more")));
        HTMLElement acts = el("div");
        acts.className = "acts";
        HTMLElement manual = link(DOCS + doc(t, "MANUAL"), word(t, "cta.manual"));
        manual.className = "cta primary";
        HTMLElement arch = link(DOCS + doc(t, "ARCHITECTURE"), word(t, "cta.arch"));
        arch.className = "cta";
        acts.append(manual, arch);
        sec.append(acts);
        return sec;
    }

    /** 문서는 <b>읽는 사람의 말로</b> 연다 — 저장소가 EN/KO 쌍을 나란히 두기 때문이다. */
    private static String doc(Tongue t, String name) {
        return t == Tongue.KO ? name + ".ko.md" : name + ".md";
    }

    private HTMLElement foot(Tongue t) {
        HTMLElement foot = el("footer");
        HTMLElement says = el("p");
        says.textContent = word(t, "foot.note");
        HTMLElement acts = el("div");
        acts.className = "acts";
        acts.append(link(REPO, word(t, "cta.repo")));
        acts.append(link(DOCS + doc(t, "MANUAL"), word(t, "cta.manual")));
        acts.append(link(REPO + "/blob/main/LICENSE", "Apache-2.0"));
        foot.append(says, acts);
        return foot;
    }

    // ── 밑감 ─────────────────────────────────────────────────────────────────
    private String word(Tongue t, String key) { return Copy.word(t, key); }

    /** 절 하나 — 제목과 리드까지. 나머지는 부르는 쪽이 채운다. */
    private HTMLElement head(Tongue t, String id) {
        HTMLElement sec = section(id);
        HTMLElement title = el("h2");
        title.textContent = word(t, "sec." + id + ".head");
        sec.append(title);
        sec.append(para("lede", word(t, "sec." + id + ".lede")));
        return sec;
    }

    private static HTMLElement section(String id) {
        HTMLElement sec = el("section");
        sec.id = id;
        return sec;
    }

    private static HTMLElement kicker(String text) {
        HTMLElement p = el("p");
        p.className = "eyebrow";
        p.textContent = text;
        return p;
    }

    private static HTMLElement para(String className, String html) {
        HTMLElement p = el("p");
        p.className = className;
        p.innerHTML = html;
        return p;
    }

    private static HTMLElement note(String title, String body) {
        HTMLElement card = el("article");
        card.className = "note";
        HTMLElement head = el("h3");
        head.textContent = title;
        HTMLElement says = el("p");
        says.innerHTML = body;
        card.append(head, says);
        return card;
    }

    private static HTMLElement snippet(String label, String body) {
        HTMLElement fig = el("figure");
        fig.className = "snippet";
        HTMLElement cap = el("figcaption");
        cap.textContent = label;
        HTMLElement pre = el("pre");
        pre.textContent = body;
        fig.append(cap, pre);
        return fig;
    }

    private static HTMLElement cell(String tag, String text) {
        HTMLElement td = el(tag);
        td.textContent = text;
        return td;
    }

    private static HTMLElement link(String href, String text) {
        HTMLElement a = el("a");
        a.setAttribute("href", href);
        if (!text.isEmpty()) a.textContent = text;
        // 저장소로 나가는 문은 새 창으로 — 읽던 자리를 잃지 않게. 페이지 안의 앵커는 그대로.
        if (href.startsWith("http")) {
            a.setAttribute("target", "_blank");
            a.setAttribute("rel", "noopener");
        }
        return a;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
