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
    /**
     * 열어 둔 파일들 — <b>여럿</b>이다. 하나만 들면 두 파일을 견주는 일(고친 것과 그것을 부르는
     * 곳)이 화면을 오가는 일이 되고, 운영 콘솔은 그 둘을 탭으로 나란히 세운다.
     * 순서는 연 순서다(LinkedHashMap): 탭 줄이 그 순서로 선다. 값이 null이면 아직 읽는 중.
     */
    private final java.util.LinkedHashMap<String, String> opened = new java.util.LinkedHashMap<>();

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
        opened.clear();
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

    /** 열려 있는 파일들, 연 순서대로. */
    public List<String> openPaths() { return new ArrayList<>(opened.keySet()); }

    /** 그 파일의 본문 — 아직 읽는 중이면 null, 열려 있지 않아도 null. */
    public String textOf(String path) { return opened.get(path); }

    public boolean isFileOpen(String path) { return opened.containsKey(path); }

    /**
     * 파일을 연다. 이미 열려 있으면 다시 읽지 않는다 — 같은 탭을 두 번 누르는 것은 그 탭으로
     * 가겠다는 뜻이지 디스크를 다시 읽겠다는 뜻이 아니다(다시 읽는 문은 판의 머리에 있다).
     */
    public void openFile(String path) {
        if (ctx == null) return;
        if (!opened.containsKey(path)) opened.put(path, null);
        emit();
        source.file(ctx, path, got -> {
            if (!opened.containsKey(path)) return;   // 늦게 온 답이 닫힌 파일을 되살리지 않게
            opened.put(path, got == null ? "" : String.valueOf(Js.asPropertyMap(got).get("text")));
            emit();
        });
    }

    /**
     * 저장 — 무엇을 보낼지(패치냐 본문이냐)는 순수 규칙이 정한다(Code.unifiedDiff).
     * 성공하면 다시 걷는다: 저장은 파일을 만들기도 해서, 트리는 이 순간 낡았다.
     */
    public void save(String path, String opened, String now, Consumer<String> why) {
        if (ctx == null) return;
        String patch = dev.sayaya.magi.client.domain.Code.unifiedDiff(opened, now, path);
        source.save(ctx, path, patch, now, w -> {
            if (w == null || w.isEmpty()) { walked = false; walk(); }
            why.accept(w);
        });
    }

    /**
     * 한 파일의 차이를 연다 — 파일과 <b>같은 자리</b>(탭 줄)에 서고, 신원은 "±경로#어느것"이다:
     * 같은 파일의 본문과 차이는 서로 다른 카드이고, 둘을 함께 열어 두는 일이 잦다.
     */
    public void openDiff(String path, String which) {
        if (ctx == null) return;
        String key = diffKey(path, which);
        if (!opened.containsKey(key)) opened.put(key, null);
        emit();
        source.diff(ctx, path, which, got -> {
            if (!opened.containsKey(key)) return;
            opened.put(key, got == null ? "" : String.valueOf(Js.asPropertyMap(got).get("text")));
            emit();
        });
    }

    public static String diffKey(String path, String which) {
        return "\u00B1" + path + "#" + (which == null ? "" : which);
    }

    public static boolean isDiff(String key) { return key.startsWith("\u00B1"); }

    public static String diffPath(String key) {
        int hash = key.lastIndexOf('#');
        return key.substring(1, hash < 0 ? key.length() : hash);
    }

    public static String diffWhich(String key) {
        int hash = key.lastIndexOf('#');
        return hash < 0 ? "" : key.substring(hash + 1);
    }

    /** 하나를 닫는다 — 나머지는 그대로 열려 있다. */
    public void closeFile(String path) { if (opened.remove(path) != null || path == null) emit(); }

    /** 전부 닫는다 — 다른 컴패니언으로 옮겨 갈 때(그 파일들은 이 워크스페이스의 것이었다). */
    public void closeAllFiles() { if (!opened.isEmpty()) { opened.clear(); emit(); } }

    // ── 찾기 ─────────────────────────────────────────────────────────────────
    // 찾는 동안 판이 보이는 것은 결과다 — 다시 걷는 일(파일을 열거나 무언가를 바꿨을 때)이
    // 그것을 트리로 되돌리면, 두 번째 결과를 누르려던 사람은 그 자리에 없는 것을 누른다
    // (운영에서 "두 번째 파일이 안 열린다"로 보고된 그 결함).
    private String query = "";
    private String where = "name";   // name | text
    private Object hits = null;
    private int findSeq = 0;

    /** 어느 작업공간인가 — 셸이 컨텍스트에 실어 보낸 것(여기서 명단을 다시 묻지 않는다). */
    public String workdir() { return ctx == null || ctx.workdir == null ? "" : ctx.workdir; }

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
                // 지운 파일은 열어 둘 수 없다 — 나머지 탭은 그대로 남는다.
                if ("delete".equals(what)) opened.remove(path);
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
