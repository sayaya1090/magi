package dev.sayaya.magi.client.interfaces;

import dev.sayaya.magi.bridge.CardSharing;
import dev.sayaya.magi.bridge.Icons;
import dev.sayaya.magi.bridge.Render;
import dev.sayaya.magi.bridge.May;
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
    }

    private void render() {
        root.replaceChildren();
        root.append(treeCard(), gitCard());
        publishCards();
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
        String path = store.openPath();
        if (path == null) { CardSharing.provide(new Object[0]); return; }
        CardSharing.provide(new Object[]{CardSharing.card(path, baseName(path),
                (Render) box -> { fileInto(box); return true; },
                () -> store.closeFile())});
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
    private void fileInto(HTMLElement box) {
        String path = store.openPath();
        String text = store.openText();
        box.replaceChildren();
        if (folded) box.setAttribute("folded", ""); else box.removeAttribute("folded");
        HTMLElement bar = cell("filebar", null);
        // 이 카드가 트리 자리에 혼자 서 있으면(폰) 돌아갈 문이 그 머리 줄에 선다 — 어디로
        // 돌아가는지는 부모가 알고, 여기서는 문만 세운다.
        if (CardSharing.alone()) {
            HTMLElement back = el("md-text-button");
            back.className = "fileback";
            back.append(Icons.orGlyph("#i-sl-chevron-left", "\u2039", "sic"));
            back.textContent = tr("nav.files");
            back.setAttribute("aria-label", tr("action.back_to", "name", tr("nav.files")));
            back.addEventListener("click", evt -> CardSharing.toList());
            bar.append(back);
        }
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
        if (path.equals(editing) && reading) { box.append(editor(path, text, acts)); return; }
        HTMLElement wrap = cell("foldwrap", null);
        HTMLElement body = cell("filebody", null);
        if (text == null) body.append(cell("filesnote", tr("files.reading")));
        else if (text.isEmpty()) body.append(cell("filesnote", tr("file.empty")));
        else read(body, path, text);
        wrap.append(body);
        box.append(wrap);
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
        Runnable repaint = () -> {
            String src = String.valueOf(Js.asPropertyMap(area).get("value"));
            behind.replaceChildren();
            String comment = Code.commentMark(path);
            StringBuilder g = new StringBuilder();
            int n = 0;
            for (String line : src.split("\n", -1)) {
                g.append(++n).append('\n');
                for (Code.Part part : Code.parts(line, comment)) {
                    if (part.cls == null) { behind.append(DomGlobal.document.createTextNode(part.text)); continue; }
                    HTMLElement m = el("span");
                    m.className = part.cls;
                    m.textContent = part.text;
                    behind.append(m);
                }
                behind.append(DomGlobal.document.createTextNode("\n"));
            }
            nums.textContent = g.toString();
        };
        area.addEventListener("input", evt -> {
            drafts.put(path, String.valueOf(Js.asPropertyMap(area).get("value")));
            repaint.run();
        });
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
