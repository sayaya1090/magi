package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.May;
import dev.sayaya.magi.client.domain.Tree;
import dev.sayaya.magi.client.usecase.CompanionStore;
import dev.sayaya.magi.client.usecase.WorkspaceStore;
import elemental2.dom.DomGlobal;
import elemental2.dom.Element;
import elemental2.dom.HTMLElement;
import jsinterop.base.Js;
import jsinterop.base.JsArrayLike;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;

import static dev.sayaya.magi.bridge.Labels.tr;

/**
 * 워크스페이스 — 코딩 에이전트의 왼쪽. 이 타입에게 왼쪽은 무엇으로 일하고 있는가다:
 * 파일 트리와 git.
 *
 * 두 카드다(운영 규칙): 삼백 개짜리 저장소에서 하나의 스크롤로 묶으면 git을 보러 내려가는
 * 동안 트리가 읽던 자리를 잃는다. 열린 가지를 펼치는 것은 그 디렉토리 하나를 더 읽는 일이지
 * 트리를 다시 읽는 일이 아니고, 파일을 열면 같은 자리에서 본문이 바뀐다.
 *
 * 마크업 클래스(.filescard/.panehead/.treerow/.treename/.gitinner/.gitline…)는 운영 그대로다
 * — console.css가 입힌다.
 *
 * 회선은 이 모듈이 직접 잡는다(FetchWorkspaceSource → /files·/find·/file·/file-do·/git·/git-do):
 * 셸이 단독으로 소유하는 것은 창당 하나여야 하는 **스트림**뿐이고, 워크스페이스는 이 타입의
 * 일이라 셸을 거칠 이유가 없다.
 *
 * 쓰는 컨트롤은 shell 능력이 있을 때만 그린다(May) — 게이트는 언제나 서버가 지고, 여기서
 * 하는 일은 눌러서 거절에 닿는 버튼을 없애는 것뿐이다. 잔여: diff·PR·룩오버.
 */
@Singleton
public class WorkspaceElement {
    private final WorkspaceStore store;
    private final HTMLElement root = el("div");
    private boolean wired = false;

    @Inject
    public WorkspaceElement(WorkspaceStore store, CompanionStore companion) {
        this.store = store;
        root.id = "files";
        companion.onContext(store::aim);
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        if (wired) return;
        wired = true;
        store.subscribe(this::render);
    }

    private void render() {
        root.replaceChildren();
        root.append(treeCard(), gitCard());
    }

    // ── 트리 ─────────────────────────────────────────────────────────────────

    private HTMLElement treeCard() {
        HTMLElement card = cell("filescard pane-files", null);
        card.append(head(tr("nav.files")));
        HTMLElement body = cell("panebody", null);
        body.append(findRow());
        // 찾는 동안 판이 보이는 것은 결과다 — 트리로 되돌리지 않는다(운영에서 배운 그 결함).
        if (store.finding()) {
            body.append(hitRows());
            card.append(body);
            if (store.openPath() != null) card.append(fileView());
            return card;
        }
        Object rows = store.rowsAt(".");
        if (!store.walked()) {
            body.append(cell("filesnote", tr("files.reading")));
        } else if (rows == null) {
            body.append(cell("filesnote", tr("files.unreadable")));
        } else if (Js.<JsArrayLike<Object>>uncheckedCast(rows).getLength() == 0) {
            body.append(empty("files.empty", "files.empty_how"));
        } else {
            branches(body, ".", rows, 0);
        }
        if (May.can("shell")) card.append(makeRow());
        card.append(body);
        // 열어 둔 파일은 트리 밑에 — 같은 왼쪽 안에서 읽는다(운영은 가운데 슬롯의 탭이지만,
        // 여기서 가운데는 대화의 것이다: 왼쪽에서 연 것은 왼쪽에서 읽는다).
        if (store.openPath() != null) card.append(fileView());
        return card;
    }

