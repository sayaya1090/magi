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

    public void subscribe(Runnable o) { observers.add(o); o.run(); }

    private void emit() { for (Runnable o : observers) o.run(); }
}
