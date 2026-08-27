package dev.sayaya.magi.client.interfaces.api;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.client.usecase.WorkspaceSource;
import elemental2.core.Global;

import javax.inject.Inject;
import javax.inject.Singleton;
import java.util.List;
import java.util.function.Consumer;

/**
 * 데모의 워크스페이스 — 걷을 수 있는 트리 한 그루와, 무엇이 바뀌었는지 말하는 git 한 벌.
 *
 * 쓰기는 받아들이되 아무것도 바꾸지 않는다: 데모는 <b>무엇을 할 수 있는지</b>를 보이는
 * 자리이고, 눌리지 않는 버튼은 그 답을 못 준다. 다만 바뀐 척도 하지 않는다.
 */
@Singleton
public class DemoWorkspaceSource implements WorkspaceSource {
    @Inject
    public DemoWorkspaceSource() {}

    @Override
    public void dirs(CompanionContext ctx, List<String> paths, Consumer<Object> cb) {
        StringBuilder json = new StringBuilder("{\"dirs\":{");
        boolean first = true;
        for (String p : paths) {
            if (!first) json.append(',');
            first = false;
            json.append('"').append(p).append("\":").append(under(p));
        }
        cb.accept(Global.JSON.parse(json.append("}}").toString()));
    }

    /** 열린 가지만 걷는다 — 펼침은 그 디렉토리 하나를 더 읽는 일이라는 규칙 그대로. */
    private static String under(String path) {
        switch (path) {
            case ".":
                return "[{\"name\":\"cmd\",\"isDir\":true},{\"name\":\"internal\",\"isDir\":true},"
                        + "{\"name\":\"docs\",\"isDir\":true},{\"name\":\"README.md\"},{\"name\":\"go.mod\"}]";
            case "cmd":
                return "[{\"name\":\"magi\",\"isDir\":true},{\"name\":\"magi-web\",\"isDir\":true}]";
            case "internal":
                return "[{\"name\":\"app\",\"isDir\":true},{\"name\":\"core\",\"isDir\":true}]";
            case "docs":
                return "[{\"name\":\"MANUAL.md\"},{\"name\":\"UI.md\"}]";
            default:
                return "[]";
        }
    }

    @Override
    public void git(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"repo\":true,\"branch\":\"main\",\"ahead\":2,\"behind\":0,"
                + "\"changes\":[{\"path\":\"internal/app/loop.go\",\"kind\":\"staged\",\"status\":\"M\"},"
                + "{\"path\":\"docs/MANUAL.md\",\"kind\":\"worktree\",\"status\":\"M\"},"
                + "{\"path\":\"scratch.txt\",\"kind\":\"worktree\",\"status\":\"?\"}]}"));
    }

    @Override
    public void file(CompanionContext ctx, String path, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"path\":\"" + path + "\",\"text\":\"     1\\t" + path
                + "\\n     2\\t\\n     3\\tThis is the demo. The real console reads the file out of the\\n"
                + "     4\\tcompanion's workspace with the agent's own read tool — the same line\\n"
                + "     5\\tnumbers it sees, so a person and their companion point at one line.\\n\"}"));
    }

    @Override
    public void find(CompanionContext ctx, String in, String q, Consumer<Object> cb) {
        boolean byName = !"text".equals(in);
        cb.accept(Global.JSON.parse(byName
                ? "{\"hits\":[\"internal/app/loop.go\",\"internal/app/loop_test.go\"],\"more\":0}"
                : "{\"hits\":[\"internal/app/loop.go:412\",\"docs/MANUAL.md:88\"],\"more\":3}"));
    }

    @Override
    public void save(CompanionContext ctx, String path, String patch, String text, Consumer<String> why) {
        // 데모는 받아 주고 잊는다 — 이 페이지에 디스크는 없다.
        why.accept("");
    }

    @Override
    public void fileDo(CompanionContext ctx, String what, String path, String to, Consumer<String> why) {
        why.accept("");
    }

    @Override
    public void complete(CompanionContext ctx, String path, String prefix, String suffix, Consumer<String> text) {
        // 데모의 이어쓰기는 한 마디짜리다 — 모델이 없는 페이지에서 무엇이 일어나는지만 보인다.
        text.accept(prefix.endsWith("(") ? ")" : prefix.trim().isEmpty() ? "" : " // ...");
    }

    @Override
    public void openFileHint(CompanionContext ctx, String path, String text) { }

    @Override
    public void diff(CompanionContext ctx, String path, String which, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"text\":\"diff --git a/" + path + " b/" + path
                + "\\n@@ -1,3 +1,4 @@\\n context line\\n-was this\\n+is this now\\n+and this\"}"));
    }

    @Override
    public void gitDo(CompanionContext ctx, String what, String path, String message, Consumer<String> why) {
        why.accept("");
    }
}
