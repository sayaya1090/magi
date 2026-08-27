package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.Icons;
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
 * — console.css가 입힌다. 잔여: 우클릭 메뉴·검색·커밋·브랜치 전환(전부 shell 능력의 것).
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
        return row;
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
        return line;
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
