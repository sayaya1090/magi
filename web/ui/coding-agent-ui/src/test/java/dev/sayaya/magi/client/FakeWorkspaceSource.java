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
    public void file(CompanionContext ctx, String path, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_opened", path);
        cb.accept(Global.JSON.parse("{\"path\":\"" + path + "\",\"text\":\"package main\\n\"}"));
    }
}
