package dev.sayaya.magi.client;

import dev.sayaya.magi.bridge.CompanionContext;
import dev.sayaya.magi.client.usecase.WorkspaceSource;
import elemental2.core.Global;
import elemental2.dom.DomGlobal;
import jsinterop.annotations.JsFunction;
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
        } else if (q.startsWith("lots")) {
            // 여덟보다 많이 걸리는 물음 하나 — 몇 개까지 싣는지는 답이 그 수를 넘겨야만 재진다.
            StringBuilder b = new StringBuilder("{\"hits\":[");
            for (int i = 0; i < 12; i++) b.append(i == 0 ? "" : ",").append("\"src/f").append(i).append(".go\"");
            cb.accept(Global.JSON.parse(b.append("]}").toString()));
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
    public void complete(CompanionContext ctx, String path, String prefix, String suffix, Consumer<String> text) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_complete", path + "|" + prefix + "|" + suffix);
        text.accept("MORE");
    }

    @JsFunction
    interface Release { void call(); }

    /**
     * 이 물음에 <b>거절</b>로 답하라고 스펙이 걸어 둔 사유 — 걸어 두지 않았으면 null.
     *
     * 빈 문자열도 값이다: 사유 없는 거절, 곧 <b>닿지 못한 것</b>이다. 그래서 있고 없음을
     * 값이 아니라 `has`로 가른다 — 없는 것과 빈 것을 같은 것으로 접으면, 이 가짜는 바로
     * 이 스펙이 재려는 그 접힘을 스스로 저지르게 된다.
     */
    private static String refuses(String key) {
        jsinterop.base.JsPropertyMap<Object> w = Js.asPropertyMap(DomGlobal.window);
        String k = "__magi_test_refuses_" + key;
        if (!w.has(k)) return null;
        Object v = w.get(k);
        return v == null ? "" : String.valueOf(v);
    }

    /** 아직 답하지 않은 물음들 — 붙들라고 했을 때만 쌓인다. */
    private final java.util.List<WorkspaceSource.Said> heldLooks = new java.util.ArrayList<>();

    @Override
    public void look(CompanionContext ctx, String path, String numbered, WorkspaceSource.Said notes) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_look", path + "|" + numbered.split("\n").length);
        String why = refuses("look");
        if (why != null) { notes.call(false, why); return; }
        // 답을 붙들 수 있다. 도는 표를 <b>세어서</b> 켠다는 계약은 두 물음이 동시에 떠 있어야만
        // 재진다 — 깃발이었다면 먼저 온 답 하나가 아직 남은 하나의 표까지 껐을 것이고, 동기로
        // 답하는 가짜에서는 그 차이가 한 번도 드러나지 않는다.
        heldLooks.add(notes);
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_look_release", (Release) this::release);
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_look_asked", heldLooks.size());
        if (!Js.isTruthy(Js.asPropertyMap(DomGlobal.window).get("__magi_test_look_hold"))) release();
    }

    private void release() {
        if (heldLooks.isEmpty()) return;
        heldLooks.remove(0).call(true, "1\tsays nothing\nand a line outside the format");
    }

    @Override
    public void openFileHint(CompanionContext ctx, String path, String text) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_openfile", path + "|" + text.length());
    }

    @Override
    public void diff(CompanionContext ctx, String path, String which, Consumer<Object> cb) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_diff", path + "|" + which);
        cb.accept(Global.JSON.parse("{\"text\":\"@@ -1,2 +1,2 @@\\n-old\\n+new\"}"));
    }

    @Override
    public void pullRequest(CompanionContext ctx, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"repo\":true,\"branch\":\"work\",\"base\":\"origin/main\",\"pushed\":false,"
                + "\"commits\":[{\"sha\":\"abc1234\",\"subject\":\"do the thing\"}],"
                + "\"diff\":\"@@ -1 +1 @@\\n-old\\n+new\"}"));
    }

    @Override
    public void draftPullRequest(CompanionContext ctx, String rules, WorkspaceSource.Said said) {
        String why = refuses("prmsg");
        if (why != null) { said.call(false, why); return; }
        said.call(true, "a drafted request");
    }

    @Override
    public void openPullRequest(CompanionContext ctx, String title, String text, WorkspaceSource.Said urlOrWhy) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_pr", title + "|" + text);
        String why = refuses("pr");
        if (why != null) { urlOrWhy.call(false, why); return; }
        urlOrWhy.call(true, "https://example.test/pr/1");
    }

    @Override
    public void draftCommitMessage(CompanionContext ctx, String rules, WorkspaceSource.Said said) {
        Js.asPropertyMap(DomGlobal.window).set("__magi_test_gitmsg", rules);
        String why = refuses("gitmsg");
        if (why != null) { said.call(false, why); return; }
        said.call(true, "a drafted message");
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
