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
                // 구 콘솔의 데모와 같은 뿌리다(자료가 아니라 화면을 견주기 위해).
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
        // 구 콘솔의 데모와 같은 작업 트리다 — 두 데모를 나란히 놓고 볼 때 다른 저장소를 보고
        // 있으면 화면 차이인지 자료 차이인지 아무도 가릴 수 없다. 종류를 하나씩 담는 것이
        // 요점이고, 그중 both가 가장 볼 값어치가 있다: 지금 커밋하면 화면에 보이는 것의 절반만
        // 실린다.
        cb.accept(Global.JSON.parse("{\"repo\":true,\"branch\":\"engine-ui-split\",\"head\":\"20ff4276\","
                + "\"upstream\":\"origin/engine-ui-split\",\"ahead\":2,\"behind\":0,"
                + "\"branches\":[\"engine-ui-split\",\"main\",\"fleet-door\"],\"changes\":["
                + "{\"path\":\"cmd/magi-web/page.js\",\"kind\":\"unstaged\"},"
                + "{\"path\":\"internal/app/git.go\",\"kind\":\"staged\"},"
                + "{\"path\":\"docs/UI.md\",\"kind\":\"both\"},"
                + "{\"path\":\"scratchpad/notes.md\",\"kind\":\"untracked\"}]}"));
    }

    @Override
    public void file(CompanionContext ctx, String path, Consumer<Object> cb) {
        // 소스 파일 하나로 보인다(구 데모와 같은 파일): 주석·문자열·수가 표시되고, 번호는 read
        // 툴의 것 그대로다 — 사람과 컴패니언이 같은 40행을 가리킬 수 있어야 한다.
        String text = "internal/app/git.go".equals(path)
                ? "    64\\t// GitFacts reads the workspace's git state, or reports that there is none.\\n"
                + "    65\\tfunc (a *App) GitFacts(ctx context.Context, workdir string) (GitState, error) {\\n"
                + "    66\\t\\tif a.plat == nil {\\n"
                + "    67\\t\\t\\treturn GitState{}, fmt.Errorf(\\\"platform unavailable\\\")\\n"
                + "    68\\t\\t}\\n"
                + "    69\\t\\tres, err := a.plat.Exec(ctx, port.Cmd{\\n"
                + "    70\\t\\t\\tPath:      \\\"git\\\",\\n"
                + "    71\\t\\t\\t// --porcelain=v2 is the format git documents for programs.\\n"
                + "    72\\t\\t\\tArgs:      []string{\\\"status\\\", \\\"--porcelain=v2\\\", \\\"--branch\\\"},\\n"
                + "    73\\t\\t\\tDir:       workdir,\\n"
                + "    74\\t\\t\\tMaxOutput: 1048576,\\n"
                + "    75\\t\\t})\\n"
                + "    76\\t\\tif err != nil || res.ExitCode != 0 {\\n"
                + "    77\\t\\t\\t// Not a checkout, no git, or a repository this account may not read.\\n"
                + "    78\\t\\t\\treturn GitState{}, nil\\n"
                + "    79\\t\\t}\\n"
                + "    80\\t\\treturn parseGitStatus(string(res.Stdout)), nil\\n"
                + "    81\\t}\\n"
                : "     1\\t# " + path + "\\n     2\\t\\n"
                + "     3\\tAn agent that runs where the work is: one companion per workspace, a daemon\\n"
                + "     4\\tholding the conversation, and this console reading what they wrote down.\\n";
        cb.accept(Global.JSON.parse("{\"path\":\"" + path + "\",\"text\":\"" + text + "\"}"));
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
    public void look(CompanionContext ctx, String path, String numbered, Consumer<String> notes) {
        notes.accept("2\tthis line does nothing");
    }

    @Override
    public void openFileHint(CompanionContext ctx, String path, String text) { }

    @Override
    public void diff(CompanionContext ctx, String path, String which, Consumer<Object> cb) {
        cb.accept(Global.JSON.parse("{\"text\":\"diff --git a/" + path + " b/" + path
                + "\\n@@ -1,3 +1,4 @@\\n context line\\n-was this\\n+is this now\\n+and this\"}"));
    }

    @Override
    public void pullRequest(CompanionContext ctx, Consumer<Object> cb) {
        // 구 콘솔의 데모와 같은 가지다 — 두 커밋과, base에 대한 차이.
        cb.accept(Global.JSON.parse("{\"repo\":true,\"branch\":\"engine-ui-split\",\"base\":\"origin/main\","
                + "\"pushed\":false,\"commits\":["
                + "{\"sha\":\"4ffe258\",\"subject\":\"web: the dock stops covering the phone\",\"when\":\"2026-08-14\"},"
                + "{\"sha\":\"c506e2e\",\"subject\":\"web: the workspace pane says it is reading\",\"when\":\"2026-08-14\"}],"
                + "\"diff\":\"diff --git a/cmd/magi-web/page.css b/cmd/magi-web/page.css\\n"
                + "@@ -2300,6 +2300,9 @@\\n"
                + "   body[at=\\\"agent\\\"][panel=\\\"state\\\"] #stop { display:none; }\\n"
                + "+  body[at=\\\"agent\\\"]:not([panel=\\\"talk\\\"]) #strip { display:none; }\\n"
                + " }\"}"));
    }

    @Override
    public void draftPullRequest(CompanionContext ctx, String rules, Consumer<String> said) {
        said.accept("web: the workspace reads like the console\n\nWhat the branch carries, in the demo's words.");
    }

    @Override
    public void openPullRequest(CompanionContext ctx, String title, String text, Consumer<String> urlOrWhy) {
        urlOrWhy.accept("https://example.com/pull/1");
    }

    @Override
    public void draftCommitMessage(CompanionContext ctx, String rules, Consumer<String> said) {
        said.accept("workspace: read a file the way the console does\n\nThe body the model would write.");
    }

    @Override
    public void gitDo(CompanionContext ctx, String what, String path, String message, Consumer<String> why) {
        why.accept("");
    }
}
