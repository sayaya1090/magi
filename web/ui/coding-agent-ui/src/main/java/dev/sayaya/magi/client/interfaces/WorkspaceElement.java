package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CardSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.May;
import dev.sayaya.magi.bridge.Windows;
import dev.sayaya.magi.client.domain.Code;
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
    private final Dialogs dialogs;
    private final HTMLElement root = el("div");
    private boolean wired = false;

    @Inject
    public WorkspaceElement(WorkspaceStore store, CompanionStore companion, Dialogs dialogs) {
        this.store = store;
        this.dialogs = dialogs;
        root.id = "files";
        companion.onContext(store::aim);
    }

    public void mount(HTMLElement frame) {
        frame.replaceChildren(root);
        if (wired) return;
        wired = true;
        store.subscribe(this::render);
        // 부모가 다른 탭으로 옮기면 트리의 표시도 따라간다.
        CardSharing.onShowing(this::render);
    }

    /** 폰의 작업공간이 지금 보이는 것 — 트리("files")냐 git이냐. 넓은 화면에서는 둘 다 선다. */
    private String shows = "files";

    private void render() {
        root.replaceChildren();
        // 한 기둥이면 한 번에 하나다(운영의 그 규칙): 마흔 개 이름 아래에 깔린 git 판은 아무도
        // 스크롤해 내려가지 않고, 그 판의 행동들은 손끝이 닿을 자리에 있지도 않다.
        // console.css가 #files[data-shows]로 그 감춤을 맡는다 — 여기서는 무엇을 보이는지만 적는다.
        if (Windows.onePane()) {
            root.setAttribute("data-shows", shows);
            if ("git".equals(shows)) {
                root.append(backRow(tr("nav.files_short"), () -> { shows = "files"; render(); }), gitCard());
            } else {
                root.append(treeCard(), gitRow(), gitCard());
            }
        } else {
            root.removeAttribute("data-shows");
            root.append(treeCard(), gitCard());
        }
        publishCards();
    }

    /** 한 기둥일 때 git으로 가는 줄 — 무엇이 뒤에 있는지(브랜치·바뀐 수)를 이고 있다. */
    private HTMLElement gitRow() {
        HTMLElement list = cell("panelist", null);
        HTMLElement row = el("button");
        row.setAttribute("type", "button");
        row.className = "panelrow state";
        row.append(Icons.orGlyph("#i-sl-clock-rotate-left", "", "panelmark"));
        row.append(cell("panelword", tr("git.section")));
        JsPropertyMap<Object> g = store.git() == null ? null : Js.uncheckedCast(store.git());
        if (g != null && Js.isTruthy(g.get("repo"))) {
            String branch = str(g, "branch");
            if (branch.isEmpty() && !str(g, "head").isEmpty()) branch = "@" + str(g, "head");
            JsArrayLike<Object> changes = Js.uncheckedCast(g.get("changes"));
            int n = changes == null ? 0 : changes.getLength();
            String said = branch;
            if (n > 0) said = (said.isEmpty() ? "" : said + " \u00B7 ") + tr("git.n_changed", "n", String.valueOf(n));
            if (!said.isEmpty()) row.append(cell("panelcount", said));
        }
        row.append(Icons.orGlyph("#i-sl-chevron-right", "\u203A", "panelgo"));
        row.addEventListener("click", evt -> { shows = "git"; render(); });
        list.append(row);
        return list;
    }

    /** 돌아가는 줄 — 낱말은 <b>가는 곳</b>이고, 읽히는 이름은 그것이 하는 일이다(운영 panelBack). */
    private HTMLElement backRow(String word, Runnable go) {
        HTMLElement box = cell("panelback", null);
        HTMLElement b = el("md-text-button");
        b.textContent = word;
        Icons.mark(b, "#i-sl-chevron-left");
        b.setAttribute("aria-label", tr("action.back_to", "name", word));
        b.addEventListener("click", evt -> go.run());
        box.append(b);
        return box;
    }

    // ── 트리 ─────────────────────────────────────────────────────────────────

    /**
     * 판의 머리 — 접는 제목과, (파일 판에만) 다시 읽는 문. 운영 paneCard의 .panerow 그대로.
     *
     * 다시 읽는 문이 git에 없는 이유: git 상태는 트리를 걸을 때 함께 온다 — 판마다 같은 걸음을
     * 청하는 버튼을 두면 무엇이 무엇을 새로 읽는지가 흐려진다(운영도 파일 판에만 둔다).
     */
    private HTMLElement paneRow(HTMLElement card, String key, String title, Runnable again) {
        HTMLElement row = cell("panerow", null);
        row.append(head(card, key, title));
        if (again == null) return row;
        HTMLElement b = el("md-icon-button");
        b.className = "paneagain";
        b.append(Icons.orGlyph("#i-sl-arrows-rotate", "\u21BB", "sic"));
        b.setAttribute("aria-label", tr("files.again"));
        b.setAttribute("title", tr("files.again"));
        // 접는 버튼 안이 아니라 옆이다 — 컨트롤 속의 컨트롤은 어디를 눌렀느냐로 두 가지 일 중
        // 하나를 하는 누름이 된다.
        b.addEventListener("click", evt -> { evt.stopPropagation(); again.run(); });
        row.append(b);
        return row;
    }

    /** 판의 머리에 적히는 이름 — 규칙은 도메인의 것(Tree.shortPath), 빈 자리의 말만 여기서. */
    private static String shortPath(String path) {
        String name = Tree.shortPath(path);
        return name.isEmpty() ? tr("nav.files") : name;
    }

    private HTMLElement treeCard() {
        HTMLElement card = cell("filescard pane-files", null);
        // 제목은 <b>어느 작업공간인가</b>이다 — 화면에 판이 하나뿐이라 "파일"이라는 말은
        // 아무것도 더해 주지 않고, 두 컴패니언을 오갈 때 바뀌는 것은 경로다(운영 규칙).
        card.append(paneRow(card, "files", shortPath(store.workdir()), store::walk));
        HTMLElement body = cell("panebody", null);
        body.append(findRow());
        // 찾는 동안 판이 보이는 것은 결과다 — 트리로 되돌리지 않는다(운영에서 배운 그 결함).
        if (store.finding()) {
            body.append(hitRows());
            card.append(body);
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
        card.append(body);
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

    /**
     * 찾기 — 누르면 묻는다. 상자를 늘 펼쳐 두지 않는 이유는 운영과 같다: 이 기둥은 좁고,
     * 늘 서 있는 입력은 "여기에 쳐라"라고 말하지만 이 판이 먼저 하는 말은 트리다.
     */
    private HTMLElement findRow() {
        HTMLElement box = cell("filefind", null);
        if (!store.finding()) {
            HTMLElement open = el("md-text-button");
            open.append(Icons.orGlyph("#i-sl-magnifying-glass", "⌕", "mk"),
                    DomGlobal.document.createTextNode(" " + tr("files.find")));
            open.addEventListener("click", evt -> ask());
            box.append(open);
            return box;
        }
        // 찾는 중이면 무엇을 찾았는지 말하고, 다시 찾기와 지우기를 준다.
        box.append(cell("findnow", tr("text".equals(store.where()) ? "files.found_in_text"
                : "files.found_in_names", "q", store.query())));
        box.append(cell("findacts", null));
        HTMLElement again = el("md-text-button");
        again.append(Icons.orGlyph("#i-sl-magnifying-glass", "⌕", "mk"),
                DomGlobal.document.createTextNode(" " + tr("files.find_again")));
        again.addEventListener("click", evt -> ask());
        HTMLElement clear = el("md-text-button");
        clear.append(Icons.orGlyph("#i-sl-xmark", "✕", "mk"),
                DomGlobal.document.createTextNode(" " + tr("files.find_clear")));
        clear.addEventListener("click", evt -> store.query(""));
        box.append(again, clear);
        return box;
    }

    /** 무엇을 어디서 찾을지 묻는다 — 이름인지 내용인지는 고르는 것이지 짐작할 일이 아니다. */
    private void ask() {
        dialogs.line(tr("files.find"), tr("files.find_who"), tr("files.find"), store.query(),
                new String[][]{{"name", "files.by_name"}, {"text", "files.by_text"}},
                store.where(), (said, where) -> {
                    store.where(where);
                    store.query(said);
                });
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

    private HTMLElement treeRow(JsPropertyMap<Object> e, String path, String name, boolean isDir, int depth) {
        HTMLElement row = el("button");
        row.setAttribute("type", "button");
        // 골라진 것은 <b>지금 보이는</b> 그 파일 하나다 — 여러 개를 열어 둘 수 있으니(탭),
        // 열려 있다는 것과 보고 있다는 것은 다른 사실이다(운영도 탭과 행에 같은 표시를 준다).
        boolean here = path.equals(CardSharing.showing());
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
        line.append(row, rowMenu(path, name, isDir));
        return line;
    }

    /**
     * 한 행에 할 수 있는 일들 — 운영과 같이 <b>메뉴 하나</b>다.
     *
     * 버튼을 늘어놓지 않는 이유: 이 기둥은 18rem이고 할 일은 여섯이다(새 파일·새 폴더·이름
     * 바꾸기·경로 복사·되돌리기·삭제). 늘 보이는 버튼 둘은 나머지 넷을 갈 곳 없게 만들고,
     * 그 둘마저 이름을 자른다.
     */
    private HTMLElement rowMenu(String path, String name, boolean isDir) {
        HTMLElement box = cell("rowmenu", null);
        HTMLElement open = el("md-icon-button");
        open.id = "rm" + (++menuCount);
        open.append(Icons.orGlyph("#i-sl-sliders", "⋯", "mk"));
        open.setAttribute("aria-label", tr("files.more_named", "name", name));
        open.setAttribute("title", tr("files.more"));
        HTMLElement menu = el("md-menu");
        menu.setAttribute("anchor", open.id);
        // 이 기둥은 제 안에서 구르는 상자다 — 메뉴를 그 안에 두면 판의 경계에서 잘리고, 가까운
        // 위치 상자를 기준으로 놓여 버튼과 한참 떨어진 데 그려진다(운영이 실측한 그 결함:
        // 첫 항목이 16px 밖에 나가 눌리지도 않았다). popover면 페이지의 상자들 밖으로 나가고,
        // 그 API가 없는 브라우저에서는 fixed가 잘림만이라도 벗어난다.
        menu.setAttribute("positioning", canPopover() ? "popover" : "fixed");
        // 이 버튼은 행에 손끝이 올라와 있을 때만 보이는 상자의 자식이다 — 메뉴로 손을 옮기면 그
        // 행을 떠나 상자가 숨고, 열려 있던 메뉴까지 함께 사라졌다. 열려 있는 동안은 세워 둔다.
        menu.addEventListener("opening", evt -> box.classList.add("showing"));
        menu.addEventListener("closed", evt -> box.classList.remove("showing"));
        // 이 행 아래에 만드는 것이 자연스럽다: 디렉토리면 그 안, 파일이면 그 옆.
        String under = isDir ? path + "/" : (path.contains("/") ? path.substring(0, path.lastIndexOf('/') + 1) : "");
        item(menu, "files.new_file", "#i-sl-file-plus", () ->
                dialogs.line(tr("files.new_file"), tr("files.new_file_who"), tr("files.new_file"),
                        under, null, null, (said, ignored) -> store.fileDo("new-file", said, null)));
        item(menu, "files.new_dir", "#i-sl-folder-plus", () ->
                dialogs.line(tr("files.new_dir"), tr("files.new_dir_who"), tr("files.new_dir"),
                        under, null, null, (said, ignored) -> store.fileDo("new-dir", said, null)));
        item(menu, "files.rename", "#i-sl-pen", () ->
                dialogs.line(tr("files.rename"), tr("files.rename_who"), tr("files.rename"),
                        path, null, null, (said, ignored) -> {
                            if (!said.equals(path)) store.fileDo("rename", path, said);
                        }));
        item(menu, "files.copy_path", "#i-sl-copy", () -> copy(path));
        item(menu, "git.discard", "#i-sl-eraser", () ->
                dialogs.confirm(tr("git.discard_head", "path", path), tr("git.discard_body"),
                        tr("git.discard"), () -> store.gitDo("discard", path, null)));
        item(menu, "files.delete", "#i-sl-trash-can", () ->
                dialogs.confirm(tr("files.delete_head", "path", path), tr("files.delete_body"),
                        tr("files.delete"), () -> store.fileDo("delete", path, null)));
        open.addEventListener("click", evt -> {
            evt.stopPropagation();
            Js.asPropertyMap(menu).set("open", !Js.isTruthy(Js.asPropertyMap(menu).get("open")));
        });
        box.append(open, menu);
        return box;
    }

    private int menuCount = 0;

    private void item(HTMLElement menu, String key, String mark, Runnable run) {
        HTMLElement it = el("md-menu-item");
        HTMLElement head = el("div");
        head.setAttribute("slot", "headline");
        head.textContent = tr(key);
        it.append(head);
        elemental2.dom.Element g = Icons.of(mark, null);
        if (g != null) { g.setAttribute("slot", "start"); it.append(g); }
        it.addEventListener("click", evt -> run.run());
        menu.append(it);
    }

    /** 경로를 클립보드로 — 터미널로 옮겨 적는 일이 이 판에서 가장 잦은 다음 행동이라서. */
    private static native boolean canPopover() /*-{
        return typeof $wnd.HTMLElement === 'function'
            && typeof $wnd.HTMLElement.prototype.showPopover === 'function';
    }-*/;

    private static native void copy(String text) /*-{
        if ($wnd.navigator.clipboard) $wnd.navigator.clipboard.writeText(text);
    }-*/;

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
    /**
     * 연 파일은 <b>가운데의 카드</b>로 간다 — 18rem 기둥은 코드를 읽는 폭이 아니다.
     *
     * 그 자리는 부모의 것이라(사실판과 한 줄을 나눠 쓴다) 여기서 그리지 않고 등록한다:
     * 무엇이 열려 있는지는 이 화면이 알고, 그 중 무엇을 보일지는 탭 줄이 정한다.
     */
    private void publishCards() {
        java.util.List<String> paths = store.openPaths();
        Object[] cards = new Object[paths.size()];
        for (int i = 0; i < paths.size(); i++) {
            String key = paths.get(i);
            cards[i] = WorkspaceStore.isDiff(key) ? diffCard(key) : fileCard(key);
        }
        CardSharing.provide(cards);
    }

    private static String baseName(String path) {
        int slash = path.lastIndexOf('/');
        return slash >= 0 && slash < path.length() - 1 ? path.substring(slash + 1) : path;
    }

    // ── 연 파일: 운영 drawFile의 이식 ───────────────────────────────────────
    //
    // 자리는 부모가 준 카드 상자(#fileview)다 — 운영도 그 id의 한 요소를 파일·디프·커밋이
    // 돌려 쓴다. 그래서 여기서 감싸는 상자를 새로 두지 않고 그 안에 바로 그린다: console.css의
    // #fileview .foldwrap / .filebody 규칙이 그 구조를 그대로 입는다.

    private boolean folded = false;              // 접어 두었는가 — 사실판과 같은 손잡이
    private String editing = null;               // 편집 중인 경로(하나뿐이다)
    private final java.util.Map<String, String> drafts = new java.util.HashMap<>();
    private String said = "";                    // 거부 사유 — 이 버퍼에 대한 말이라 버퍼 위에 선다

    /** 이 파일의 카드 속 — 머리 줄(경로·손잡이·행동)과, 접히는 본문. */
    private HTMLElement fileCard(String path) {
        String text = store.textOf(path);
        HTMLElement box = el("div");
        // 노드가 제 이름과 신원을 진다(카드 계약): id는 무엇인가, title은 탭에 적히는 짧은 이름.
        box.id = path;
        box.setAttribute("title", baseName(path));
        // 배치에서는 없는 상자다 — 운영의 #fileview는 바와 접힘을 <b>직계</b>로 두고, 그 사이에
        // 상자가 하나 끼면 그 자리의 여백 규칙이 조용히 비켜간다.
        box.style.setProperty("display", "contents");
        CardSharing.closable(box, () -> store.closeFile(path));
        if (folded) box.setAttribute("folded", ""); else box.removeAttribute("folded");
        HTMLElement bar = cell("filebar", null);
        // 이 카드가 트리 자리에 혼자 서 있으면(폰) 돌아갈 문이 그 머리 줄에 선다 — 어디로
        // 돌아가는지는 부모가 알고, 여기서는 문만 세운다.
        if (CardSharing.alone()) bar.append(backToList());
        bar.append(foldCaret(box, path));
        // 경로는 <b>통째로</b>다 — 이름은 탭이 이미 말했고, 사람이 복사해 명령에 붙이는 것은
        // 이 줄이다. 반쪽 경로는 아무도 붙여 넣을 수 없다(운영의 그 이유).
        bar.append(cell("filedir", path));
        HTMLElement acts = cell("fileacts", null);
        bar.append(acts);
        boolean reading = text != null;
        // 고치기는 <b>할 수 있는 사람</b>에게만 보인다 — 서버는 어차피 거절하고, 403을 답하는
        // 버튼은 사람들이 누르지 않는 법을 배운다. 문은 shell이다: 그 워크스페이스에서 명령을
        // 돌릴 수 있으면 이미 어느 파일이든 쓸 수 있다.
        if (May.can("shell") && reading && !path.equals(editing)) {
            HTMLElement go = el("md-text-button");
            go.append(Icons.orGlyph("#i-sl-pen-to-square", "\u270E", "sic"));
            go.textContent = tr("action.edit");
            go.addEventListener("click", evt -> { editing = path; said = ""; publishCards(); });
            acts.append(go);
        }
        box.append(bar);
        if (path.equals(editing) && reading) { box.append(editor(path, text, acts)); return box; }
        HTMLElement wrap = cell("foldwrap", null);
        HTMLElement body = cell("filebody", null);
        if (text == null) body.append(cell("filesnote", tr("files.reading")));
        else if (text.isEmpty()) body.append(cell("filesnote", tr("file.empty")));
        else read(body, path, text);
        wrap.append(body);
        box.append(wrap);
        return box;
    }

    /**
     * 차이 하나 — 파일 본문과 같은 자리에 서는 다른 카드다. 색은 줄의 첫 글자가 정한다(운영
     * diffLineClass): 파서가 아니라 표시다.
     */
    private HTMLElement diffCard(String key) {
        String path = WorkspaceStore.diffPath(key), which = WorkspaceStore.diffWhich(key);
        String text = store.textOf(key);
        HTMLElement box = el("div");
        box.id = key;
        box.setAttribute("title", baseName(path) + " \u00B1");
        box.style.setProperty("display", "contents");
        CardSharing.closable(box, () -> store.closeFile(key));
        HTMLElement bar = cell("filebar", null);
        if (CardSharing.alone()) bar.append(backToList());
        bar.append(cell("filedir", path + "  \u00B7  " + tr("staged".equals(which) ? "diff.staged"
                : "untracked".equals(which) ? "diff.untracked" : "diff.unstaged")));
        box.append(bar);
        if (text == null) { box.append(cell("filesnote", tr("files.reading"))); return box; }
        if (text.trim().isEmpty()) { box.append(cell("filesnote", tr("diff.same"))); return box; }
        // 이 판은 번호 기둥이 없다 — 스크롤 상자도 그 격자 없이 한 덩어리다(운영 .diffscroll).
        HTMLElement wrap = cell("filebody diffscroll", null);
        HTMLElement body = el("pre");
        body.className = "filecode diffbody";
        for (String line : text.split("\n", -1)) {
            HTMLElement row = el("span");
            row.className = diffClass(line);
            row.textContent = line + "\n";
            body.append(row);
        }
        wrap.append(body);
        box.append(wrap);
        return box;
    }

    /** 거울에 한 조각 — 표시가 없으면 그냥 글자다. */
    private static void emit(HTMLElement into, String text, String cls) {
        if (text.isEmpty()) return;
        if (cls == null) { into.append(DomGlobal.document.createTextNode(text)); return; }
        HTMLElement m = el("span");
        m.className = cls;
        m.textContent = text;
        into.append(m);
    }

    /** 모델이 내민 이어쓰기의 모양 — 캐럿이 그 앞에 서 있는 흐린 글자(운영 .editcomplete). */
    private static HTMLElement ghostSpan(String text) {
        HTMLElement g = el("span");
        g.className = "editcomplete";
        g.textContent = text;
        return g;
    }

    private static native int caretOf(HTMLElement area) /*-{
        return area.selectionStart == null ? -1 : area.selectionStart;
    }-*/;

    private static native void setCaret(HTMLElement area, int at) /*-{
        area.selectionStart = area.selectionEnd = at;
    }-*/;

    /** 탭 한 번 — execCommand로 넣는다: value에 대입하면 되돌리기 더미가 통째로 지워진다. */
    private static native void insertTab(HTMLElement area) /*-{
        if ($doc.execCommand) $doc.execCommand('insertText', false, '\t');
    }-*/;

    /**
     * 입력기가 글자를 만드는 중인가 — Tab과 Enter는 입력기의 키이기도 하다. 묻지 않고 가로채면
     * 한글 음절을 맺는 순간 탭이 끼어든다(운영 composing()과 같은 이유).
     */
    private static native boolean composing(elemental2.dom.KeyboardEvent e) /*-{
        return !!(e.isComposing || e.keyCode === 229);
    }-*/;

    private static String diffClass(String line) {
        if (line.startsWith("diff --git ") || line.startsWith("index ") || line.startsWith("--- ")
                || line.startsWith("+++ ") || line.startsWith("old mode ") || line.startsWith("new mode ")
                || line.startsWith("deleted file ") || line.startsWith("new file ") || line.startsWith("rename ")
                || line.startsWith("similarity ") || line.startsWith("copy ") || line.startsWith("Binary ")) {
            return "dl dfile";
        }
        char c = line.isEmpty() ? ' ' : line.charAt(0);
        return "dl" + (c == '+' ? " add" : c == '-' ? " cut" : c == '@' ? " at" : "");
    }

    /** 트리로 돌아가는 문 — 카드가 혼자 선 자리(폰)에서만. */
    private HTMLElement backToList() {
        HTMLElement back = el("md-text-button");
        back.className = "fileback";
        back.append(Icons.orGlyph("#i-sl-chevron-left", "\u2039", "sic"));
        back.textContent = tr("nav.files");
        back.setAttribute("aria-label", tr("action.back_to", "name", tr("nav.files")));
        back.addEventListener("click", evt -> CardSharing.toList());
        return back;
    }

    /** 접는 손잡이 — 열린 파일은 안 읽는 동안에도 화면의 60vh다(운영이 단 그 이유). */
    private HTMLElement foldCaret(HTMLElement box, String path) {
        HTMLElement caret = el("button");
        caret.setAttribute("type", "button");
        caret.className = "foldcaret hit48";
        caret.setAttribute("aria-expanded", folded ? "false" : "true");
        // 무엇을 접는지로 이름 짓는다 — 옆 판의 제목을 달고 있으면 다른 판을 접는 것처럼 읽힌다.
        caret.setAttribute("aria-label", tr("action.fold_named", "name", path));
        caret.append(Icons.orGlyph("#i-sl-chevron-down", "\u25BE", "caret"));
        caret.addEventListener("click", evt -> { folded = !folded; publishCards(); });
        return caret;
    }

    /**
     * 읽는 그림 — 번호 기둥과 본문 기둥, 한 상자가 함께 구른다.
     *
     * 번호를 다시 매기지도 지우지도 않는 이유: 사람과 컴패니언이 서로 다른 40행을 가리키는 것이
     * 그 정리의 값이다(운영 주석). 기둥으로 가르는 것은 끌어 복사할 때 번호가 딸려오지 않게.
     */
    private void read(HTMLElement body, String path, String text) {
        HTMLElement nums = el("pre");
        nums.className = "filegutter";
        // 스크린 리더에서 숨긴다 — 줄마다 끼는 맨 숫자 기둥은 건너뛸 수 없는 잡음이다.
        nums.setAttribute("aria-hidden", "true");
        nums.textContent = Code.gutter(text);
        HTMLElement code = el("pre");
        code.className = "filecode";
        String comment = Code.commentMark(path);
        for (String line : text.split("\n", -1)) {
            for (Code.Part part : Code.parts(Code.bodyOf(line), comment)) {
                if (part.cls == null) { code.append(DomGlobal.document.createTextNode(part.text)); continue; }
                HTMLElement m = el("span");
                m.className = part.cls;
                m.textContent = part.text;
                code.append(m);
            }
            code.append(DomGlobal.document.createTextNode("\n"));
        }
        body.append(nums, code);
    }

    /**
     * 고치는 그림 — <b>같은 그림에 캐럿이 있는 것</b>.
     *
     * 색은 글자 <i>아래</i> 놓인 사본에 칠한다: textarea는 문자열 하나를 들 뿐 색 있는 조각을
     * 들지 못한다. 필드를 투명하게 하고 같은 활자·같은 크기의 pre를 밑에 깔면, 최악의 경우가
     * "완벽히 읽히는 캐럿 뒤로 색이 한 픽셀 어긋난 것"이다(운영이 고른 그 타협).
     */
    private HTMLElement editor(String path, String text, HTMLElement acts) {
        HTMLElement box = cell("fileedit", null);
        HTMLElement note = cell("filesnote editsaid", said);
        if (said.isEmpty()) note.setAttribute("hidden", "");
        HTMLElement area = el("textarea");
        area.className = "fileeditarea";
        area.setAttribute("spellcheck", "false");
        area.setAttribute("wrap", "off");
        area.setAttribute("aria-label", path);
        String opened = Code.plainText(text);
        // 초고가 먼저다 — 반쯤 고치다 온 파일로 돌아오는 것은 그 반쯤으로 돌아오는 것이다.
        String start = drafts.containsKey(path) ? drafts.get(path) : opened;
        Js.asPropertyMap(area).set("value", start);
        HTMLElement behind = el("pre");
        behind.className = "filecode editghost";
        behind.setAttribute("aria-hidden", "true");
        HTMLElement nums = el("pre");
        nums.className = "filegutter";
        nums.setAttribute("aria-hidden", "true");
        // 모델이 내민 이어쓰기 — 버퍼가 아니라 <b>거울</b>에만 산다: 사람이 Tab으로 가져가거나
        // 그냥 타이핑해 지나가면 사라지는 글이라, 필드의 값에 넣는 순간 그것은 남의 글이 된다.
        final String[] ghost = {""};
        final int[] ghostAt = {-1};
        Runnable repaint = () -> {
            String src = String.valueOf(Js.asPropertyMap(area).get("value"));
            behind.replaceChildren();
            String comment = Code.commentMark(path);
            StringBuilder g = new StringBuilder();
            int n = 0;
            int pos = 0;
            boolean[] placed = {false};
            int at = ghost[0].isEmpty() ? -1 : Math.min(ghostAt[0], src.length());
            // 줄 단위로 훑는다 — 주석은 <b>그 줄</b> 끝까지이고, 버퍼를 통째로 넘기면 파일의 첫
            // `//`가 그 뒤 전부를 삼킨다(운영이 밟은 그 결함).
            for (String line : src.split("\n", -1)) {
                g.append(++n).append('\n');
                int col = pos;
                for (Code.Part part : Code.parts(line, comment)) {
                    String t = part.text;
                    if (at >= col && at <= col + t.length() && !placed[0]) {
                        emit(behind, t.substring(0, at - col), part.cls);
                        behind.append(ghostSpan(ghost[0]));
                        placed[0] = true;
                        emit(behind, t.substring(at - col), part.cls);
                    } else {
                        emit(behind, t, part.cls);
                    }
                    col += t.length();
                }
                if (!placed[0] && at == col) { behind.append(ghostSpan(ghost[0])); placed[0] = true; }
                behind.append(DomGlobal.document.createTextNode("\n"));
                pos = col + 1;
            }
            nums.textContent = g.toString();
        };
        HTMLElement save = el("md-filled-button");
        save.append(Icons.orGlyph("#i-sl-floppy-disk", "\u2913", "sic"));
        save.textContent = tr("action.save");
        save.addEventListener("click", evt -> {
            Js.asPropertyMap(save).set("disabled", true);
            String now = String.valueOf(Js.asPropertyMap(area).get("value"));
            store.save(path, opened, now, why -> {
                Js.asPropertyMap(save).set("disabled", false);
                if (why != null && !why.isEmpty()) {
                    // 여기서의 거부는 대개 파일이 움직인 것이다 — 열어 둔 사이 컴패니언이 고쳤다.
                    said = why;
                    publishCards();
                    return;
                }
                said = "";
                editing = null;
                drafts.remove(path);
                // 그린 것이 아니라 <b>다시 읽은 것</b>을 보여 준다: 디스크의 그 파일이 사실이고,
                // 툴이 조금 다르게 썼을 수 있다(마지막 개행 같은 것).
                store.openFile(path);
            });
        });
        HTMLElement stop = el("md-text-button");
        stop.append(Icons.orGlyph("#i-sl-xmark", "\u2715", "sic"));
        stop.textContent = tr("action.cancel");
        stop.addEventListener("click", evt -> {
            editing = null;
            said = "";
            drafts.remove(path);
            publishCards();
        });
        final double[] tick = {-1};
        final int[] asked = {0};
        Runnable dismiss = () -> { if (!ghost[0].isEmpty()) { ghost[0] = ""; repaint.run(); } };
        Runnable complete = () -> {
            if (!May.can("prompt")) return;
            int caret = caretOf(area);
            if (caret < 0) return;
            String all = String.valueOf(Js.asPropertyMap(area).get("value"));
            String prefix = all.substring(0, Math.min(caret, all.length()));
            String suffix = all.substring(Math.min(caret, all.length()));
            if (prefix.trim().isEmpty() && suffix.trim().isEmpty()) return;
            final int mine = ++asked[0];
            store.complete(path, prefix, suffix, said -> {
                if (mine != asked[0]) return;           // 더 새 요청이 이것을 앞질렀다
                if (caretOf(area) != caret) return;     // 기다리는 사이 캐럿이 움직였다
                ghost[0] = said == null ? "" : said;
                ghostAt[0] = caret;
                repaint.run();
            });
        };
        area.addEventListener("input", evt -> {
            String now = String.valueOf(Js.asPropertyMap(area).get("value"));
            drafts.put(path, now);
            dismiss.run();
            repaint.run();
            // 타이핑이 멎으면 묻는다 — 글자마다 물으면 백엔드를 타이핑 속도로 태운다.
            if (tick[0] >= 0) DomGlobal.clearTimeout(tick[0]);
            tick[0] = DomGlobal.setTimeout(a -> complete.run(), 350);
            // 컴패니언의 곁사본도 따라간다 — 아직 디스크에 없는 그 편집에 대해 답할 수 있게.
            store.openFileHint(path, now);
        });
        // 캐럿이 움직이면 그 자리의 이어쓰기는 낡은 것이다 — 마우스도 화살표도 마찬가지다.
        area.addEventListener("pointerdown", evt -> dismiss.run());
        area.addEventListener("blur", evt -> dismiss.run());
        area.addEventListener("keydown", evt -> {
            elemental2.dom.KeyboardEvent k = Js.uncheckedCast(evt);
            if ("Escape".equals(k.key)) { dismiss.run(); return; }
            // Tab은 유령이 있으면 그것을 가져오고, 없으면 <b>탭</b>이다 — Tab이 다음 컨트롤로
            // 걸어가는 편집기는 편집기인 척하는 입력칸이다. Shift+Tab은 그대로 둔다: 키보드로
            // 읽는 사람이 이 칸에서 빠져나가는 유일한 문이다.
            if ("Tab".equals(k.key) && !k.shiftKey && !composing(k)) {
                evt.preventDefault();
                if (!ghost[0].isEmpty() && caretOf(area) == ghostAt[0]) {
                    String all = String.valueOf(Js.asPropertyMap(area).get("value"));
                    int at = Math.min(ghostAt[0], all.length());
                    String next = all.substring(0, at) + ghost[0] + all.substring(at);
                    Js.asPropertyMap(area).set("value", next);
                    setCaret(area, at + ghost[0].length());
                    drafts.put(path, next);
                    ghost[0] = "";
                    repaint.run();
                    store.openFileHint(path, next);
                    return;
                }
                dismiss.run();
                insertTab(area);
                drafts.put(path, String.valueOf(Js.asPropertyMap(area).get("value")));
                repaint.run();
                return;
            }
            if (!"Tab".equals(k.key) && !k.metaKey && !k.ctrlKey) {
                if ("ArrowLeft".equals(k.key) || "ArrowRight".equals(k.key) || "ArrowUp".equals(k.key)
                        || "ArrowDown".equals(k.key) || "Home".equals(k.key) || "End".equals(k.key)) {
                    dismiss.run();
                }
            }
            // ⌘S·⌃S는 저장이다 — 편집기에서 그 키가 뜻하는 것이 그것이고, 브라우저의 기본
            // 동작(페이지를 HTML로 저장)은 편집 중인 사람이 원한 적 없는 일이다.
            if ((k.metaKey || k.ctrlKey) && ("s".equals(k.key) || "S".equals(k.key))) {
                evt.preventDefault();
                Js.<HTMLElement>uncheckedCast(save).click();
            }
        });
        // 시작하는 컨트롤과 끝내는 둘이 같은 자리에 선다 — 움직이는 컨트롤은 두 번 찾게 된다.
        acts.append(save, stop);
        HTMLElement wrap = cell("filebody editbody", null);
        HTMLElement stack = cell("editstack", null);
        stack.append(behind, area);
        wrap.append(nums, stack);
        box.append(note, wrap);
        repaint.run();
        return box;
    }

    // ── git ──────────────────────────────────────────────────────────────────

    private HTMLElement gitCard() {
        HTMLElement card = cell("filescard pane-git", null);
        card.append(paneRow(card, "git", tr("git.section"), null));
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
        // 무리를 말로 — 상태 글자(M·A·??)는 git을 아는 사람에게만 말한다(운영 GIT_KIND).
        row.append(cell("gitkind", tr(kindWord(str(c, "kind")))));
        HTMLElement name = cell("treename", str(c, "path"));
        // 18rem 기둥에서 잘리는 이름이라, 통째로는 어딘가에 있어야 한다.
        name.setAttribute("title", str(c, "path"));
        row.append(name);
        final String path = str(c, "path");
        row.addEventListener("click", evt -> store.openFile(path));
        line.append(row);
        if (!May.can("shell")) return line;
        HTMLElement acts = gitActs(c);
        line.append(acts);
        // 오른쪽 버튼도 같은 메뉴를 연다 — 트리 행이 그렇듯이. 바뀐 파일의 행은 사람이 stage·
        // unstage·discard를 가장 먼저 찾는 자리인데, 여기엔 둘째 누름이 아예 없었다.
        line.addEventListener("contextmenu", evt -> {
            elemental2.dom.Element opener = acts.firstElementChild;
            if (opener == null) return;
            evt.preventDefault();
            Js.<HTMLElement>uncheckedCast(opener).click();
        });
        return line;
    }

    /**
     * 한 파일에 하는 일 — 메뉴 하나(⋯)로 모은다. 행마다 버튼을 둘셋 늘어놓으면 18rem 기둥에서
     * 이름이 먼저 잘리고, 무엇이 달라졌는지 보는 문 같은 것은 자리가 없어 아예 빠진다.
     */
    private static String kindWord(String kind) {
        switch (kind) {
            case "staged": return "git.staged";
            case "unstaged": return "git.unstaged";
            case "both": return "git.both";
            case "untracked": return "git.untracked";
            case "conflict": return "git.conflict";
            default: return "git.changed";
        }
    }

    private HTMLElement gitActs(JsPropertyMap<Object> c) {
        String path = str(c, "path"), kind = str(c, "kind");
        HTMLElement box = cell("gitacts", null);
        HTMLElement open = el("md-icon-button");
        open.id = "ga" + (++menuCount);
        open.append(Icons.orGlyph("#i-sl-sliders", "\u22EF", "mk"));
        // 다섯 개의 똑같은 "더 보기"는 스크린 리더에게 다섯 번의 "더 보기"다 — 읽히는 이름이
        // 어느 파일의 것인지 말한다(툴팁은 짧은 낱말 그대로).
        open.setAttribute("aria-label", tr("files.more_named", "name", baseName(path)));
        open.setAttribute("title", tr("files.more"));
        HTMLElement menu = el("md-menu");
        menu.setAttribute("anchor", open.id);
        menu.setAttribute("positioning", canPopover() ? "popover" : "fixed");
        menu.addEventListener("opening", evt -> box.classList.add("showing"));
        menu.addEventListener("closed", evt -> box.classList.remove("showing"));
        // 무엇이 달라졌나 — 이 목록에서 사람이 가장 먼저 묻는 것이고, 답은 카드로 선다.
        item(menu, "diff.show", "#i-sl-file-lines", () -> store.openDiff(path,
                "untracked".equals(kind) ? "untracked" : "staged".equals(kind) ? "staged" : ""));
        if (!"staged".equals(kind)) {
            item(menu, "git.stage", "#i-sl-plus", () -> store.gitDo("stage", path, null));
        }
        if ("staged".equals(kind) || "both".equals(kind)) {
            item(menu, "git.unstage", "#i-sl-reply", () -> store.gitDo("unstage", path, null));
        }
        if (!"untracked".equals(kind)) {
            // 되돌리기는 남이 쓴 것을 지우는 일이라 이름을 대고 묻는다.
            item(menu, "git.discard", "#i-sl-eraser", () ->
                    dialogs.confirm(tr("git.discard_head", "path", path), tr("git.discard_body"),
                            tr("git.discard"), () -> store.gitDo("discard", path, null)));
        }
        open.addEventListener("click", evt -> {
            evt.stopPropagation();
            Js.asPropertyMap(menu).set("open", !Js.isTruthy(Js.asPropertyMap(menu).get("open")));
        });
        box.append(open, menu);
        return box;
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
        // 치워 두거나 도로 꺼내거나 — 둘 중 어느 쪽이 뜻이 있는지는 작업 트리의 사실이라,
        // 둘을 함께 내놓지 않는다(운영 규칙).
        JsArrayLike<Object> changes = Js.uncheckedCast(g.get("changes"));
        boolean dirty = changes != null && changes.getLength() > 0;
        HTMLElement stash = el("md-icon-button");
        stash.append(Icons.orGlyph(dirty ? "#i-sl-floppy-disk" : "#i-sl-arrows-rotate",
                dirty ? "\u2913" : "\u21BB", "mk"));
        stash.setAttribute("aria-label", tr(dirty ? "git.stash" : "git.unstash"));
        stash.setAttribute("title", tr(dirty ? "git.stash" : "git.unstash"));
        stash.addEventListener("click", evt -> {
            if (!dirty) { store.gitDo("unstash", null, null); return; }
            // 치우는 것은 손에 든 것을 통째로 내려놓는 일이라 한 번 묻는다.
            dialogs.confirm(tr("git.stash_head"), tr("git.stash_body"), tr("git.stash"),
                    () -> store.gitDo("stash", null, null));
        });
        box.append(stash);
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

    /**
     * 접는 제목. git은 처음부터 접혀 있다(운영 규칙): 트리와 git이 한 기둥에 쌓이는 폭에서,
     * 바쁜 저장소의 펼친 git 판이 사람이 보러 온 트리를 띠 하나로 밀어냈다. 어느 쪽이든 사람이
     * 고른 것은 기억한다 — localStorage `pane.<key>`.
     */
    private static HTMLElement head(HTMLElement card, String key, String title) {
        boolean shut = shutAtFirst(key);
        if (shut) card.classList.add("shut");
        HTMLElement h = el("button");
        h.setAttribute("type", "button");
        h.className = "panehead state";
        h.setAttribute("aria-expanded", String.valueOf(!shut));
        h.append(Icons.orGlyph("#i-sl-chevron-down", "\u25BE", "panecaret"), cell("panetitle", title));
        h.addEventListener("click", evt -> {
            boolean now = !card.classList.contains("shut");
            card.classList.toggle("shut", now);
            h.setAttribute("aria-expanded", String.valueOf(!now));
            remember("pane." + key, now ? "shut" : "open");
        });
        return h;
    }

    private static boolean shutAtFirst(String key) {
        String stored = recall("pane." + key);
        return "shut".equals(stored) || (stored == null && "git".equals(key));
    }

    private static native String recall(String key) /*-{
        try { return $wnd.localStorage.getItem(key); } catch (e) { return null; }
    }-*/;

    private static native void remember(String key, String value) /*-{
        try { $wnd.localStorage.setItem(key, value); } catch (e) {}
    }-*/;

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
