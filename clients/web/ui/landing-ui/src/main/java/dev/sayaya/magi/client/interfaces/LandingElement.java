package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.client.domain.Page;
import dev.sayaya.magi.client.domain.Tongue;
import dev.sayaya.magi.client.usecase.TongueStore;
import dev.sayaya.magi.client.usecase.WordStore;
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
    private final WordStore words;
    private final HTMLElement root = el("div");

    @Inject
    public LandingElement(TongueStore tongues, WordStore words) {
        this.tongues = tongues;
        this.words = words;
        root.id = "page";
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        // 말이 바뀌었다는 소식이 아니라 <b>그 말의 팩이 왔다</b>는 소식을 듣는다 — 팩보다 먼저
        // 그리면 키 문자열이 한 프레임 깜빡인다.
        words.subscribe(this::render);
        words.start();
    }

    private void render() {
        if (!words.ready()) return;
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
        for (String one : Page.NAV) doors.append(link("#" + one, word("nav." + one)));
        bar.append(doors);
        HTMLElement acts = el("div");
        acts.className = "acts";
        HTMLElement demo = link("demo/", word("cta.demo"));
        demo.className = "ghost";
        acts.append(demo);
        HTMLElement repo = link(REPO, word("cta.repo"));
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
        button.setAttribute("aria-label", word("lang.switch"));
        button.setAttribute("lang", t.other().code());
        button.addEventListener("click", evt -> tongues.toggle());
        return button;
    }

    // ── 본문 ─────────────────────────────────────────────────────────────────
    private HTMLElement main(Tongue t) {
        HTMLElement main = el("main");
        main.append(hero(), problem(), council(), record(), fleet(), clients(), features(), start(t));
        return main;
    }

    private HTMLElement hero() {
        HTMLElement sec = section("hero");
        sec.append(kicker(word("hero.eyebrow")));
        HTMLElement name = el("h1");
        name.textContent = "magi";
        sec.append(name);
        sec.append(para("tagline", word("hero.tagline")));
        sec.append(para("lede", word("hero.lede")));
        HTMLElement acts = el("div");
        acts.className = "acts";
        HTMLElement start = link("#start", word("cta.start"));
        start.className = "cta primary";
        HTMLElement demo = link("demo/", word("cta.demo"));
        demo.className = "cta";
        acts.append(start, demo);
        sec.append(acts);
        sec.append(snippet(word("hero.install"), Page.INSTALL));
        sec.append(para("fineprint", word("hero.note")));
        return sec;
    }

    private HTMLElement problem() {
        HTMLElement sec = head("what");
        HTMLElement fig = el("figure");
        fig.className = "term";
        HTMLElement cap = el("figcaption");
        cap.className = "termhead";
        cap.textContent = word("figure.title");
        HTMLElement pre = el("pre");
        pre.textContent = String.join("\n", Page.TRANSCRIPT);
        HTMLElement below = el("figcaption");
        below.className = "under";
        below.textContent = word("figure.caption");
        fig.append(cap, pre, below);
        sec.append(fig);
        return sec;
    }

    private HTMLElement council() {
        HTMLElement sec = head("council");
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
            lens.textContent = word("council." + one + ".lens");
            card.append(who, lens, para("walk", word("council." + one + ".walk")));
            members.append(card);
        }
        sec.append(members);
        HTMLElement pair = el("div");
        pair.className = "pair";
        pair.append(note(word("council.walk.head"), word("council.walk.body")));
        pair.append(note(word("council.close.head"), word("council.close.body")));
        sec.append(pair);
        sec.append(tally());
        sec.append(para("note", word("tally.note")));
        return sec;
    }

    private HTMLElement tally() {
        HTMLElement table = el("table");
        table.className = "tally";
        HTMLElement head = el("tr");
        head.append(cell("th", word("tally.col.rule")), cell("th", word("tally.col.when")));
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
            row.append(rule, cell("td", word("tally." + Page.TALLY[i] + ".when")));
            body.append(row);
        }
        table.append(thead, body);
        return table;
    }

    private HTMLElement record() {
        HTMLElement sec = head("record");
        HTMLElement list = el("ul");
        list.className = "knows";
        for (String key : Page.RECORD) {
            HTMLElement item = el("li");
            item.innerHTML = word(key);
            list.append(item);
        }
        sec.append(list);
        HTMLElement pull = el("p");
        pull.className = "pull";
        pull.textContent = word("record.line");
        sec.append(pull);
        return sec;
    }

    private HTMLElement fleet() {
        HTMLElement sec = head("fleet");
        HTMLElement list = el("ul");
        list.className = "fleetlist";
        for (String key : Page.FLEET) {
            HTMLElement item = el("li");
            HTMLElement what = el("b");
            what.textContent = word(key + ".t");
            HTMLElement says = el("span");
            says.innerHTML = word(key + ".b");
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
            img.setAttribute("alt", word("shot." + Page.SHOTS[i] + ".cap"));
            img.setAttribute("loading", "lazy");
            a.append(img);
            HTMLElement cap = el("figcaption");
            cap.textContent = word("shot." + Page.SHOTS[i] + ".cap");
            fig.append(a, cap);
            shots.append(fig);
        }
        sec.append(shots);
        return sec;
    }

    /**
     * 사람이 앉는 자리들. 사진이 있는 자리에는 사진이 서고, 없는 자리는 글만 선다 — 비슷하게
     * 생긴 남의 사진으로 칸을 채우면 그 자리가 실제로 어떤지에 대해 거짓을 말하게 된다.
     */
    private HTMLElement clients() {
        HTMLElement sec = head("clients");
        HTMLElement seats = el("div");
        seats.className = "seats";
        for (int i = 0; i < Page.SEATS.length; i++) {
            String one = Page.SEATS[i];
            HTMLElement card = el("article");
            card.className = "seat";
            if (!Page.SEAT_IMG[i].isEmpty()) {
                HTMLElement img = el("img");
                img.setAttribute("src", Page.SEAT_IMG[i]);
                img.setAttribute("alt", word("seat." + one + ".t"));
                img.setAttribute("loading", "lazy");
                card.append(img);
            }
            HTMLElement line = el("h3");
            HTMLElement name = el("span");
            name.textContent = word("seat." + one + ".t");
            HTMLElement chip = el("span");
            chip.className = "chip " + Page.SEAT_STATUS[i];
            chip.textContent = word("seat." + Page.SEAT_STATUS[i]);
            line.append(name, chip);
            card.append(line);
            HTMLElement says = el("p");
            says.innerHTML = word("seat." + one + ".b");
            card.append(says);
            seats.append(card);
        }
        sec.append(seats);
        return sec;
    }

    private HTMLElement features() {
        HTMLElement sec = section("features");
        HTMLElement title = el("h2");
        title.textContent = word("sec.feature.head");
        sec.append(title);
        HTMLElement cards = el("div");
        cards.className = "cards";
        for (String one : Page.FEATURES) {
            cards.append(note(word("feature." + one + ".t"), word("feature." + one + ".b")));
        }
        sec.append(cards);
        return sec;
    }

    private HTMLElement start(Tongue t) {
        HTMLElement sec = head("start");
        sec.append(snippet(word("start.build"), Page.BUILD));
        sec.append(snippet(word("start.run"), Page.RUN));
        sec.append(para("note", word("start.more")));
        HTMLElement acts = el("div");
        acts.className = "acts";
        HTMLElement manual = link(DOCS + doc(t, "MANUAL"), word("cta.manual"));
        manual.className = "cta primary";
        HTMLElement arch = link(DOCS + doc(t, "ARCHITECTURE"), word("cta.arch"));
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
        says.textContent = word("foot.note");
        HTMLElement acts = el("div");
        acts.className = "acts";
        acts.append(link(REPO, word("cta.repo")));
        acts.append(link(DOCS + doc(t, "MANUAL"), word("cta.manual")));
        acts.append(link(REPO + "/blob/main/LICENSE", "Apache-2.0"));
        foot.append(says, acts);
        return foot;
    }

    // ── 밑감 ─────────────────────────────────────────────────────────────────
    /** 말은 팩에서 온다 — 어느 말인지는 스토어가 안다. */
    private String word(String key) { return words.word(key); }

    /** 절 하나 — 제목과 리드까지. 나머지는 부르는 쪽이 채운다. */
    private HTMLElement head(String id) {
        HTMLElement sec = section(id);
        HTMLElement title = el("h2");
        title.textContent = word("sec." + id + ".head");
        sec.append(title);
        sec.append(para("lede", word("sec." + id + ".lede")));
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