    private void branches(HTMLElement into, String dir, Object rowsObj, int depth) {
        JsArrayLike<Object> rows = Js.uncheckedCast(rowsObj);
        for (int i = 0; i < rows.getLength(); i++) {
            JsPropertyMap<Object> e = Js.uncheckedCast(rows.getAt(i));
            String name = str(e, "name");
            boolean isDir = Js.isTruthy(e.get("isDir"));
            String path = Tree.childPath(dir, name);
            into.append(treeRow(e, path, name, isDir, depth));
            if (isDir && store.isOpen(path)) {
                Object kids = store.rowsAt(path);
                if (kids != null) branches(into, path, kids, depth + 1);
            }
        }
    }

    /** 찾기 — 이름으로, 또는 내용으로. 어디를 뒤지는지는 고르는 것이지 짐작할 일이 아니다. */
    private HTMLElement findRow() {
        HTMLElement box = cell("findrow", null);
        HTMLElement field = el("md-outlined-text-field");
        field.id = "wsfind";
        field.setAttribute("label", tr("files.find"));
        Js.asPropertyMap(field).set("value", store.query());
        field.addEventListener("input", evt -> store.query(value(field)));
        HTMLElement where = el("md-outlined-select");
        where.className = "findwhere";
        where.setAttribute("label", tr("files.find_where"));
        for (String[] o : new String[][]{{"name", "files.by_name"}, {"text", "files.by_text"}}) {
            HTMLElement opt = el("md-select-option");
            opt.setAttribute("value", o[0]);
            if (o[0].equals(store.where())) opt.setAttribute("selected", "");
            HTMLElement h = el("div");
            h.setAttribute("slot", "headline");
            h.textContent = tr(o[1]);
            opt.append(h);
            where.append(opt);
        }
        where.addEventListener("change", evt -> store.where(value(where)));
        box.append(field, where);
        if (store.finding()) {
            HTMLElement clear = el("md-text-button");
            clear.className = "findclear";
            clear.textContent = tr("files.find_clear");
            clear.addEventListener("click", evt -> store.query(""));
            box.append(clear);
        }
        return box;
    }

    /** 결과 — 이름 검색은 경로를, 내용 검색은 grep이 낸 그대로(path:line:text)를 답한다. */
    private HTMLElement hitRows() {
        HTMLElement box = cell("hits", null);
        JsPropertyMap<Object> got = Js.uncheckedCast(store.hits());
        if (got == null) { box.append(cell("filesnote", tr("files.reading"))); return box; }
        JsArrayLike<Object> hits = Js.uncheckedCast(got.get("hits"));
        if (hits == null || hits.getLength() == 0) {
            box.append(cell("filesnote", tr("files.no_match")));
            return box;
        }
        for (int i = 0; i < hits.getLength(); i++) {
            String hit = String.valueOf(hits.getAt(i));
            HTMLElement row = el("button");
            row.setAttribute("type", "button");
            row.className = "treerow hit state";
            row.append(cell("treename", hit));
            // 내용 검색의 답은 path:line:text 다 — 여는 것은 그 앞의 경로다.
            final String path = "text".equals(store.where()) && hit.contains(":")
                    ? hit.substring(0, hit.indexOf(':')) : hit;
            row.addEventListener("click", evt -> store.openFile(path));
            box.append(row);
        }
        double more = num(got, "more");
        if (more > 0) box.append(cell("filesnote", tr("files.more", "n", String.valueOf((int) more))));
        return box;
    }

    /** 새로 만들기 — 파일과 디렉토리. 이름을 묻고, 만든 뒤 다시 걷는다. */
    private HTMLElement makeRow() {
        HTMLElement box = cell("makerow", null);
        HTMLElement field = el("md-outlined-text-field");
        field.id = "wsnew";
        field.setAttribute("label", tr("files.path"));
        HTMLElement file = el("md-text-button");
        file.className = "makefile";
        file.textContent = tr("files.new_file");
        file.addEventListener("click", evt -> {
            String p = value(field).trim();
            if (p.isEmpty()) return;
            store.fileDo("new-file", p, null);
            Js.asPropertyMap(field).set("value", "");
        });
        HTMLElement dir = el("md-text-button");
        dir.className = "makedir";
        dir.textContent = tr("files.new_dir");
        dir.addEventListener("click", evt -> {
            String p = value(field).trim();
            if (p.isEmpty()) return;
            store.fileDo("new-dir", p, null);
            Js.asPropertyMap(field).set("value", "");
        });
        box.append(field, file, dir);
        return box;
    }

