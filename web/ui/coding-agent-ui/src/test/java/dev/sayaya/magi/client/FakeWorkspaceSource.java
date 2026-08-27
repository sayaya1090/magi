package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.client.usecase.WorkspaceSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.base.Js;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.List;
import java.util.function.Consumer;

/** 뿌리 셋(디렉토리 하나 포함)과 그 안 둘, 그리고 변경 둘을 든 저장소. */
@Singleton
public class FakeWorkspaceSource implements WorkspaceSource {
    @Inject
    public FakeWorkspaceSource() {}

    @Override
    public void dirs(CompanionContext ctx, List<String> paths, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_dirs", String.join(",", paths));
        StringBuilder b = new StringBuilder("{\"dirs\":{");
        boolean first = true;
        for (String p : paths) {
            if (!first) b.append(',');
            first = false;
            if (".".equals(p)) {
                b.append("\".\":[{\"name\":\"src\",\"isDir\":true},")
                 .append("{\"name\":\"README.md\",\"isDir\":false},")
                 .append("{\"name\":\"go.mod\",\"isDir\":false}]");
            } else if ("src".equals(p)) {
                b.append("\"src\":[{\"name\":\"main.go\",\"isDir\":false},")
                 .append("{\"name\":\"util.go\",\"isDir\":false}]");
            } else {
                b.append('"').append(p).append("\":[]");
            }
        }
        b.append("}}");
        cb.accept(Global.JSON.parse(b.toString()));
    }

    @Override
    public void git(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse(
                "{\"repo\":true,\"branch\":\"main\",\"ahead\":2,\"behind\":0,\"changes\":[" +
                "{\"path\":\"src/main.go\",\"kind\":\"staged\",\"status\":\"M\"}," +
                "{\"path\":\"README.md\",\"kind\":\"worktree\",\"status\":\"M\"}]}"));
    }

    @Override
    public void find(CompanionContext ctx, String in, String q, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_find", in + ":" + q);
        if ("text".equals(in)) {
            cb.accept(Global.JSON.parse("{\"hits\":[\"src/main.go:12:func main\"],\"more\":3}"));
        } else {
            cb.accept(Global.JSON.parse("{\"hits\":[\"src/main.go\",\"src/util.go\"]}"));
        }
    }

    @Override
    public void save(CompanionContext ctx, String path, String patch, String text, Consumer<String> why) {
        // 무엇으로 보냈는지가 스펙의 관심사다 — 패치면 패치, 아니면 본문.
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_save",
                path + "|" + (patch == null || patch.isEmpty() ? "text:" + text : "patch:" + patch));
        why.accept("");
    }

    @Override
    public void fileDo(CompanionContext ctx, String what, String path, String to, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_filedo",
                what + "|" + path + "|" + (to == null ? "" : to));
        why.accept("");
    }

    @Override
    public void diff(CompanionContext ctx, String path, String which, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_diff", path + "|" + which);
        cb.accept(Global.JSON.parse("{\"text\":\"@@ -1,2 +1,2 @@\\n-old\\n+new\"}"));
    }

    @Override
    public void gitDo(CompanionContext ctx, String what, String path, String message, Consumer<String> why) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_gitdo",
                what + "|" + (path == null ? "" : path) + "|" + (message == null ? "" : message));
        why.accept("");
    }

    @Override
    public void file(CompanionContext ctx, String path, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_opened", path);
        // 에이전트의 read 툴이 내는 그 모양(번호⇥본문) — 화면이 기둥을 가르는지 재려면 그래야 한다.
        cb.accept(Global.JSON.parse("{\"path\":\"" + path
                + "\",\"text\":\"1\\tpackage main\\n2\\t\\n3\\tfunc main() {} // go\\n\"}"));
    }
}
