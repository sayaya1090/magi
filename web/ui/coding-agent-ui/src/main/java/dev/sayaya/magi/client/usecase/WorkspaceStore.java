package dev.sayaya.magi.client.usecase;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.client.domain.Tree;
import jsinterop.base.Js;
import jsinterop.base.JsPropertyMap;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Set;
import java.util.function.Consumer;

/**
 * 워크스페이스의 저장소 — 읽은 디렉토리, 열린 가지, git, 그리고 열어 둔 파일.
 *
 * 걸음은 한 번에 하나다(운영 loadTree.busy): 화면에 도착하는 것과 뒤따르는 첫 프레임이
 * 같은 디렉토리를 두 번 걷던 일이 실측된 적 있다. 가지를 펼치는 것은 그 디렉토리 하나를
 * 더 읽는 일이지 트리를 다시 읽는 일이 아니다.
 */
@Singleton
public class WorkspaceStore {
    private final WorkspaceSource source;
    private final List<Runnable> observers = new ArrayList<>();
    private final Set<String> open = new LinkedHashSet<>();
    private JsPropertyMap<Object> dirs = Js.uncheckedCast(JsPropertyMap.of());
    private Object git = null;
    private boolean walked = false;
    private boolean walking = false;
    private CompanionContext ctx = null;
    private String openPath = null;   // 열어 둔 파일의 경로(없으면 null)
    private String openText = null;

    @Inject
    public WorkspaceStore(WorkspaceSource source) { this.source = source; }

    public void aim(CompanionContext c) {
        boolean moved = ctx == null || c == null || !c.socket.equals(ctx.socket);
        ctx = c;
        if (!moved) return;
        // 다른 워크스페이스다 — 읽은 것도 열어 둔 것도 이 컴패니언의 것이 아니다.
        dirs = Js.uncheckedCast(JsPropertyMap.of());
        open.clear();
        git = null;
        walked = false;
        openPath = null;
        openText = null;
        emit();
        walk();
    }

    /** 한 걸음 — 열린 가지들만 읽고, git은 그 곁에서 따로 온다(둘은 다른 물음이다). */
    public void walk() {
        if (ctx == null || walking) return;
        walking = true;
        List<String> want = Tree.wanted(open);
        source.dirs(ctx, want, got -> {
            walking = false;
            walked = true;
            if (got != null) {
                JsPropertyMap<Object> map = Js.uncheckedCast(Js.asPropertyMap(got).get("dirs"));
                if (map != null) map.forEach(k -> dirs.set(k, map.get(k)));
            }
            emit();
        });
        source.git(ctx, g -> { git = g; emit(); });
    }

    /** 가지를 펼치거나 접는다 — 펼치면 그 디렉토리 하나를 더 읽는다. */
    public void toggle(String path) {
        if (open.contains(path)) { open.remove(path); emit(); return; }
        open.add(path);
        emit();
        walk();
    }

    public boolean isOpen(String path) { return open.contains(path); }

    public Object rowsAt(String path) { return dirs.get(path); }

    public boolean walked() { return walked; }

    public Object git() { return git; }

    public String openPath() { return openPath; }

    public String openText() { return openText; }

    /** 파일 하나를 연다 — 같은 자리(슬롯)에서 본문이 바뀐다. */
    public void openFile(String path) {
        if (ctx == null) return;
        openPath = path;
        openText = null;
        emit();
        source.file(ctx, path, got -> {
            if (!path.equals(openPath)) return;   // 늦게 온 답이 다른 파일 위에 앉지 않게
            openText = got == null ? "" : String.valueOf(Js.asPropertyMap(got).get("text"));
            emit();
        });
    }

    public void closeFile() { openPath = null; openText = null; emit(); }

    // ── 찾기 ─────────────────────────────────────────────────────────────────
    // 찾는 동안 판이 보이는 것은 결과다 — 다시 걷는 일(파일을 열거나 무언가를 바꿨을 때)이
    // 그것을 트리로 되돌리면, 두 번째 결과를 누르려던 사람은 그 자리에 없는 것을 누른다
    // (운영에서 "두 번째 파일이 안 열린다"로 보고된 그 결함).
    private String query = "";
    private String where = "name";   // name | text
    private Object hits = null;
    private int findSeq = 0;

    public String query() { return query; }
    public String where() { return where; }
    public Object hits() { return hits; }
    public boolean finding() { return !query.trim().isEmpty(); }

    public void where(String in) {
        where = in;
        if (finding()) find();
        else emit();
    }

    public void query(String q) {
        query = q == null ? "" : q;
        if (!finding()) { hits = null; emit(); return; }
        find();
    }

    private void find() {
        if (ctx == null) return;
        final int mine = ++findSeq;
        source.find(ctx, where, query, got -> {
            if (mine != findSeq) return;   // 뒤에 떠난 물음이 이미 오는 중이다
            hits = got;
            emit();
        });
    }

    // ── 쓰기 ─────────────────────────────────────────────────────────────────
    // 무엇이 됐는지는 버튼이 아니라 다시 읽은 것이 말한다(운영 gitRun의 그 규칙):
    // 이 콘솔이 바꾼 디렉토리는 낡은 게 아니라 틀린 것이므로, 읽은 것을 버리고 다시 걷는다.
    private String lastWhy = "";

    public String lastWhy() { return lastWhy; }

    public void fileDo(String what, String path, String to) {
        if (ctx == null) return;
        source.fileDo(ctx, what, path, to, why -> {
            lastWhy = why == null ? "" : why;
            if (lastWhy.isEmpty()) {
                dirs = Js.uncheckedCast(JsPropertyMap.of());
                if ("delete".equals(what) && path.equals(openPath)) { openPath = null; openText = null; }
                walk();
            }
            emit();
        });
    }

    public void gitDo(String what, String path, String message) {
        if (ctx == null) return;
        source.gitDo(ctx, what, path, message, why -> {
            lastWhy = why == null ? "" : why;
            if (lastWhy.isEmpty()) {
                // 브랜치가 바뀌면 파일도 바뀐다 — 트리도 다시 걷는다.
                dirs = Js.uncheckedCast(JsPropertyMap.of());
                walk();
            }
            emit();
        });
    }

    public void subscribe(Runnable o) { observers.add(o); o.run(); }

    private void emit() { for (Runnable o : observers) o.run(); }
}