    private HTMLElement treeRow(JsPropertyMap<Object> e, String path, String name, boolean isDir, int depth) {
        HTMLElement row = el("button");
        row.setAttribute("type", "button");
        boolean here = path.equals(store.openPath());
        row.className = "treerow state" + (isDir ? " dir" : "") + (here ? " now" : "");
        // 깊이는 숫자로 — 들여쓰기와 그 안내선이 한 값에서 온다(운영 규칙).
        row.style.setProperty("--d", String.valueOf(depth));
        Element mark = Icons.orGlyph(isDir ? "#i-sl-chevron-right" : "#i-sl-file-lines",
                isDir ? "\u25B8" : "\u00B7",
                "treemark" + (isDir && store.isOpen(path) ? " open" : ""));
        row.append(mark, cell("treename", name));
        row.addEventListener("click", evt -> {
            if (isDir) store.toggle(path);
            else store.openFile(path);
        });
        if (!May.can("shell")) return row;
        // 행과 그 손잡이는 한 줄로 — 트리가 다시 그려져도 짝이 흩어지지 않는다.
        HTMLElement line = cell("treeline", null);
        line.append(row, rowActs(path, name));
        return line;
    }

    /** 한 행에 하는 일 — 이름 바꾸기, 그리고 지우기(되돌릴 수 없으니 두 번 묻는다). */
    private HTMLElement rowActs(String path, String name) {
        HTMLElement box = cell("rowacts", null);
        HTMLElement rename = el("md-text-button");
        rename.className = "act rename";
        rename.textContent = tr("files.rename");
        rename.setAttribute("aria-label", tr("files.rename") + " — " + name);
        rename.addEventListener("click", evt -> {
            evt.stopPropagation();
            String to = DomGlobal.window.prompt(tr("files.rename_who"), path);
            if (to != null && !to.trim().isEmpty() && !to.equals(path)) store.fileDo("rename", path, to.trim());
        });
        HTMLElement drop = el("md-text-button");
        drop.className = "act drop";
        drop.setAttribute("aria-label", tr("files.delete") + " — " + name);
        arm(drop, tr("files.delete"), () -> store.fileDo("delete", path, null));
        box.append(rename, drop);
        return box;
    }

