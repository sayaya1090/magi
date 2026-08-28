package dev.sayaya.magi.demo;

import elemental2.core.Global;
import elemental2.dom.RequestInit;
import elemental2.dom.Response;
import elemental2.promise.Promise;

/**
 * 작업공간 — 트리·git·파일·차이·찾기, 그리고 가지가 낼 PR.
 *
 * 구 콘솔의 데모와 같은 저장소다: 두 데모를 나란히 놓고 볼 때 다른 저장소를 보고 있으면
 * 화면 차이인지 자료 차이인지 아무도 가릴 수 없다. 바꾸는 부름(저장·파일 조작·git)은 받아
 * 주고 잊는다 — 이 페이지에 디스크는 없다.
 */
final class Workspace {
    private Workspace() {}

    static Promise<Response> answer(String path, String url, RequestInit init) {
        // 편집기의 유령 글자 — 타이핑이 <b>멈출 때</b> 뜬다. 아래 삼킴 목록에 들어 있던 동안은
        // 빈 답이었고, 빈 답은 지우라는 뜻이라 데모에서 이 기능이 통째로 없는 것으로 보였다
        // (실측: 운영은 fmt.Errorf 한 줄을 그리는데 신규는 .editcomplete 자체가 없었다).
        // 짝은 컴포저의 이어쓰기(/suggest)이고, 그쪽은 Companion이 같은 이유로 답한다.
        if (Mock.wrote(init) && "/complete".equals(path)) {
            return Mock.json("\n\treturn nil, fmt.Errorf(\"demo: %w\", err)");
        }
        // 모델이 대신 써 주는 초안 둘 — 커밋 메시지와 PR 본문. 받아만 두면 빈 상자가 뜨고,
        // 빈 상자는 "이 버튼은 아무것도 못 한다"로 읽힌다. 채워진 상자를 사람이 고치는 것이
        // 이 컨트롤의 계약이므로 둘 다 초안을 몸으로 돌려준다(운영 /git-msg가 그 모양).
        //
        // PR 쪽은 운영과 <b>일부러</b> 다르다: 운영은 같은 초안을 몸이 아니라 배너에 적고
        // 204를 준다(script.go의 회의 셋과 한 묶음). 그래서 운영에서 이 버튼을 누르면 상자는
        // 빈 채다 — 실측: PROD after="" / NEXT after="web: keep the dock off the phone…".
        // 신규 데모에는 그 배너가 없으니(잔여 항목) 같은 204를 흉내내면 버튼이 죽은 것으로만
        // 보인다. 초안이 초안으로 보이는 쪽을 고른다.
        if (Mock.wrote(init) && "/git-msg".equals(path)) {
            return Mock.json("git: name the branch the card is showing\n\n"
                    + "The pane read the checkout every time it drew, so a detached head was drawn "
                    + "as a branch called nothing at all. It says the sha now, and says that is what it is.");
        }
        if (Mock.wrote(init) && "/pr-msg".equals(path)) {
            return Mock.json("web: keep the dock off the phone\n\n"
                    + "The strip of running work belongs with the conversation. On the other three "
                    + "screens it was ninety pixels of somebody else\u2019s business over the thing the "
                    + "reader came to use, and the file card\u2019s own actions were drawn underneath it.");
        }
        if (Mock.wrote(init)) {
            switch (path) {
                // PR을 여는 것은 초안과 다르다 — 답이 주소이고, 이 페이지에 열 저장소는 없다.
                // 빈 주소가 "열지 않았다"이다(운영도 이 길은 빈 몸으로 답한다).
                case "/git-pr":
                case "/save": case "/file-do": case "/git-do": case "/open-file":
                case "/look": case "/pr": case "/council":
                    return Mock.json("");
                default: break;
            }
        }
        switch (path) {
            case "/files": return Mock.json(dirs(url));
            case "/git": return Mock.json("{\"repo\":true,\"branch\":\"engine-ui-split\",\"head\":\"20ff4276\","
                + "\"upstream\":\"origin/engine-ui-split\",\"ahead\":2,\"behind\":0,"
                + "\"branches\":[\"engine-ui-split\",\"main\",\"fleet-door\"],\"changes\":["
                + "{\"path\":\"cmd/magi-web/page.js\",\"kind\":\"unstaged\"},"
                + "{\"path\":\"internal/app/git.go\",\"kind\":\"staged\"},"
                + "{\"path\":\"docs/UI.md\",\"kind\":\"both\"},"
                + "{\"path\":\"scratchpad/notes.md\",\"kind\":\"untracked\"}]}");
            case "/file": return Mock.json(file(Mock.param(url, "path")));
            case "/find": {
                boolean byName = !"text".equals(Mock.param(url, "in"));
                return Mock.json(byName
                        ? "{\"hits\":[\"internal/app/loop.go\",\"internal/app/loop_test.go\"],\"more\":0}"
                        : "{\"hits\":[\"internal/app/loop.go:412\",\"docs/MANUAL.md:88\"],\"more\":3}");
            }
            case "/diff": {
                String p = Mock.param(url, "path");
                return Mock.json("{\"text\":\"diff --git a/" + p + " b/" + p
                        + "\\n@@ -1,3 +1,4 @@\\n context line\\n-was this\\n+is this now\\n+and this\"}");
            }
            case "/pr": return Mock.json("{\"repo\":true,\"branch\":\"engine-ui-split\",\"base\":\"origin/main\","
                + "\"pushed\":false,\"commits\":["
                + "{\"sha\":\"4ffe258\",\"subject\":\"web: the dock stops covering the phone\",\"when\":\"2026-08-14\"},"
                + "{\"sha\":\"c506e2e\",\"subject\":\"web: the workspace pane says it is reading\",\"when\":\"2026-08-14\"}],"
                + "\"diff\":\"diff --git a/cmd/magi-web/page.css b/cmd/magi-web/page.css\\n"
                + "@@ -2300,6 +2300,9 @@\\n"
                + "   body[at=\\\"agent\\\"][panel=\\\"state\\\"] #stop { display:none; }\\n"
                + "+  body[at=\\\"agent\\\"]:not([panel=\\\"talk\\\"]) #strip { display:none; }\\n"
                + " }\"}");
            default: return null;
        }
    }

    /** 물은 디렉토리들만 — 펼침은 그 디렉토리 하나를 더 읽는 일이라는 규칙 그대로. */
    private static String dirs(String url) {
        StringBuilder json = new StringBuilder("{\"dirs\":{");
        boolean first = true;
        for (String p : Mock.params(url, "path")) {
            if (!first) json.append(',');
            first = false;
            json.append('"').append(p).append("\":").append(under(p));
        }
        return json.append("}}").toString();
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


    private static String file(String path) {

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
        return "{\"path\":\"" + path + "\",\"text\":\"" + text + "\"}";
    }
}