    /** 두 번 눌러야 도는 것 — 지우기·되돌리기처럼 되돌릴 수 없는 일에만(운영 arm). */
    private static void arm(HTMLElement btn, String word, Runnable act) {
        btn.textContent = word;
        final boolean[] armed = {false};
        final double[] timer = {-1};
        btn.addEventListener("click", evt -> {
            evt.stopPropagation();
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

    private static String value(HTMLElement f) {
        Object v = Js.asPropertyMap(f).get("value");
        return v == null ? "" : String.valueOf(v);
    }

    /** 연 파일 — 아직 오지 않았으면 온다고 말하고, 비었으면 비었다고 말한다. */
    private HTMLElement fileView() {
        HTMLElement box = cell("fileview", null);
        HTMLElement top = cell("filetop", null);
        top.append(cell("filepath", store.openPath()));
        HTMLElement close = el("md-text-button");
        close.className = "fileclose";
        close.textContent = tr("action.close");
        close.addEventListener("click", evt -> store.closeFile());
        top.append(close);
        box.append(top);
        String text = store.openText();
        if (text == null) box.append(cell("filesnote", tr("files.reading")));
        else if (text.isEmpty()) box.append(cell("filesnote", tr("files.empty_file")));
        else {
            HTMLElement pre = el("pre");
            pre.className = "filebody";
            pre.textContent = text;
            box.append(pre);
        }
        return box;
    }

    // ── git ──────────────────────────────────────────────────────────────────

    private HTMLElement gitCard() {
        HTMLElement card = cell("filescard pane-git", null);
        card.append(head(tr("git.section")));
        HTMLElement body = cell("panebody", null);
        JsPropertyMap<Object> g = Js.uncheckedCast(store.git());
        if (g == null) {
            // 닿지 못한 것과 저장소가 아닌 것은 다른 사실이다 — 둘 다 말한다(운영 규칙).
            body.append(cell("filesnote", tr(store.walked() ? "git.unreachable" : "git.reading")));
            card.append(body);
            return card;
        }
        if (!Js.isTruthy(g.get("repo"))) {
            body.append(cell("filesnote", tr("git.not_a_repo")));
            card.append(body);
            return card;
        }
        HTMLElement inner = cell("gitinner", null);
        HTMLElement top = cell("gittop", null);
        top.append(Icons.orGlyph("#i-sl-layer-group", "\u2387", "gitmark"));
        String branch = str(g, "branch");
        top.append(cell("gitbranch", branch.isEmpty() ? tr("git.detached") : branch));
        double ahead = num(g, "ahead"), behind = num(g, "behind");
        if (ahead > 0) top.append(cell("gitab ahead", "\u2191" + (int) ahead));
        if (behind > 0) top.append(cell("gitab behind", "\u2193" + (int) behind));
        inner.append(top);
        if (May.can("shell")) inner.append(branchActs(g));
        JsArrayLike<Object> changes = Js.uncheckedCast(g.get("changes"));
        int n = changes == null ? 0 : changes.getLength();
        if (n == 0) {
            inner.append(cell("gitclean", tr("git.clean")));
            body.append(inner);
            card.append(body);
            return card;
        }
        // 커밋이 실어 갈 것과 남길 것 — 편집기의 git 판이 나누는 그 두 무리(운영 규칙).
        appendGroup(inner, "git.group_staged", changes, true);
        appendGroup(inner, "git.group_changed", changes, false);
        if (May.can("shell")) {
            boolean anyStaged = false;
            for (int i = 0; i < changes.getLength(); i++) {
                if (Tree.staged(str(Js.uncheckedCast(changes.getAt(i)), "kind"))) { anyStaged = true; break; }
            }
            inner.append(commitRow(anyStaged));
        }
        body.append(inner);
        card.append(body);
        return card;
    }

    private void appendGroup(HTMLElement into, String key, JsArrayLike<Object> changes, boolean staged) {
        boolean any = false;
        for (int i = 0; i < changes.getLength(); i++) {
            JsPropertyMap<Object> c = Js.uncheckedCast(changes.getAt(i));
            if (Tree.staged(str(c, "kind")) != staged) continue;
            if (!any) { into.append(cell("gitgroup", tr(key))); any = true; }
            into.append(gitLine(c));
        }
    }

    private HTMLElement gitLine(JsPropertyMap<Object> c) {
        HTMLElement line = cell("gitline", null);
        HTMLElement row = el("button");
        row.setAttribute("type", "button");
        row.className = "treerow gitrow state " + str(c, "kind");
        row.append(cell("gitmarkk", str(c, "status")), cell("treename", str(c, "path")));
        final String path = str(c, "path");
        row.addEventListener("click", evt -> store.openFile(path));
        line.append(row);
        if (!May.can("shell")) return line;
        HTMLElement acts = cell("gitacts", null);
        // 커밋이 실어 갈 것과 남길 것 사이를 옮기는 일 — 어느 쪽인지는 행이 이미 말한다.
        boolean staged = Tree.staged(str(c, "kind"));
        HTMLElement move = el("md-text-button");
        move.className = "act " + (staged ? "unstage" : "stage");
        move.textContent = tr(staged ? "git.unstage" : "git.stage");
        move.setAttribute("aria-label", tr(staged ? "git.unstage" : "git.stage") + " — " + path);
        move.addEventListener("click", evt -> {
            evt.stopPropagation();
            store.gitDo(staged ? "unstage" : "stage", path, null);
        });
        // 되돌리기는 타이핑을 잃는 일이라 두 번 묻는다(지우기와 같은 규칙, 다른 물음).
        HTMLElement discard = el("md-text-button");
        discard.className = "act discard";
        discard.setAttribute("aria-label", tr("git.discard") + " — " + path);
        arm(discard, tr("git.discard"), () -> store.gitDo("discard", path, null));
        acts.append(move, discard);
        line.append(acts);
        return line;
    }

    /** 브랜치에 하는 일 — 가는 것(전환), 새로 내는 것, 그리고 오가는 것(pull·push). */
    private HTMLElement branchActs(JsPropertyMap<Object> g) {
        HTMLElement box = cell("gitbranchacts", null);
        JsArrayLike<Object> names = Js.uncheckedCast(g.get("branches"));
        String here = str(g, "branch");
        if (names != null && names.getLength() > 1 && !here.isEmpty()) {
            HTMLElement pick = el("md-outlined-select");
            pick.className = "gitpick";
            pick.setAttribute("label", tr("git.branch"));
            for (int i = 0; i < names.getLength(); i++) {
                String name = String.valueOf(names.getAt(i));
                HTMLElement o = el("md-select-option");
                o.setAttribute("value", name);
                if (name.equals(here)) o.setAttribute("selected", "");
                HTMLElement h = el("div");
                h.setAttribute("slot", "headline");
                h.textContent = name;
                o.append(h);
                pick.append(o);
            }
            // 전환은 발밑의 파일을 전부 바꾼다 — 화살표로 메뉴를 훑다 일어나면 안 되는 일이라
            // 확인을 거친다(운영 규칙).
            pick.addEventListener("change", evt -> {
                String to = value(pick);
                if (to.isEmpty() || to.equals(here)) return;
                if (DomGlobal.window.confirm(tr("git.switch_body"))) store.gitDo("switch", null, to);
                else Js.asPropertyMap(pick).set("value", here);
            });
            box.append(pick);
        }
        HTMLElement neu = el("md-text-button");
        neu.className = "act newbranch";
        neu.textContent = tr("git.new_branch");
        neu.addEventListener("click", evt -> {
            String name = DomGlobal.window.prompt(tr("git.new_branch_who"), "");
            if (name != null && !name.trim().isEmpty()) store.gitDo("new-branch", null, name.trim());
        });
        box.append(neu);
        if (Js.isTruthy(g.get("upstream"))) {
            HTMLElement pull = el("md-text-button");
            pull.className = "act pull";
            pull.textContent = tr("git.pull");
            pull.addEventListener("click", evt -> store.gitDo("pull", null, null));
            box.append(pull);
        }
        if (Js.isTruthy(g.get("upstream")) || num(g, "ahead") > 0) {
            HTMLElement push = el("md-text-button");
            push.className = "act push";
            push.textContent = tr("git.push");
            push.addEventListener("click", evt -> store.gitDo("push", null, null));
            box.append(push);
        }
        return box;
    }

    /** 커밋 — 실린 것이 있을 때만 눌린다. 메시지 없이 커밋하지 않는다. */
    private HTMLElement commitRow(boolean anyStaged) {
        HTMLElement box = cell("gitcommit", null);
        HTMLElement msg = el("md-outlined-text-field");
        msg.id = "gitmsg";
        msg.setAttribute("label", tr("git.commit_who"));
        msg.setAttribute("type", "textarea");
        msg.setAttribute("rows", "1");
        HTMLElement go = el("md-filled-tonal-button");
        go.id = "gitcommitgo";
        go.textContent = tr("git.commit");
        if (!anyStaged) go.setAttribute("disabled", "");
        go.addEventListener("click", evt -> {
            String m = value(msg).trim();
            if (m.isEmpty() || !anyStaged) return;
            store.gitDo("commit", null, m);
            Js.asPropertyMap(msg).set("value", "");
        });
        box.append(msg, go);
        return box;
    }

    // ── 잔손 ─────────────────────────────────────────────────────────────────

    private static HTMLElement head(String title) {
        HTMLElement h = el("button");
        h.setAttribute("type", "button");
        h.className = "panehead state";
        h.setAttribute("aria-expanded", "true");
        h.append(Icons.orGlyph("#i-sl-chevron-down", "\u25BE", "panecaret"), cell("panetitle", title));
        return h;
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

    private static double num(JsPropertyMap<Object> m, String k) {
        Object v = m.get(k);
        return v == null ? 0 : Js.coerceToDouble(v);
    }

    private static HTMLElement cell(String cls, String text) {
        HTMLElement d = el("div");
        d.className = cls;
        if (text != null) d.textContent = text;
        return d;
    }

    private static HTMLElement el(String tag) { return Js.uncheckedCast(DomGlobal.document.createElement(tag)); }
}
